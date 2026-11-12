package application

import (
	"testing"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/protocoltest"
)

// processFactory はプラグインのプロセスを持つファクトリーのフェイク。
// 設定項目の問い合わせでプロセスを起動する（LazyRemoteServerFactory と同じ動き）
type processFactory struct {
	*protocoltest.Factory
	running bool
	starts  int
	stops   int
}

func (f *processFactory) EnsureStarted() error {
	if !f.running {
		f.running = true
		f.starts++
	}
	return nil
}

func (f *processFactory) StopProcess() {
	if f.running {
		f.running = false
		f.stops++
	}
}

func (f *processFactory) IsProcessRunning() bool { return f.running }

func (f *processFactory) GetConfigFields(variantID string) []protocol.ConfigField {
	_ = f.EnsureStarted()
	return f.Factory.GetConfigFields(variantID)
}

// issue #51: サーバーを追加していないプロトコルの設定項目を問い合わせると、プラグインのプロセスが残っていた
func TestGetProtocolSchema_DoesNotLeavePluginRunning(t *testing.T) {
	svc := NewPLCService()
	f := &processFactory{Factory: protocoltest.NewFactory("proc", "v", "Proc")}
	svc.RegisterPluginFactory(f)

	// サーバーがないとき: 問い合わせには答え、起動したプロセスは止める
	schema, err := svc.GetProtocolSchema("proc")
	if err != nil {
		t.Fatalf("GetProtocolSchema: %v", err)
	}
	if len(schema.Variants) != 1 || len(schema.Variants[0].Fields) != 1 {
		t.Errorf("スキーマが違う: %+v", schema)
	}
	if f.running || f.starts != 1 || f.stops != 1 {
		t.Errorf("サーバーなし: running=%v starts=%d stops=%d, want false / 1 / 1", f.running, f.starts, f.stops)
	}

	// サーバーがあるとき: プロセスは動かしたままにする
	if err := svc.AddServer("proc", "v"); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if _, err := svc.GetProtocolSchema("proc"); err != nil {
		t.Fatalf("GetProtocolSchema: %v", err)
	}
	if !f.running || f.stops != 1 {
		t.Errorf("サーバーあり: running=%v stops=%d, want true / 1", f.running, f.stops)
	}

	// サーバーを削除したあとの問い合わせでも残らない
	if err := svc.RemoveServer("proc"); err != nil {
		t.Fatalf("RemoveServer: %v", err)
	}
	if _, err := svc.GetProtocolSchema("proc"); err != nil {
		t.Fatalf("GetProtocolSchema: %v", err)
	}
	if f.running {
		t.Error("サーバーを削除したあとの問い合わせでプロセスが残った")
	}
}
