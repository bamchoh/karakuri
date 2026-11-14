package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
	"karakuri/internal/infrastructure/adapter"
)

// === クライアントの接続先と変数のマッピング ===
//
// 変数は、サーバーのレジスタと同じ形（variable.ProtocolMapping）で接続先のアドレスにマッピングする。
// protocolType は "client:<接続先の ID>"。同期の状態は接続先ごとの adapter.DeviceSync が持つ。
//
// 同期はポーリング中だけ行う（ポーリングのゴルーチンが通信する）:
//   - 周期ごとに、まず書き込み未反映の変数を書き、読み取り項目を読み、マッピングした範囲を読む
//   - 変数が変わって書き込み未反映が増えたら、周期を待たずに書く
//   - 書き込みや読み取りがタイムアウト・エラーになったら、その周期の残りの通信はしない（次の周期にやり直す）

const (
	clientMappingPrefix  = "client:"
	defaultFailThreshold = 3
	minFailThreshold     = 1
	maxFailThreshold     = 100

	// SyncStateStopped は、ポーリングしていない接続先にマッピングした変数の状態（DTO だけで使う）
	syncStateStopped = "stopped"
)

// clientMappingKey は接続先へのマッピングの protocolType
func clientMappingKey(clientID string) string {
	return clientMappingPrefix + clientID
}

// clientIDFromMappingKey はマッピングの protocolType から接続先の ID を取り出す（接続先でなければ false）
func clientIDFromMappingKey(protocolType string) (string, bool) {
	if !strings.HasPrefix(protocolType, clientMappingPrefix) {
		return "", false
	}
	return strings.TrimPrefix(protocolType, clientMappingPrefix), true
}

// detachSync は変数との同期を外す
func (inst *clientInstance) detachSync() {
	if inst.sync != nil {
		inst.sync.Detach()
	}
}

// removeMappingsForKey は、protocolType が key のマッピングをすべての変数から外す
func (s *PLCService) removeMappingsForKey(key string) {
	for id, ms := range s.variableStore.GetAllMappingsForProtocol(key) {
		if len(ms) == 0 {
			continue
		}
		var kept []variable.ProtocolMapping
		for _, m := range s.variableStore.GetMappings(id) {
			if m.ProtocolType != key {
				kept = append(kept, m)
			}
		}
		_ = s.variableStore.SetMappings(id, kept)
	}
}

func toMemoryAreaDTOs(areas []protocol.MemoryArea) []MemoryAreaDTO {
	result := make([]MemoryAreaDTO, len(areas))
	for i, area := range areas {
		result[i] = MemoryAreaDTO{
			ID:             area.ID,
			DisplayName:    area.DisplayName,
			IsBit:          area.IsBit,
			Size:           int(area.Size),
			ReadOnly:       area.ReadOnly,
			ByteAddressing: area.ByteAddressing,
			OneOrigin:      area.OneOrigin,
			ShortName:      area.ShortName,
			BitsOf:         area.BitsOf,
		}
	}
	return result
}

// clientMemoryAreas は接続先のマッピングできるエリアを返す（マッピングできなければ nil）
func (m *clientManager) clientMemoryAreas(clientID string) []MemoryAreaDTO {
	m.mu.Lock()
	inst := m.clients[clientID]
	m.mu.Unlock()
	if inst == nil {
		return nil
	}
	mf, ok := inst.factory.(protocol.MappableClientFactory)
	if !ok {
		return nil
	}
	return toMemoryAreaDTOs(mf.MappingAreas())
}

// GetMappingTargets は変数をマッピングできる先（サーバーと、マッピングできる接続先）を返す
func (s *PLCService) GetMappingTargets() []MappingTargetDTO {
	var result []MappingTargetDTO
	for _, inst := range s.GetServerInstances() {
		result = append(result, MappingTargetDTO{
			ProtocolType:           inst.ProtocolType,
			DisplayName:            inst.DisplayName,
			Label:                  inst.ProtocolType,
			SupportsNodePublishing: inst.SupportsNodePublishing,
		})
	}
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.sortedDTOsLocked() {
		if m.clients[c.ID].sync == nil {
			continue
		}
		result = append(result, MappingTargetDTO{
			ProtocolType: clientMappingKey(c.ID),
			DisplayName:  "接続先: " + c.Name + "（" + c.DisplayName + "）",
			Label:        c.Name,
			IsClient:     true,
		})
	}
	return result
}

// GetVariableDeviceStatuses は、接続先にマッピングした変数の同期の状態を返す
func (s *PLCService) GetVariableDeviceStatuses() []VariableDeviceStatusDTO {
	m := s.clients
	type target struct {
		id, name string
		sync     *adapter.DeviceSync
		running  bool
	}
	m.mu.Lock()
	targets := make([]target, 0, len(m.clients))
	for _, inst := range m.clients {
		if inst.sync != nil {
			targets = append(targets, target{inst.id, inst.name, inst.sync, inst.poller != nil})
		}
	}
	m.mu.Unlock()

	var result []VariableDeviceStatusDTO
	for _, t := range targets {
		for varID, st := range t.sync.Statuses() {
			state := st.State
			if !t.running && state != adapter.SyncStateInvalid {
				state = syncStateStopped
			}
			result = append(result, VariableDeviceStatusDTO{
				VariableID:   varID,
				ClientID:     t.id,
				ClientName:   t.name,
				State:        state,
				WritePending: st.WritePending,
				Error:        st.Error,
				At:           formatTime(st.At),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].VariableID != result[j].VariableID {
			return result[i].VariableID < result[j].VariableID
		}
		return result[i].ClientID < result[j].ClientID
	})
	return result
}

// VariableDeviceStatus は変数の同期の状態を返す（スクリプトの plc.getVariableStatus 用）。
// 接続先にマッピングしていなければ nil。複数の接続先にマッピングしていれば、最初の接続先のもの
func (s *PLCService) VariableDeviceStatus(variableID string) *VariableDeviceStatusDTO {
	for _, st := range s.GetVariableDeviceStatuses() {
		if st.VariableID == variableID {
			st := st
			return &st
		}
	}
	return nil
}

// SetClientFailThreshold は、読み取りが続けて何回失敗したら「通信エラー」にするかを変える
func (s *PLCService) SetClientFailThreshold(id string, n int) error {
	if n < minFailThreshold || n > maxFailThreshold {
		return fmt.Errorf("回数は %d〜%d で指定してください", minFailThreshold, maxFailThreshold)
	}
	m := s.clients
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, err := m.instanceLocked(id)
	if err != nil {
		return err
	}
	inst.failThreshold = n
	return nil
}

func (inst *clientInstance) mappingSummary() *ClientMappingSummaryDTO {
	if inst.sync == nil {
		return nil
	}
	sum := &ClientMappingSummaryDTO{}
	for _, st := range inst.sync.Statuses() {
		sum.Variables++
		switch st.State {
		case adapter.SyncStateOK:
			sum.OK++
		case adapter.SyncStateCommError:
			sum.CommError++
		case adapter.SyncStateInvalid:
			sum.Invalid++
		}
		if st.WritePending {
			sum.WritePending++
		}
	}
	return sum
}

// --- ポーリングのゴルーチンからの通信 ---

func isLinkFailure(st protocol.CommandStatus) bool {
	return st == protocol.CommandTimeout || st == protocol.CommandError
}

// flushWrites は書き込み未反映の変数を機器に書く。タイムアウト・エラーになったら残りは次に回す。
// 止めたら（ctx が取り消されたら）false
func (m *clientManager) flushWrites(ctx context.Context, inst *clientInstance, client protocol.Client) bool {
	if inst.sync == nil {
		return true
	}
	mf := inst.factory.(protocol.MappableClientFactory)
	for _, w := range inst.sync.PendingWrites() {
		max := mf.MaxWriteCount(w.Area)
		n := len(w.Image.Words)
		if w.Image.Bits != nil {
			n = len(w.Image.Bits)
		}
		if max <= 0 {
			inst.sync.WriteFailed(w, "このエリアには書き込めません")
			continue
		}
		failed := ""
		link := false
		for off := 0; off < n && failed == ""; off += max {
			end := min(off+max, n)
			var words []uint16
			var bits []bool
			if w.Image.Bits != nil {
				bits = w.Image.Bits[off:end]
			} else {
				words = w.Image.Words[off:end]
			}
			req, err := mf.BuildWriteRequest(w.Area, w.Address+uint32(off), words, bits)
			if err != nil {
				failed = err.Error()
				break
			}
			res := client.Send(ctx, req)
			if ctx.Err() != nil {
				return false
			}
			if res.Status != protocol.CommandOK {
				failed = res.Error
				link = isLinkFailure(res.Status)
			}
		}
		if failed != "" {
			inst.sync.WriteFailed(w, "書き込み: "+failed)
			if link {
				return true
			}
			continue
		}
		inst.sync.WriteDone(w)
	}
	return true
}

// syncReads はマッピングした範囲を読んで、変数に反映する。止めたら false
func (m *clientManager) syncReads(ctx context.Context, inst *clientInstance, client protocol.Client, threshold int) bool {
	if inst.sync == nil {
		return true
	}
	mf := inst.factory.(protocol.MappableClientFactory)
	linkDown := ""
	for _, b := range inst.sync.PlanReads() {
		if linkDown != "" {
			inst.sync.ReadFailed(b, linkDown, threshold)
			continue
		}
		max := mf.MaxReadCount(b.Area)
		if max <= 0 {
			inst.sync.ReadFailed(b, "このエリアは読めません", threshold)
			continue
		}
		at := time.Now()
		words := make([]uint16, 0, b.Count)
		bits := make([]bool, 0, b.Count)
		failed := ""
		for off := 0; off < b.Count && failed == ""; off += max {
			count := min(max, b.Count-off)
			req, err := mf.BuildReadRequest(b.Area, b.Address+uint32(off), count)
			if err != nil {
				failed = err.Error()
				break
			}
			res := client.Send(ctx, req)
			if ctx.Err() != nil {
				return false
			}
			if res.Status != protocol.CommandOK {
				failed = res.Error
				if isLinkFailure(res.Status) {
					linkDown = res.Error
				}
				break
			}
			got := 0
			for _, v := range res.Values {
				switch {
				case b.IsBit && v.Bit != nil:
					bits = append(bits, *v.Bit)
					got++
				case !b.IsBit && v.Word != nil:
					words = append(words, *v.Word)
					got++
				}
			}
			if got != count {
				failed = fmt.Sprintf("応答の個数が %d ではありません（%d）", count, got)
			}
		}
		if failed != "" {
			inst.sync.ReadFailed(b, failed, threshold)
			continue
		}
		inst.sync.ApplyRead(b, words, bits, at)
	}
	return true
}
