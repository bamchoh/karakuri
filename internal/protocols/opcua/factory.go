package opcua

import (
	"fmt"
	"strings"
	"sync"

	"karakuri/internal/domain/protocol"
)

const VERSION = "1.0.0"

// ProtocolOPCUA は OPC UA のプロトコルタイプ
const ProtocolOPCUA protocol.ProtocolType = "opcua"

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

func (f *Factory) ProtocolType() protocol.ProtocolType { return ProtocolOPCUA }
func (f *Factory) DisplayName() string                 { return "OPC UA" }

func (f *Factory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{{ID: "", DisplayName: f.DisplayName()}}
}

func (f *Factory) CreateConfigFromVariant(_ string) protocol.ProtocolConfig { return DefaultConfig() }

// 設定のキー。アプリケーション情報は旧 C# 版 opcua-plugin と同じキー名（manufacture_name の綴りも同じ）
const (
	keyHost             = "host"
	keyPort             = "port"
	keyEndpointHost     = "endpointHost"
	keyApplicationName  = "application_name"
	keyManufacturerName = "manufacture_name"
	keyProductURI       = "product_uri"
	keyNamespace        = "namespace"
)

// 設定画面のカテゴリ
const (
	categoryNetwork     = "ネットワーク"
	categoryApplication = "アプリケーション情報"
)

func (f *Factory) GetConfigFields(_ string) []protocol.ConfigField {
	return []protocol.ConfigField{
		{
			Name:        keyHost,
			Label:       "アドレス",
			Description: "待ち受けるネットワークアドレス。0.0.0.0 で全インターフェースに対応します。",
			Type:        "text",
			Required:    true,
			Default:     DefaultHost,
			Category:    categoryNetwork,
		},
		{
			Name:        keyPort,
			Label:       "ポート番号",
			Description: "OPC UA サーバーの待ち受けポート番号。標準ポートは 4840 です。",
			Type:        "number",
			Required:    true,
			Default:     DefaultPort,
			Min:         intPtr(1),
			Max:         intPtr(65535),
			Category:    categoryNetwork,
		},
		{
			Name:        keyEndpointHost,
			Label:       "エンドポイントのホスト名",
			Description: "クライアントに通知するエンドポイント URL（opc.tcp://<ホスト名>:<ポート>）のホスト名。空欄の場合はこの PC のホスト名を使います。",
			Type:        "text",
			Required:    false,
			Default:     "",
			Category:    categoryNetwork,
		},
		{
			Name:        keyApplicationName,
			Label:       "アプリケーション名 (ApplicationName)",
			Description: "OPC UA サーバーで公開されるアプリケーションの名称。クライアントのサーバー一覧や BuildInfo.ProductName に表示されます。",
			Type:        "text",
			Required:    true,
			Default:     DefaultApplicationName,
			Category:    categoryApplication,
		},
		{
			Name:        keyManufacturerName,
			Label:       "メーカー名 (ManufacturerName)",
			Description: "OPC UA サーバーで公開されるメーカー名（BuildInfo.ManufacturerName）。",
			Type:        "text",
			Required:    true,
			Default:     DefaultManufacturerName,
			Category:    categoryApplication,
		},
		{
			Name:        keyProductURI,
			Label:       "プロダクト URI (ProductUri)",
			Description: "OPC UA サーバーで公開されるプロダクト URI（ApplicationDescription.ProductUri と BuildInfo.ProductUri）。",
			Type:        "text",
			Required:    true,
			Default:     DefaultProductURI,
			Category:    categoryApplication,
		},
		{
			Name:        keyNamespace,
			Label:       "ネームスペース (Namespace)",
			Description: "公開する変数ノードの名前空間 URI。NodeId の ns 番号はこの URI に割り当てられます（通常 ns=2）。",
			Type:        "text",
			Required:    true,
			Default:     DefaultNamespaceURI,
			Category:    categoryApplication,
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
		keyHost:             cfg.Host,
		keyPort:             cfg.Port,
		keyEndpointHost:     cfg.EndpointHost,
		keyApplicationName:  cfg.ApplicationName,
		keyManufacturerName: cfg.ManufacturerName,
		keyProductURI:       cfg.ProductURI,
		keyNamespace:        cfg.NamespaceURI,
	}
}

// MapToConfig は設定マップを Config に変換する。
// マップにないキー（項目追加前に保存された設定など）は既定値のままにする。
func (f *Factory) MapToConfig(_ string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	cfg := DefaultConfig()
	setString := func(key string, dst *string) {
		if v, ok := settings[key].(string); ok {
			*dst = strings.TrimSpace(v)
		}
	}
	setString(keyHost, &cfg.Host)
	switch v := settings[keyPort].(type) {
	case float64:
		cfg.Port = int(v)
	case int:
		cfg.Port = v
	}
	setString(keyEndpointHost, &cfg.EndpointHost)
	setString(keyApplicationName, &cfg.ApplicationName)
	setString(keyManufacturerName, &cfg.ManufacturerName)
	setString(keyProductURI, &cfg.ProductURI)
	setString(keyNamespace, &cfg.NamespaceURI)
	return cfg, cfg.Validate()
}

func intPtr(v int) *int { return &v }
