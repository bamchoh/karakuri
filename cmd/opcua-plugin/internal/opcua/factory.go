package opcua

import (
	"fmt"
	"sync"

	"karakuri/internal/domain/protocol"
)

const VERSION = "1.0.0"

var _ protocol.ServerFactory = (*Factory)(nil)
var _ protocol.VariableStoreInjector = (*Factory)(nil)

// Factory は uastack ベースの OPC UA サーバーを作成するファクトリー
type Factory struct {
	mu       sync.RWMutex
	accessor protocol.VariableStoreAccessor // CreateAndStart 時に注入される
}

// InjectVariableStore は VariableStoreInjector の実装
func (f *Factory) InjectVariableStore(accessor protocol.VariableStoreAccessor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accessor = accessor
}

func (f *Factory) Version() string                     { return VERSION }
func (f *Factory) ProtocolType() protocol.ProtocolType { return protocol.ProtocolOPCUA }
func (f *Factory) DisplayName() string                 { return "OPC UA" }

func (f *Factory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{{ID: "", DisplayName: f.DisplayName()}}
}

func (f *Factory) CreateConfigFromVariant(_ string) protocol.ProtocolConfig { return DefaultConfig() }
func (f *Factory) DefaultConfig() protocol.ProtocolConfig                   { return DefaultConfig() }

func (f *Factory) GetConfigFields(_ string) []protocol.ConfigField {
	return []protocol.ConfigField{
		{
			Name:        "host",
			Label:       "アドレス",
			Description: "待ち受けるネットワークアドレス。0.0.0.0 で全インターフェースに対応します。",
			Type:        "text",
			Required:    true,
			Default:     "0.0.0.0",
		},
		{
			Name:        "port",
			Label:       "ポート番号",
			Description: "OPC UA サーバーの待ち受けポート番号。標準ポートは 4840 です。",
			Type:        "number",
			Required:    true,
			Default:     4840,
			Min:         intPtr(1),
			Max:         intPtr(65535),
		},
		{
			Name:        "endpointHost",
			Label:       "エンドポイントのホスト名",
			Description: "クライアントに通知するエンドポイント URL（opc.tcp://<ホスト名>:<ポート>）のホスト名。空欄の場合はこの PC のホスト名を使います。",
			Type:        "text",
			Required:    false,
			Default:     "",
		},
	}
}

func (f *Factory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	return protocol.ProtocolCapabilities{SupportsNodePublishing: true}
}

func (f *Factory) CreateServer(config protocol.ProtocolConfig, _ protocol.DataStore) (protocol.ProtocolServer, error) {
	cfg, ok := config.(*Config)
	if !ok {
		return nil, fmt.Errorf("invalid config type for OPC UA server: %T", config)
	}
	f.mu.RLock()
	accessor := f.accessor
	f.mu.RUnlock()
	return NewServer(cfg, accessor), nil
}

func (f *Factory) CreateDataStore() protocol.DataStore { return &emptyDataStore{} }

func (f *Factory) ConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	cfg, ok := config.(*Config)
	if !ok {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"host":         cfg.Host,
		"port":         cfg.Port,
		"endpointHost": cfg.EndpointHost,
	}
}

func (f *Factory) MapToConfig(_ string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	cfg := DefaultConfig()
	if v, ok := settings["host"].(string); ok {
		cfg.Host = v
	}
	switch v := settings["port"].(type) {
	case float64:
		cfg.Port = int(v)
	case int:
		cfg.Port = v
	}
	if v, ok := settings["endpointHost"].(string); ok {
		cfg.EndpointHost = v
	}
	return cfg, cfg.Validate()
}

func intPtr(v int) *int { return &v }
