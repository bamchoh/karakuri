package opcua

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bamchoh/uastack/client"
	"github.com/bamchoh/uastack/ua"

	"karakuri/internal/domain/protocol"
)

// fakeAccessor はテスト用のメモリ上の VariableStoreAccessor（ホストの VariableStore の代わり）
type fakeAccessor struct {
	mu      sync.Mutex
	pubs    []protocol.NodePublishingInfo
	values  map[string]interface{}
	structs map[string][]protocol.StructFieldInfo
	subs    []func(string)
}

func newFakeAccessor() *fakeAccessor {
	return &fakeAccessor{
		values: map[string]interface{}{},
		structs: map[string][]protocol.StructFieldInfo{
			"Motor": {{Name: "speed", DataType: "INT"}, {Name: "on", DataType: "BOOL"}},
		},
	}
}

func (a *fakeAccessor) publish(id, name, dataType, mode string, value interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pubs = append(a.pubs, protocol.NodePublishingInfo{VariableID: id, VariableName: name, DataType: dataType, AccessMode: mode})
	a.values[id] = value
}

func (a *fakeAccessor) GetEnabledNodePublishings(string) []protocol.NodePublishingInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]protocol.NodePublishingInfo(nil), a.pubs...)
}

func (a *fakeAccessor) ReadVariableValue(id string) (interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.values[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	return v, nil
}

func (a *fakeAccessor) WriteVariableValue(id string, v interface{}) error {
	a.mu.Lock()
	a.values[id] = v
	subs := append([]func(string){}, a.subs...)
	a.mu.Unlock()
	for _, f := range subs {
		f(id)
	}
	return nil
}

func (a *fakeAccessor) WriteVariableFieldValue(id, fieldPath string, v interface{}) error {
	a.mu.Lock()
	root := a.values[id]
	switch {
	case strings.HasPrefix(fieldPath, "["): // "[i]"（下限 0 前提のテスト用簡易実装）
		var i int
		fmt.Sscanf(fieldPath, "[%d]", &i)
		arr := append([]interface{}(nil), root.([]interface{})...)
		arr[i] = v
		a.values[id] = arr
	default:
		m := map[string]interface{}{}
		for k, e := range root.(map[string]interface{}) {
			m[k] = e
		}
		m[fieldPath] = v
		a.values[id] = m
	}
	a.mu.Unlock()
	return a.WriteVariableValue(id, a.mustRead(id))
}

func (a *fakeAccessor) mustRead(id string) interface{} {
	v, _ := a.ReadVariableValue(id)
	return v
}

func (a *fakeAccessor) GetStructFields(typeName string) []protocol.StructFieldInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.structs[typeName]
}

func (a *fakeAccessor) SubscribeVariableChanges(ctx context.Context, onChange func(string)) error {
	a.mu.Lock()
	a.subs = append(a.subs, onChange)
	a.mu.Unlock()
	<-ctx.Done()
	return nil
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startTestServer(t *testing.T, acc *fakeAccessor) (*Server, *client.Client, uint16) {
	t.Helper()
	port := freePort(t)
	cfg := &Config{Host: "127.0.0.1", Port: port}
	srv := NewServer(cfg, acc)
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, fmt.Sprintf("opc.tcp://127.0.0.1:%d", port), client.Config{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })

	ns := srv.nodes.ns
	return srv, c, ns
}

func TestServer_BrowseReadWrite(t *testing.T) {
	acc := newFakeAccessor()
	acc.publish("v-int", "Counter", "INT", "readwrite", int64(42))
	acc.publish("v-real", "Temp", "REAL", "read", float64(1.5))
	acc.publish("v-arr", "Values", "ARRAY[0..2] OF DINT", "readwrite", []interface{}{int64(1), int64(2), int64(3)})
	acc.publish("v-str", "Motor1", "Motor", "readwrite", map[string]interface{}{"speed": int64(100), "on": true})

	_, c, ns := startTestServer(t, acc)
	ctx := context.Background()

	// PLCVariables フォルダの下に公開変数が並ぶ
	refs, err := c.BrowseChildren(ctx, ua.NewStringNodeId(ns, folderNodeName))
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	names := map[string]bool{}
	for _, r := range refs {
		names[r.BrowseName.Name] = true
	}
	for _, want := range []string{"Counter", "Temp", "Values", "Motor1"} {
		if !names[want] {
			t.Errorf("browse result missing %q: %v", want, names)
		}
	}

	// スカラーの読み取り（PLC 型に合った組み込み型で返る）
	dv, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "v-int"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, ok := dv.Value.Value().(int16); !ok || got != 42 {
		t.Errorf("Counter = %#v, want int16(42)", dv.Value.Value())
	}

	// 構造体フィールドの子ノード
	dv, err = c.ReadValue(ctx, ua.NewStringNodeId(ns, "v-str.speed"))
	if err != nil {
		t.Fatalf("Read field: %v", err)
	}
	if got, ok := dv.Value.Value().(int16); !ok || got != 100 {
		t.Errorf("Motor1.speed = %#v, want int16(100)", dv.Value.Value())
	}

	// 配列（型付き配列）と配列要素の子ノード
	dv, err = c.ReadValue(ctx, ua.NewStringNodeId(ns, "v-arr"))
	if err != nil {
		t.Fatalf("Read array: %v", err)
	}
	if got, ok := dv.Value.Value().([]int32); !ok || len(got) != 3 || got[2] != 3 {
		t.Errorf("Values = %#v, want []int32{1,2,3}", dv.Value.Value())
	}

	// ルートへの書き込みがホストに届く
	if err := c.WriteValue(ctx, ua.NewStringNodeId(ns, "v-int"), ua.NewVariant(int16(7))); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := toInt64(acc.mustRead("v-int")); got != 7 {
		t.Errorf("host value = %v, want 7", got)
	}

	// 子ノードへの書き込みはフィールドパス付きで届き、親ノードの値も更新される
	if err := c.WriteValue(ctx, ua.NewStringNodeId(ns, "v-arr[1]"), ua.NewVariant(int32(20))); err != nil {
		t.Fatalf("Write element: %v", err)
	}
	waitFor(t, func() bool {
		dv, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "v-arr"))
		arr, _ := dv.Value.Value().([]int32)
		return err == nil && len(arr) == 3 && arr[1] == 20
	})

	// 読み取り専用ノードへの書き込みは拒否される
	if err := c.WriteValue(ctx, ua.NewStringNodeId(ns, "v-real"), ua.NewVariant(float32(2))); err == nil {
		t.Errorf("write to read-only node succeeded")
	}
	// 型が違う書き込みは拒否される
	if err := c.WriteValue(ctx, ua.NewStringNodeId(ns, "v-int"), ua.NewVariant("x")); err == nil {
		t.Errorf("write with wrong type succeeded")
	}
}

func TestServer_HostChangeNotifiesSubscription(t *testing.T) {
	acc := newFakeAccessor()
	acc.publish("v-int", "Counter", "INT", "readwrite", int64(1))
	_, c, ns := startTestServer(t, acc)
	ctx := context.Background()

	sub, err := c.CreateSubscription(ctx, client.SubscriptionParams{PublishingInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if _, err := sub.MonitorValues(ctx, ua.NewStringNodeId(ns, "v-int")); err != nil {
		t.Fatalf("MonitorValues: %v", err)
	}

	// スクリプト等でホスト側の値が変わった想定
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = acc.WriteVariableValue("v-int", int64(99))
	}()

	timeout := time.After(5 * time.Second)
	for {
		select {
		case n := <-sub.Notifications():
			for _, dc := range n.DataChanges {
				if v, ok := dc.Value.Value.Value().(int16); ok && v == 99 {
					return
				}
			}
		case <-timeout:
			t.Fatal("data change notification for 99 not received")
		}
	}
}

func TestServer_OnNodePublishingUpdated(t *testing.T) {
	acc := newFakeAccessor()
	acc.publish("v-a", "A", "INT", "readwrite", int64(1))
	srv, c, ns := startTestServer(t, acc)
	ctx := context.Background()

	acc.publish("v-b", "B", "LREAL", "read", float64(3.25))
	srv.OnNodePublishingUpdated()

	dv, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "v-b"))
	if err != nil {
		t.Fatalf("Read new node: %v", err)
	}
	if got, ok := dv.Value.Value().(float64); !ok || got != 3.25 {
		t.Errorf("B = %#v, want 3.25", dv.Value.Value())
	}

	// 公開をやめた変数のノードは消える
	acc.mu.Lock()
	acc.pubs = acc.pubs[1:]
	acc.mu.Unlock()
	srv.OnNodePublishingUpdated()
	if _, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "v-a")); err == nil {
		t.Errorf("unpublished node v-a is still readable")
	}
}

func TestServer_StartFailsWhenPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	srv := NewServer(&Config{Host: "127.0.0.1", Port: port}, newFakeAccessor())
	if err := srv.Start(context.Background()); err == nil {
		_ = srv.Stop()
		t.Fatal("Start succeeded on a port in use")
	}
	if srv.Status() != protocol.StatusError {
		t.Errorf("status = %v, want StatusError", srv.Status())
	}
}

func TestServer_RestartAfterStop(t *testing.T) {
	acc := newFakeAccessor()
	port := freePort(t)
	srv := NewServer(&Config{Host: "127.0.0.1", Port: port}, acc)
	for i := 0; i < 2; i++ {
		if err := srv.Start(context.Background()); err != nil {
			t.Fatalf("Start #%d: %v", i+1, err)
		}
		if err := srv.Stop(); err != nil {
			t.Fatalf("Stop #%d: %v", i+1, err)
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within 5s")
}
