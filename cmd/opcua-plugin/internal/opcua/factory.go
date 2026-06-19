package opcua

import (
	"fmt"
	"sync"

	"modbus_simulator/internal/domain/protocol"
)

// インターフェース実装確認
var _ protocol.ServerFactory = (*OpcuaServerFactory)(nil)
var _ protocol.VariableStoreInjector = (*OpcuaServerFactory)(nil)

// OpcuaServerFactory は OPC UA サーバーを作成するファクトリー
type OpcuaServerFactory struct {
	mu       sync.RWMutex
	accessor protocol.VariableStoreAccessor // AddServer 時に遅延注入
}

// InjectVariableStore は VariableStoreInjector インターフェースの実装
func (f *OpcuaServerFactory) InjectVariableStore(accessor protocol.VariableStoreAccessor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accessor = accessor
}

func (f *OpcuaServerFactory) ProtocolType() protocol.ProtocolType {
	return "opcua"
}

func (f *OpcuaServerFactory) DisplayName() string {
	return "OPC UA"
}

func (f *OpcuaServerFactory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{
		{ID: "opcua", DisplayName: "OPC UA"},
	}
}

func (f *OpcuaServerFactory) CreateConfigFromVariant(variantID string) protocol.ProtocolConfig {
	return defaultOpcuaConfig()
}

func (f *OpcuaServerFactory) DefaultConfig() protocol.ProtocolConfig {
	return defaultOpcuaConfig()
}

func (f *OpcuaServerFactory) GetConfigFields(variantID string) []protocol.ConfigField {
	return []protocol.ConfigField{
		{
			Name:        "host",
			Label:       "ホスト",
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
	}
}

func (f *OpcuaServerFactory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	return protocol.ProtocolCapabilities{
		SupportsNodePublishing: true,
	}
}

func (f *OpcuaServerFactory) CreateServer(config protocol.ProtocolConfig, store protocol.DataStore) (protocol.ProtocolServer, error) {
	cfg, ok := config.(*OpcuaConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for OPC UA server")
	}

	f.mu.RLock()
	accessor := f.accessor
	f.mu.RUnlock()

	return newOpcuaServer(cfg, accessor), nil
}

func (f *OpcuaServerFactory) CreateDataStore() protocol.DataStore {
	return newOpcuaDataStore()
}

func (f *OpcuaServerFactory) ConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	cfg, ok := config.(*OpcuaConfig)
	if !ok {
		return nil
	}
	return map[string]interface{}{
		"host": cfg.Host,
		"port": cfg.Port,
	}
}

func (f *OpcuaServerFactory) MapToConfig(variantID string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	cfg := defaultOpcuaConfig()

	if host, ok := settings["host"].(string); ok {
		cfg.Host = host
	}
	if port, ok := settings["port"].(float64); ok {
		cfg.Port = int(port)
	} else if port, ok := settings["port"].(int); ok {
		cfg.Port = port
	}

	return cfg, cfg.Validate()
}

func intPtr(v int) *int {
	return &v
}
