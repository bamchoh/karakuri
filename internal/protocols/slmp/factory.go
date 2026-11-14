package slmp

import (
	"fmt"
	"strings"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
)

const variantEthernet = "ethernet"

// 待ち受ける運び方
const (
	transportBoth = "both"
	transportUDP  = "udp"
	transportTCP  = "tcp"
)

// Config は SLMP サーバーの設定
type Config struct {
	Host      string
	Port      int
	Transport string // both / udp / tcp
}

// DefaultConfig は既定の設定を返す
func DefaultConfig() *Config {
	return &Config{Host: "0.0.0.0", Port: defaultPort, Transport: transportBoth}
}

func (c *Config) ProtocolType() protocol.ProtocolType { return ProtocolSLMP }
func (c *Config) Variant() string                     { return variantEthernet }

func (c *Config) Validate() error {
	switch {
	case c.Port < 1 || c.Port > 65535:
		return fmt.Errorf("ポートは 1〜65535 で指定してください")
	case c.Transport != transportBoth && c.Transport != transportUDP && c.Transport != transportTCP:
		return fmt.Errorf("不明な通信方式です: %s", c.Transport)
	}
	return nil
}

func (c *Config) Clone() protocol.ProtocolConfig {
	clone := *c
	return &clone
}

// Factory は SLMP サーバーのファクトリー
type Factory struct{}

func (f *Factory) ProtocolType() protocol.ProtocolType { return ProtocolSLMP }
func (f *Factory) DisplayName() string                 { return "SLMP" }

func (f *Factory) CreateServer(config protocol.ProtocolConfig, store protocol.DataStore) (protocol.ProtocolServer, error) {
	c, ok := config.(*Config)
	if !ok {
		return nil, fmt.Errorf("invalid config type: expected slmp.Config")
	}
	return NewServer(c, store), nil
}

func (f *Factory) CreateDataStore() protocol.DataStore { return NewDataStore() }

func (f *Factory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{{ID: variantEthernet, DisplayName: "SLMP（Ethernet）"}}
}

func (f *Factory) CreateConfigFromVariant(_ string) protocol.ProtocolConfig { return DefaultConfig() }

func (f *Factory) GetConfigFields(_ string) []protocol.ConfigField {
	def := DefaultConfig()
	return []protocol.ConfigField{
		{Name: "host", Label: "アドレス", Description: "待ち受けるネットワークアドレス。0.0.0.0 で全インターフェースに対応します。", Type: "text", Required: true, Default: def.Host},
		{Name: "port", Label: "ポート", Description: "SLMP の待ち受けポート番号（TCP と UDP で共通）。実際の PLC では、パラメーターで決めた番号です。", Type: "number", Required: true, Default: def.Port, Min: fields.IntPtr(1), Max: fields.IntPtr(65535)},
		{Name: "transport", Label: "通信方式", Description: "待ち受ける通信方式。フレームは 3E と 4E（バイナリコード）のどちらも受け付けます。", Type: "select", Required: true, Default: def.Transport, Options: []protocol.FieldOption{
			{Value: transportBoth, Label: "TCP と UDP"},
			{Value: transportTCP, Label: "TCP だけ"},
			{Value: transportUDP, Label: "UDP だけ"},
		}},
	}
}

func (f *Factory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	return protocol.ProtocolCapabilities{}
}

func (f *Factory) ConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	c, ok := config.(*Config)
	if !ok {
		return nil
	}
	return map[string]interface{}{"host": c.Host, "port": c.Port, "transport": c.Transport}
}

// MapToConfig は map から設定を作る（map にない項目は既定値のまま）
func (f *Factory) MapToConfig(_ string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	c := DefaultConfig()
	fields.SetString(settings, "host", &c.Host)
	fields.SetInt(settings, "port", &c.Port)
	fields.SetString(settings, "transport", &c.Transport)
	c.Host = strings.TrimSpace(c.Host)
	return c, nil
}
