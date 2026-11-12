package opcua

import (
	"context"
	"fmt"
	"testing"

	"github.com/bamchoh/uastack/ua"
	"github.com/bamchoh/uastack/ua/id"

	"karakuri/internal/domain/protocol"
)

func TestServer_NodeAddresses(t *testing.T) {
	acc := newFakeAccessor()
	acc.publish("v-int", "Counter", "INT", "readwrite", int64(7))
	cfg := testConfig(freePort(t))
	cfg.NamespaceURI = "urn:acme:line-a"
	srv, c, _ := startTestServerWithConfig(t, acc, cfg)
	ctx := context.Background()
	vars := []protocol.NodeVariable{{ID: "v-int", Name: "Counter"}}

	// 起動中: NamespaceArray で実際に割り当てられた番号と変数名を使う
	dv, err := c.ReadValue(ctx, ua.NewNumericNodeId(0, id.Server_NamespaceArray))
	if err != nil {
		t.Fatalf("read namespace array: %v", err)
	}
	nsArray, _ := dv.Value.Value().([]string)
	nsIndex := -1
	for i, uri := range nsArray {
		if uri == cfg.NamespaceURI {
			nsIndex = i
		}
	}
	if nsIndex < 0 {
		t.Fatalf("namespace %q not in %v", cfg.NamespaceURI, nsArray)
	}
	got := srv.NodeAddresses(vars)["v-int"].Address
	if want := fmt.Sprintf("ns=%d;s=Counter", nsIndex); got != want {
		t.Errorf("running: NodeAddresses = %q, want %q", got, want)
	}
	// 返した NodeID でノードを読める
	if _, err := c.ReadValue(ctx, ua.NewStringNodeId(uint16(nsIndex), "Counter")); err != nil {
		t.Errorf("read by returned NodeID: %v", err)
	}

	// 停止中: 番号が決まらないので ns=??? とする
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	got = srv.NodeAddresses(vars)["v-int"].Address
	if want := "ns=???;s=Counter"; got != want {
		t.Errorf("stopped: NodeAddresses = %q, want %q", got, want)
	}
}

// NodeID は変数名から作るため、変数名を変えると古い NodeID は消え、新しい名前で読める
func TestServer_RenameChangesNodeID(t *testing.T) {
	acc := newFakeAccessor()
	acc.publish("v-str", "Motor1", "Motor", "readwrite", map[string]interface{}{"speed": int64(100), "on": true})
	srv, c, ns := startTestServer(t, acc)
	ctx := context.Background()

	if _, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "Motor1.speed")); err != nil {
		t.Fatalf("read before rename: %v", err)
	}

	acc.mu.Lock()
	acc.pubs[0].VariableName = "Pump"
	acc.mu.Unlock()
	srv.OnNodePublishingUpdated()

	dv, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "Pump.speed"))
	if err != nil {
		t.Fatalf("read renamed child: %v", err)
	}
	if got, ok := dv.Value.Value().(int16); !ok || got != 100 {
		t.Errorf("Pump.speed = %#v, want 100", dv.Value.Value())
	}
	for _, old := range []string{"Motor1", "Motor1.speed"} {
		if _, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, old)); err == nil {
			t.Errorf("old node %q is still readable", old)
		}
	}
	// 書き込みは変数 ID でホストに届く
	if err := c.WriteValue(ctx, ua.NewStringNodeId(ns, "Pump.speed"), ua.NewVariant(int16(5))); err != nil {
		t.Fatalf("write renamed child: %v", err)
	}
	if m, _ := acc.mustRead("v-str").(map[string]interface{}); toInt64(m["speed"]) != 5 {
		t.Errorf("host value = %#v, want speed=5", m)
	}
}

// NodeID が他のノードと重なる変数名は Error を返し、ノードも作らない
func TestServer_UnpublishableNames(t *testing.T) {
	acc := newFakeAccessor()
	acc.publish("v-ok", "Motor_1", "INT", "readwrite", int64(1))
	acc.publish("v-dot", "Motor1.speed", "INT", "readwrite", int64(2))
	acc.publish("v-idx", "Values[0]", "INT", "readwrite", int64(3))
	acc.publish("v-folder", folderNodeName, "INT", "readwrite", int64(4))
	srv, c, ns := startTestServer(t, acc)
	ctx := context.Background()

	addrs := srv.NodeAddresses([]protocol.NodeVariable{
		{ID: "v-ok", Name: "Motor_1"}, {ID: "v-dot", Name: "Motor1.speed"},
		{ID: "v-idx", Name: "Values[0]"}, {ID: "v-folder", Name: folderNodeName},
	})
	if a := addrs["v-ok"]; a.Error != "" || a.Address == "" {
		t.Errorf("v-ok = %+v, want no error", a)
	}
	for _, id := range []string{"v-dot", "v-idx", "v-folder"} {
		if addrs[id].Error == "" {
			t.Errorf("%s: no error", id)
		}
	}

	// 公開できる変数だけがフォルダに並ぶ
	refs, err := c.BrowseChildren(ctx, ua.NewStringNodeId(ns, folderNodeName))
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("children = %d, want 1 (%+v)", len(refs), refs)
	}
	if _, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "Motor1.speed")); err == nil {
		t.Errorf("unpublishable node is readable")
	}
}
