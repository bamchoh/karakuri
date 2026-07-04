package adapter

import (
	"sync"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
)

// VariableBackedDataStore はVariableStoreと連動するDataStoreアダプター
// 内部に従来のDataStoreを保持し、変数マッピングによる双方向同期を行う
type VariableBackedDataStore struct {
	inner        protocol.DataStore
	varStore     *variable.VariableStore
	protocolType string
	mu           sync.Mutex // 同期ループ防止用
	syncing      bool
}

// NewVariableBackedDataStore は新しいVariableBackedDataStoreを作成する
func NewVariableBackedDataStore(inner protocol.DataStore, varStore *variable.VariableStore, protocolType string) *VariableBackedDataStore {
	adapter := &VariableBackedDataStore{
		inner:        inner,
		varStore:     varStore,
		protocolType: protocolType,
	}

	// VariableStoreの変更リスナーとして登録
	varStore.AddListener(adapter)

	// 初期同期: 既存の変数値をDataStoreに反映
	adapter.syncAllVariablesToDataStore()

	return adapter
}

// Unwrap は内側の DataStore を返す。
// プロトコルファクトリーが具体型を必要とする場合に使用する。
func (a *VariableBackedDataStore) Unwrap() protocol.DataStore {
	return a.inner
}

// Detach はリスナーを解除する（プロトコル切り替え時に呼ぶ）
func (a *VariableBackedDataStore) Detach() {
	a.varStore.RemoveListener(a)
}

// OnVariableChanged はVariableStoreからの変更通知を処理する
// 変数値 → DataStoreへの書き込み
func (a *VariableBackedDataStore) OnVariableChanged(v *variable.Variable, mappings []variable.ProtocolMapping, _ string, _ interface{}) {
	a.mu.Lock()
	if a.syncing {
		a.mu.Unlock()
		return
	}
	a.syncing = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.syncing = false
		a.mu.Unlock()
	}()

	for _, m := range mappings {
		if m.ProtocolType != a.protocolType {
			continue
		}
		a.writeVariableToInner(v, &m)
	}
}

// writeVariableToInner は変数の値を内部DataStoreに書き込む
func (a *VariableBackedDataStore) writeVariableToInner(v *variable.Variable, m *variable.ProtocolMapping) {
	if v.DataType.IsBitType() {
		val := variable.ValueToBool(v.Value, v.DataType)
		a.inner.WriteBit(m.MemoryArea, m.Address, val)
	} else if v.DataType.IsArrayType() {
		elemType, size, err := variable.ParseArrayType(v.DataType)
		if err != nil {
			return
		}
		words := variable.ArrayValueToWords(v.Value, elemType, size, m.Endianness, a.varStore)
		for i, w := range words {
			a.inner.WriteWord(m.MemoryArea, m.Address+uint32(i), w)
		}
	} else if v.DataType.IsStructType() {
		structDef, err := a.varStore.GetStructType(string(v.DataType))
		if err != nil || structDef == nil {
			return
		}
		words := variable.StructValueToWords(v.Value, structDef, m.Endianness, a.varStore)
		for i, w := range words {
			a.inner.WriteWord(m.MemoryArea, m.Address+uint32(i), w)
		}
	} else {
		words := variable.ValueToWords(v.Value, v.DataType, m.Endianness)
		for i, w := range words {
			a.inner.WriteWord(m.MemoryArea, m.Address+uint32(i), w)
		}
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

// syncDataStoreToVariable はDataStore上のアドレス書き込みを対応する変数に反映する
func (a *VariableBackedDataStore) syncWordToVariable(area string, address uint32) {
	a.mu.Lock()
	if a.syncing {
		a.mu.Unlock()
		return
	}
	a.syncing = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.syncing = false
		a.mu.Unlock()
	}()

	v, m := a.varStore.FindVariableByMapping(a.protocolType, area, address)
	if v == nil || m == nil {
		return
	}

	wordCount := v.DataType.WordCountWithResolver(a.varStore)
	words, err := a.inner.ReadWords(m.MemoryArea, m.Address, uint16(wordCount))
	if err != nil {
		return
	}

	var newValue interface{}
	if v.DataType.IsArrayType() {
		elemType, size, parseErr := variable.ParseArrayType(v.DataType)
		if parseErr != nil {
			return
		}
		newValue, err = variable.WordsToArrayValue(words, elemType, size, m.Endianness, a.varStore)
	} else if v.DataType.IsStructType() {
		structDef, getErr := a.varStore.GetStructType(string(v.DataType))
		if getErr != nil || structDef == nil {
			return
		}
		newValue, err = variable.WordsToStructValue(words, structDef, m.Endianness, a.varStore)
	} else {
		newValue, err = variable.WordsToValue(words, v.DataType, m.Endianness)
	}
	if err != nil {
		return
	}

	a.varStore.UpdateValue(v.ID, newValue)
}

// syncBitToVariable はDataStore上のビット書き込みを対応する変数に反映する
func (a *VariableBackedDataStore) syncBitToVariable(area string, address uint32) {
	a.mu.Lock()
	if a.syncing {
		a.mu.Unlock()
		return
	}
	a.syncing = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.syncing = false
		a.mu.Unlock()
	}()

	v, _ := a.varStore.FindVariableByMapping(a.protocolType, area, address)
	if v == nil {
		return
	}

	bit, err := a.inner.ReadBit(area, address)
	if err != nil {
		return
	}

	a.varStore.UpdateValue(v.ID, bit)
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
	go a.syncBitToVariable(area, address)
	return nil
}

func (a *VariableBackedDataStore) WriteBits(area string, address uint32, values []bool) error {
	if err := a.inner.WriteBits(area, address, values); err != nil {
		return err
	}
	for i := range values {
		go a.syncBitToVariable(area, address+uint32(i))
	}
	return nil
}

func (a *VariableBackedDataStore) WriteWord(area string, address uint32, value uint16) error {
	if err := a.inner.WriteWord(area, address, value); err != nil {
		return err
	}
	go a.syncWordToVariable(area, address)
	return nil
}

func (a *VariableBackedDataStore) WriteWords(area string, address uint32, values []uint16) error {
	if err := a.inner.WriteWords(area, address, values); err != nil {
		return err
	}
	for i := range values {
		go a.syncWordToVariable(area, address+uint32(i))
	}
	return nil
}

func (a *VariableBackedDataStore) Snapshot() map[string]interface{} {
	return a.inner.Snapshot()
}

func (a *VariableBackedDataStore) Restore(data map[string]interface{}) error {
	if err := a.inner.Restore(data); err != nil {
		return err
	}
	// 復元後、DataStoreの値を変数に反映
	a.syncAllDataStoreToVariables()
	return nil
}

func (a *VariableBackedDataStore) ClearAll() {
	a.inner.ClearAll()
}

// syncAllDataStoreToVariables はDataStoreの全マッピング済みアドレスを変数に反映する
func (a *VariableBackedDataStore) syncAllDataStoreToVariables() {
	allVars := a.varStore.GetAllVariables()
	for _, v := range allVars {
		mappings := a.varStore.GetMappings(v.ID)
		for _, m := range mappings {
			if m.ProtocolType != a.protocolType {
				continue
			}

			if v.DataType.IsBitType() {
				bit, err := a.inner.ReadBit(m.MemoryArea, m.Address)
				if err == nil {
					a.varStore.UpdateValue(v.ID, bit)
				}
			} else {
				wordCount := v.DataType.WordCountWithResolver(a.varStore)
				words, err := a.inner.ReadWords(m.MemoryArea, m.Address, uint16(wordCount))
				if err != nil {
					continue
				}
				var newValue interface{}
				var convErr error
				if v.DataType.IsArrayType() {
					elemType, size, parseErr := variable.ParseArrayType(v.DataType)
					if parseErr != nil {
						continue
					}
					newValue, convErr = variable.WordsToArrayValue(words, elemType, size, m.Endianness, a.varStore)
				} else if v.DataType.IsStructType() {
					structDef, getErr := a.varStore.GetStructType(string(v.DataType))
					if getErr != nil || structDef == nil {
						continue
					}
					newValue, convErr = variable.WordsToStructValue(words, structDef, m.Endianness, a.varStore)
				} else {
					newValue, convErr = variable.WordsToValue(words, v.DataType, m.Endianness)
				}
				if convErr == nil {
					a.varStore.UpdateValue(v.ID, newValue)
				}
			}
		}
	}
}

// protocol.DataStoreインターフェースを満たすことを確認
var _ protocol.DataStore = (*VariableBackedDataStore)(nil)
