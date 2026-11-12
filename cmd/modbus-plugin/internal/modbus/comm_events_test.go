package modbus

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/simonvetter/modbus"
)

// recordingEmitter は発行された通信イベントを数える
type recordingEmitter struct {
	mu          sync.Mutex
	rx, tx      int
	connections []int
}

func (e *recordingEmitter) EmitRx() { e.mu.Lock(); e.rx++; e.mu.Unlock() }
func (e *recordingEmitter) EmitTx() { e.mu.Lock(); e.tx++; e.mu.Unlock() }
func (e *recordingEmitter) EmitConnection(count int) {
	e.mu.Lock()
	e.connections = append(e.connections, count)
	e.mu.Unlock()
}
func (e *recordingEmitter) snapshot() (int, int, []int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rx, e.tx, append([]int(nil), e.connections...)
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// issue #17: プラグイン化のあと、Modbus サーバーに通信イベントの送信先が渡らず、
// 通信インジケーターにイベントが届かなくなっていた
func TestModbusServer_EmitsCommunicationEvents(t *testing.T) {
	cfg := DefaultTCPConfig()
	cfg.TCPAddress = "127.0.0.1"
	cfg.TCPPort = freeTCPPort(t)
	srv := NewModbusServer(cfg, NewModbusDataStore(100, 100, 100, 100))
	emitter := &recordingEmitter{}
	srv.SetEventEmitter(emitter)

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = srv.Stop()
		}
	}()

	client, err := modbus.NewClient(&modbus.ClientConfiguration{
		URL:     fmt.Sprintf("tcp://127.0.0.1:%d", cfg.TCPPort),
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer client.Close()

	// UnitID 1 と 2 から 1 回ずつ読む
	for _, unit := range []uint8{1, 2} {
		client.SetUnitId(unit)
		if _, err := client.ReadRegisters(0, 2, modbus.HOLDING_REGISTER); err != nil {
			t.Fatalf("read (unit %d): %v", unit, err)
		}
	}

	rx, tx, conns := emitter.snapshot()
	if rx != 2 || tx != 2 {
		t.Errorf("rx=%d tx=%d, want 2 / 2", rx, tx)
	}
	// 接続数は UnitID ごとに数える: 1 → 2
	if len(conns) != 2 || conns[0] != 1 || conns[1] != 2 {
		t.Errorf("connections = %v, want [1 2]", conns)
	}

	// 止めると接続数は 0 に戻る
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	stopped = true
	if _, _, conns := emitter.snapshot(); len(conns) != 3 || conns[2] != 0 {
		t.Errorf("connections after stop = %v, want [1 2 0]", conns)
	}
}

// 送信先を渡さなくても動く（イベントを出さないだけ）
func TestModbusServer_WorksWithoutEmitter(t *testing.T) {
	cfg := DefaultTCPConfig()
	cfg.TCPAddress = "127.0.0.1"
	cfg.TCPPort = freeTCPPort(t)
	srv := NewModbusServer(cfg, NewModbusDataStore(100, 100, 100, 100))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
