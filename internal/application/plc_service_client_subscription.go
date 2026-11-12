package application

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"karakuri/internal/domain/protocol"
)

// === クライアントのサブスクリプション（値の変化の通知） ===
//
// ポーリングはホストが周期ごとに読むが、サブスクリプションは機器の側が値の変化を知らせる。
// 対応するプロトコル（protocol.SubscriptionClientFactory）の接続先だけが使える。
//
// 接続先ごとに、監視項目の一覧と通知の間隔を持つ。監視中は 1 つのゴルーチンが
// protocol.SubscribingClient.RunSubscription を動かし、知らせ（値・状態）を項目に入れる。
// - つながらない・切れたときは、プロトコルの実装がつなぎ直す。その間、項目は最後に届いた値を残す
// - 監視中に項目・間隔・接続先の設定を変えたら、監視を作り直す（止めて、始め直す）
// - 監視項目と間隔はプロジェクトに保存する。監視中かどうかは保存しない
//
// 項目の状態は clientManager.mu で守る。止めるときは、ロックの中で取り消しだけして、
// ゴルーチンの終わりはロックの外で待つ（ゴルーチンは知らせを入れるときに clientManager.mu を取るため）。

const (
	defaultSubscriptionIntervalMs = 1000
	minSubscriptionIntervalMs     = 50
	maxSubscriptionIntervalMs     = 3_600_000
	maxSubscriptionItems          = 200

	subscriptionStateConnecting = "connecting" // つなごうとしている（まだ監視できていない）
	subscriptionStateOK         = "ok"         // 監視中
	subscriptionStateError      = "error"      // つながっていない（つなぎ直している）
)

type subscriptionItem struct {
	id      string
	params  map[string]interface{}
	summary string

	status   string // "" (まだ届いていない) / "ok" / "exception" / "error"
	errMsg   string
	at       time.Time              // 最後に知らせが届いた時刻
	values   []ClientResultValueDTO // 最後に届いた値
	valuesAt time.Time
	count    int64 // 値が届いた回数
}

func (it *subscriptionItem) toDTO() ClientSubscriptionItemDTO {
	// values は届くたびに新しいスライスに差し替えるので、そのまま渡してよい（中身は書き換えない）
	return ClientSubscriptionItemDTO{
		ID:       it.id,
		Params:   copyParams(it.params),
		Summary:  it.summary,
		Status:   it.status,
		Error:    it.errMsg,
		At:       formatTime(it.at),
		Values:   it.values,
		ValuesAt: formatTime(it.valuesAt),
		Count:    it.count,
	}
}

// subscriptionFactory は、サブスクリプションに対応するプロトコルならそのファクトリーを返す
func (inst *clientInstance) subscriptionFactory() (protocol.SubscriptionClientFactory, bool) {
	sf, ok := inst.factory.(protocol.SubscriptionClientFactory)
	return sf, ok
}

func (inst *clientInstance) subscriptionDTOLocked() *ClientSubscriptionDTO {
	items := make([]ClientSubscriptionItemDTO, len(inst.subItems))
	for i, it := range inst.subItems {
		items[i] = it.toDTO()
	}
	return &ClientSubscriptionDTO{
		IntervalMs: inst.subIntervalMs,
		Running:    inst.subscriber != nil,
		State:      inst.subState,
		Error:      inst.subError,
		Items:      items,
	}
}

// stopSubscriberLocked は監視を止めるよう伝え、終わりを待つための poller を返す（止まっていれば nil）
func (inst *clientInstance) stopSubscriberLocked() *poller {
	p := inst.subscriber
	inst.subscriber = nil
	if p != nil {
		p.cancel()
		inst.subState = ""
		inst.subError = ""
	}
	return p
}

// startSubscriberLocked は監視を始める（監視中なら何もしない）
func (m *clientManager) startSubscriberLocked(inst *clientInstance) error {
	if inst.subscriber != nil {
		return nil
	}
	sc, ok := inst.client.(protocol.SubscribingClient)
	if _, supported := inst.subscriptionFactory(); !supported || !ok {
		return fmt.Errorf("このプロトコルはサブスクリプションに対応していません")
	}
	if len(inst.subItems) == 0 {
		return fmt.Errorf("監視項目がありません")
	}
	req := protocol.SubscriptionRequest{
		Interval: time.Duration(inst.subIntervalMs) * time.Millisecond,
		Items:    make([]protocol.MonitoredItemRequest, len(inst.subItems)),
	}
	for i, it := range inst.subItems {
		req.Items[i] = protocol.MonitoredItemRequest{ID: it.id, Params: copyParams(it.params)}
		// 始め直すたびに、項目の状態は「まだ届いていない」に戻す（最後に届いた値は残す）
		it.status, it.errMsg = "", ""
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &poller{cancel: cancel, done: make(chan struct{})}
	inst.subscriber = p
	inst.subState = subscriptionStateConnecting
	inst.subError = ""
	go func() {
		defer close(p.done)
		sc.RunSubscription(ctx, req, func(ev protocol.SubscriptionEvent) {
			m.recordSubscriptionEvent(inst, p, ev)
		})
	}()
	return nil
}

// recordSubscriptionEvent は、監視からの知らせを項目に入れる。止めたあと・作り直したあとの古い監視からの知らせは捨てる
func (m *clientManager) recordSubscriptionEvent(inst *clientInstance, p *poller, ev protocol.SubscriptionEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst.subscriber != p {
		return
	}
	if ev.ItemID == "" {
		if ev.Status == protocol.CommandOK {
			inst.subState, inst.subError = subscriptionStateOK, ""
		} else {
			inst.subState, inst.subError = subscriptionStateError, ev.Error
		}
		return
	}
	for _, it := range inst.subItems {
		if it.id != ev.ItemID {
			continue
		}
		now := time.Now()
		it.status = string(ev.Status)
		it.errMsg = ev.Error
		it.at = now
		if ev.Status == protocol.CommandOK {
			it.values = toResultValueDTOs(ev.Values)
			it.valuesAt = now
			it.count++
		}
		return
	}
}

// restartSubscription は、監視中なら監視を作り直す（項目・間隔を変えたあとに呼ぶ）
func (m *clientManager) restartSubscription(id string) {
	m.mu.Lock()
	inst, ok := m.clients[id]
	var p *poller
	if ok {
		p = inst.stopSubscriberLocked()
	}
	m.mu.Unlock()
	if p == nil {
		return
	}
	p.wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	// 待っている間に削除されていなければ、始め直す（項目がなくなっていたら、止めたままにする）
	if cur, ok := m.clients[id]; ok && cur == inst {
		_ = m.startSubscriberLocked(inst)
	}
}

// GetClientSubscription は接続先のサブスクリプションの状態（間隔・監視項目と最後に届いた値）を返す
func (s *PLCService) GetClientSubscription(id string) (*ClientSubscriptionDTO, error) {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		return nil, err
	}
	return inst.subscriptionDTOLocked(), nil
}

// AddClientSubscriptionItems は監視項目を末尾に足す（1 回の入力から、いくつも足されることがある）。
// 監視中なら、監視を作り直す
func (s *PLCService) AddClientSubscriptionItems(id string, params map[string]interface{}) ([]ClientSubscriptionItemDTO, error) {
	m := s.clients
	m.mu.Lock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	items, err := inst.newSubscriptionItems(params)
	if err == nil && len(inst.subItems)+len(items) > maxSubscriptionItems {
		err = fmt.Errorf("監視項目は %d 個までです", maxSubscriptionItems)
	}
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	inst.subItems = append(inst.subItems, items...)
	dtos := make([]ClientSubscriptionItemDTO, len(items))
	for i, it := range items {
		dtos[i] = it.toDTO()
	}
	m.mu.Unlock()
	m.restartSubscription(id)
	return dtos, nil
}

// newSubscriptionItems は入力から監視項目を作る（対応していないプロトコル・使えない入力はエラー）
func (inst *clientInstance) newSubscriptionItems(params map[string]interface{}) ([]*subscriptionItem, error) {
	sf, ok := inst.subscriptionFactory()
	if !ok {
		return nil, fmt.Errorf("このプロトコルはサブスクリプションに対応していません")
	}
	specs, err := sf.ExpandMonitoredItems(params)
	if err != nil {
		return nil, err
	}
	items := make([]*subscriptionItem, len(specs))
	for i, spec := range specs {
		items[i] = &subscriptionItem{id: uuid.NewString(), params: copyParams(spec.Params), summary: spec.Summary}
	}
	return items, nil
}

// RemoveClientSubscriptionItem は監視項目を削除する。監視中なら、監視を作り直す（最後の 1 つを消したら止める）
func (s *PLCService) RemoveClientSubscriptionItem(id, itemID string) error {
	m := s.clients
	m.mu.Lock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	found := false
	for i, it := range inst.subItems {
		if it.id == itemID {
			inst.subItems = append(inst.subItems[:i:i], inst.subItems[i+1:]...)
			found = true
			break
		}
	}
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("監視項目が見つかりません: %s", itemID)
	}
	m.restartSubscription(id)
	return nil
}

// SetClientSubscriptionInterval は通知の間隔（ms）を変える。監視中なら、監視を作り直す
func (s *PLCService) SetClientSubscriptionInterval(id string, intervalMs int) error {
	if intervalMs < minSubscriptionIntervalMs || intervalMs > maxSubscriptionIntervalMs {
		return fmt.Errorf("間隔は %d〜%d ms で指定してください", minSubscriptionIntervalMs, maxSubscriptionIntervalMs)
	}
	m := s.clients
	m.mu.Lock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	changed := inst.subIntervalMs != intervalMs
	inst.subIntervalMs = intervalMs
	m.mu.Unlock()
	if changed {
		m.restartSubscription(id)
	}
	return nil
}

// StartClientSubscription は監視を始める（監視中なら何もしない）
func (s *PLCService) StartClientSubscription(id string) error {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		return err
	}
	return m.startSubscriberLocked(inst)
}

// StopClientSubscription は監視を止め、接続を閉じ終わるまで待つ
func (s *PLCService) StopClientSubscription(id string) error {
	m := s.clients
	m.mu.Lock()
	inst, err := m.instanceLocked(id)
	var p *poller
	if err == nil {
		p = inst.stopSubscriberLocked()
	}
	m.mu.Unlock()
	p.wait()
	return err
}

// subscriptionSnapshotLocked はプロジェクトに保存するサブスクリプションの設定を返す（既定のままなら nil）
func (inst *clientInstance) subscriptionSnapshotLocked() *ClientSubscriptionSnapshotDTO {
	if inst == nil || (len(inst.subItems) == 0 && inst.subIntervalMs == defaultSubscriptionIntervalMs) {
		return nil
	}
	items := make([]ClientSubscriptionItemSnapshotDTO, len(inst.subItems))
	for i, it := range inst.subItems {
		items[i] = ClientSubscriptionItemSnapshotDTO{Params: copyParams(it.params)}
	}
	return &ClientSubscriptionSnapshotDTO{IntervalMs: inst.subIntervalMs, Items: items}
}

// restoreSubscriptionLocked はプロジェクトのサブスクリプションの設定を戻す（使えない項目は読み飛ばす）
func (inst *clientInstance) restoreSubscriptionLocked(snap *ClientSubscriptionSnapshotDTO) {
	if snap == nil {
		return
	}
	if _, ok := inst.subscriptionFactory(); !ok {
		return
	}
	if snap.IntervalMs >= minSubscriptionIntervalMs && snap.IntervalMs <= maxSubscriptionIntervalMs {
		inst.subIntervalMs = snap.IntervalMs
	}
	for _, s := range snap.Items {
		items, err := inst.newSubscriptionItems(s.Params)
		if err != nil || len(inst.subItems)+len(items) > maxSubscriptionItems {
			continue
		}
		inst.subItems = append(inst.subItems, items...)
	}
}
