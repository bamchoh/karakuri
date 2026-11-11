package application

import (
	"strings"
	"testing"

	"karakuri/internal/domain/protocol"
)

// nodeFactory は "!" を含む変数名を公開できないノード公開型プロトコルのフェイク
type nodeFactory struct{ *fakeServerFactory }

func (f *nodeFactory) CreateServer(config protocol.ProtocolConfig, _ protocol.DataStore) (protocol.ProtocolServer, error) {
	return &nodeServer{fakeServer{cfg: config}}, nil
}

type nodeServer struct{ fakeServer }

func (s *nodeServer) NodeAddresses(vars []protocol.NodeVariable) map[string]protocol.NodeAddress {
	m := map[string]protocol.NodeAddress{}
	for _, v := range vars {
		a := protocol.NodeAddress{Address: "node:" + v.Name}
		if strings.Contains(v.Name, "!") {
			a.Error = "! は使えません"
		}
		m[v.ID] = a
	}
	return m
}

func TestNodeAddresses_RejectUnpublishableNames(t *testing.T) {
	svc := newTestService(t)
	svc.RegisterPluginFactory(&nodeFactory{newFakeModbusFactory("node", "", "Node")})
	if err := svc.AddServer("node", ""); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	good, _ := svc.CreateVariable("Good", "INT", 0)
	bad, _ := svc.CreateVariable("Bad!", "INT", 0)
	pub := &NodePublishingDTO{ProtocolType: "node", Enabled: true, AccessMode: "readwrite"}

	// アドレスとエラーはプロトコルが返したものをそのまま渡す（サーバー未起動でも取得できる）
	addrs := svc.GetNodeAddresses("node", []string{good.ID, bad.ID})
	if len(addrs) != 2 {
		t.Fatalf("addrs = %+v", addrs)
	}
	if a := addrs[0]; a.VariableID != good.ID || a.Address != "node:Good" || a.Error != "" {
		t.Errorf("good = %+v", a)
	}
	if a := addrs[1]; a.VariableID != bad.ID || a.Error == "" {
		t.Errorf("bad = %+v, want error", a)
	}

	// 公開できない変数は公開設定の保存を拒否する（公開をやめる保存はできる）
	if err := svc.UpdateVariableNodePublishing(bad.ID, "node", pub); err == nil {
		t.Errorf("publishing unpublishable variable succeeded")
	}
	if err := svc.UpdateVariableNodePublishing(bad.ID, "node", &NodePublishingDTO{ProtocolType: "node"}); err != nil {
		t.Errorf("disabling: %v", err)
	}
	if err := svc.UpdateVariableNodePublishing(good.ID, "node", pub); err != nil {
		t.Fatalf("publishing good variable: %v", err)
	}

	// 公開中の変数を公開できない名前に変えることはできない。公開していない変数は変えられる
	if _, err := svc.UpdateVariable(good.ID, "Good!", "INT"); err == nil {
		t.Errorf("renaming published variable to unpublishable name succeeded")
	}
	if _, err := svc.UpdateVariable(good.ID, "Better", "INT"); err != nil {
		t.Errorf("renaming to publishable name: %v", err)
	}
	if _, err := svc.UpdateVariable(bad.ID, "Worse!", "INT"); err != nil {
		t.Errorf("renaming unpublished variable: %v", err)
	}
}
