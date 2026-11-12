package application

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
	"karakuri/internal/infrastructure/adapter"
)

// === クライアント（外部の機器へつなぐ側） ===
//
// 接続先（clientInstance）はいくつでも追加できる。通信を待つ間 PLCService.mu を持たないよう、
// クライアントの状態は clientManager が自分のロックで守る。

const maxClientCommandLogs = 200

type clientInstance struct {
	id           string
	name         string
	protocolType protocol.ProtocolType
	factory      protocol.ClientFactory
	settings     map[string]interface{} // NormalizeSettings 済み
	client       protocol.Client        // 設定を変えたら作り直す
	order        int

	// ポーリング（plc_service_client_polling.go）
	pollIntervalMs int
	pollItems      []*pollItem
	pollSeq        int
	poller         *poller // nil なら停止中
	pollCycles     int64
	lastCycle      time.Duration

	// 変数とのマッピング（plc_service_client_sync.go）。マッピングできないプロトコルなら nil
	sync          *adapter.DeviceSync
	failThreshold int           // 読み取りが続けて何回失敗したら「通信エラー」にするか
	wake          chan struct{} // 書き込み未反映が増えたら、ポーリングのゴルーチンを起こす

	// コマンドグループ（plc_service_client_commands.go）
	commandGroups []*commandGroup
}

type clientManager struct {
	mu        sync.Mutex
	varStore  *variable.VariableStore
	factories map[protocol.ProtocolType]protocol.ClientFactory
	clients   map[string]*clientInstance
	seq       int
	logs      []ClientCommandLogDTO // 古い順。最大 maxClientCommandLogs 件
	logSeq    int64
}

func newClientManager(varStore *variable.VariableStore) *clientManager {
	return &clientManager{
		varStore:  varStore,
		factories: make(map[protocol.ProtocolType]protocol.ClientFactory),
		clients:   make(map[string]*clientInstance),
	}
}

// RegisterClientFactory はクライアントのファクトリーを登録する（起動時に、組み込むプロトコルの分だけ呼ぶ）
func (s *PLCService) RegisterClientFactory(factory protocol.ClientFactory) {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factories[factory.ProtocolType()] = factory
}

// toFieldDTOs はプロトコルの設定項目の定義を DTO にする
func toFieldDTOs(fields []protocol.ConfigField) []FieldDTO {
	dtos := make([]FieldDTO, len(fields))
	for j, f := range fields {
		dtos[j] = FieldDTO{
			Name:        f.Name,
			Label:       f.Label,
			Description: f.Description,
			Type:        f.Type,
			Required:    f.Required,
			Default:     f.Default,
			Min:         f.Min,
			Max:         f.Max,
			Category:    f.Category,
		}
		if f.Options != nil {
			dtos[j].Options = make([]OptionDTO, len(f.Options))
			for k, o := range f.Options {
				dtos[j].Options[k] = OptionDTO{Value: o.Value, Label: o.Label}
			}
		}
		if f.Condition != nil {
			dtos[j].ShowWhen = &ConditionDTO{Field: f.Condition.Field, Value: f.Condition.Value}
		}
	}
	return dtos
}

// GetClientProtocols はクライアントとして使えるプロトコル（接続先の設定項目・コマンドの定義）の一覧を返す
func (s *PLCService) GetClientProtocols() []ClientProtocolDTO {
	m := s.clients
	m.mu.Lock()
	factories := make([]protocol.ClientFactory, 0, len(m.factories))
	for _, f := range m.factories {
		factories = append(factories, f)
	}
	m.mu.Unlock()

	sort.Slice(factories, func(i, j int) bool { return factories[i].ProtocolType() < factories[j].ProtocolType() })
	result := make([]ClientProtocolDTO, len(factories))
	for i, f := range factories {
		cmds := f.Commands()
		cmdDTOs := make([]ClientCommandDTO, len(cmds))
		for j, c := range cmds {
			cmdDTOs[j] = ClientCommandDTO{ID: c.ID, DisplayName: c.DisplayName, Fields: toFieldDTOs(c.Fields), Pollable: c.Pollable}
		}
		result[i] = ClientProtocolDTO{
			ProtocolType:     string(f.ProtocolType()),
			DisplayName:      f.DisplayName(),
			ConnectionFields: toFieldDTOs(f.ConnectionFields()),
			Commands:         cmdDTOs,
		}
	}
	return result
}

func (inst *clientInstance) toDTO() ClientDTO {
	settings := make(map[string]interface{}, len(inst.settings))
	for k, v := range inst.settings {
		settings[k] = v
	}
	return ClientDTO{
		ID:           inst.id,
		Name:         inst.name,
		ProtocolType: string(inst.protocolType),
		DisplayName:  inst.factory.DisplayName(),
		Settings:     settings,
		Connected:    inst.client != nil && inst.client.Connected(),
		Polling:      inst.poller != nil,
	}
}

// GetClients は接続先の一覧を、追加した順に返す
func (s *PLCService) GetClients() []ClientDTO {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sortedDTOsLocked()
}

func (m *clientManager) sortedDTOsLocked() []ClientDTO {
	insts := make([]*clientInstance, 0, len(m.clients))
	for _, inst := range m.clients {
		insts = append(insts, inst)
	}
	sort.Slice(insts, func(i, j int) bool { return insts[i].order < insts[j].order })
	result := make([]ClientDTO, len(insts))
	for i, inst := range insts {
		result[i] = inst.toDTO()
	}
	return result
}

// AddClient は接続先を追加する。settings が nil なら既定値にする
func (s *PLCService) AddClient(protocolType, name string, settings map[string]interface{}) (*ClientDTO, error) {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addLocked("", protocolType, name, settings)
}

// addLocked は接続先を追加する。id が空なら新しい ID を作る（プロジェクトの読み込みでは、保存した ID を使う。
// 変数のマッピングは ID で接続先を指すため）
func (m *clientManager) addLocked(id, protocolType, name string, settings map[string]interface{}) (*ClientDTO, error) {
	factory, ok := m.factories[protocol.ProtocolType(protocolType)]
	if !ok {
		return nil, fmt.Errorf("クライアントとして使えないプロトコルです: %s", protocolType)
	}
	normalized, err := factory.NormalizeSettings(settings)
	if err != nil {
		return nil, err
	}
	client, err := factory.NewClient(normalized)
	if err != nil {
		return nil, err
	}
	m.seq++
	name = strings.TrimSpace(name)
	if name == "" {
		name = m.unusedDefaultNameLocked()
	}
	if _, used := m.clients[id]; id == "" || used {
		id = uuid.NewString()
	}
	inst := &clientInstance{
		id:           id,
		name:         name,
		protocolType: factory.ProtocolType(),
		factory:      factory,
		settings:     normalized,
		client:       client,
		order:        m.seq,

		pollIntervalMs: defaultPollIntervalMs,
		failThreshold:  defaultFailThreshold,
		wake:           make(chan struct{}, 1),
	}
	if mf, ok := factory.(protocol.MappableClientFactory); ok {
		inst.sync = adapter.NewDeviceSync(m.varStore, clientMappingKey(inst.id), mf.MappingAreas(), inst.wakePoller)
	}
	m.clients[inst.id] = inst
	dto := inst.toDTO()
	return &dto, nil
}

// unusedDefaultNameLocked は、まだ使われていない「接続先 N」のうち N が最も小さい名前を返す
func (m *clientManager) unusedDefaultNameLocked() string {
	used := make(map[string]bool, len(m.clients))
	for _, inst := range m.clients {
		used[inst.name] = true
	}
	for n := 1; ; n++ {
		if name := fmt.Sprintf("接続先 %d", n); !used[name] {
			return name
		}
	}
}

// UpdateClient は接続先の名前と設定を変える。設定を変えると接続を閉じる（次の送信でつなぎ直す）
func (s *PLCService) UpdateClient(id, name string, settings map[string]interface{}) error {
	old, err := s.clients.update(id, name, settings)
	if old != nil {
		// 送信中なら、終わるまで待ってから閉じる（ロックの外で待つ）
		_ = old.Close()
	}
	return err
}

func (m *clientManager) update(id, name string, settings map[string]interface{}) (protocol.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.clients[id]
	if !ok {
		return nil, fmt.Errorf("接続先が見つかりません: %s", id)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("名前を入力してください")
	}
	normalized, err := inst.factory.NormalizeSettings(settings)
	if err != nil {
		return nil, err
	}
	client, err := inst.factory.NewClient(normalized)
	if err != nil {
		return nil, err
	}
	old := inst.client
	inst.name = name
	inst.settings = normalized
	inst.client = client
	return old, nil
}

// RemoveClient は接続先を削除する（接続を閉じる）
func (s *PLCService) RemoveClient(id string) error {
	m := s.clients
	m.mu.Lock()
	inst, ok := m.clients[id]
	var p *poller
	if ok {
		delete(m.clients, id)
		p = inst.stopPollerLocked()
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("接続先が見つかりません: %s", id)
	}
	p.wait()
	inst.detachSync()
	if inst.client != nil {
		_ = inst.client.Close()
	}
	// この接続先へのマッピングを、変数から外す
	s.removeMappingsForKey(clientMappingKey(id))
	go s.emitVariablesChanged()
	return nil
}

// DisconnectClient は接続先との接続を閉じる（次の送信でつなぎ直す）
func (s *PLCService) DisconnectClient(id string) error {
	m := s.clients
	m.mu.Lock()
	inst, ok := m.clients[id]
	var client protocol.Client
	if ok {
		client = inst.client
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("接続先が見つかりません: %s", id)
	}
	if client != nil {
		return client.Close()
	}
	return nil
}

// BuildClientRequest は、コマンドと項目の値から送るリクエストを作り、16 進の文字列（"03 00 00 00 0A"）で返す
func (s *PLCService) BuildClientRequest(protocolType, commandID string, params map[string]interface{}) (string, error) {
	m := s.clients
	m.mu.Lock()
	factory, ok := m.factories[protocol.ProtocolType(protocolType)]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("クライアントとして使えないプロトコルです: %s", protocolType)
	}
	req, err := factory.BuildRequest(commandID, params)
	if err != nil {
		return "", err
	}
	return formatHex(req), nil
}

// SendClientRequest は、16 進の文字列で指定したリクエストを接続先に送り、結果を返す（履歴にも残す）。
// 通信の失敗（タイムアウトなど）は結果の status で返す。error を返すのは、送る前に分かる誤り（接続先がない・16 進が読めない）だけ
func (s *PLCService) SendClientRequest(id, requestHex string) (*ClientCommandLogDTO, error) {
	req, err := parseHex(requestHex)
	if err != nil {
		return nil, err
	}
	if len(req) == 0 {
		return nil, fmt.Errorf("リクエストを入力してください")
	}
	return s.sendAndLog(id, req, "")
}

// sendAndLog はリクエストを接続先に送り、結果を履歴に残す。label は履歴に出す名前（保存したコマンドなら「グループ名 / コマンド名」）
func (s *PLCService) sendAndLog(id string, req []byte, label string) (*ClientCommandLogDTO, error) {
	m := s.clients
	m.mu.Lock()
	inst, ok := m.clients[id]
	var client protocol.Client
	var name string
	if ok {
		client = inst.client
		name = inst.name
	}
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("接続先が見つかりません: %s", id)
	}

	at := time.Now()
	result := client.Send(context.Background(), req)

	values := toResultValueDTOs(result.Values)
	entry := ClientCommandLogDTO{
		ClientID:   id,
		ClientName: name,
		At:         at.Format(time.RFC3339Nano),
		Request:    formatHex(req),
		Sent:       formatHex(result.Sent),
		Received:   formatHex(result.Received),
		ElapsedMs:  float64(result.Elapsed.Microseconds()) / 1000,
		Status:     string(result.Status),
		Error:      result.Error,
		Summary:    result.Summary,
		Values:     values,
		Label:      label,
	}

	m.mu.Lock()
	m.logSeq++
	entry.Seq = m.logSeq
	m.logs = append(m.logs, entry)
	if over := len(m.logs) - maxClientCommandLogs; over > 0 {
		m.logs = append([]ClientCommandLogDTO(nil), m.logs[over:]...)
	}
	m.mu.Unlock()

	return &entry, nil
}

// GetClientCommandLogs は送ったコマンドの履歴を、新しい順に返す
func (s *PLCService) GetClientCommandLogs() []ClientCommandLogDTO {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]ClientCommandLogDTO, len(m.logs))
	for i, e := range m.logs {
		result[len(m.logs)-1-i] = e
	}
	return result
}

// ClearClientCommandLogs はコマンドの履歴を消す
func (s *PLCService) ClearClientCommandLogs() {
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = nil
}

// exportClients はプロジェクトに保存する接続先の一覧を返す
func (m *clientManager) export() []ClientSnapshotDTO {
	m.mu.Lock()
	defer m.mu.Unlock()
	dtos := m.sortedDTOsLocked()
	if len(dtos) == 0 {
		return nil
	}
	result := make([]ClientSnapshotDTO, len(dtos))
	for i, d := range dtos {
		result[i] = ClientSnapshotDTO{
			ID:           d.ID,
			Name:         d.Name,
			ProtocolType: d.ProtocolType,
			Settings:     d.Settings,
			Polling:      m.clients[d.ID].pollingSnapshotLocked(),

			CommandGroups: m.clients[d.ID].commandGroupsSnapshotLocked(),
		}
	}
	return result
}

// replaceAll は接続先をすべて閉じて、snapshots の内容に置き換える（プロジェクトの読み込み）。
// 使えないプロトコル・設定の接続先・読み取り項目は読み飛ばす。ポーリングは停止した状態で始める
//
// 古い接続先のポーリングが止まり、変数との同期を外してから、新しい接続先を作る
// （同じ ID の接続先の同期が、同時に 2 つ動かないようにするため）
func (m *clientManager) replaceAll(snapshots []ClientSnapshotDTO) {
	m.mu.Lock()
	old := m.clients
	pollers := make([]*poller, 0, len(old))
	for _, inst := range old {
		pollers = append(pollers, inst.stopPollerLocked())
	}
	m.clients = make(map[string]*clientInstance)
	m.mu.Unlock()

	for _, p := range pollers {
		p.wait()
	}
	closeClients(old) // old はもう m から外したので、ロックの外で読んでよい

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, snap := range snapshots {
		dto, err := m.addLocked(snap.ID, snap.ProtocolType, snap.Name, snap.Settings)
		if err != nil {
			continue
		}
		m.clients[dto.ID].restorePollingLocked(snap.Polling)
		m.clients[dto.ID].restoreCommandGroupsLocked(snap.CommandGroups)
	}
}

// closeAll はポーリングを止め、変数との同期を外し、接続を閉じる（アプリの終了）
func (m *clientManager) closeAll() {
	m.mu.Lock()
	old := m.clients
	pollers := make([]*poller, 0, len(old))
	for _, inst := range old {
		pollers = append(pollers, inst.stopPollerLocked())
	}
	m.clients = make(map[string]*clientInstance)
	m.mu.Unlock()
	for _, p := range pollers {
		p.wait()
	}
	closeClients(old)
}

// closeClients は変数との同期を外し、接続を閉じる（送信中なら終わるまで待つので、clientManager.mu の外で呼ぶ）
func closeClients(insts map[string]*clientInstance) {
	for _, inst := range insts {
		inst.detachSync()
		if inst.client != nil {
			_ = inst.client.Close()
		}
	}
}

// formatHex はバイト列を "03 00 0A" の形にする
func formatHex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, x := range b {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%02X", x)
	}
	return sb.String()
}

// parseHex は 16 進の文字列を読む。空白・カンマ・コロン・"0x" の前置きは無視する（"03 00 00 00 0A"、"0300 0000 0a"、"0x03,0x00" など）
func parseHex(s string) ([]byte, error) {
	var sb strings.Builder
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == ':' || r == '\t' || r == '\n' || r == '\r' || r == '-'
	}) {
		if strings.HasPrefix(tok, "0x") || strings.HasPrefix(tok, "0X") {
			tok = tok[2:]
			if len(tok) == 1 {
				tok = "0" + tok
			}
		}
		sb.WriteString(tok)
	}
	digits := sb.String()
	if len(digits)%2 != 0 {
		return nil, fmt.Errorf("16 進の桁数が奇数です（2 桁で 1 バイト）")
	}
	b, err := hex.DecodeString(digits)
	if err != nil {
		return nil, fmt.Errorf("16 進として読めない文字があります")
	}
	return b, nil
}
