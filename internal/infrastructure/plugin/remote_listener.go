package plugin

import (
	"context"
	"fmt"
	"sync"

	"karakuri/internal/domain/variable"
)

// RemoteVariableChangeListener は VariableStore の変更を監視してプラグインの DataStore に同期する。
// また、プラグインの DataChange ストリームを受信して VariableStore を更新する（双方向同期）。
//
// 循環防止:
//   - VariableStore → Plugin DataStore: OnVariableChanged() で gRPC WriteWords を呼び出す
//   - Plugin DataStore → VariableStore: SubscribeChanges ストリームを受信して UpdateValue を呼び出す
//   - 循環防止のため syncing フラグを使用する
type RemoteVariableChangeListener struct {
	remoteDS     *RemoteDataStore
	varStore     *variable.VariableStore
	protocolType string

	mu      sync.Mutex
	syncing bool
}

// NewRemoteVariableChangeListener は RemoteVariableChangeListener を作成し、
// VariableStore のリスナーとして登録する
func NewRemoteVariableChangeListener(
	remoteDS *RemoteDataStore,
	varStore *variable.VariableStore,
	protocolType string,
) *RemoteVariableChangeListener {
	l := &RemoteVariableChangeListener{
		remoteDS:     remoteDS,
		varStore:     varStore,
		protocolType: protocolType,
	}
	varStore.AddListener(l)
	// 初期同期: 既存の変数値をプラグインの DataStore に反映
	l.syncAllVariablesToDataStore()
	return l
}

// Detach はリスナーを解除する
func (l *RemoteVariableChangeListener) Detach() {
	l.varStore.RemoveListener(l)
}

// OnVariableChanged は VariableStore からの変更通知を処理する（Variable → Plugin DataStore）
func (l *RemoteVariableChangeListener) OnVariableChanged(v *variable.Variable, mappings []variable.ProtocolMapping, _ string, _ interface{}) {
	l.mu.Lock()
	if l.syncing {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	for _, m := range mappings {
		if m.ProtocolType != l.protocolType {
			continue
		}
		l.writeVariableToRemote(v, &m)
	}
}

// StartChangeSubscription はプラグインの DataChange ストリームを受信して VariableStore を更新する。
// ctx がキャンセルされるまでブロックする。PLCService の AddServer 後に goroutine で起動すること。
func (l *RemoteVariableChangeListener) StartChangeSubscription(ctx context.Context) error {
	stream, err := l.remoteDS.SubscribeChanges(ctx)
	if err != nil {
		return fmt.Errorf("DataChange ストリーム接続失敗: %w", err)
	}

	for {
		change, err := stream.Recv()
		if err != nil {
			// ctx がキャンセルされた場合は正常終了
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("DataChange ストリーム受信エラー: %w", err)
		}

		// Plugin DataStore → VariableStore 同期
		l.mu.Lock()
		l.syncing = true
		l.mu.Unlock()

		if change.IsBit {
			l.syncBitChangeToVariable(change.Area, change.Address, change.BitValues)
		} else {
			l.syncWordChangeToVariable(change.Area, change.Address, change.Values)
		}

		l.mu.Lock()
		l.syncing = false
		l.mu.Unlock()
	}
}

// syncWordChangeToVariable は DataChange のワード変更を対応する変数に反映する
func (l *RemoteVariableChangeListener) syncWordChangeToVariable(area string, address uint32, values []uint32) {
	v, m := l.varStore.FindVariableByMapping(l.protocolType, area, address)
	if v == nil || m == nil {
		return
	}

	wordCount := v.DataType.WordCountWithResolver(l.varStore)
	words := make([]uint16, wordCount)
	for i := 0; i < wordCount && i < len(values); i++ {
		words[i] = uint16(values[i])
	}
	// 不足分は gRPC で読み取り（DataChange には変更部分のみが含まれる場合がある）
	if len(values) < wordCount {
		fetched, err := l.remoteDS.ReadWords(m.MemoryArea, m.Address, uint16(wordCount))
		if err != nil {
			return
		}
		words = fetched
	}

	var newValue interface{}
	var err error
	if v.DataType.IsArrayType() {
		elemType, size, parseErr := variable.ParseArrayType(v.DataType)
		if parseErr != nil {
			return
		}
		newValue, err = variable.WordsToArrayValue(words, elemType, size, m.Endianness, l.varStore)
	} else if v.DataType.IsStructType() {
		structDef, getErr := l.varStore.GetStructType(string(v.DataType))
		if getErr != nil || structDef == nil {
			return
		}
		newValue, err = variable.WordsToStructValue(words, structDef, m.Endianness, l.varStore)
	} else {
		newValue, err = variable.WordsToValue(words, v.DataType, m.Endianness)
	}
	if err != nil {
		return
	}
	l.varStore.UpdateValue(v.ID, newValue)
}

// syncBitChangeToVariable は DataChange のビット変更を対応する変数に反映する
func (l *RemoteVariableChangeListener) syncBitChangeToVariable(area string, address uint32, bitValues []bool) {
	v, _ := l.varStore.FindVariableByMapping(l.protocolType, area, address)
	if v == nil || len(bitValues) == 0 {
		return
	}
	l.varStore.UpdateValue(v.ID, bitValues[0])
}

// writeVariableToRemote は変数の値をプラグインの DataStore に書き込む
func (l *RemoteVariableChangeListener) writeVariableToRemote(v *variable.Variable, m *variable.ProtocolMapping) {
	if v.DataType.IsBitType() {
		val := variable.ValueToBool(v.Value, v.DataType)
		_ = l.remoteDS.WriteBit(m.MemoryArea, m.Address, val)
	} else if v.DataType.IsArrayType() {
		elemType, size, err := variable.ParseArrayType(v.DataType)
		if err != nil {
			return
		}
		words := variable.ArrayValueToWords(v.Value, elemType, size, m.Endianness, l.varStore)
		_ = l.remoteDS.WriteWords(m.MemoryArea, m.Address, words)
	} else if v.DataType.IsStructType() {
		structDef, err := l.varStore.GetStructType(string(v.DataType))
		if err != nil || structDef == nil {
			return
		}
		words := variable.StructValueToWords(v.Value, structDef, m.Endianness, l.varStore)
		_ = l.remoteDS.WriteWords(m.MemoryArea, m.Address, words)
	} else {
		words := variable.ValueToWords(v.Value, v.DataType, m.Endianness)
		_ = l.remoteDS.WriteWords(m.MemoryArea, m.Address, words)
	}
}

// SyncHostWordWriteToVariable はホスト（UI/スクリプト）がプラグインの DataStore に
// ワードを書き込んだ後、対応する変数を更新する。
// プラグイン側は「ホスト→プラグイン書き込み時は通知しない」ため、
// ホスト側でこのメソッドを呼び出して変数を同期する。
func (l *RemoteVariableChangeListener) SyncHostWordWriteToVariable(area string, address uint32) {
	l.mu.Lock()
	if l.syncing {
		l.mu.Unlock()
		return
	}
	l.syncing = true
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.syncing = false
		l.mu.Unlock()
	}()

	// values=nil（len=0）なので syncWordChangeToVariable 内部でリモートから読み取る
	l.syncWordChangeToVariable(area, address, nil)
}

// SyncHostBitWriteToVariable はホスト（UI/スクリプト）がプラグインの DataStore に
// ビットを書き込んだ後、対応する変数を更新する。
func (l *RemoteVariableChangeListener) SyncHostBitWriteToVariable(area string, address uint32) {
	l.mu.Lock()
	if l.syncing {
		l.mu.Unlock()
		return
	}
	l.syncing = true
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.syncing = false
		l.mu.Unlock()
	}()

	bit, err := l.remoteDS.ReadBit(area, address)
	if err != nil {
		return
	}
	l.syncBitChangeToVariable(area, address, []bool{bit})
}

// syncAllVariablesToDataStore は起動時に全変数をプラグインの DataStore に同期する
func (l *RemoteVariableChangeListener) syncAllVariablesToDataStore() {
	allVars := l.varStore.GetAllVariables()
	for _, v := range allVars {
		mappings := l.varStore.GetMappings(v.ID)
		for _, m := range mappings {
			if m.ProtocolType == l.protocolType {
				l.writeVariableToRemote(v, &m)
			}
		}
	}
}
