package application

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"karakuri/internal/domain/protocol"
)

// === クライアントのポーリング（周期的な読み取り） ===
//
// 接続先ごとに、読み取り項目の一覧と周期を持つ。ポーリング中は 1 つのゴルーチン（poller）が、
// 周期ごとに項目を上から順に送り、結果を項目に入れる。
// - 読めなかった項目は、最後に読めた値を残す（状態は status で示す）
// - ある項目がタイムアウト・エラーになったら、その周期の残りの項目は送らない（status = "skipped"）。
//   接続できない機器に、項目の数だけタイムアウトを待たないため
// - 1 周が周期より長くかかったら、待たずに次の周を始める
// - ポーリングの送受信は、コマンドの履歴に入れない
//
// 項目の結果は clientManager.mu で守る。poller は送受信の間ロックを持たない。

const (
	defaultPollIntervalMs = 1000
	minPollIntervalMs     = 50
	maxPollIntervalMs     = 3_600_000
	maxPollItems          = 50

	pollStatusSkipped = "skipped"
)

type pollItem struct {
	id        string
	commandID string
	params    map[string]interface{}
	format    string
	request   []byte
	summary   string

	status     string
	errMsg     string
	at         time.Time
	elapsed    time.Duration
	values     []ClientResultValueDTO // 最後に読めた値
	valuesAt   time.Time
	okCount    int64
	errorCount int64
}

type poller struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// wait はポーリングのゴルーチンが終わるのを待つ（nil なら何もしない）。clientManager.mu の外で呼ぶ
func (p *poller) wait() {
	if p != nil {
		<-p.done
	}
}

// stopPollerLocked はポーリングを止めるよう伝え、終わりを待つための poller を返す（止まっていれば nil）
func (inst *clientInstance) stopPollerLocked() *poller {
	p := inst.poller
	inst.poller = nil
	if p != nil {
		p.cancel()
	}
	return p
}

func copyParams(params map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(params))
	for k, v := range params {
		cp[k] = v
	}
	return cp
}

func toResultValueDTOs(values []protocol.ResultValue) []ClientResultValueDTO {
	dtos := make([]ClientResultValueDTO, len(values))
	for i, v := range values {
		dtos[i] = ClientResultValueDTO{Label: v.Label, Value: v.Value}
		if v.Bit != nil {
			b := *v.Bit
			dtos[i].Bit = &b
		}
		if v.Word != nil {
			w := int(*v.Word)
			dtos[i].Word = &w
		}
	}
	return dtos
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

func (it *pollItem) toDTO() ClientPollItemDTO {
	// values は記録のたびに新しいスライスに差し替えるので、そのまま渡してよい（中身は書き換えない）
	return ClientPollItemDTO{
		ID:         it.id,
		CommandID:  it.commandID,
		Params:     copyParams(it.params),
		Format:     it.format,
		Request:    formatHex(it.request),
		Summary:    it.summary,
		Status:     it.status,
		Error:      it.errMsg,
		At:         formatTime(it.at),
		ElapsedMs:  float64(it.elapsed.Microseconds()) / 1000,
		Values:     it.values,
		ValuesAt:   formatTime(it.valuesAt),
		OKCount:    it.okCount,
		ErrorCount: it.errorCount,
	}
}

func (inst *clientInstance) pollingDTOLocked() *ClientPollingDTO {
	items := make([]ClientPollItemDTO, len(inst.pollItems))
	for i, it := range inst.pollItems {
		items[i] = it.toDTO()
	}
	return &ClientPollingDTO{
		IntervalMs:  inst.pollIntervalMs,
		Running:     inst.poller != nil,
		Cycles:      inst.pollCycles,
		LastCycleMs: float64(inst.lastCycle.Microseconds()) / 1000,
		Items:       items,

		FailThreshold: inst.failThreshold,
		Mapping:       inst.mappingSummary(),
	}
}

// newPollItem は読み取り項目を作る（ポーリングに使えないコマンド・組み立てられない項目はエラー）
func (inst *clientInstance) newPollItem(commandID string, params map[string]interface{}, format string) (*pollItem, error) {
	pollable := false
	for _, c := range inst.factory.Commands() {
		if c.ID == commandID {
			pollable = c.Pollable
			break
		}
	}
	if !pollable {
		return nil, fmt.Errorf("このコマンドはポーリングに使えません: %s", commandID)
	}
	req, err := inst.factory.BuildRequest(commandID, params)
	if err != nil {
		return nil, err
	}
	return &pollItem{
		commandID: commandID,
		params:    copyParams(params),
		format:    format,
		request:   req,
		summary:   inst.factory.DescribeRequest(req),
	}, nil
}

func (m *clientManager) instanceLocked(id string) (*clientInstance, error) {
	inst, ok := m.clients[id]
	if !ok {
		return nil, fmt.Errorf("接続先が見つかりません: %s", id)
	}
	return inst, nil
}

// GetClientPolling は接続先のポーリングの状態（周期・読み取り項目と最後の結果）を返す
func (s *PLCService) GetClientPolling(id string) (*ClientPollingDTO, error) {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		return nil, err
	}
	return inst.pollingDTOLocked(), nil
}

// AddClientPollItem は読み取り項目を末尾に足す。ポーリング中なら、次の周から読む
func (s *PLCService) AddClientPollItem(id, commandID string, params map[string]interface{}, format string) (*ClientPollItemDTO, error) {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		return nil, err
	}
	if len(inst.pollItems) >= maxPollItems {
		return nil, fmt.Errorf("読み取り項目は %d 個までです", maxPollItems)
	}
	it, err := inst.newPollItem(commandID, params, format)
	if err != nil {
		return nil, err
	}
	it.id = uuid.NewString()
	inst.pollItems = append(inst.pollItems, it)
	dto := it.toDTO()
	return &dto, nil
}

// UpdateClientPollItem は読み取り項目を変える。リクエストが変わったら、それまでの結果は消す
func (s *PLCService) UpdateClientPollItem(id, itemID, commandID string, params map[string]interface{}, format string) error {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		return err
	}
	for i, old := range inst.pollItems {
		if old.id != itemID {
			continue
		}
		it, err := inst.newPollItem(commandID, params, format)
		if err != nil {
			return err
		}
		if formatHex(it.request) == formatHex(old.request) {
			// 表示形式だけの変更なら、結果は残す
			old.commandID, old.params, old.format = it.commandID, it.params, it.format
			return nil
		}
		it.id = old.id
		inst.pollItems[i] = it
		return nil
	}
	return fmt.Errorf("読み取り項目が見つかりません: %s", itemID)
}

// RemoveClientPollItem は読み取り項目を削除する
func (s *PLCService) RemoveClientPollItem(id, itemID string) error {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		return err
	}
	for i, it := range inst.pollItems {
		if it.id == itemID {
			inst.pollItems = append(inst.pollItems[:i:i], inst.pollItems[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("読み取り項目が見つかりません: %s", itemID)
}

// SetClientPollInterval はポーリングの周期（ms）を変える。ポーリング中なら、次の待ちから使う
func (s *PLCService) SetClientPollInterval(id string, intervalMs int) error {
	if intervalMs < minPollIntervalMs || intervalMs > maxPollIntervalMs {
		return fmt.Errorf("周期は %d〜%d ms で指定してください", minPollIntervalMs, maxPollIntervalMs)
	}
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		return err
	}
	inst.pollIntervalMs = intervalMs
	inst.wakePoller() // 長い周期を待っている最中でも、新しい周期で待ち直す
	return nil
}

// wakePoller はポーリングのゴルーチンを起こす（書き込み未反映を書き、周期を読み直す）
func (inst *clientInstance) wakePoller() {
	select {
	case inst.wake <- struct{}{}:
	default:
	}
}

// StartClientPolling はポーリングを始める（ポーリング中なら何もしない）
func (s *PLCService) StartClientPolling(id string) error {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		return err
	}
	if inst.poller != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &poller{cancel: cancel, done: make(chan struct{})}
	inst.poller = p
	inst.pollCycles = 0
	inst.pollFailing = false
	go m.runPoller(ctx, inst, p)
	m.log.Infof(clientLogSource(inst.name), "ポーリングを開始しました（周期 %d ms）", inst.pollIntervalMs)
	return nil
}

// StopClientPolling はポーリングを止め、送信中のリクエストが終わるまで待つ
func (s *PLCService) StopClientPolling(id string) error {
	m := s.clients
	m.mu.Lock()
	inst, err := m.instanceLocked(id)
	var p *poller
	var name string
	if err == nil {
		p = inst.stopPollerLocked()
		name = inst.name
	}
	m.mu.Unlock()
	p.wait()
	if p != nil {
		m.log.Infof(clientLogSource(name), "ポーリングを停止しました")
	}
	return err
}

type pollTarget struct {
	id      string
	request []byte
}

// runPoller はポーリングのゴルーチン。周期ごとに、書き込み未反映の変数を書き、読み取り項目を読み、
// マッピングした範囲を読む。周期を待つ間に書き込み未反映が増えたら（inst.wake）、すぐに書く。
// inst.factory / inst.sync / inst.wake は作ったあとに変わらないので、ロックなしで読んでよい
func (m *clientManager) runPoller(ctx context.Context, inst *clientInstance, p *poller) {
	defer close(p.done)
	currentClient := func() protocol.Client {
		m.mu.Lock()
		defer m.mu.Unlock()
		return inst.client
	}
	for {
		start := time.Now()

		m.mu.Lock()
		client := inst.client
		threshold := inst.failThreshold
		targets := make([]pollTarget, len(inst.pollItems))
		for i, it := range inst.pollItems {
			targets[i] = pollTarget{id: it.id, request: it.request}
		}
		m.mu.Unlock()

		if !m.flushWrites(ctx, inst, client) {
			return
		}
		m.pollOnce(ctx, inst, client, targets)
		if ctx.Err() != nil {
			return
		}
		if !m.syncReads(ctx, inst, client, threshold) {
			return
		}

		m.mu.Lock()
		inst.pollCycles++
		inst.lastCycle = time.Since(start)
		m.mu.Unlock()

	wait:
		for {
			// 周期は待つたびに読み直す（待っている間に変えられても、新しい周期で待つ）
			m.mu.Lock()
			interval := time.Duration(inst.pollIntervalMs) * time.Millisecond
			m.mu.Unlock()
			d := time.Until(start.Add(interval))
			if d <= 0 {
				break
			}
			timer := time.NewTimer(d)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				break wait
			case <-inst.wake:
				timer.Stop()
				if !m.flushWrites(ctx, inst, currentClient()) {
					return
				}
			}
		}
	}
}

// pollOnce は読み取り項目を 1 周送る
func (m *clientManager) pollOnce(ctx context.Context, inst *clientInstance, client protocol.Client, targets []pollTarget) {
	var failed string // この周で失敗した項目の理由（空なら失敗していない）
	for _, t := range targets {
		if ctx.Err() != nil {
			return
		}
		if failed != "" {
			m.recordPoll(inst, t.id, func(it *pollItem) {
				it.status = pollStatusSkipped
				it.errMsg = "前の項目が失敗したので、この周期は送っていません（" + failed + "）"
				it.at = time.Now()
				it.elapsed = 0
				it.errorCount++
			})
			continue
		}
		at := time.Now()
		res := client.Send(ctx, t.request)
		if ctx.Err() != nil {
			// 止めたために失敗したものは記録しない
			return
		}
		m.recordPoll(inst, t.id, func(it *pollItem) {
			it.status = string(res.Status)
			it.errMsg = res.Error
			it.at = at
			it.elapsed = res.Elapsed
			if res.Status == protocol.CommandOK {
				it.values = toResultValueDTOs(res.Values)
				it.valuesAt = at
				it.okCount++
			} else {
				it.errorCount++
			}
		})
		if res.Status == protocol.CommandTimeout || res.Status == protocol.CommandError {
			failed = res.Error
		}
	}
	if len(targets) > 0 {
		m.logPollState(inst, failed)
	}
}

// logPollState は、ポーリングの通信が失敗し始めたときと、戻ったときに、ログを出す（毎周は出さない）
func (m *clientManager) logPollState(inst *clientInstance, failed string) {
	m.mu.Lock()
	was := inst.pollFailing
	inst.pollFailing = failed != ""
	name := inst.name
	m.mu.Unlock()
	switch {
	case failed != "" && !was:
		m.log.Errorf(clientLogSource(name), "ポーリングで通信エラーが起きました: %s", failed)
	case failed == "" && was:
		m.log.Infof(clientLogSource(name), "ポーリングの通信が戻りました")
	}
}

// recordPoll は、項目がまだあれば（送っている間に削除・変更されていなければ）結果を入れる
func (m *clientManager) recordPoll(inst *clientInstance, itemID string, update func(it *pollItem)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range inst.pollItems {
		if it.id == itemID {
			update(it)
			return
		}
	}
}

// pollingSnapshotLocked はプロジェクトに保存するポーリングの設定を返す（既定のままなら nil）
func (inst *clientInstance) pollingSnapshotLocked() *ClientPollingSnapshotDTO {
	if inst == nil || (len(inst.pollItems) == 0 && inst.pollIntervalMs == defaultPollIntervalMs && inst.failThreshold == defaultFailThreshold) {
		return nil
	}
	items := make([]ClientPollItemSnapshotDTO, len(inst.pollItems))
	for i, it := range inst.pollItems {
		items[i] = ClientPollItemSnapshotDTO{CommandID: it.commandID, Params: copyParams(it.params), Format: it.format}
	}
	return &ClientPollingSnapshotDTO{IntervalMs: inst.pollIntervalMs, FailThreshold: inst.failThreshold, Items: items}
}

// restorePollingLocked はプロジェクトのポーリングの設定を戻す（使えない項目は読み飛ばす）
func (inst *clientInstance) restorePollingLocked(snap *ClientPollingSnapshotDTO) {
	if snap == nil {
		return
	}
	if snap.IntervalMs >= minPollIntervalMs && snap.IntervalMs <= maxPollIntervalMs {
		inst.pollIntervalMs = snap.IntervalMs
	}
	if snap.FailThreshold >= minFailThreshold && snap.FailThreshold <= maxFailThreshold {
		inst.failThreshold = snap.FailThreshold
	}
	for _, s := range snap.Items {
		if len(inst.pollItems) >= maxPollItems {
			break
		}
		it, err := inst.newPollItem(s.CommandID, s.Params, s.Format)
		if err != nil {
			continue
		}
		it.id = uuid.NewString()
		inst.pollItems = append(inst.pollItems, it)
	}
}
