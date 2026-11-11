package pluginruntime

import (
	"context"
	"testing"

	"karakuri/internal/domain/protocol"
	pb "karakuri/pb/pluginpb"
)

// prefixConfig は NodeAddresses の接頭辞だけを持つテスト用設定
type prefixConfig struct{ prefix string }

func (c *prefixConfig) ProtocolType() protocol.ProtocolType { return "test" }
func (c *prefixConfig) Variant() string                     { return "" }
func (c *prefixConfig) Validate() error                     { return nil }
func (c *prefixConfig) Clone() protocol.ProtocolConfig      { cp := *c; return &cp }

type addrServer struct {
	protocol.ProtocolServer
	cfg    *prefixConfig
	status protocol.ServerStatus
}

func (s *addrServer) Status() protocol.ServerStatus { return s.status }
func (s *addrServer) NodeAddresses(ids []string) map[string]string {
	m := map[string]string{}
	for _, id := range ids {
		m[id] = s.cfg.prefix + id
	}
	return m
}

type addrFactory struct {
	protocol.ServerFactory
	nodePublishing bool
}

func (f *addrFactory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	return protocol.ProtocolCapabilities{SupportsNodePublishing: f.nodePublishing}
}
func (f *addrFactory) CreateConfigFromVariant(string) protocol.ProtocolConfig {
	return &prefixConfig{prefix: "default:"}
}
func (f *addrFactory) MapToConfig(_ string, m map[string]interface{}) (protocol.ProtocolConfig, error) {
	p, _ := m["prefix"].(string)
	return &prefixConfig{prefix: p}, nil
}
func (f *addrFactory) CreateDataStore() protocol.DataStore { return nil }
func (f *addrFactory) CreateServer(cfg protocol.ProtocolConfig, _ protocol.DataStore) (protocol.ProtocolServer, error) {
	return &addrServer{cfg: cfg.(*prefixConfig), status: protocol.StatusStopped}, nil
}

func TestServer_GetNodeAddresses(t *testing.T) {
	ctx := context.Background()
	req := &pb.GetNodeAddressesRequest{SettingsJson: `{"prefix":"req:"}`, VariableIds: []string{"a"}}
	get := func(s *Server, r *pb.GetNodeAddressesRequest) string {
		t.Helper()
		resp, err := s.GetNodeAddresses(ctx, r)
		if err != nil {
			t.Fatalf("GetNodeAddresses: %v", err)
		}
		return resp.Addresses["a"]
	}

	// サーバー未作成: リクエストの設定で計算する（設定が空なら既定値）
	s := newServer(&addrFactory{nodePublishing: true})
	if got := get(s, req); got != "req:a" {
		t.Errorf("no server = %q, want req:a", got)
	}
	if got := get(s, &pb.GetNodeAddressesRequest{VariableIds: []string{"a"}}); got != "default:a" {
		t.Errorf("no settings = %q, want default:a", got)
	}

	// 起動中: 動いているサーバーに問い合わせる
	s.server = &addrServer{cfg: &prefixConfig{prefix: "running:"}, status: protocol.StatusRunning}
	if got := get(s, req); got != "running:a" {
		t.Errorf("running = %q, want running:a", got)
	}

	// 停止中: プラグインが持つ設定は古い可能性があるため、リクエストの設定を使う
	s.server = &addrServer{cfg: &prefixConfig{prefix: "stale:"}, status: protocol.StatusStopped}
	if got := get(s, req); got != "req:a" {
		t.Errorf("stopped = %q, want req:a", got)
	}

	// ノード公開型でないプロトコルは空
	s = newServer(&addrFactory{nodePublishing: false})
	if got := get(s, req); got != "" {
		t.Errorf("non node-publishing = %q, want empty", got)
	}
}
