package adapter

import (
	"slices"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
)

// VariableBackedDataStore は、プロトコルの DataStore（レジスタ）を包み、変数と双方向に同期する。
//
//   - 変数 → レジスタ: 変数が変わると（OnVariableChanged）、マッピング先のレジスタに書く
//   - レジスタ → 変数: この DataStore に書かれると（Write*）、その範囲にマッピングされた変数に反映する
//
// どちらも、呼び出しが戻った時点で反映が終わっている（別のゴルーチンに回さない）。
// プロトコルのサーバーにはこの DataStore を渡すので、クライアントの書き込みもここを通る。
//
// 同期が行き来しないようにする仕組み:
//   - 変数 → レジスタは内側の DataStore に直接書く（この型の Write* を通らないので、変数へ戻らない）
//   - レジスタ → 変数は UpdateValueFrom で自分を通知先から外す（レジスタへ書き戻さない）
//
// 別々の書き込み元（クライアントとスクリプトなど）が同じ変数を同時に書いた場合、
// どちらが残るかは決まらない（後から書いた方が残る）。
//
// 同じメモリを指すエリア（MemoryArea.BitsOf: ワードのエリアと、それをビットごとに見たエリア）があるプロトコルでは、
// 片方に書かれたら、もう片方にマッピングした変数にも反映する（syncSharedAreas）。
type VariableBackedDataStore struct {
	inner        protocol.DataStore
	varStore     *variable.VariableStore
	protocolType string

	// 同じメモリを指すエリア。作ったあとは変えない
	wordAreaOf map[string]string   // ビットのエリア → 同じメモリのワードのエリア
	bitAreasOf map[string][]string // ワードのエリア → 同じメモリのビットのエリア
}

// NewVariableBackedDataStore は新しいVariableBackedDataStoreを作成する
func NewVariableBackedDataStore(inner protocol.DataStore, varStore *variable.VariableStore, protocolType string) *VariableBackedDataStore {
	adapter := &VariableBackedDataStore{
		inner:        inner,
		varStore:     varStore,
		protocolType: protocolType,
		wordAreaOf:   make(map[string]string),
		bitAreasOf:   make(map[string][]string),
	}
	for _, area := range inner.GetAreas() {
		if area.BitsOf != "" {
			adapter.wordAreaOf[area.ID] = area.BitsOf
			adapter.bitAreasOf[area.BitsOf] = append(adapter.bitAreasOf[area.BitsOf], area.ID)
		}
	}

	// VariableStoreの変更リスナーとして登録
	varStore.AddListener(adapter)

	// 初期同期: 既存の変数値をDataStoreに反映
	adapter.syncAllVariablesToDataStore()

	return adapter
}

// Detach はリスナーを解除する（サーバーを削除するときに呼ぶ）
func (a *VariableBackedDataStore) Detach() {
	a.varStore.RemoveListener(a)
}

// ============================================================
// 変数 → レジスタ
// ============================================================

// OnVariableChanged はVariableStoreからの変更通知を処理する
func (a *VariableBackedDataStore) OnVariableChanged(v *variable.Variable, mappings []variable.ProtocolMapping) {
	for _, m := range mappings {
		if m.ProtocolType != a.protocolType {
			continue
		}
		a.writeVariableToInner(v, &m)
		start, end := a.mappingBitRange(v, &m)
		a.syncSharedAreas(m.MemoryArea, start, end, v.ID)
	}
}

// ============================================================
// 同じメモリを指すエリア
// ============================================================

// sharedAreas は、area と同じメモリを指す、ほかのエリアを返す（なければ nil）
func (a *VariableBackedDataStore) sharedAreas(area string) []string {
	if word, ok := a.wordAreaOf[area]; ok {
		others := []string{word}
		for _, bit := range a.bitAreasOf[word] {
			if bit != area {
				others = append(others, bit)
			}
		}
		return others
	}
	return a.bitAreasOf[area]
}

// mappingBitRange は、変数がマッピング先で占める範囲を、ビットの番号（ワード × 16 + ビット）で返す
func (a *VariableBackedDataStore) mappingBitRange(v *variable.Variable, m *variable.ProtocolMapping) (start, end uint64) {
	if v.DataType.IsBitType() {
		// BOOL は、ワードのエリアでもビットの番号で読み書きする（writeVariableToInner）
		return uint64(m.Address), uint64(m.Address) + 1
	}
	return a.wordsBitRange(m.MemoryArea, m.Address, v.DataType.WordCountWithResolver(a.varStore.TypeResolver()))
}

// wordsBitRange は、area の address から count ワードの範囲を、ビットの番号で返す
func (a *VariableBackedDataStore) wordsBitRange(area string, address uint32, count int) (start, end uint64) {
	if _, isBitArea := a.wordAreaOf[area]; isBitArea {
		return uint64(address), uint64(address) + uint64(count)*16
	}
	return uint64(address) * 16, (uint64(address) + uint64(count)) * 16
}

// syncSharedAreas は、area のビットの範囲 [start, end) が書かれたときに、
// 同じメモリを指すほかのエリアにマッピングした変数を、メモリの値で更新する。
// except は、書き込みのもとになった変数の ID（その変数は更新しない）
func (a *VariableBackedDataStore) syncSharedAreas(area string, start, end uint64, except string) {
	others := a.sharedAreas(area)
	if len(others) == 0 {
		return
	}
	for varID, mappings := range a.varStore.GetAllMappingsForProtocol(a.protocolType) {
		if varID == except {
			continue
		}
		for i := range mappings {
			m := &mappings[i]
			if !slices.Contains(others, m.MemoryArea) {
				continue
			}
			v, err := a.varStore.GetVariable(varID)
			if err != nil {
				break
			}
			if s, e := a.mappingBitRange(v, m); s < end && start < e {
				if value, ok := a.readVariableFromInner(v, m); ok {
					a.varStore.UpdateValueFrom(v.ID, value, a)
				}
			}
		}
	}
}

// writeVariableToInner は変数の値を内部DataStoreに書き込む
func (a *VariableBackedDataStore) writeVariableToInner(v *variable.Variable, m *variable.ProtocolMapping) {
	if v.DataType.IsBitType() {
		val := variable.ValueToBool(v.Value, v.DataType)
		a.inner.WriteBit(m.MemoryArea, m.Address, val)
		return
	}

	words, ok := encodeVariableWords(a.varStore, v, m.Endianness)
	if !ok {
		return
	}
	for i, w := range words {
		a.inner.WriteWord(m.MemoryArea, m.Address+uint32(i), w)
	}
}

// syncAllVariablesToDataStore は全変数を内部DataStoreに同期する
func (a *VariableBackedDataStore) syncAllVariablesToDataStore() {
	allVars := a.varStore.GetAllVariables()
	for _, v := range allVars {
		mappings := a.varStore.GetMappings(v.ID)
		for _, m := range mappings {
			if m.ProtocolType == a.protocolType {
				a.writeVariableToInner(v, &m)
			}
		}
	}
}

// ============================================================
// レジスタ → 変数
// ============================================================

// readVariableFromInner は、変数のマッピング先のレジスタを読んで変数の値にする
func (a *VariableBackedDataStore) readVariableFromInner(v *variable.Variable, m *variable.ProtocolMapping) (interface{}, bool) {
	if v.DataType.IsBitType() {
		bit, err := a.inner.ReadBit(m.MemoryArea, m.Address)
		return bit, err == nil
	}

	wordCount := v.DataType.WordCountWithResolver(a.varStore.TypeResolver())
	words, err := a.inner.ReadWords(m.MemoryArea, m.Address, uint16(wordCount))
	if err != nil {
		return nil, false
	}
	return decodeVariableWords(a.varStore, v, words, m.Endianness)
}

// syncWordsToVariables は、書き込まれたワードの範囲にマッピングされた変数を、レジスタの値で更新する。
// 変数ごとに 1 回だけ、変数全体を読み直して更新する（変数の一部のワードだけが書かれた場合も正しい値になる）
func (a *VariableBackedDataStore) syncWordsToVariables(area string, address uint32, count int) {
	updated := make(map[string]bool)
	for i := 0; i < count; i++ {
		v, m := a.varStore.FindVariableByMapping(a.protocolType, area, address+uint32(i))
		if v == nil || m == nil || updated[v.ID] {
			continue
		}
		updated[v.ID] = true
		if value, ok := a.readVariableFromInner(v, m); ok {
			a.varStore.UpdateValueFrom(v.ID, value, a)
		}
	}
}

// syncBitsToVariables は、書き込まれたビットの範囲にマッピングされた変数を、ビットの値で更新する
func (a *VariableBackedDataStore) syncBitsToVariables(area string, address uint32, count int) {
	for i := 0; i < count; i++ {
		addr := address + uint32(i)
		v, _ := a.varStore.FindVariableByMapping(a.protocolType, area, addr)
		if v == nil {
			continue
		}
		bit, err := a.inner.ReadBit(area, addr)
		if err != nil {
			continue
		}
		a.varStore.UpdateValueFrom(v.ID, bit, a)
	}
}

// ============================================================
// protocol.DataStore インターフェースの実装
// 読み取りはそのまま inner に委譲
// 書き込みは inner に書き込み後、対応する変数を更新
// ============================================================

func (a *VariableBackedDataStore) GetAreas() []protocol.MemoryArea {
	return a.inner.GetAreas()
}

func (a *VariableBackedDataStore) ReadBit(area string, address uint32) (bool, error) {
	return a.inner.ReadBit(area, address)
}

func (a *VariableBackedDataStore) ReadBits(area string, address uint32, count uint16) ([]bool, error) {
	return a.inner.ReadBits(area, address, count)
}

func (a *VariableBackedDataStore) ReadWord(area string, address uint32) (uint16, error) {
	return a.inner.ReadWord(area, address)
}

func (a *VariableBackedDataStore) ReadWords(area string, address uint32, count uint16) ([]uint16, error) {
	return a.inner.ReadWords(area, address, count)
}

func (a *VariableBackedDataStore) WriteBit(area string, address uint32, value bool) error {
	if err := a.inner.WriteBit(area, address, value); err != nil {
		return err
	}
	a.syncBitsToVariables(area, address, 1)
	a.syncSharedAreas(area, uint64(address), uint64(address)+1, "")
	return nil
}

func (a *VariableBackedDataStore) WriteBits(area string, address uint32, values []bool) error {
	if err := a.inner.WriteBits(area, address, values); err != nil {
		return err
	}
	a.syncBitsToVariables(area, address, len(values))
	a.syncSharedAreas(area, uint64(address), uint64(address)+uint64(len(values)), "")
	return nil
}

func (a *VariableBackedDataStore) WriteWord(area string, address uint32, value uint16) error {
	if err := a.inner.WriteWord(area, address, value); err != nil {
		return err
	}
	a.syncWordsToVariables(area, address, 1)
	start, end := a.wordsBitRange(area, address, 1)
	a.syncSharedAreas(area, start, end, "")
	return nil
}

func (a *VariableBackedDataStore) WriteWords(area string, address uint32, values []uint16) error {
	if err := a.inner.WriteWords(area, address, values); err != nil {
		return err
	}
	a.syncWordsToVariables(area, address, len(values))
	start, end := a.wordsBitRange(area, address, len(values))
	a.syncSharedAreas(area, start, end, "")
	return nil
}
