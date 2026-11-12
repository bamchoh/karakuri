package application

import (
	"testing"

	"karakuri/internal/domain/protocol/protocoltest"
)

// stoppableFactory はプラグインのプロセスを持つファクトリーのフェイク（起動・停止の回数を数える）
type stoppableFactory struct {
	*protocoltest.Factory
	running bool
	stops   int
}

func (f *stoppableFactory) EnsureStarted() error {
	f.running = true
	return nil
}

func (f *stoppableFactory) StopProcess() {
	if f.running {
		f.running = false
		f.stops++
	}
}

// プロジェクトを読み込むと、それまでのサーバーのプラグインのプロセスを止める。
// 止めないと、読み込む前のレジスタの値や UnitID の設定がプラグインに残り、
// 読み込んだプロジェクトにないプロトコルのプロセスも動き続ける
func TestImportProject_StopsPluginProcesses(t *testing.T) {
	svc := NewPLCService()
	a := &stoppableFactory{Factory: protocoltest.NewFactory("proto-a", "v", "A")}
	b := &stoppableFactory{Factory: protocoltest.NewFactory("proto-b", "v", "B")}
	svc.RegisterPluginFactory(a)
	svc.RegisterPluginFactory(b)
	for _, p := range []string{"proto-a", "proto-b"} {
		if err := svc.AddServer(p, "v"); err != nil {
			t.Fatalf("AddServer(%s): %v", p, err)
		}
	}
	if err := svc.StartServer("proto-a"); err != nil {
		t.Fatal(err)
	}

	// proto-a だけを含むプロジェクトを読み込む
	project := &ProjectDataDTO{Servers: []ServerSnapshotDTO{{ProtocolType: "proto-a", Variant: "v"}}}
	if err := svc.ImportProject(project); err != nil {
		t.Fatalf("ImportProject: %v", err)
	}

	// どちらのプロセスも一度止まる。proto-a は読み込みで起動し直され、proto-b は止まったまま
	if a.stops != 1 || !a.running {
		t.Errorf("proto-a: stops=%d running=%v, want 1 / true", a.stops, a.running)
	}
	if b.stops != 1 || b.running {
		t.Errorf("proto-b: stops=%d running=%v, want 1 / false", b.stops, b.running)
	}
	instances := svc.GetServerInstances()
	if len(instances) != 1 || instances[0].ProtocolType != "proto-a" || instances[0].Status != "Stopped" {
		t.Errorf("読み込み後のサーバー = %+v", instances)
	}
}
