package opcua

import (
	"context"
	"fmt"
	"os"
	"time"

	uaclient "github.com/bamchoh/uastack/client"
	"github.com/bamchoh/uastack/ua"

	"karakuri/internal/domain/protocol"
)

// OPC UA クライアントのサブスクリプション（値の変化の通知）。
//
// 監視項目 1 つが、1 つのノードの Value 属性の MonitoredItem になる。
// コマンド（Send）とは別のセッションで動かす（コマンドのタイムアウトで接続を閉じても、監視が切れないようにするため）。
// 接続が切れたら uastack がつなぎ直し、サブスクリプションと MonitoredItem を作り直す。
// それでも続けられないとき（サーバーがサブスクリプションを捨てたなど）は、接続から作り直す。

const (
	// つながらないとき、つなぎ直すまでの待ち時間（失敗が続くと、上限まで倍にしていく）
	subscriptionRetryMin = time.Second
	subscriptionRetryMax = 5 * time.Second
	// 監視用のセッションのタイムアウト（サーバーに頼む値）
	subscriptionSessionTimeout = time.Minute
)

var _ protocol.SubscriptionClientFactory = (*ClientFactory)(nil)
var _ protocol.SubscribingClient = (*Client)(nil)

// MonitoredItemFields は、監視項目を追加するときの入力項目を返す
func (f *ClientFactory) MonitoredItemFields() []protocol.ConfigField {
	return []protocol.ConfigField{{
		Name: "nodeId", Label: "NodeId", Type: "textarea", Required: true, Default: "",
		Description: nodeIDDescription + "いくつも監視するときは、1 行に 1 つずつ入れます（行の数だけ監視項目を追加します）。配列のノードは、要素ごとの値を出します。",
	}}
}

// ExpandMonitoredItems は入力（1 行に 1 つの NodeId）を、ノードごとの監視項目にする
func (f *ClientFactory) ExpandMonitoredItems(params map[string]interface{}) ([]protocol.MonitoredItemSpec, error) {
	ids, err := nodeIDsParam(params)
	if err != nil {
		return nil, err
	}
	specs := make([]protocol.MonitoredItemSpec, len(ids))
	for i, id := range ids {
		specs[i] = protocol.MonitoredItemSpec{
			Params:  map[string]interface{}{"nodeId": id.String()},
			Summary: id.String(),
		}
	}
	return specs, nil
}

// subscriptionDial は、監視用のセッションをつなぐ（切れたら uastack がつなぎ直す）
func (c *Client) subscriptionDial(ctx context.Context, onState func(uaclient.ConnState, error)) (*uaclient.Client, error) {
	cfg := c.dialConfig()
	cfg.SessionName = "Karakuri (subscription)"
	cfg.AutoReconnect = true
	cfg.MaxReconnectInterval = subscriptionRetryMax
	// セッションを保つ間隔は、こちらで決める。任せると uastack（v0.1.1）は、サーバーが決めたセッションの
	// タイムアウトから間隔を出すが、その値をロックなしで読むので、つなぎ直しでセッションを作り直す処理と
	// データ競合になる（競合検出で失敗する）
	cfg.SessionTimeout = subscriptionSessionTimeout
	cfg.KeepAliveInterval = subscriptionSessionTimeout / 3
	cfg.OnStateChange = onState
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return uaclient.Dial(ctx, c.settings.EndpointURL, cfg)
}

// RunSubscription は監視を始め、ctx が取り消されるまで戻らない
func (c *Client) RunSubscription(ctx context.Context, req protocol.SubscriptionRequest, onEvent func(protocol.SubscriptionEvent)) {
	// onEvent は 1 つずつ呼ぶ決まり。接続の状態の変化（uastack のゴルーチンから来る）も、このゴルーチンで知らせる
	states := make(chan error, 8)
	retry := subscriptionRetryMin
	for ctx.Err() == nil {
		started := time.Now()
		err := c.runSubscriptionOnce(ctx, req, states, onEvent)
		if ctx.Err() != nil {
			return
		}
		onEvent(protocol.SubscriptionEvent{Status: protocol.CommandError, Error: err.Error()})
		// しばらく監視できていたなら、すぐにつなぎ直す。つながらないのが続くなら、間を空ける
		if time.Since(started) > subscriptionRetryMax {
			retry = subscriptionRetryMin
		}
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		retry = min(retry*2, subscriptionRetryMax)
	}
}

// runSubscriptionOnce は、接続 → サブスクリプションの作成 → 通知の受け取りを 1 回行う。
// 続けられなくなったら理由を返す（ctx が取り消されたときも戻る）
func (c *Client) runSubscriptionOnce(ctx context.Context, req protocol.SubscriptionRequest, states chan error, onEvent func(protocol.SubscriptionEvent)) error {
	// 前の接続の状態の知らせが残っていたら捨てる
	for len(states) > 0 {
		<-states
	}
	conn, err := c.subscriptionDial(ctx, func(state uaclient.ConnState, err error) {
		var report error
		switch state {
		case uaclient.StateConnected:
		case uaclient.StateReconnecting:
			report = fmt.Errorf("接続が切れました。つなぎ直しています（%v）", err)
		default:
			return // 閉じたときは、通知のチャネルが閉じるので、そちらで知る
		}
		select {
		case states <- report:
		default: // たまっているなら捨てる（最新の状態は、次の変化で届く）
		}
	})
	if err != nil {
		if isTimeout(err) {
			return fmt.Errorf("%s に接続できませんでした（タイムアウト）", c.settings.EndpointURL)
		}
		return fmt.Errorf("%s に接続できませんでした: %v", c.settings.EndpointURL, err)
	}
	c.subConn.Store(conn)
	defer func() {
		c.subConn.Store(nil)
		closeCtx, cancel := context.WithTimeout(context.Background(), closeWait)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	sub, err := conn.CreateSubscription(callCtx, uaclient.SubscriptionParams{PublishingInterval: req.Interval})
	if err != nil {
		return fmt.Errorf("サブスクリプションを作れませんでした: %v", err)
	}

	// ClientHandle（1 始まり）で、通知がどの項目のものかを引く
	creates := make([]ua.MonitoredItemCreateRequest, 0, len(req.Items))
	itemIDs := make([]string, 0, len(req.Items)) // itemIDs[handle-1]
	for _, it := range req.Items {
		nodeID, err := nodeIDParam(it.Params)
		if err != nil {
			onEvent(protocol.SubscriptionEvent{ItemID: it.ID, Status: protocol.CommandError, Error: err.Error()})
			continue
		}
		itemIDs = append(itemIDs, it.ID)
		creates = append(creates, ua.MonitoredItemCreateRequest{
			ItemToMonitor:  ua.ReadValueId{NodeId: nodeID, AttributeId: ua.AttributeIdValue},
			MonitoringMode: ua.MonitoringModeReporting,
			RequestedParameters: ua.MonitoringParameters{
				ClientHandle:     uint32(len(itemIDs)),
				SamplingInterval: -1, // サブスクリプションの間隔と同じ
				QueueSize:        1,
				DiscardOldest:    true,
			},
		})
	}
	if len(creates) > 0 {
		created, err := sub.Monitor(callCtx, ua.TimestampsToReturnNeither, creates...)
		if err != nil {
			return fmt.Errorf("監視項目を作れませんでした: %v", err)
		}
		for i, m := range created {
			if m.Status.IsBad() {
				onEvent(protocol.SubscriptionEvent{
					ItemID: itemIDs[i], Status: protocol.CommandException,
					Error: "監視できません: " + statusText(m.Status),
				})
			}
		}
	}
	onEvent(protocol.SubscriptionEvent{Status: protocol.CommandOK})

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case stateErr := <-states:
			if stateErr != nil {
				onEvent(protocol.SubscriptionEvent{Status: protocol.CommandError, Error: stateErr.Error()})
			} else {
				onEvent(protocol.SubscriptionEvent{Status: protocol.CommandOK})
			}
		case n, ok := <-sub.Notifications():
			switch {
			case !ok:
				return fmt.Errorf("サブスクリプションが終わりました")
			case n.Err != nil:
				return fmt.Errorf("接続が切れました: %v", n.Err)
			case n.StatusChange.IsBad():
				return fmt.Errorf("サーバーがサブスクリプションを終わらせました: %s", statusText(n.StatusChange))
			}
			for _, dc := range n.DataChanges {
				if dc.ClientHandle == 0 || int(dc.ClientHandle) > len(itemIDs) {
					continue
				}
				ev := protocol.SubscriptionEvent{ItemID: itemIDs[dc.ClientHandle-1], Status: protocol.CommandOK}
				if st := dc.Value.Status; st.IsBad() {
					ev.Status = protocol.CommandException
					ev.Error = "値が Bad です: " + statusText(st)
				} else {
					ev.Values = variantRows("", "Value", dc.Value.Value)
					if st != 0 {
						ev.Values = append(ev.Values, protocol.ResultValue{Label: "ステータス", Value: statusText(st)})
					}
				}
				onEvent(ev)
			}
		}
	}
}

// dialConfig は、接続先の設定から uastack のクライアントの設定を作る（Send の接続と、監視の接続で共通）
func (c *Client) dialConfig() uaclient.Config {
	host, _ := os.Hostname()
	if host == "" {
		host = "localhost"
	}
	cfg := uaclient.Config{
		ApplicationName: "Karakuri",
		ApplicationURI:  "urn:" + host + ":karakuri:client",
		ProductURI:      "urn:karakuri",
		SecurityPolicy:  c.settings.policyURI(),
		SecurityMode:    c.settings.messageSecurityMode(),
		RequestTimeout:  c.timeout,
	}
	if c.settings.SecurityPolicy != policyNone {
		cfg.PKIDir = clientPKIDir()
		cfg.AutoAcceptUntrustedCertificates = true
	}
	if c.settings.UserName != "" {
		cfg.Identity = uaclient.UserName(c.settings.UserName, c.settings.Password)
	}
	return cfg
}
