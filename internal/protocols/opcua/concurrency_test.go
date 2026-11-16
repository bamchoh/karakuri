package opcua

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/bamchoh/uastack/ua"

	"karakuri/internal/domain/variable"
	"karakuri/internal/infrastructure/adapter"
)

// 複数のセッションから同時に読み書きしても、書いた値が自分の変数に届き、読み返せる。
// 同じ構造体の別のフィールドに、2 つのセッションから同時に書いても、両方が残る。
// その間、ホストの側（スクリプトなど）も別の変数を書き換え続ける（変数の変更の通知とノードの更新が同時に走る）。
// 実際の変数ストアを使う。競合は CI の -race で見つける
func TestServer_ConcurrentSessions(t *testing.T) {
	const sessions, rounds = 6, 20
	vars := variable.NewVariableStore()
	motor, err := variable.NewStructTypeDef("Motor", []variable.StructField{
		{Name: "speed", DataType: "UINT"},
		{Name: "count", DataType: "UINT"},
	}, vars.TypeResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := vars.RegisterStructType(motor); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	create := func(name, dataType string) {
		v, err := vars.CreateVariable(name, variable.DataType(dataType), nil)
		if err != nil {
			t.Fatalf("CreateVariable(%s): %v", name, err)
		}
		vars.SetNodePublishing(v.ID, string(ProtocolOPCUA), &variable.NodePublishing{Enabled: true, AccessMode: "readwrite"})
		ids[name] = v.ID
	}
	for i := range sessions {
		create(fmt.Sprintf("C%d", i), "INT")
	}
	create("M", "Motor")
	create("Host", "DINT")

	srv := NewServer(testConfig(freePort(t)), adapter.NewVariableStoreAccessor(vars))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	ns := srv.nodes.ns
	ctx := context.Background()

	// ホストの側が、Host を書き換え続ける
	stop := make(chan struct{})
	var host sync.WaitGroup
	host.Add(1)
	go func() {
		defer host.Done()
		for n := int32(0); ; n++ {
			select {
			case <-stop:
				return
			default:
				_ = vars.UpdateValue(ids["Host"], n)
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, sessions)
	for i := range sessions {
		c := dialTestClient(t, srv)
		wg.Add(1)
		go func() {
			defer wg.Done()
			own := ua.NewStringNodeId(ns, fmt.Sprintf("C%d", i))
			field := ua.NewStringNodeId(ns, []string{"M.speed", "M.count"}[i%2])
			for k := range rounds {
				v := int16(i*100 + k)
				if err := c.WriteValue(ctx, own, ua.NewVariant(v)); err != nil {
					errs <- fmt.Errorf("セッション %d の %d 回目の書き込み: %v", i, k, err)
					return
				}
				dv, err := c.ReadValue(ctx, own)
				if err != nil {
					errs <- fmt.Errorf("セッション %d の %d 回目の読み出し: %v", i, k, err)
					return
				}
				if got, ok := dv.Value.Value().(int16); !ok || got != v {
					errs <- fmt.Errorf("セッション %d の %d 回目: C%d = %#v, want %d", i, k, i, dv.Value.Value(), v)
					return
				}
				if i < 2 { // 2 つのセッションが、M の別のフィールドに書く
					if err := c.WriteValue(ctx, field, ua.NewVariant(uint16(1000+k))); err != nil {
						errs <- fmt.Errorf("セッション %d の %d 回目のフィールドの書き込み: %v", i, k, err)
						return
					}
				}
				if _, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "Host")); err != nil {
					errs <- fmt.Errorf("セッション %d の %d 回目の Host の読み出し: %v", i, k, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	host.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// ホストの変数ストアに、最後に書いた値が残っている（同じ構造体の別のフィールドへの書き込みが、互いを消さない）
	for i := range sessions {
		v, _ := vars.GetVariable(ids[fmt.Sprintf("C%d", i)])
		if want := int16(i*100 + rounds - 1); v.Value != want {
			t.Errorf("C%d = %#v, want %d", i, v.Value, want)
		}
	}
	m, _ := vars.GetVariable(ids["M"])
	fields := m.Value.(map[string]interface{})
	if want := uint16(1000 + rounds - 1); fields["speed"] != want || fields["count"] != want {
		t.Errorf("M = %v, want speed・count とも %d", fields, want)
	}
}
