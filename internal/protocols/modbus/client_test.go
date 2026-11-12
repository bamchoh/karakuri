package modbus

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
)

func TestBuildRequest(t *testing.T) {
	f := &TCPClientFactory{}
	cases := []struct {
		name    string
		cmd     string
		params  map[string]interface{}
		want    []byte
		wantErr string
	}{
		{"FC03", "3", map[string]interface{}{"address": float64(0x10), "count": float64(10)}, []byte{0x03, 0x00, 0x10, 0x00, 0x0A}, ""},
		{"FC01 最大個数", "1", map[string]interface{}{"address": float64(0), "count": float64(2000)}, []byte{0x01, 0x00, 0x00, 0x07, 0xD0}, ""},
		{"FC03 個数オーバー", "3", map[string]interface{}{"address": float64(0), "count": float64(126)}, nil, "個数"},
		{"FC03 アドレス+個数が範囲外", "3", map[string]interface{}{"address": float64(65535), "count": float64(2)}, nil, "65536"},
		{"FC05 ON", "5", map[string]interface{}{"address": float64(3), "value": "on"}, []byte{0x05, 0x00, 0x03, 0xFF, 0x00}, ""},
		{"FC05 OFF", "5", map[string]interface{}{"address": float64(3), "value": "off"}, []byte{0x05, 0x00, 0x03, 0x00, 0x00}, ""},
		{"FC06 16 進", "6", map[string]interface{}{"address": float64(1), "value": "0x1234"}, []byte{0x06, 0x00, 0x01, 0x12, 0x34}, ""},
		{"FC06 負の数", "6", map[string]interface{}{"address": float64(1), "value": "-1"}, []byte{0x06, 0x00, 0x01, 0xFF, 0xFF}, ""},
		{"FC06 範囲外", "6", map[string]interface{}{"address": float64(1), "value": "65536"}, nil, "1 ワード"},
		// 仕様書の例: アドレス 19 から 10 点、CD 01
		{"FC15", "15", map[string]interface{}{"address": float64(19), "values": "1,0,1,1,0,0,1,1 1 0"}, []byte{0x0F, 0x00, 0x13, 0x00, 0x0A, 0x02, 0xCD, 0x01}, ""},
		{"FC16", "16", map[string]interface{}{"address": float64(1), "values": "0x000A, 258"}, []byte{0x10, 0x00, 0x01, 0x00, 0x02, 0x04, 0x00, 0x0A, 0x01, 0x02}, ""},
		{"FC16 読めない値", "16", map[string]interface{}{"address": float64(1), "values": "1, x"}, nil, "数値"},
		{"不明なコマンド", "99", map[string]interface{}{"address": float64(0)}, nil, "不明"},
		{"アドレスなし", "3", map[string]interface{}{"count": float64(1)}, nil, "アドレス"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := f.BuildRequest(c.cmd, c.params)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("got % X, want % X", got, c.want)
			}
		})
	}
}

// すべてのコマンドの定義が BuildRequest で組み立てられる（既定値のままで）。ポーリングに使えるのは読み取り（FC01〜04）だけ
func TestCommands_DefaultsBuild(t *testing.T) {
	f := &TCPClientFactory{}
	for _, cmd := range f.Commands() {
		wantPollable := cmd.ID == "1" || cmd.ID == "2" || cmd.ID == "3" || cmd.ID == "4"
		if cmd.Pollable != wantPollable {
			t.Errorf("%s: Pollable = %v", cmd.DisplayName, cmd.Pollable)
		}
		params := map[string]interface{}{}
		for _, field := range cmd.Fields {
			params[field.Name] = field.Default
		}
		if _, err := f.BuildRequest(cmd.ID, params); err != nil {
			t.Errorf("%s: %v", cmd.DisplayName, err)
		}
	}
}

func TestDecodeResponse(t *testing.T) {
	t.Run("FC03", func(t *testing.T) {
		status, values, _ := decodeResponse([]byte{0x03, 0x00, 0x05, 0x00, 0x02}, []byte{0x03, 0x04, 0x00, 0x0A, 0xFF, 0xFF})
		if status != protocol.CommandOK || len(values) != 2 {
			t.Fatalf("status=%s values=%v", status, values)
		}
		if values[0].Label != "アドレス 5" || values[0].Value != "10 (0x000A)" || values[1].Value != "65535 (0xFFFF)" {
			t.Fatalf("values=%v", values)
		}
		if values[0].Word == nil || *values[0].Word != 10 || *values[1].Word != 0xFFFF || values[0].Bit != nil {
			t.Fatalf("元の値（Word）が入っていない: %+v", values)
		}
	})
	t.Run("FC01", func(t *testing.T) {
		status, values, _ := decodeResponse([]byte{0x01, 0x00, 0x00, 0x00, 0x03}, []byte{0x01, 0x01, 0x05})
		if status != protocol.CommandOK || len(values) != 3 || values[0].Value != "ON" || values[1].Value != "OFF" || values[2].Value != "ON" {
			t.Fatalf("status=%s values=%v", status, values)
		}
		if values[0].Bit == nil || !*values[0].Bit || *values[1].Bit || values[0].Word != nil {
			t.Fatalf("元の値（Bit）が入っていない: %+v", values)
		}
	})
	t.Run("例外応答", func(t *testing.T) {
		status, _, msg := decodeResponse([]byte{0x03, 0x00, 0x00, 0x00, 0x01}, []byte{0x83, 0x02})
		if status != protocol.CommandException || !strings.Contains(msg, "不正なデータアドレス") {
			t.Fatalf("status=%s msg=%s", status, msg)
		}
	})
	t.Run("バイト数が合わない", func(t *testing.T) {
		status, _, _ := decodeResponse([]byte{0x03, 0x00, 0x00, 0x00, 0x02}, []byte{0x03, 0x02, 0x00, 0x01})
		if status != protocol.CommandError {
			t.Fatalf("status=%s", status)
		}
	})
	t.Run("知らないファンクションコードはデータをそのまま", func(t *testing.T) {
		status, values, _ := decodeResponse([]byte{0x2B, 0x0E}, []byte{0x2B, 0x0E, 0x01})
		if status != protocol.CommandOK || len(values) != 1 || values[0].Value != "0E 01" {
			t.Fatalf("status=%s values=%v", status, values)
		}
	})
}

// fakeServer は Modbus TCP のサーバーのふり。受け取ったフレームごとに handle を呼び、返したバイト列を送り返す
type fakeServer struct {
	ln       net.Listener
	accepted chan struct{}
}

func newFakeServer(t *testing.T, handle func(frame []byte) [][]byte) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{ln: ln, accepted: make(chan struct{}, 10)}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.accepted <- struct{}{}
			go func(conn net.Conn) {
				defer conn.Close()
				for {
					header := make([]byte, 7)
					if _, err := io.ReadFull(conn, header); err != nil {
						return
					}
					body := make([]byte, int(binary.BigEndian.Uint16(header[4:6]))-1)
					if _, err := io.ReadFull(conn, body); err != nil {
						return
					}
					for _, out := range handle(append(header, body...)) {
						if _, err := conn.Write(out); err != nil {
							return
						}
					}
				}
			}(conn)
		}
	}()
	return s
}

func (s *fakeServer) client(t *testing.T, timeoutMs int) protocol.Client {
	t.Helper()
	port := s.ln.Addr().(*net.TCPAddr).Port
	c, err := (&TCPClientFactory{}).NewClient(map[string]interface{}{
		"host": "127.0.0.1", "port": port, "unitId": 7, "timeoutMs": timeoutMs,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// reply は受け取ったフレームと同じトランザクション ID・UnitID で、pdu を返すフレームを作る
func reply(frame []byte, txID uint16, pdu []byte) []byte {
	out := binary.BigEndian.AppendUint16(nil, txID)
	out = binary.BigEndian.AppendUint16(out, 0)
	out = binary.BigEndian.AppendUint16(out, uint16(len(pdu)+1))
	out = append(out, frame[6])
	return append(out, pdu...)
}

func TestTCPClient_SendAndReceive(t *testing.T) {
	frames := make(chan []byte, 1)
	srv := newFakeServer(t, func(frame []byte) [][]byte {
		frames <- frame
		return [][]byte{reply(frame, binary.BigEndian.Uint16(frame), []byte{0x03, 0x02, 0x12, 0x34})}
	})
	c := srv.client(t, 1000)
	res := c.Send(context.Background(), []byte{0x03, 0x00, 0x00, 0x00, 0x01})
	if res.Status != protocol.CommandOK {
		t.Fatalf("status=%s err=%s", res.Status, res.Error)
	}
	// MBAP: トランザクション ID 1、プロトコル ID 0、長さ 6、UnitID 7
	wantSent := []byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x07, 0x03, 0x00, 0x00, 0x00, 0x01}
	gotFrame := <-frames
	if !bytes.Equal(res.Sent, wantSent) || !bytes.Equal(gotFrame, wantSent) {
		t.Fatalf("sent=% X server got=% X", res.Sent, gotFrame)
	}
	if len(res.Values) != 1 || res.Values[0].Value != "4660 (0x1234)" {
		t.Fatalf("values=%v", res.Values)
	}
	if !c.Connected() {
		t.Fatal("接続中のはず")
	}
}

func TestTCPClient_Exception(t *testing.T) {
	srv := newFakeServer(t, func(frame []byte) [][]byte {
		return [][]byte{reply(frame, binary.BigEndian.Uint16(frame), []byte{0x86, 0x02})}
	})
	c := srv.client(t, 1000)
	res := c.Send(context.Background(), []byte{0x06, 0x00, 0x00, 0x00, 0x01})
	if res.Status != protocol.CommandException {
		t.Fatalf("status=%s", res.Status)
	}
	if !c.Connected() {
		t.Fatal("例外応答では接続を閉じない")
	}
}

// 応答しない機器ではタイムアウトになり、接続を閉じる。次の送信でつなぎ直す
func TestTCPClient_TimeoutThenReconnect(t *testing.T) {
	var calls atomic.Int32
	srv := newFakeServer(t, func(frame []byte) [][]byte {
		if calls.Add(1) == 1 {
			return nil // 1 回目は応答しない
		}
		return [][]byte{reply(frame, binary.BigEndian.Uint16(frame), []byte{0x03, 0x02, 0x00, 0x01})}
	})
	c := srv.client(t, 100)
	start := time.Now()
	res := c.Send(context.Background(), []byte{0x03, 0x00, 0x00, 0x00, 0x01})
	if res.Status != protocol.CommandTimeout {
		t.Fatalf("status=%s err=%s", res.Status, res.Error)
	}
	if el := time.Since(start); el < 90*time.Millisecond || el > 2*time.Second {
		t.Fatalf("elapsed=%v", el)
	}
	if c.Connected() {
		t.Fatal("タイムアウトしたら接続を閉じる")
	}
	res = c.Send(context.Background(), []byte{0x03, 0x00, 0x00, 0x00, 0x01})
	if res.Status != protocol.CommandOK {
		t.Fatalf("2 回目: status=%s err=%s", res.Status, res.Error)
	}
	if len(srv.accepted) != 2 {
		t.Fatalf("接続の回数 = %d, want 2", len(srv.accepted))
	}
}

// 前のリクエストへの遅れた応答（トランザクション ID が違う）は読み捨てる
func TestTCPClient_SkipsStaleResponse(t *testing.T) {
	srv := newFakeServer(t, func(frame []byte) [][]byte {
		tx := binary.BigEndian.Uint16(frame)
		return [][]byte{
			reply(frame, tx-1, []byte{0x03, 0x02, 0xDE, 0xAD}),
			reply(frame, tx, []byte{0x03, 0x02, 0x00, 0x2A}),
		}
	})
	c := srv.client(t, 1000)
	res := c.Send(context.Background(), []byte{0x03, 0x00, 0x00, 0x00, 0x01})
	if res.Status != protocol.CommandOK || res.Values[0].Value != "42 (0x002A)" {
		t.Fatalf("status=%s values=%v", res.Status, res.Values)
	}
}

func TestTCPClient_ConnectRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	c, err := (&TCPClientFactory{}).NewClient(map[string]interface{}{"host": "127.0.0.1", "port": port, "timeoutMs": 500})
	if err != nil {
		t.Fatal(err)
	}
	res := c.Send(context.Background(), []byte{0x03, 0x00, 0x00, 0x00, 0x01})
	if res.Status != protocol.CommandError && res.Status != protocol.CommandTimeout {
		t.Fatalf("status=%s", res.Status)
	}
	if !strings.Contains(res.Error, strconv.Itoa(port)) {
		t.Fatalf("エラーに接続先が入っていない: %s", res.Error)
	}
}

func TestTCPClient_RejectsTooLongPDU(t *testing.T) {
	c, _ := (&TCPClientFactory{}).NewClient(nil)
	res := c.Send(context.Background(), make([]byte, maxPDULength+1))
	if res.Status != protocol.CommandError {
		t.Fatalf("status=%s", res.Status)
	}
}

func TestNormalizeSettings(t *testing.T) {
	f := &TCPClientFactory{}
	got, err := f.NormalizeSettings(map[string]interface{}{"host": " 192.168.0.10 ", "port": float64(1502)})
	if err != nil {
		t.Fatal(err)
	}
	if got["host"] != "192.168.0.10" || got["port"] != 1502 || got["unitId"] != 1 || got["timeoutMs"] != 1000 {
		t.Fatalf("got=%v", got)
	}
	if _, err := f.NormalizeSettings(map[string]interface{}{"unitId": float64(256)}); err == nil {
		t.Fatal("UnitID 256 はエラー")
	}
	if _, err := f.NormalizeSettings(map[string]interface{}{"host": ""}); err == nil {
		t.Fatal("空のホストはエラー")
	}
}
