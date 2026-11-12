package modbus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/simonvetter/modbus"
)

// startTCPServer は 127.0.0.1 の空きポートで Modbus TCP サーバーを起動する
func startTCPServer(t *testing.T) (*ModbusServer, *recordingEmitter, int) {
	t.Helper()
	cfg := DefaultTCPConfig()
	cfg.TCPAddress = "127.0.0.1"
	cfg.TCPPort = freeTCPPort(t)
	srv := NewModbusServer(cfg, NewModbusDataStore(100, 100, 100, 100))
	emitter := &recordingEmitter{}
	srv.SetEventEmitter(emitter)
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv, emitter, cfg.TCPPort
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
	srv, emitter, port := startTCPServer(t)
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
	// 受信は記録するが、送信と接続数には数えない
	rx, tx, conns := emitter.snapshot()
	if rx != 2 || tx != 1 {
		t.Errorf("rx=%d tx=%d, want 2 / 1", rx, tx)
	}
	if len(conns) != 1 || conns[0] != 1 {
		t.Errorf("connections = %v, want [1]", conns)
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
	srv, _, port := startTCPServer(t)
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

// 中継を入れても、クライアントからの読み書きはこれまでどおり動く
func TestTCP_ClientReadWrite(t *testing.T) {
	srv, _, port := startTCPServer(t)
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
	// 大きなリクエスト・応答（125 レジスタは範囲外なので、範囲内の最大で確認する）
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
	_, _, port := startTCPServer(t)
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
	srv, _, port := startTCPServer(t)
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
