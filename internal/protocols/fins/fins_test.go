package fins

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hexOf(b []byte) string {
	return strings.ToUpper(hex.EncodeToString(b))
}

// リクエストのフレームに対する応答を、バイト列で確かめる
func TestHandler_Frames(t *testing.T) {
	store := NewDataStore()
	h := newHandler(store)
	h.now = func() time.Time { return time.Date(2026, 11, 13, 9, 8, 7, 0, time.UTC) } // 金曜日
	_ = store.WriteWords(AreaDM, 100, []uint16{0x1234, 0xABCD})
	_ = store.WriteWord(AreaCIO, 100, 0x0021) // CIO 100.00 と 100.05 が ON

	// ヘッダー: 宛先 0.1.0、送信元 0.5.0、SID 0x7A
	const head = "80 00 02 00 01 00 00 05 00 7A"
	const replyHead = "C0 00 02 00 05 00 00 01 00 7A"
	cases := []struct {
		name, request, want string
	}{
		{"DM のワードを読む", "01 01 82 00 64 00 00 02", "01 01 00 00 12 34 AB CD"},
		{"CIO のビットを読む（100.04 から 3 個）", "01 01 30 00 64 04 00 03", "01 01 00 00 00 01 00"},
		{"古い機種の CIO の種別（0x80）", "01 01 80 00 64 00 00 01", "01 01 00 00 00 21"},
		{"DM のワードを書く", "01 02 82 00 C8 00 00 02 00 01 00 02", "01 02 00 00"},
		{"WR のビットを書く", "01 02 31 00 0A 0F 00 02 01 01", "01 02 00 00"},
		{"同じ値で埋める", "01 03 82 01 00 00 00 03 55 AA", "01 03 00 00"},
		{"複合読み出し（D100、CIO 100.05、D101）", "01 04 82 00 64 00 30 00 64 05 82 00 65 00", "01 04 00 00 82 12 34 30 01 82 AB CD"},
		{"時計", "07 01", "07 01 00 00 26 11 13 09 08 07 05"},
		{"知らないコマンド", "23 01", "23 01 04 01"},
		{"知らないエリア種別", "01 01 77 00 00 00 00 01", "01 01 11 01"},
		{"先頭アドレスが範囲外", "01 01 B1 02 00 00 00 01", "01 01 11 03"},
		{"終わりが範囲を超える", "01 01 B1 01 FF 00 00 02", "01 01 11 04"},
		{"個数とデータが合わない", "01 02 82 00 00 00 00 02 00 01", "01 02 10 03"},
		{"コマンドが短い", "01 01 82 00 00", "01 01 10 02"},
		{"コマンドが長い", "01 01 82 00 00 00 00 01 00", "01 01 10 01"},
		{"読む個数が多すぎる", "01 01 82 00 00 00 03 E8", "01 01 11 0B"},
	}
	for _, c := range cases {
		got := h.handle(unhex(t, head+c.request))
		want := unhex(t, replyHead+c.want)
		if !bytes.Equal(got, want) {
			t.Errorf("%s:\n got  %s\n want %s", c.name, hexOf(got), hexOf(want))
		}
	}

	// 書いた内容
	if w, _ := store.ReadWords(AreaDM, 200, 2); w[0] != 1 || w[1] != 2 {
		t.Errorf("D200 = %v", w)
	}
	if w, _ := store.ReadWords(AreaWR, 10, 2); w[0] != 0x8000 || w[1] != 0x0001 {
		t.Errorf("W10.15 から 2 ビット = %04X", w)
	}
	if w, _ := store.ReadWords(AreaDM, 256, 3); w[0] != 0x55AA || w[2] != 0x55AA {
		t.Errorf("D256 = %04X", w)
	}

	// 応答しないもの: 短すぎる・応答のフレーム・応答不要のリクエスト（書き込みは行う）
	if got := h.handle(unhex(t, "80 00 02")); got != nil {
		t.Errorf("短いフレームに応答した: %s", hexOf(got))
	}
	if got := h.handle(unhex(t, "C0 00 02 00 01 00 00 05 00 7A 01 01 00 00")); got != nil {
		t.Errorf("応答のフレームに応答した: %s", hexOf(got))
	}
	if got := h.handle(unhex(t, "81 00 02 00 01 00 00 05 00 7A 01 02 82 00 00 00 00 01 00 09")); got != nil {
		t.Errorf("応答不要のリクエストに応答した: %s", hexOf(got))
	}
	if w, _ := store.ReadWord(AreaDM, 0); w != 9 {
		t.Errorf("応答不要の書き込み: D0 = %d", w)
	}
}

// ビットのエリアとワードのエリアは、同じメモリを指す
func TestDataStore_BitAndWordViews(t *testing.T) {
	s := NewDataStore()
	if err := s.WriteBit(AreaCIO+bitSuffix, 100*16+5, true); err != nil {
		t.Fatal(err)
	}
	if w, _ := s.ReadWord(AreaCIO, 100); w != 0x0020 {
		t.Errorf("CIO 100 = %04X, want 0020", w)
	}
	_ = s.WriteWord(AreaCIO, 101, 0x8001)
	if bits, _ := s.ReadBits(AreaCIO+bitSuffix, 101*16, 16); !bits[0] || !bits[15] || bits[1] {
		t.Errorf("CIO 101 のビット = %v", bits)
	}
	if _, err := s.ReadWords(AreaWR, 511, 2); err == nil {
		t.Error("範囲外を読めた")
	}
	if err := s.WriteBit("XX", 0, true); err == nil {
		t.Error("ないエリアに書けた")
	}
}

func startServer(t *testing.T, transport string) (*Server, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	srv := NewServer(&Config{Host: "127.0.0.1", Port: port, Transport: transport, Node: 10}, NewDataStore())
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv, port
}

func newClient(t *testing.T, port int, transport string) *Client {
	t.Helper()
	c, err := (&ClientFactory{}).NewClient(map[string]interface{}{"host": "127.0.0.1", "port": port, "transport": transport, "timeoutMs": 300})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c.(*Client)
}

// クライアントからサーバーへ、UDP と TCP の両方でつないで読み書きする
func TestClientAndServer(t *testing.T) {
	for _, transport := range []string{transportUDP, transportTCP} {
		t.Run(transport, func(t *testing.T) {
			srv, port := startServer(t, transportBoth)
			c := newClient(t, port, transport)
			f := &ClientFactory{}
			send := func(cmd string, params map[string]interface{}) protocol.CommandResult {
				t.Helper()
				req, err := f.BuildRequest(cmd, params)
				if err != nil {
					t.Fatal(err)
				}
				return c.Send(context.Background(), req)
			}

			if r := send(commandWrite, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": 100, "values": "1234, 0xBEEF, -1"}); r.Status != protocol.CommandOK {
				t.Fatalf("書き込み: %+v", r)
			}
			r := send(commandRead, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": 100, "count": 3})
			if r.Status != protocol.CommandOK || len(r.Values) != 3 || r.Values[1].Label != "D101" || *r.Values[1].Word != 0xBEEF || *r.Values[2].Word != 0xFFFF {
				t.Fatalf("読み出し: %+v", r)
			}
			if r.Summary != "0101 メモリエリアの読み出し D100 から 3 個" {
				t.Errorf("要約 = %q", r.Summary)
			}

			// ビット: CIO 100.14 から 3 個（ワードをまたぐ）
			if r := send(commandWrite, map[string]interface{}{"area": AreaCIO, "unit": unitBit, "address": 100, "bit": 14, "values": "1 0 1"}); r.Status != protocol.CommandOK {
				t.Fatalf("ビットの書き込み: %+v", r)
			}
			r = send(commandRead, map[string]interface{}{"area": AreaCIO, "unit": unitBit, "address": 100, "bit": 14, "count": 3})
			if r.Status != protocol.CommandOK || len(r.Values) != 3 || r.Values[2].Label != "CIO 101.00" || !*r.Values[0].Bit || *r.Values[1].Bit || !*r.Values[2].Bit {
				t.Fatalf("ビットの読み出し: %+v", r)
			}
			if w, _ := srv.store.ReadWords(AreaCIO, 100, 2); w[0] != 0x4000 || w[1] != 0x0001 {
				t.Errorf("サーバーの CIO 100 = %04X", w)
			}

			// 送ったバイト列: FINS/TCP はヘッダーが付く。ノードアドレスは自動
			wantSent := "800002000100000100" // ICF RSV GCT DNA DA1 DA2 SNA SA1 SA2（UDP: 127.0.0.1 の最後の数字）
			if transport == transportTCP {
				// サーバーのノードは 10、クライアントは割り当て（127.0.0.1 → 1）
				wantSent = "46494E530000001A0000000200000000" + "800002000A00000100"
			}
			if got := hexOf(r.Sent); !strings.HasPrefix(got, wantSent) {
				t.Errorf("送ったバイト列 = %s, want 先頭が %s", got, wantSent)
			}

			// 範囲外は異常終了（接続は閉じない）
			r = c.Send(context.Background(), unhex(t, "01 01 B1 02 00 00 00 01"))
			if r.Status != protocol.CommandException || !strings.Contains(r.Error, "0x1103") || !c.Connected() {
				t.Fatalf("範囲外: %+v", r)
			}
			// 16 進で入れた、解釈を持たないコマンド
			if r := c.Send(context.Background(), unhex(t, "23 01")); r.Status != protocol.CommandException || !strings.Contains(r.Error, "0x0401") {
				t.Fatalf("知らないコマンド: %+v", r)
			}
			if r := send(commandCPU, nil); r.Status != protocol.CommandOK || r.Values[0].Value != cpuModel {
				t.Fatalf("CPU ユニット情報: %+v", r)
			}
			if r := send(commandStatus, nil); r.Status != protocol.CommandOK || r.Values[0].Value != "運転中" {
				t.Fatalf("ステータス: %+v", r)
			}

			// サーバーを止めると失敗し、起動し直すと、つなぎ直して読める（メモリは残る）
			if err := srv.Stop(); err != nil {
				t.Fatal(err)
			}
			if r := send(commandRead, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": 100, "count": 1}); r.Status == protocol.CommandOK || c.Connected() {
				t.Fatalf("停止後: %+v", r)
			}
			if err := srv.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			if r := send(commandRead, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": 100, "count": 1}); r.Status != protocol.CommandOK || *r.Values[0].Word != 1234 {
				t.Fatalf("起動し直したあと: %+v", r)
			}
		})
	}
}

// FINS/TCP のノードアドレスのやりとりを、バイト列で確かめる
func TestServer_TCPHandshake(t *testing.T) {
	_, port := startServer(t, transportTCP)
	dial := func() net.Conn {
		conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		return conn
	}
	read := func(conn net.Conn, n int) string {
		buf := make([]byte, n)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Fatal(err)
		}
		return hexOf(buf)
	}

	// クライアントがノードアドレス 0x22 を名乗る → そのまま返し、サーバーのノード（10）を知らせる
	conn := dial()
	_, _ = conn.Write(unhex(t, "46494E53 0000000C 00000000 00000000 00000022"))
	if got := read(conn, 24); got != "46494E53000000100000000100000000000000220000000A" {
		t.Errorf("やりとりの応答 = %s", got)
	}
	// FINS フレーム（D0 を 1 ワード読む）
	_, _ = conn.Write(unhex(t, "46494E53 0000001A 00000002 00000000 80 00 02 00 0A 00 00 22 00 01 01 01 82 00 00 00 00 01"))
	if got := read(conn, 32); got != "46494E53000000180000000200000000"+"C00002002200000A0001"+"010100000000" {
		t.Errorf("FINS フレームの応答 = %s", got)
	}

	// やりとりの前に FINS フレームを送ると、エラーを知らせて閉じる
	conn = dial()
	_, _ = conn.Write(unhex(t, "46494E53 0000000A 00000002 00000000 0101"))
	if got := read(conn, 16); got != "46494E53000000080000000300000003" {
		t.Errorf("エラーの通知 = %s", got)
	}
	// "FINS" で始まらないもの
	conn = dial()
	_, _ = conn.Write(unhex(t, "00000000 0000000C 00000000 00000000 00000000"))
	if got := read(conn, 16); got != "46494E53000000080000000300000001" {
		t.Errorf("エラーの通知 = %s", got)
	}
}

// 応答しない相手はタイムアウトになり、接続を閉じる
func TestClient_Timeout(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	c := newClient(t, pc.LocalAddr().(*net.UDPAddr).Port, transportUDP)
	c.timeout = 100 * time.Millisecond
	r := c.Send(context.Background(), unhex(t, "01 01 82 00 00 00 00 01"))
	if r.Status != protocol.CommandTimeout || c.Connected() {
		t.Fatalf("応答しない相手: %+v", r)
	}
}

// 変数マッピング用のリクエスト: ビットのエリアのアドレスは「ワード × 16 + ビット」
func TestClientMapping_Requests(t *testing.T) {
	f := &ClientFactory{}
	if req, err := f.BuildReadRequest(AreaDM, 100, 2); err != nil || hexOf(req) != "01018200640000"+"02" {
		t.Errorf("DM の読み出し = %s, %v", hexOf(req), err)
	}
	if req, err := f.BuildReadRequest(AreaCIO+bitSuffix, 100*16+5, 1); err != nil || hexOf(req) != "0101300064050001" {
		t.Errorf("CIO のビットの読み出し = %s, %v", hexOf(req), err)
	}
	if req, err := f.BuildWriteRequest(AreaWR+bitSuffix, 17, nil, []bool{true, false}); err != nil || hexOf(req) != "01023100010100020100" {
		t.Errorf("WR のビットの書き込み = %s, %v", hexOf(req), err)
	}
	if req, err := f.BuildReadRequest(AreaDM+bitSuffix, 100*16+5, 1); err != nil || hexOf(req) != "0101020064050001" {
		t.Errorf("DM のビットの読み出し = %s, %v", hexOf(req), err)
	}
	if req, err := f.BuildReadRequest(AreaC+flagSuffix, 5, 2); err != nil || hexOf(req) != "0101098005000002" {
		t.Errorf("カウンタのアップフラグの読み出し = %s, %v", hexOf(req), err)
	}
	// タイマ・カウンタには「ワードの中のビット」はない
	if _, err := f.BuildReadRequest(AreaT+bitSuffix, 0, 1); err == nil {
		t.Error("タイマのビットのエリアを読めた")
	}
	if f.MaxReadCount(AreaDM) != maxReadCount || f.MaxWriteCount("XX") != 0 {
		t.Error("読み書きできる個数")
	}
}
