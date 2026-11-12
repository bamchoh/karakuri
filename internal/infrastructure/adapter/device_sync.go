package adapter

import (
	"slices"
	"sort"
	"sync"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
)

// DeviceSync は、変数と外部の機器（クライアントの接続先）のアドレスを同期するための状態を持つ。
// 通信そのものはしない。何を読むか（PlanReads）・何を書くか（PendingWrites）を返し、
// 通信の結果（ApplyRead / ReadFailed / WriteDone / WriteFailed）を受け取って変数と状態に反映する。
//
// マッピングは variable.ProtocolMapping で、ProtocolType がこの DeviceSync のキー（"client:<接続先の ID>"）のもの。
//
// 決まり:
//   - 機器が先: マッピングしてから一度も機器から読めていない変数は、機器に書かない（変数の初期値で機器を上書きしないため）
//   - 読めた変数の値が変わると（画面・スクリプト・サーバーのクライアントなど、この同期以外から）、「書き込み未反映」にする。
//     書き込みが反映されるまでは、読み取った値で変数を上書きしない
//   - 読み取りが続けて失敗した回数が threshold 以上になったら「通信エラー」にする。値は最後に読めたまま残す
//   - 機器の側で書き込めないエリア（MemoryArea.ReadOnly）は読むだけ。変数を変えても、次の読み取りで機器の値に戻す
//   - ビットのエリアには BOOL の変数だけをマッピングできる
//
// OnVariableChanged は変数ストアから（どのゴルーチンからでも）呼ばれる。ほかのメソッドはポーリングのゴルーチンから呼ぶ。
// 変数ストアの更新（UpdateValueFrom）は d.mu の外で行う。
type DeviceSync struct {
	varStore  *variable.VariableStore
	key       string
	areas     map[string]protocol.MemoryArea
	onPending func() // 書き込み未反映が増えたときに呼ぶ（すぐに送るため）

	mu      sync.Mutex
	entries map[string]*syncEntry // 変数 ID → 状態
}

// 変数の同期の状態
const (
	SyncStateUnknown   = ""           // まだ読めていない
	SyncStateOK        = "ok"         // 最後の読み取りが成功
	SyncStateCommError = "comm_error" // 読み取りが続けて失敗した
	SyncStateInvalid   = "invalid"    // マッピングが使えない（ビットのエリアに BOOL 以外など）
)

// DeviceImage はマッピング先の内容（ワードのエリアなら Words、ビットのエリアなら Bits）
type DeviceImage struct {
	Words []uint16
	Bits  []bool
}

func (a DeviceImage) equal(b DeviceImage) bool {
	return slices.Equal(a.Words, b.Words) && slices.Equal(a.Bits, b.Bits)
}

// ReadBlock は 1 つにまとめて読む範囲（MaxReadCount より大きければ、呼ぶ側が分けて読む）
type ReadBlock struct {
	Area    string
	Address uint32
	Count   int
	IsBit   bool
}

// PendingWrite は機器に書く内容
type PendingWrite struct {
	VariableID string
	Area       string
	Address    uint32
	Image      DeviceImage
}

// VariableSyncStatus は変数 1 つの同期の状態
type VariableSyncStatus struct {
	State        string
	WritePending bool
	Error        string    // 最後の失敗の理由（読み取りまたは書き込み）
	At           time.Time // 最後に読めた時刻
}

type syncEntry struct {
	// マッピングと型（変わったら状態を作り直す）
	area       string
	address    uint32
	endianness string
	dataType   variable.DataType
	count      int // ワード数（ビットのエリアなら 1）

	synced       bool // 機器から一度でも読めたか
	last         DeviceImage
	pending      *DeviceImage
	writeErr     string
	readFailures int
	state        string
	readErr      string
	at           time.Time
}

// NewDeviceSync は DeviceSync を作り、変数ストアの通知先に登録する
func NewDeviceSync(varStore *variable.VariableStore, key string, areas []protocol.MemoryArea, onPending func()) *DeviceSync {
	d := &DeviceSync{
		varStore:  varStore,
		key:       key,
		areas:     make(map[string]protocol.MemoryArea, len(areas)),
		onPending: onPending,
		entries:   make(map[string]*syncEntry),
	}
	for _, a := range areas {
		d.areas[a.ID] = a
	}
	varStore.AddListener(d)
	return d
}

// Key はマッピングの ProtocolType に使うキーを返す
func (d *DeviceSync) Key() string { return d.key }

// Detach は変数ストアの通知先から外す（接続先を削除するときに呼ぶ）
func (d *DeviceSync) Detach() {
	d.varStore.RemoveListener(d)
}

func (d *DeviceSync) mappingOf(mappings []variable.ProtocolMapping) *variable.ProtocolMapping {
	for i := range mappings {
		if mappings[i].ProtocolType == d.key {
			return &mappings[i]
		}
	}
	return nil
}

// entryLocked は変数のマッピングに合った状態を返す（マッピングか型が変わっていたら作り直す）
func (d *DeviceSync) entryLocked(v *variable.Variable, m *variable.ProtocolMapping) *syncEntry {
	area, ok := d.areas[m.MemoryArea]
	count := 1
	if ok && !area.IsBit {
		count = v.DataType.WordCountWithResolver(d.varStore.TypeResolver())
	}
	e := d.entries[v.ID]
	if e == nil || e.area != m.MemoryArea || e.address != m.Address || e.endianness != m.Endianness || e.dataType != v.DataType || e.count != count {
		e = &syncEntry{area: m.MemoryArea, address: m.Address, endianness: m.Endianness, dataType: v.DataType, count: count}
		switch {
		case !ok:
			e.state, e.readErr = SyncStateInvalid, "接続先にないエリアです: "+m.MemoryArea
		case area.IsBit && !v.DataType.IsBitType():
			e.state, e.readErr = SyncStateInvalid, "ビットのエリアには BOOL の変数だけを割り当てられます"
		case count < 1:
			e.state, e.readErr = SyncStateInvalid, "変数の大きさが分かりません"
		case area.Size > 0 && uint64(m.Address)+uint64(count) > uint64(area.Size):
			e.state, e.readErr = SyncStateInvalid, "エリアの範囲を超えています"
		}
		d.entries[v.ID] = e
	}
	return e
}

// imageOf は変数の値を、マッピング先に書く内容にする
func (d *DeviceSync) imageOf(v *variable.Variable, e *syncEntry) (DeviceImage, bool) {
	if d.areas[e.area].IsBit {
		return DeviceImage{Bits: []bool{variable.ValueToBool(v.Value, v.DataType)}}, true
	}
	words, ok := encodeVariableWords(d.varStore, v, e.endianness)
	if !ok || len(words) != e.count {
		return DeviceImage{}, false
	}
	return DeviceImage{Words: words}, true
}

// OnVariableChanged は変数が変わったときに呼ばれる（この同期が読み取りで更新したときは呼ばれない）
func (d *DeviceSync) OnVariableChanged(v *variable.Variable, mappings []variable.ProtocolMapping) {
	m := d.mappingOf(mappings)
	if m == nil {
		return
	}
	d.mu.Lock()
	e := d.entryLocked(v, m)
	notify := false
	if e.state != SyncStateInvalid && e.synced && d.areas[e.area].ReadOnly {
		// 読むだけのエリア: 機器に書けないので、次の読み取りで機器の値に戻す
		e.synced = false
	} else if e.state != SyncStateInvalid && e.synced {
		if img, ok := d.imageOf(v, e); ok {
			if img.equal(e.last) {
				e.pending = nil // 機器と同じ値に戻った
			} else {
				e.pending = &img
				notify = true
			}
		}
	}
	d.mu.Unlock()
	if notify && d.onPending != nil {
		d.onPending()
	}
}

type mappedVar struct {
	v *variable.Variable
	e *syncEntry
}

// mappedLocked はこの接続先にマッピングされた変数と状態を返す（マッピングがなくなった変数の状態は消す）
func (d *DeviceSync) mappedLocked() []mappedVar {
	byVar := d.varStore.GetAllMappingsForProtocol(d.key)
	result := make([]mappedVar, 0, len(byVar))
	for id, ms := range byVar {
		v, err := d.varStore.GetVariable(id)
		if err != nil || len(ms) == 0 {
			continue
		}
		m := ms[0]
		result = append(result, mappedVar{v: v, e: d.entryLocked(v, &m)})
	}
	for id := range d.entries {
		if _, ok := byVar[id]; !ok {
			delete(d.entries, id)
		}
	}
	return result
}

// PlanReads は読む範囲を返す。エリアごとに、重なるか隣り合う範囲を 1 つにまとめる
func (d *DeviceSync) PlanReads() []ReadBlock {
	d.mu.Lock()
	defer d.mu.Unlock()
	type span struct{ start, end uint32 } // [start, end)
	spans := make(map[string][]span)
	for _, mv := range d.mappedLocked() {
		if mv.e.state == SyncStateInvalid {
			continue
		}
		spans[mv.e.area] = append(spans[mv.e.area], span{mv.e.address, mv.e.address + uint32(mv.e.count)})
	}
	var blocks []ReadBlock
	for area, ss := range spans {
		sort.Slice(ss, func(i, j int) bool { return ss[i].start < ss[j].start })
		cur := ss[0]
		flush := func() {
			blocks = append(blocks, ReadBlock{Area: area, Address: cur.start, Count: int(cur.end - cur.start), IsBit: d.areas[area].IsBit})
		}
		for _, s := range ss[1:] {
			if s.start <= cur.end {
				if s.end > cur.end {
					cur.end = s.end
				}
				continue
			}
			flush()
			cur = s
		}
		flush()
	}
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].Area != blocks[j].Area {
			return blocks[i].Area < blocks[j].Area
		}
		return blocks[i].Address < blocks[j].Address
	})
	return blocks
}

func (e *syncEntry) inBlock(b ReadBlock) bool {
	return e.area == b.Area && e.address >= b.Address && uint64(e.address)+uint64(e.count) <= uint64(b.Address)+uint64(b.Count)
}

// ApplyRead は読めた内容を、範囲に入る変数に反映する。words / bits は block.Address からの内容
func (d *DeviceSync) ApplyRead(block ReadBlock, words []uint16, bits []bool, at time.Time) {
	type update struct {
		id    string
		value interface{}
	}
	var updates []update

	d.mu.Lock()
	for _, mv := range d.mappedLocked() {
		e := mv.e
		if e.state == SyncStateInvalid || !e.inBlock(block) {
			continue
		}
		off := int(e.address - block.Address)
		var img DeviceImage
		if block.IsBit {
			if off >= len(bits) {
				continue
			}
			img.Bits = []bool{bits[off]}
		} else {
			if off+e.count > len(words) {
				continue
			}
			img.Words = slices.Clone(words[off : off+e.count])
		}
		e.readFailures = 0
		e.state = SyncStateOK
		e.readErr = ""
		e.at = at
		if e.pending != nil {
			// 書き込みが反映されるまでは、読み取った値で上書きしない
			continue
		}
		if e.synced && img.equal(e.last) {
			continue
		}
		var value interface{}
		ok := true
		if block.IsBit {
			value = img.Bits[0]
		} else {
			value, ok = decodeVariableWords(d.varStore, mv.v, img.Words, e.endianness)
		}
		if !ok {
			e.state, e.readErr = SyncStateInvalid, "読んだ内容を変数の値にできません"
			continue
		}
		e.last = img
		e.synced = true
		updates = append(updates, update{id: mv.v.ID, value: value})
	}
	d.mu.Unlock()

	for _, u := range updates {
		// 自分を通知先から外す（読んだ値を機器へ書き戻さない）
		_ = d.varStore.UpdateValueFrom(u.id, u.value, d)
	}
}

// ReadFailed は範囲の読み取りが失敗したことを記録する。続けて threshold 回失敗したら「通信エラー」にする
func (d *DeviceSync) ReadFailed(block ReadBlock, errMsg string, threshold int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, mv := range d.mappedLocked() {
		e := mv.e
		if e.state == SyncStateInvalid || !e.inBlock(block) {
			continue
		}
		e.readFailures++
		e.readErr = errMsg
		if e.readFailures >= threshold {
			e.state = SyncStateCommError
		}
	}
}

// PendingWrites は機器に書く内容（書き込み未反映の変数）を、エリアとアドレスの順に返す
func (d *DeviceSync) PendingWrites() []PendingWrite {
	d.mu.Lock()
	defer d.mu.Unlock()
	var result []PendingWrite
	for _, mv := range d.mappedLocked() {
		e := mv.e
		if e.pending == nil || e.state == SyncStateInvalid {
			continue
		}
		result = append(result, PendingWrite{
			VariableID: mv.v.ID,
			Area:       e.area,
			Address:    e.address,
			Image:      DeviceImage{Words: slices.Clone(e.pending.Words), Bits: slices.Clone(e.pending.Bits)},
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Area != result[j].Area {
			return result[i].Area < result[j].Area
		}
		return result[i].Address < result[j].Address
	})
	return result
}

// WriteDone は書き込みが反映されたことを記録する（送っている間に値がまた変わっていたら、書き込み未反映のまま）
func (d *DeviceSync) WriteDone(w PendingWrite) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e := d.entries[w.VariableID]
	if e == nil || e.area != w.Area || e.address != w.Address {
		return
	}
	e.last = w.Image
	e.writeErr = ""
	if e.pending != nil && e.pending.equal(w.Image) {
		e.pending = nil
	}
}

// WriteFailed は書き込みが失敗したことを記録する（書き込み未反映のまま。次の周期に送り直す）
func (d *DeviceSync) WriteFailed(w PendingWrite, errMsg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if e := d.entries[w.VariableID]; e != nil {
		e.writeErr = errMsg
	}
}

// Statuses はマッピングされた変数の同期の状態を返す（変数 ID → 状態）
func (d *DeviceSync) Statuses() map[string]VariableSyncStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make(map[string]VariableSyncStatus)
	for _, mv := range d.mappedLocked() {
		e := mv.e
		errMsg := e.readErr
		if e.writeErr != "" {
			errMsg = e.writeErr
		}
		result[mv.v.ID] = VariableSyncStatus{State: e.state, WritePending: e.pending != nil, Error: errMsg, At: e.at}
	}
	return result
}
