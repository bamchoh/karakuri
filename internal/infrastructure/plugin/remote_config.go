package plugin

import (
	"context"
	"encoding/json"

	"karakuri/internal/domain/protocol"
)

// remoteProtocolConfig は gRPC 越しに管理されるプロトコル設定
type remoteProtocolConfig struct {
	protocolType protocol.ProtocolType
	variantID    string
	settingsJSON string // map[string]interface{} を JSON シリアライズした文字列
}

func (c *remoteProtocolConfig) ProtocolType() protocol.ProtocolType { return c.protocolType }
func (c *remoteProtocolConfig) Variant() string                     { return c.variantID }

func (c *remoteProtocolConfig) Validate() error {
	// バリデーションは MapToConfig RPC 呼び出し時に行うため、ここでは常に nil を返す
	return nil
}

func (c *remoteProtocolConfig) Clone() protocol.ProtocolConfig {
	return &remoteProtocolConfig{
		protocolType: c.protocolType,
		variantID:    c.variantID,
		settingsJSON: c.settingsJSON,
	}
}

// ToMap は設定を map[string]interface{} に変換する（内部ヘルパー）
func (c *remoteProtocolConfig) ToMap() map[string]interface{} {
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(c.settingsJSON), &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

// backgroundCtx は gRPC 呼び出し用のコンテキストを返す
func backgroundCtx() context.Context {
	return context.Background()
}
