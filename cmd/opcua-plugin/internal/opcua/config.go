package opcua

import (
	"fmt"
	"strings"

	"karakuri/internal/domain/protocol"
)

// 設定の既定値
const (
	DefaultHost             = "0.0.0.0"
	DefaultPort             = 4840
	DefaultApplicationName  = "Karakuri PLC Simulator"
	DefaultManufacturerName = "karakuri"
	DefaultProductURI       = "urn:karakuri"
	DefaultNamespaceURI     = "urn:karakuri:plc"
)

// opcuaBaseNamespace は名前空間 0（OPC UA 標準）の URI。変数の名前空間には使えない
const opcuaBaseNamespace = "http://opcfoundation.org/UA/"

// Config は OPC UA サーバーの設定
type Config struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	// EndpointHost はクライアントに通知するエンドポイント URL のホスト名。
	// 空の場合、Host が 0.0.0.0 ならマシンのホスト名、それ以外なら Host を使う。
	EndpointHost string `json:"endpointHost"`

	// アプリケーション情報（GetEndpoints の ApplicationDescription と Server オブジェクトの BuildInfo で公開される）
	ApplicationName  string `json:"application_name"`
	ManufacturerName string `json:"manufacture_name"`
	ProductURI       string `json:"product_uri"`
	// NamespaceURI は公開変数ノードの名前空間 URI
	NamespaceURI string `json:"namespace"`
}

// DefaultConfig はデフォルト設定を返す
func DefaultConfig() *Config {
	return &Config{
		Host:             DefaultHost,
		Port:             DefaultPort,
		ApplicationName:  DefaultApplicationName,
		ManufacturerName: DefaultManufacturerName,
		ProductURI:       DefaultProductURI,
		NamespaceURI:     DefaultNamespaceURI,
	}
}

func (c *Config) ProtocolType() protocol.ProtocolType { return protocol.ProtocolOPCUA }
func (c *Config) Variant() string                     { return "" }

func (c *Config) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("ホストを指定してください")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("ポート番号は 1〜65535 の範囲で指定してください")
	}
	if strings.TrimSpace(c.ApplicationName) == "" {
		return fmt.Errorf("アプリケーション名を指定してください")
	}
	if strings.TrimSpace(c.ManufacturerName) == "" {
		return fmt.Errorf("メーカー名を指定してください")
	}
	if strings.TrimSpace(c.ProductURI) == "" {
		return fmt.Errorf("プロダクト URI を指定してください")
	}
	ns := strings.TrimSpace(c.NamespaceURI)
	if ns == "" {
		return fmt.Errorf("ネームスペースを指定してください")
	}
	if ns == opcuaBaseNamespace {
		return fmt.Errorf("ネームスペースに %s（OPC UA 標準の名前空間）は使えません", opcuaBaseNamespace)
	}
	return nil
}

func (c *Config) Clone() protocol.ProtocolConfig {
	cp := *c
	return &cp
}

// listenAddr は net.Listen に渡すアドレスを返す
func (c *Config) listenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// endpointURL は公開するエンドポイント URL を返す。空文字列なら uastack の既定（ホスト名）を使う。
func (c *Config) endpointURL() string {
	host := strings.TrimSpace(c.EndpointHost)
	if host == "" && c.Host != "0.0.0.0" && c.Host != "::" {
		host = c.Host
	}
	if host == "" {
		return ""
	}
	return fmt.Sprintf("opc.tcp://%s:%d", host, c.Port)
}
