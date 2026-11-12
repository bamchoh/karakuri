package adapter

import (
	"sync"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
)

// =====================================================================
// テスト用 DataStore モック
// =====================================================================

// testDataStore は protocol.DataStore の最小実装（テスト用）
type testDataStore struct {
	mu    sync.RWMutex
	bits  map[string]map[uint32]bool
	words map[string]map[uint32]uint16
	areas []protocol.MemoryArea
}

func newTestDataStore() *testDataStore {
	return &testDataStore{
		bits: map[string]map[uint32]bool{
			"coils": {},
		},
		words: map[string]map[uint32]uint16{
			"holding": {},
		},
		areas: []protocol.MemoryArea{
			{ID: "coils", IsBit: true, Size: 1000},
			{ID: "holding", Size: 1000},
		},
	}
}

func (d *testDataStore) GetAreas() []protocol.MemoryArea { return d.areas }

func (d *testDataStore) ReadBit(area string, addr uint32) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.bits[area][addr], nil
}

func (d *testDataStore) WriteBit(area string, addr uint32, val bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.bits[area] == nil {
		d.bits[area] = make(map[uint32]bool)
	}
	d.bits[area][addr] = val
	return nil
}

func (d *testDataStore) ReadBits(area string, addr uint32, count uint16) ([]bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]bool, count)
	for i := uint16(0); i < count; i++ {
		result[i] = d.bits[area][addr+uint32(i)]
	}
	return result, nil
}

func (d *testDataStore) WriteBits(area string, addr uint32, vals []bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.bits[area] == nil {
		d.bits[area] = make(map[uint32]bool)
	}
	for i, v := range vals {
		d.bits[area][addr+uint32(i)] = v
	}
	return nil
}

func (d *testDataStore) ReadWord(area string, addr uint32) (uint16, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.words[area][addr], nil
}

func (d *testDataStore) WriteWord(area string, addr uint32, val uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.words[area] == nil {
		d.words[area] = make(map[uint32]uint16)
	}
	d.words[area][addr] = val
	return nil
}

func (d *testDataStore) ReadWords(area string, addr uint32, count uint16) ([]uint16, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]uint16, count)
	for i := uint16(0); i < count; i++ {
		result[i] = d.words[area][addr+uint32(i)]
	}
	return result, nil
}

func (d *testDataStore) WriteWords(area string, addr uint32, vals []uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.words[area] == nil {
		d.words[area] = make(map[uint32]uint16)
	}
	for i, v := range vals {
		d.words[area][addr+uint32(i)] = v
	}
	return nil
}

func (d *testDataStore) Snapshot() map[string]interface{}       { return map[string]interface{}{} }
func (d *testDataStore) Restore(_ map[string]interface{}) error { return nil }
func (d *testDataStore) ClearAll()                              {}

// =====================================================================
// テストセットアップヘルパー
// =====================================================================

// setupAdapter は VariableBackedDataStore とその依存関係を作成する
func setupAdapter(protocolType string) (*VariableBackedDataStore, *testDataStore, *variable.VariableStore) {
	inner := newTestDataStore()
	varStore := variable.NewVariableStore()
	adapter := NewVariableBackedDataStore(inner, varStore, protocolType)
	return adapter, inner, varStore
}

// =====================================================================
// Unwrap
// =====================================================================

func TestVariableBackedDataStore_Unwrap(t *testing.T) {
	inner := newTestDataStore()
	varStore := variable.NewVariableStore()
	adapter := NewVariableBackedDataStore(inner, varStore, "test")

	got := adapter.Unwrap()
	if got != inner {
		t.Error("Unwrap() should return the inner DataStore")
	}
}

// =====================================================================
// GetAreas / 基本的な読み書き委譲
// =====================================================================

func TestVariableBackedDataStore_GetAreas(t *testing.T) {
	adapter, _, _ := setupAdapter("test")
	areas := adapter.GetAreas()
	if len(areas) != 2 {
		t.Errorf("GetAreas: got %d areas, want 2", len(areas))
	}
}

func TestVariableBackedDataStore_ReadWriteWord_Delegation(t *testing.T) {
	adapter, inner, _ := setupAdapter("test")

	// WriteWord はinnerに委譲される
	if err := adapter.WriteWord("holding", 0, 12345); err != nil {
		t.Fatalf("WriteWord error: %v", err)
	}

	// 短時間待機（非同期sync完了を待つ）
	time.Sleep(20 * time.Millisecond)

	// ReadWord はinnerから読み取る
	val, err := adapter.ReadWord("holding", 0)
	if err != nil {
		t.Fatalf("ReadWord error: %v", err)
	}
	if val != 12345 {
		t.Errorf("ReadWord: got %d, want 12345", val)
	}

	// inner に直接書かれていることを確認
	innerVal, _ := inner.ReadWord("holding", 0)
	if innerVal != 12345 {
		t.Errorf("inner.ReadWord: got %d, want 12345", innerVal)
	}
}

func TestVariableBackedDataStore_ReadWriteBit_Delegation(t *testing.T) {
	adapter, inner, _ := setupAdapter("test")

	if err := adapter.WriteBit("coils", 5, true); err != nil {
		t.Fatalf("WriteBit error: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	val, err := adapter.ReadBit("coils", 5)
	if err != nil {
		t.Fatalf("ReadBit error: %v", err)
	}
	if !val {
		t.Error("ReadBit: got false, want true")
	}

	innerVal, _ := inner.ReadBit("coils", 5)
	if !innerVal {
		t.Error("inner.ReadBit: should reflect the written value")
	}
}

// =====================================================================
// OnVariableChanged → writeVariableToInner
// =====================================================================

func TestVariableBackedDataStore_OnVariableChanged_WritesToInner(t *testing.T) {
	_, inner, varStore := setupAdapter("test")

	// 変数作成
	v, err := varStore.CreateVariable("x", variable.TypeINT, float64(0))
	if err != nil {
		t.Fatalf("CreateVariable error: %v", err)
	}

	// マッピング設定
	err = varStore.SetMappings(v.ID, []variable.ProtocolMapping{
		{ProtocolType: "test", MemoryArea: "holding", Address: 10, Endianness: "big"},
	})
	if err != nil {
		t.Fatalf("SetMappings error: %v", err)
	}

	// 変数を更新 → OnVariableChanged 経由で inner に反映
	err = varStore.UpdateValue(v.ID, float64(99))
	if err != nil {
		t.Fatalf("UpdateValue error: %v", err)
	}

	// innerのアドレス10に書かれていること
	w, err := inner.ReadWord("holding", 10)
	if err != nil {
		t.Fatalf("inner.ReadWord error: %v", err)
	}
	if w != 99 {
		t.Errorf("inner.ReadWord(holding, 10) = %d, want 99", w)
	}
}

func TestVariableBackedDataStore_OnVariableChanged_WrongProtocol(t *testing.T) {
	adapter, inner, varStore := setupAdapter("myprotocol")

	v, _ := varStore.CreateVariable("x", variable.TypeINT, float64(0))
	// 別プロトコルのマッピング
	_ = varStore.SetMappings(v.ID, []variable.ProtocolMapping{
		{ProtocolType: "otherprotocol", MemoryArea: "holding", Address: 10, Endianness: "big"},
	})

	_ = varStore.UpdateValue(v.ID, float64(42))

	// myprotocol のinnerには書かれないこと
	w, _ := inner.ReadWord("holding", 10)
	if w != 0 {
		t.Errorf("inner should not be written for wrong protocol, got %d", w)
	}

	// adapterを使って_を使う
	_ = adapter.GetAreas()
}

// =====================================================================
// WriteWord → syncWordToVariable（DataStore → Variable の同期）
// =====================================================================

func TestVariableBackedDataStore_WriteWord_SyncsToVariable(t *testing.T) {
	adapter, _, varStore := setupAdapter("test")

	// 変数作成
	v, _ := varStore.CreateVariable("sensor", variable.TypeINT, float64(0))
	_ = varStore.SetMappings(v.ID, []variable.ProtocolMapping{
		{ProtocolType: "test", MemoryArea: "holding", Address: 20, Endianness: "big"},
	})

	// adapter 経由で書き込み（外部PLCからの書き込みを模擬）
	if err := adapter.WriteWord("holding", 20, 500); err != nil {
		t.Fatalf("WriteWord error: %v", err)
	}

	// 非同期syncが完了するまで待機
	time.Sleep(30 * time.Millisecond)

	// 変数に反映されていること
	got, err := varStore.GetVariable(v.ID)
	if err != nil {
		t.Fatalf("GetVariable error: %v", err)
	}
	if got.Value != int16(500) {
		t.Errorf("variable value after WriteWord: got %v (%T), want int16(500)", got.Value, got.Value)
	}
}

func TestVariableBackedDataStore_WriteBit_SyncsToVariable(t *testing.T) {
	adapter, _, varStore := setupAdapter("test")

	v, _ := varStore.CreateVariable("flag", variable.TypeBOOL, false)
	_ = varStore.SetMappings(v.ID, []variable.ProtocolMapping{
		{ProtocolType: "test", MemoryArea: "coils", Address: 3, Endianness: "big"},
	})

	if err := adapter.WriteBit("coils", 3, true); err != nil {
		t.Fatalf("WriteBit error: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	got, _ := varStore.GetVariable(v.ID)
	if got.Value != true {
		t.Errorf("variable value after WriteBit: got %v, want true", got.Value)
	}
}

// =====================================================================
// WriteWords → 複数ワード同期
// =====================================================================

func TestVariableBackedDataStore_WriteWords_SyncsToVariable(t *testing.T) {
	adapter, _, varStore := setupAdapter("test")

	// DINT は2ワード
	v, _ := varStore.CreateVariable("big", variable.TypeDINT, float64(0))
	_ = varStore.SetMappings(v.ID, []variable.ProtocolMapping{
		{ProtocolType: "test", MemoryArea: "holding", Address: 50, Endianness: "big"},
	})

	// big endian: 0x00010000 → words[0]=0x0001, words[1]=0x0000 → int32(65536)
	if err := adapter.WriteWords("holding", 50, []uint16{0x0001, 0x0000}); err != nil {
		t.Fatalf("WriteWords error: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	got, _ := varStore.GetVariable(v.ID)
	if got.Value != int32(65536) {
		t.Errorf("variable value after WriteWords: got %v (%T), want int32(65536)", got.Value, got.Value)
	}
}

// =====================================================================
// Detach
// =====================================================================

func TestVariableBackedDataStore_Detach(t *testing.T) {
	inner := newTestDataStore()
	varStore := variable.NewVariableStore()
	adapter := NewVariableBackedDataStore(inner, varStore, "test")

	v, _ := varStore.CreateVariable("x", variable.TypeINT, float64(0))
	_ = varStore.SetMappings(v.ID, []variable.ProtocolMapping{
		{ProtocolType: "test", MemoryArea: "holding", Address: 0, Endianness: "big"},
	})

	// Detach でリスナーから外れる
	adapter.Detach()

	// 変数を更新してもinnerに反映されない
	_ = varStore.UpdateValue(v.ID, float64(777))

	// adapter が listener でなくなっているので inner には書かれない
	// （書かれた場合は OnVariableChanged が呼ばれているはずがない）
	w, _ := inner.ReadWord("holding", 0)
	if w != 0 {
		t.Errorf("after Detach, inner should not be updated, got %d", w)
	}
}

// =====================================================================
// 初期同期（NewVariableBackedDataStore 時の既存変数 → inner 反映）
// =====================================================================

func TestVariableBackedDataStore_InitialSync(t *testing.T) {
	inner := newTestDataStore()
	varStore := variable.NewVariableStore()

	// 先に変数とマッピングを設定
	v, _ := varStore.CreateVariable("preexist", variable.TypeINT, float64(99))
	_ = varStore.SetMappings(v.ID, []variable.ProtocolMapping{
		{ProtocolType: "proto", MemoryArea: "holding", Address: 5, Endianness: "big"},
	})

	// アダプター作成時に初期同期が行われる
	_ = NewVariableBackedDataStore(inner, varStore, "proto")

	// inner のアドレス5に変数値が書かれていること
	w, err := inner.ReadWord("holding", 5)
	if err != nil {
		t.Fatalf("inner.ReadWord error: %v", err)
	}
	if w != 99 {
		t.Errorf("initial sync: inner word at 5 = %d, want 99", w)
	}
}

// =====================================================================
// ReadWords / ReadBits の委譲
// =====================================================================

func TestVariableBackedDataStore_ReadWords_Delegation(t *testing.T) {
	adapter, inner, _ := setupAdapter("test")

	// innerにデータを書き込む
	_ = inner.WriteWord("holding", 0, 100)
	_ = inner.WriteWord("holding", 1, 200)
	_ = inner.WriteWord("holding", 2, 300)

	words, err := adapter.ReadWords("holding", 0, 3)
	if err != nil {
		t.Fatalf("ReadWords error: %v", err)
	}
	if words[0] != 100 || words[1] != 200 || words[2] != 300 {
		t.Errorf("ReadWords: got %v, want [100, 200, 300]", words)
	}
}

func TestVariableBackedDataStore_ReadBits_Delegation(t *testing.T) {
	adapter, inner, _ := setupAdapter("test")

	_ = inner.WriteBit("coils", 0, true)
	_ = inner.WriteBit("coils", 1, false)
	_ = inner.WriteBit("coils", 2, true)

	bits, err := adapter.ReadBits("coils", 0, 3)
	if err != nil {
		t.Fatalf("ReadBits error: %v", err)
	}
	if !bits[0] || bits[1] || !bits[2] {
		t.Errorf("ReadBits: got %v, want [true, false, true]", bits)
	}
}

// =====================================================================
// WriteBits の委譲
// =====================================================================

func TestVariableBackedDataStore_WriteBits_Delegation(t *testing.T) {
	adapter, inner, _ := setupAdapter("test")

	if err := adapter.WriteBits("coils", 10, []bool{true, false, true}); err != nil {
		t.Fatalf("WriteBits error: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	b0, _ := inner.ReadBit("coils", 10)
	b1, _ := inner.ReadBit("coils", 11)
	b2, _ := inner.ReadBit("coils", 12)
	if !b0 || b1 || !b2 {
		t.Errorf("WriteBits: inner got [%v, %v, %v], want [true, false, true]", b0, b1, b2)
	}
}

// =====================================================================
// ClearAll の委譲
// =====================================================================

func TestVariableBackedDataStore_ClearAll(t *testing.T) {
	adapter, inner, _ := setupAdapter("test")

	_ = inner.WriteWord("holding", 0, 999)
	_ = inner.WriteBit("coils", 0, true)

	// adapter.ClearAll はinnerに委譲
	adapter.ClearAll()

	// inner のデータがクリアされること（実装はinnerに委譲なので、
	// testDataStoreのClearAllは何もしないが、インターフェースを満たす）
	// ここではパニックしないことだけ確認
}

// =====================================================================
// 型アサーション確認（interface実装）
// =====================================================================

func TestVariableBackedDataStore_ImplementsDataStore(t *testing.T) {
	// コンパイル時のチェックと同様。実行時にも確認
	inner := newTestDataStore()
	varStore := variable.NewVariableStore()
	var ds protocol.DataStore = NewVariableBackedDataStore(inner, varStore, "test")
	if ds == nil {
		t.Error("VariableBackedDataStore should implement protocol.DataStore")
	}
}

// =====================================================================
// 同期の確実さ
// =====================================================================

// 1 回の書き込みが複数の変数にまたがっても、すべての変数が、書き込みが戻った時点で更新されている。
// （以前は反映を別のゴルーチンに回し、処理中は他の反映を捨てていたため、一部の変数が更新されないことがあった）
func TestVariableBackedDataStore_WriteWords_UpdatesEveryVariableImmediately(t *testing.T) {
	adapter, _, varStore := setupAdapter("test")

	const n = 50
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		v, err := varStore.CreateVariable("v"+string(rune('A'+i/26))+string(rune('a'+i%26)), variable.TypeINT, float64(0))
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = v.ID
		_ = varStore.SetMappings(v.ID, []variable.ProtocolMapping{
			{ProtocolType: "test", MemoryArea: "holding", Address: uint32(100 + i), Endianness: "big"},
		})
	}

	for round := 1; round <= 20; round++ {
		values := make([]uint16, n)
		for i := range values {
			values[i] = uint16(round*100 + i)
		}
		if err := adapter.WriteWords("holding", 100, values); err != nil {
			t.Fatalf("WriteWords: %v", err)
		}
		// 待たずに確認する
		for i, id := range ids {
			got, _ := varStore.GetVariable(id)
			if got.Value != int16(round*100+i) {
				t.Fatalf("round %d: 変数 %d = %v, want %d", round, i, got.Value, round*100+i)
			}
		}
	}
}

// レジスタへの書き込みと変数の変更が同時に起きても、どちらの反映も捨てない
func TestVariableBackedDataStore_ConcurrentWritesAreNotDropped(t *testing.T) {
	adapter, inner, varStore := setupAdapter("test")
	a, _ := varStore.CreateVariable("a", variable.TypeINT, float64(0))
	b, _ := varStore.CreateVariable("b", variable.TypeINT, float64(0))
	_ = varStore.SetMappings(a.ID, []variable.ProtocolMapping{{ProtocolType: "test", MemoryArea: "holding", Address: 10, Endianness: "big"}})
	_ = varStore.SetMappings(b.ID, []variable.ProtocolMapping{{ProtocolType: "test", MemoryArea: "holding", Address: 20, Endianness: "big"}})

	// a はレジスタ側から、b は変数側から、同時に書き続ける
	const rounds = 500
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 1; i <= rounds; i++ {
			_ = adapter.WriteWord("holding", 10, uint16(i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 1; i <= rounds; i++ {
			_ = varStore.UpdateValue(b.ID, float64(i))
		}
	}()
	wg.Wait()

	// どちらも最後の値が、変数とレジスタの両方に入っている
	gotA, _ := varStore.GetVariable(a.ID)
	if gotA.Value != int16(rounds) {
		t.Errorf("レジスタから書いた変数 a = %v, want %d", gotA.Value, rounds)
	}
	if w, _ := inner.ReadWord("holding", 20); w != rounds {
		t.Errorf("変数 b のレジスタ = %d, want %d", w, rounds)
	}
}

// 変数を変えたとき、レジスタに書くのは 1 回だけ（レジスタ → 変数 → レジスタと行き来しない）
func TestVariableBackedDataStore_NoEcho(t *testing.T) {
	adapter, _, varStore := setupAdapter("test")
	v, _ := varStore.CreateVariable("v", variable.TypeINT, float64(0))
	_ = varStore.SetMappings(v.ID, []variable.ProtocolMapping{{ProtocolType: "test", MemoryArea: "holding", Address: 10, Endianness: "big"}})

	counter := &countingListener{}
	varStore.AddListener(counter)
	defer varStore.RemoveListener(counter)

	// レジスタへの書き込み 1 回につき、変数の変更の通知は 1 回
	if err := adapter.WriteWord("holding", 10, 5); err != nil {
		t.Fatal(err)
	}
	if counter.count != 1 {
		t.Errorf("レジスタへの書き込み 1 回で、変数の変更が %d 回通知された", counter.count)
	}
}

type countingListener struct{ count int }

func (l *countingListener) OnVariableChanged(*variable.Variable, []variable.ProtocolMapping, string, interface{}) {
	l.count++
}
