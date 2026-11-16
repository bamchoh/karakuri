package modbus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/simonvetter/modbus"
)

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// startTCPServer は 127.0.0.1 の空きポートで Modbus TCP サーバーを起動する
func startTCPServer(t *testing.T) (*ModbusServer, int) {
	t.Helper()
	cfg := DefaultTCPConfig()
	cfg.TCPAddress = "127.0.0.1"
	cfg.TCPPort = freeTCPPort(t)
	srv := NewModbusServer(cfg, NewModbusDataStore(100, 100, 100, 100))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv, cfg.TCPPort
}

func dialTCP(t *testing.T, port int) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// readHoldingRequest は保持レジスタを 1 つ読むリクエスト（FC 03、アドレス 0）のフレームを作る
func readHoldingRequest(txID byte, unitID byte) []byte {
	return []byte{0, txID, 0, 0, 0, 6, unitID, 3, 0, 0, 0, 1}
}

// readResponse は応答のフレームを 1 つ読む。timeout までに来なければ nil を返す
func readResponse(t *testing.T, conn net.Conn, timeout time.Duration) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	header := make([]byte, mbapHeaderSize)
	if _, err := io.ReadFull(conn, header); err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil
		}
		t.Fatalf("応答を読めない: %v", err)
	}
	body := make([]byte, int(header[4])<<8|int(header[5])-1)
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatalf("応答の本体を読めない: %v", err)
	}
	return append(header, body...)
}

// issue #51: Modbus TCP は、オフにした UnitID に例外応答を返していた（画面の説明は「応答しません」）
func TestTCP_DisabledUnitIDGetsNoResponse(t *testing.T) {
	srv, port := startTCPServer(t)
	srv.SetUnitIdEnabled(2, false)
	conn := dialTCP(t, port)

	// オンの UnitID には通常の応答（FC 03、2 バイト）
	if _, err := conn.Write(readHoldingRequest(1, 1)); err != nil {
		t.Fatal(err)
	}
	if got, want := readResponse(t, conn, 2*time.Second), []byte{0, 1, 0, 0, 0, 5, 1, 3, 2, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("UnitID 1 の応答 = % X, want % X", got, want)
	}

	// オフの UnitID には何も返さない（例外応答も返さない）
	if _, err := conn.Write(readHoldingRequest(2, 2)); err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, conn, 500*time.Millisecond); got != nil {
		t.Fatalf("オフにした UnitID 2 に応答した: % X", got)
	}
	// 接続は切れておらず、同じ接続でオンの UnitID には続けて応答する
	if _, err := conn.Write(readHoldingRequest(3, 1)); err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, conn, 2*time.Second); got == nil || got[1] != 3 || got[7] != 3 {
		t.Fatalf("オフの UnitID のあとの UnitID 1 の応答 = % X", got)
	}

	// 起動中にオンへ戻すと応答する
	srv.SetUnitIdEnabled(2, true)
	if _, err := conn.Write(readHoldingRequest(4, 2)); err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, conn, 2*time.Second); got == nil || got[6] != 2 || got[7] != 3 {
		t.Fatalf("オンに戻した UnitID 2 の応答 = % X", got)
	}
}

// 1 回の送信に複数のリクエストが入っていても、フレームごとに判定する
func TestTCP_FiltersEachFrameInOneWrite(t *testing.T) {
	srv, port := startTCPServer(t)
	srv.SetDisabledUnitIDs([]uint8{2, 4})
	conn := dialTCP(t, port)

	var batch []byte
	for i := byte(1); i <= 5; i++ {
		batch = append(batch, readHoldingRequest(i, i)...)
	}
	// 途中で切れて届いても読める
	if _, err := conn.Write(batch[:17]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := conn.Write(batch[17:]); err != nil {
		t.Fatal(err)
	}

	var answered []byte
	for {
		res := readResponse(t, conn, 500*time.Millisecond)
		if res == nil {
			break
		}
		if res[7] != 3 {
			t.Errorf("UnitID %d の応答が通常の応答でない: % X", res[6], res)
		}
		answered = append(answered, res[6])
	}
	if !bytes.Equal(answered, []byte{1, 3, 5}) {
		t.Errorf("応答した UnitID = %v, want [1 3 5]", answered)
	}
}

// 他社製のクライアント（simonvetter/modbus）から、全部の機能コードで読み書きできる
func TestTCP_ClientReadWrite(t *testing.T) {
	srv, port := startTCPServer(t)
	client, err := modbus.NewClient(&modbus.ClientConfiguration{
		URL:     fmt.Sprintf("tcp://127.0.0.1:%d", port),
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer client.Close()
	client.SetUnitId(1)

	// 保持レジスタ（FC 06 / 16 / 03）
	if err := client.WriteRegister(10, 1234); err != nil {
		t.Fatalf("WriteRegister: %v", err)
	}
	if err := client.WriteRegisters(11, []uint16{1, 2, 3}); err != nil {
		t.Fatalf("WriteRegisters: %v", err)
	}
	if got, err := client.ReadRegisters(10, 4, modbus.HOLDING_REGISTER); err != nil || fmt.Sprint(got) != "[1234 1 2 3]" {
		t.Errorf("ReadRegisters = %v, %v", got, err)
	}
	if got, _ := srv.store.ReadWords(AreaHoldingRegs, 10, 1); fmt.Sprint(got) != "[1234]" {
		t.Errorf("DataStore の保持レジスタ = %v", got)
	}
	// 大きなリクエスト・応答（DataStore が 100 個なので、範囲内の最大で確認する）
	if got, err := client.ReadRegisters(0, 100, modbus.HOLDING_REGISTER); err != nil || len(got) != 100 {
		t.Errorf("100 レジスタの読み取り = %d 件, %v", len(got), err)
	}
	// 範囲外は例外応答（応答自体は返る）
	if _, err := client.ReadRegisters(200, 1, modbus.HOLDING_REGISTER); !errors.Is(err, modbus.ErrIllegalDataAddress) {
		t.Errorf("範囲外の保持レジスタの読み取りのエラー = %v, want ErrIllegalDataAddress", err)
	}
	if _, err := client.ReadRegisters(200, 1, modbus.INPUT_REGISTER); !errors.Is(err, modbus.ErrIllegalDataAddress) {
		t.Errorf("範囲外の入力レジスタの読み取りのエラー = %v, want ErrIllegalDataAddress", err)
	}
	if _, err := client.ReadCoils(200, 1); !errors.Is(err, modbus.ErrIllegalDataAddress) {
		t.Errorf("範囲外のコイルの読み取りのエラー = %v, want ErrIllegalDataAddress", err)
	}
	if _, err := client.ReadDiscreteInputs(200, 1); !errors.Is(err, modbus.ErrIllegalDataAddress) {
		t.Errorf("範囲外の入力の読み取りのエラー = %v, want ErrIllegalDataAddress", err)
	}
	if err := client.WriteRegister(200, 1); !errors.Is(err, modbus.ErrIllegalDataAddress) {
		t.Errorf("範囲外の書き込みのエラー = %v, want ErrIllegalDataAddress", err)
	}

	// コイル（FC 05 / 15 / 01）
	if err := client.WriteCoil(3, true); err != nil {
		t.Errorf("WriteCoil: %v", err)
	}
	if err := client.WriteCoils(4, []bool{true, false, true}); err != nil {
		t.Errorf("WriteCoils: %v", err)
	}
	if got, err := client.ReadCoils(3, 4); err != nil || fmt.Sprint(got) != "[true true false true]" {
		t.Errorf("ReadCoils = %v, %v", got, err)
	}

	// オフにした UnitID は、クライアントから見るとタイムアウトになる
	srv.SetUnitIdEnabled(9, false)
	client.SetUnitId(9)
	if _, err := client.ReadRegisters(0, 1, modbus.HOLDING_REGISTER); !errors.Is(err, modbus.ErrRequestTimedOut) {
		t.Errorf("オフにした UnitID の読み取りのエラー = %v, want ErrRequestTimedOut", err)
	}
}

func TestTCP_StartFailsWhenPortInUse(t *testing.T) {
	_, port := startTCPServer(t)
	cfg := DefaultTCPConfig()
	cfg.TCPAddress = "127.0.0.1"
	cfg.TCPPort = port
	other := NewModbusServer(cfg, NewModbusDataStore(100, 100, 100, 100))
	if err := other.Start(context.Background()); err == nil {
		_ = other.Stop()
		t.Fatal("使用中のポートで Start がエラーにならなかった")
	}
}

func TestTCP_StopClosesConnectionsAndRestartWorks(t *testing.T) {
	srv, port := startTCPServer(t)
	conn := dialTCP(t, port)
	if _, err := conn.Write(readHoldingRequest(1, 1)); err != nil {
		t.Fatal(err)
	}
	if readResponse(t, conn, 2*time.Second) == nil {
		t.Fatal("応答がない")
	}

	// 止めると、開いていた接続が閉じ、ポートが空く
	done := make(chan error, 1)
	go func() { done <- srv.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("接続が開いたままだと Stop が終わらない")
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Error("Stop のあとも接続が開いたまま")
		}
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond); err == nil {
		_ = c.Close()
		t.Error("Stop のあとも接続できる")
	}

	// 同じポートで起動し直せる
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("2 回目の Start: %v", err)
	}
	conn2 := dialTCP(t, port)
	if _, err := conn2.Write(readHoldingRequest(1, 1)); err != nil {
		t.Fatal(err)
	}
	if readResponse(t, conn2, 2*time.Second) == nil {
		t.Fatal("起動し直したあと応答がない")
	}
}

// 仕様に合わないリクエストには、RTU / ASCII と同じ例外応答を返し、接続は閉じない。
// 以前は simonvetter/modbus のサーバーを使っていて、個数の誤りには例外を返さずに接続を閉じていた
func TestTCP_InvalidRequestsGetExceptions(t *testing.T) {
	srv, port := startTCPServer(t)
	srv.SetUnitIdEnabled(2, false)
	conn := dialTCP(t, port)

	cases := []struct {
		name string
		pdu  string // 機能コード + データ
		want string // 応答の PDU
	}{
		{"FC03 の個数 0", "03 00 00 00 00", "83 03"},
		{"FC03 の個数 126", "03 00 00 00 7E", "83 03"},
		{"FC01 の個数 2001", "01 00 00 07 D1", "81 03"},
		{"FC16 のバイト数が個数と合わない", "10 00 00 00 02 02 00 01", "90 03"},
		{"FC15 の個数 1969", "0F 00 00 07 B1 F7" + strings.Repeat(" 00", 247), "8F 03"},
		{"開始 + 個数が 65536 を超える", "03 FF FF 00 02", "83 02"},
		{"DataStore の範囲外", "03 00 64 00 01", "83 02"},
		{"データが短すぎる", "06 00 01", "86 03"},
		{"FC05 の値が FF00 / 0000 でない", "05 00 01 12 34", "85 03"},
		{"対応していない機能コード", "2B 0E 01 00", "AB 01"},
		{"正しいリクエスト", "03 00 00 00 01", "03 02 00 00"},
	}
	for i, c := range cases {
		pdu := seedBytes(c.pdu)
		req := append([]byte{0, byte(i), 0, 0, 0, byte(len(pdu) + 1), 1}, pdu...)
		if _, err := conn.Write(req); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		res := readResponse(t, conn, 2*time.Second)
		if res == nil {
			t.Fatalf("%s: 応答がない（接続を閉じた？）", c.name)
		}
		if res[1] != byte(i) || res[6] != 1 || !bytes.Equal(res[7:], seedBytes(c.want)) {
			t.Errorf("%s: 応答 = % X, want PDU %s", c.name, res, c.want)
		}
	}

	// オフにした UnitID 宛ては、誤ったリクエストにも応答しない
	for _, pdu := range []string{"03 00 00 00 00", "06 00 01"} {
		b := seedBytes(pdu)
		if _, err := conn.Write(append([]byte{0, 99, 0, 0, 0, byte(len(b) + 1), 2}, b...)); err != nil {
			t.Fatal(err)
		}
	}
	if got := readResponse(t, conn, 300*time.Millisecond); got != nil {
		t.Fatalf("オフにした UnitID 2 に応答した: % X", got)
	}

	// プロトコル ID が 0 でないフレームは読み捨て、次のフレームには応答する
	if _, err := conn.Write(append(seedBytes("00 50 00 01 00 06 01 03 00 00 00 01"), readHoldingRequest(51, 1)...)); err != nil {
		t.Fatal(err)
	}
	if got := readResponse(t, conn, 2*time.Second); got == nil || got[1] != 51 {
		t.Fatalf("プロトコル ID が 0 でないフレームのあとの応答 = % X, want トランザクション ID 51 の応答", got)
	}
}

// 長さの欄が正しくない（機能コードがない・254 を超える）フレームは、区切りを信用できないので接続を閉じる
func TestTCP_ClosesOnBadLength(t *testing.T) {
	_, port := startTCPServer(t)
	for _, frame := range []string{"00 01 00 00 00 01 01", "00 01 00 00 00 FF 01 03", "00 01 00 00 00 00 01"} {
		conn := dialTCP(t, port)
		if _, err := conn.Write(seedBytes(frame)); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		// 読み残したバイトがあると、閉じたときに RST が届く（EOF ではなくエラーになる）。どちらも閉じている
		n, err := conn.Read(make([]byte, 16))
		var netErr net.Error
		if err == nil || (errors.As(err, &netErr) && netErr.Timeout()) {
			t.Errorf("%s: 接続が閉じない（%d バイト, %v）", frame, n, err)
		}
	}
}
