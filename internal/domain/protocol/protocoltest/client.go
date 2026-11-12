package protocoltest

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"karakuri/internal/domain/protocol"
)

// ===== クライアント =====

// ClientFactory はフェイクのクライアントのファクトリー。
// 設定は "target"（文字列、必須）と "timeoutMs"（数値）を持つ。コマンドは "echo"（項目 "data" の文字列をそのまま送る）だけ。
// 作ったクライアントは Respond で応答を決める（既定は送ったバイト列をそのまま返す）。
type ClientFactory struct {
	Type protocol.ProtocolType

	mu      sync.Mutex
	Respond func(request []byte) protocol.CommandResult
	Clients []*Client // 作ったクライアント（作った順）
}

// NewClientFactory はフェイクのクライアントのファクトリーを作る
func NewClientFactory(pt protocol.ProtocolType) *ClientFactory {
	return &ClientFactory{Type: pt}
}

func (f *ClientFactory) ProtocolType() protocol.ProtocolType { return f.Type }
func (f *ClientFactory) DisplayName() string                 { return "Fake " + string(f.Type) }

func (f *ClientFactory) ConnectionFields() []protocol.ConfigField {
	return []protocol.ConfigField{
		{Name: "target", Label: "接続先", Type: "text", Required: true, Default: "fake"},
		{Name: "timeoutMs", Label: "タイムアウト", Type: "number", Required: true, Default: 100},
	}
}

func (f *ClientFactory) NormalizeSettings(settings map[string]interface{}) (map[string]interface{}, error) {
	out := map[string]interface{}{"target": "fake", "timeoutMs": 100}
	if v, ok := settings["target"].(string); ok {
		if v == "" {
			return nil, fmt.Errorf("接続先を入力してください")
		}
		out["target"] = v
	}
	switch v := settings["timeoutMs"].(type) {
	case float64:
		out["timeoutMs"] = int(v)
	case int:
		out["timeoutMs"] = v
	}
	return out, nil
}

func (f *ClientFactory) Commands() []protocol.CommandDefinition {
	return []protocol.CommandDefinition{
		{
			ID: "echo", DisplayName: "エコー", Pollable: true,
			Fields: []protocol.ConfigField{{Name: "data", Label: "データ", Type: "text", Default: "AB"}},
		},
		{
			ID: "write", DisplayName: "書き込み（ポーリングには使えない）",
			Fields: []protocol.ConfigField{{Name: "data", Label: "データ", Type: "text", Default: "W"}},
		},
	}
}

// RequestHelp は、16 進で入れるリクエストの説明を返す（protocol.RequestHelpProvider）
func (f *ClientFactory) RequestHelp() protocol.RequestHelp {
	return protocol.RequestHelp{Description: "送る文字を 16 進で入れます。", Example: "41 42"}
}

func (f *ClientFactory) DescribeRequest(request []byte) string {
	return "echo " + string(request)
}

func (f *ClientFactory) BuildRequest(commandID string, params map[string]interface{}) ([]byte, error) {
	if commandID != "echo" && commandID != "write" {
		return nil, fmt.Errorf("不明なコマンドです: %s", commandID)
	}
	s, _ := params["data"].(string)
	if s == "" {
		return nil, fmt.Errorf("データを入力してください")
	}
	return []byte(s), nil
}

func (f *ClientFactory) NewClient(settings map[string]interface{}) (protocol.Client, error) {
	c := &Client{factory: f, Settings: copySettings(settings)}
	f.mu.Lock()
	f.Clients = append(f.Clients, c)
	f.mu.Unlock()
	return c, nil
}

// SetRespond は応答を決める関数を差し替える
func (f *ClientFactory) SetRespond(fn func(request []byte) protocol.CommandResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Respond = fn
}

// CreatedClients は作ったクライアントのコピーを返す
func (f *ClientFactory) CreatedClients() []*Client {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*Client(nil), f.Clients...)
}

// Client はフェイクのクライアント
type Client struct {
	factory   *ClientFactory
	Settings  map[string]interface{}
	connected atomic.Bool
	closed    atomic.Int32 // Close を呼ばれた回数
	sent      atomic.Int32 // Send を呼ばれた回数
}

func (c *Client) Send(_ context.Context, request []byte) protocol.CommandResult {
	c.sent.Add(1)
	c.factory.mu.Lock()
	respond := c.factory.Respond
	c.factory.mu.Unlock()
	if respond != nil {
		res := respond(request)
		c.connected.Store(res.Status == protocol.CommandOK || res.Status == protocol.CommandException)
		return res
	}
	c.connected.Store(true)
	return protocol.CommandResult{
		Sent: request, Received: request, Elapsed: time.Millisecond,
		Status: protocol.CommandOK, Summary: "echo",
		Values: []protocol.ResultValue{{Label: "データ", Value: string(request)}},
	}
}

func (c *Client) Connected() bool { return c.connected.Load() }

func (c *Client) Close() error {
	c.closed.Add(1)
	c.connected.Store(false)
	return nil
}

// CloseCount は Close を呼ばれた回数を返す
func (c *Client) CloseCount() int { return int(c.closed.Load()) }

// SendCount は Send を呼ばれた回数を返す
func (c *Client) SendCount() int { return int(c.sent.Load()) }
