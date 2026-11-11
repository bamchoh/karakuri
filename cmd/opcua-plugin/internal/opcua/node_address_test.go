package opcua

import (
	"context"
	"fmt"
	"testing"

	"github.com/bamchoh/uastack/ua"
	"github.com/bamchoh/uastack/ua/id"
)

func TestServer_NodeAddresses(t *testing.T) {
	acc := newFakeAccessor()
	acc.publish("v-int", "Counter", "INT", "readwrite", int64(7))
	cfg := testConfig(freePort(t))
	cfg.NamespaceURI = "urn:acme:line-a"
	srv, c, _ := startTestServerWithConfig(t, acc, cfg)
	ctx := context.Background()

	// 起動中: NamespaceArray で実際に割り当てられた番号を使う
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
	got := srv.NodeAddresses([]string{"v-int"})["v-int"]
	if want := fmt.Sprintf("ns=%d;s=v-int", nsIndex); got != want {
		t.Errorf("running: NodeAddresses = %q, want %q", got, want)
	}
	// 返した NodeID でノードを読める
	if _, err := c.ReadValue(ctx, ua.NewStringNodeId(uint16(nsIndex), "v-int")); err != nil {
		t.Errorf("read by returned NodeID: %v", err)
	}

	// 停止中: 番号が決まらないのでネームスペース URI で表す
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	got = srv.NodeAddresses([]string{"v-int"})["v-int"]
	if want := "nsu=urn:acme:line-a;s=v-int"; got != want {
		t.Errorf("stopped: NodeAddresses = %q, want %q", got, want)
	}
}
