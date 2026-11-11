package opcua

import (
	"fmt"
	"strings"

	"karakuri/internal/domain/protocol"
)

// Config は OPC UA サーバーの設定
type Config struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	// EndpointHost はクライアントに通知するエンドポイント URL のホスト名。
	// 空の場合、Host が 0.0.0.0 ならマシンのホスト名、それ以外なら Host を使う。
	EndpointHost string `json:"endpointHost"`
}

// DefaultConfig はデフォルト設定を返す
func DefaultConfig() *Config {
	return &Config{Host: "0.0.0.0", Port: 4840}
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
