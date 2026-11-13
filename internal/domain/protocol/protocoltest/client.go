package protocoltest

import (
	"context"
	"fmt"
	"strings"
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
	closed    atomic.Int32    // Close を呼ばれた回数
	sent      atomic.Int32    // Send を呼ばれた回数
	subs      []*Subscription // factory.mu で守る
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

// ===== サブスクリプション =====

// MonitoredItemFields は監視項目の入力項目を返す（protocol.SubscriptionClientFactory）
func (f *ClientFactory) MonitoredItemFields() []protocol.ConfigField {
	return []protocol.ConfigField{{Name: "keys", Label: "キー", Type: "text", Required: true}}
}

// ExpandMonitoredItems は "a,b" を、キーごとの監視項目にする
func (f *ClientFactory) ExpandMonitoredItems(params map[string]interface{}) ([]protocol.MonitoredItemSpec, error) {
	s, _ := params["keys"].(string)
	var specs []protocol.MonitoredItemSpec
	for _, key := range strings.Split(s, ",") {
		if key = strings.TrimSpace(key); key != "" {
			specs = append(specs, protocol.MonitoredItemSpec{Params: map[string]interface{}{"keys": key}, Summary: "watch " + key})
		}
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("キーを入力してください")
	}
	return specs, nil
}

// Subscription は、フェイクのクライアントで動いている監視 1 つ
type Subscription struct {
	Request protocol.SubscriptionRequest
	events  chan protocol.SubscriptionEvent
	done    chan struct{}
}

// Emit は監視からの知らせを 1 つ送る（監視が終わっていたら false）
func (s *Subscription) Emit(ev protocol.SubscriptionEvent) bool {
	select {
	case s.events <- ev:
		return true
	case <-s.done:
		return false
	}
}

// EmitValue は、キー key の項目に値を届ける
func (s *Subscription) EmitValue(key, value string) bool {
	for _, it := range s.Request.Items {
		if it.Params["keys"] == key {
			return s.Emit(protocol.SubscriptionEvent{
				ItemID: it.ID, Status: protocol.CommandOK,
				Values: []protocol.ResultValue{{Label: key, Value: value}},
			})
		}
	}
	return false
}

// Done は、監視が終わったら閉じる
func (s *Subscription) Done() <-chan struct{} { return s.done }

// RunSubscription は「監視中」を知らせたあと、Emit で送られた知らせを流す（protocol.SubscribingClient）
func (c *Client) RunSubscription(ctx context.Context, req protocol.SubscriptionRequest, onEvent func(protocol.SubscriptionEvent)) {
	sub := &Subscription{Request: req, events: make(chan protocol.SubscriptionEvent), done: make(chan struct{})}
	defer close(sub.done)
	c.factory.mu.Lock()
	c.subs = append(c.subs, sub)
	c.factory.mu.Unlock()
	onEvent(protocol.SubscriptionEvent{Status: protocol.CommandOK})
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-sub.events:
			onEvent(ev)
		}
	}
}

// Subscriptions は、このクライアントで始まった監視（始まった順。終わったものも含む）を返す
func (c *Client) Subscriptions() []*Subscription {
	c.factory.mu.Lock()
	defer c.factory.mu.Unlock()
	return append([]*Subscription(nil), c.subs...)
}

// ===== アドレスの参照 =====

// Browse は、フェイクのアドレスの一覧を返す（protocol.BrowsableClient）。
// いちばん上は "folder"（子に "folder/a"、"folder/b" を持つ）。"missing" の子はエラー
func (c *Client) Browse(_ context.Context, parent string) ([]protocol.BrowseEntry, error) {
	switch parent {
	case "":
		return []protocol.BrowseEntry{{Address: "folder", Name: "Folder", Kind: "Folder", HasChildren: true}}, nil
	case "folder":
		return []protocol.BrowseEntry{
			{Address: "folder/a", Name: "A", Kind: "Value", Detail: "INT", Selectable: true, Fields: map[string]string{"keys": "a"}},
			{Address: "folder/b", Name: "B", Kind: "Value", Selectable: true},
		}, nil
	}
	return nil, fmt.Errorf("アドレスがありません: %s", parent)
}
