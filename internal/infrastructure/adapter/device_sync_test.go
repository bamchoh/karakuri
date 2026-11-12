package adapter

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
)

const syncKey = "client:test"

var syncAreas = []protocol.MemoryArea{
	{ID: "coils", IsBit: true, Size: 1000},
	{ID: "inputs", IsBit: true, Size: 1000, ReadOnly: true},
	{ID: "holding", Size: 1000},
	{ID: "inputRegs", Size: 1000, ReadOnly: true},
}

type syncFixture struct {
	t       *testing.T
	store   *variable.VariableStore
	sync    *DeviceSync
	pending atomic.Int32
}

func newSyncFixture(t *testing.T) *syncFixture {
	f := &syncFixture{t: t, store: variable.NewVariableStore()}
	f.sync = NewDeviceSync(f.store, syncKey, syncAreas, func() { f.pending.Add(1) })
	t.Cleanup(f.sync.Detach)
	return f
}

func (f *syncFixture) variable(name string, dt variable.DataType, value interface{}, area string, addr uint32) *variable.Variable {
	f.t.Helper()
	v, err := f.store.CreateVariable(name, dt, value)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.store.SetMappings(v.ID, []variable.ProtocolMapping{{ProtocolType: syncKey, MemoryArea: area, Address: addr, Endianness: "big"}}); err != nil {
		f.t.Fatal(err)
	}
	return v
}

func (f *syncFixture) value(id string) interface{} {
	f.t.Helper()
	v, err := f.store.GetVariable(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return v.Value
}

func (f *syncFixture) status(id string) VariableSyncStatus {
	return f.sync.Statuses()[id]
}

func TestDeviceSync_PlanReadsMergesAdjacent(t *testing.T) {
	f := newSyncFixture(t)
	f.variable("a", variable.TypeINT, 0, "holding", 10)
	f.variable("b", variable.TypeDINT, 0, "holding", 11) // 11〜12（a と隣り合う）
	f.variable("c", variable.TypeINT, 0, "holding", 20)  // 離れている
	f.variable("d", variable.TypeBOOL, false, "coils", 5)
	f.variable("e", variable.TypeBOOL, false, "coils", 6)
	f.variable("x", variable.TypeINT, 0, "holding", 999)
	// 他の接続先のマッピングは対象外
	other, _ := f.store.CreateVariable("other", variable.TypeINT, 0)
	_ = f.store.SetMappings(other.ID, []variable.ProtocolMapping{{ProtocolType: "client:other", MemoryArea: "holding", Address: 0}})

	got := f.sync.PlanReads()
	want := []ReadBlock{
		{Area: "coils", Address: 5, Count: 2, IsBit: true},
		{Area: "holding", Address: 10, Count: 3},
		{Area: "holding", Address: 20, Count: 1},
		{Area: "holding", Address: 999, Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestDeviceSync_InvalidMappings(t *testing.T) {
	f := newSyncFixture(t)
	a := f.variable("a", variable.TypeINT, 0, "coils", 0)      // ビットのエリアに INT
	b := f.variable("b", variable.TypeDINT, 0, "holding", 999) // 範囲を超える
	c := f.variable("c", variable.TypeINT, 0, "nope", 0)       // ないエリア
	if blocks := f.sync.PlanReads(); len(blocks) != 0 {
		t.Fatalf("使えないマッピングは読まない: %+v", blocks)
	}
	for _, v := range []*variable.Variable{a, b, c} {
		if s := f.status(v.ID); s.State != SyncStateInvalid || s.Error == "" {
			t.Errorf("%s: %+v", v.Name, s)
		}
	}
}

// 機器が先: 読めるまでは書かない。読めたあとに変わったら書き込み未反映にする
func TestDeviceSync_DeviceFirstThenWrite(t *testing.T) {
	f := newSyncFixture(t)
	v := f.variable("v", variable.TypeINT, int16(5), "holding", 10)

	// 読む前に変えても、書かない
	_ = f.store.UpdateValue(v.ID, int16(6))
	if w := f.sync.PendingWrites(); len(w) != 0 {
		t.Fatalf("読む前に書いている: %+v", w)
	}
	if s := f.status(v.ID); s.State != SyncStateUnknown {
		t.Fatalf("読む前の状態: %+v", s)
	}

	// 機器の値で変数を上書きする
	block := ReadBlock{Area: "holding", Address: 10, Count: 1}
	f.sync.ApplyRead(block, []uint16{100}, nil, time.Now())
	if got := f.value(v.ID); got != int16(100) {
		t.Fatalf("読んだ値 = %v", got)
	}
	if s := f.status(v.ID); s.State != SyncStateOK || s.WritePending {
		t.Fatalf("読んだあと: %+v", s)
	}
	// 読み取りで変えたときは書き込み未反映にしない（機器へ書き戻さない）
	if w := f.sync.PendingWrites(); len(w) != 0 {
		t.Fatalf("読んだ値を書き戻している: %+v", w)
	}

	// 変数を変えると書き込み未反映
	_ = f.store.UpdateValue(v.ID, int16(-2))
	if f.pending.Load() == 0 {
		t.Fatal("onPending が呼ばれていない")
	}
	w := f.sync.PendingWrites()
	if len(w) != 1 || !reflect.DeepEqual(w[0].Image.Words, []uint16{0xFFFE}) || w[0].Address != 10 {
		t.Fatalf("書く内容: %+v", w)
	}
	if !f.status(v.ID).WritePending {
		t.Fatal("書き込み未反映のはず")
	}

	// 反映されるまでは、読んだ値で上書きしない
	f.sync.ApplyRead(block, []uint16{100}, nil, time.Now())
	if got := f.value(v.ID); got != int16(-2) {
		t.Fatalf("書き込み未反映の値を上書きした: %v", got)
	}

	// 書き込みが失敗したら、未反映のまま理由を残す
	f.sync.WriteFailed(w[0], "応答がありません")
	if s := f.status(v.ID); !s.WritePending || s.Error != "応答がありません" {
		t.Fatalf("書き込み失敗: %+v", s)
	}

	f.sync.WriteDone(w[0])
	if s := f.status(v.ID); s.WritePending || s.Error != "" {
		t.Fatalf("書き込み後: %+v", s)
	}
	// 機器が同じ値を返しても、変数は変わらない
	f.sync.ApplyRead(block, []uint16{0xFFFE}, nil, time.Now())
	if got := f.value(v.ID); got != int16(-2) {
		t.Fatalf("値 = %v", got)
	}
	// 機器の側で変わったら、変数に反映する
	f.sync.ApplyRead(block, []uint16{7}, nil, time.Now())
	if got := f.value(v.ID); got != int16(7) {
		t.Fatalf("機器の変化 = %v", got)
	}
}

// 送っている間にまた値が変わったら、書き込み未反映のまま（新しい値を次に送る）
func TestDeviceSync_ChangedWhileWriting(t *testing.T) {
	f := newSyncFixture(t)
	v := f.variable("v", variable.TypeINT, int16(0), "holding", 0)
	f.sync.ApplyRead(ReadBlock{Area: "holding", Count: 1}, []uint16{0}, nil, time.Now())
	_ = f.store.UpdateValue(v.ID, int16(1))
	w := f.sync.PendingWrites()[0]
	_ = f.store.UpdateValue(v.ID, int16(2))
	f.sync.WriteDone(w)
	next := f.sync.PendingWrites()
	if len(next) != 1 || next[0].Image.Words[0] != 2 {
		t.Fatalf("新しい値が残っていない: %+v", next)
	}
	// 機器と同じ値に戻したら、書かなくてよい
	_ = f.store.UpdateValue(v.ID, int16(1))
	if w := f.sync.PendingWrites(); len(w) != 0 {
		t.Fatalf("機器と同じ値なのに書く: %+v", w)
	}
}

func TestDeviceSync_ReadOnlyAreaIsNeverWritten(t *testing.T) {
	f := newSyncFixture(t)
	v := f.variable("v", variable.TypeINT, int16(0), "inputRegs", 0)
	b := f.variable("b", variable.TypeBOOL, false, "inputs", 0)
	f.sync.ApplyRead(ReadBlock{Area: "inputRegs", Count: 1}, []uint16{9}, nil, time.Now())
	f.sync.ApplyRead(ReadBlock{Area: "inputs", Count: 1, IsBit: true}, nil, []bool{true}, time.Now())
	if f.value(v.ID) != int16(9) || f.value(b.ID) != true {
		t.Fatalf("読めていない: %v %v", f.value(v.ID), f.value(b.ID))
	}
	_ = f.store.UpdateValue(v.ID, int16(1))
	_ = f.store.UpdateValue(b.ID, false)
	if w := f.sync.PendingWrites(); len(w) != 0 {
		t.Fatalf("読むだけのエリアに書く: %+v", w)
	}
	// 機器の値が変わっていなくても、次の読み取りで機器の値に戻す
	f.sync.ApplyRead(ReadBlock{Area: "inputRegs", Count: 1}, []uint16{9}, nil, time.Now())
	f.sync.ApplyRead(ReadBlock{Area: "inputs", Count: 1, IsBit: true}, nil, []bool{true}, time.Now())
	if f.value(v.ID) != int16(9) || f.value(b.ID) != true {
		t.Fatalf("機器の値に戻っていない: %v %v", f.value(v.ID), f.value(b.ID))
	}
}

func TestDeviceSync_BitsAndMultiWord(t *testing.T) {
	f := newSyncFixture(t)
	b := f.variable("b", variable.TypeBOOL, false, "coils", 3)
	d := f.variable("d", variable.TypeDINT, int32(0), "holding", 10)
	f.sync.ApplyRead(ReadBlock{Area: "coils", Address: 2, Count: 3, IsBit: true}, nil, []bool{false, true, false}, time.Now())
	f.sync.ApplyRead(ReadBlock{Area: "holding", Address: 9, Count: 4}, []uint16{1, 0x0001, 0x0002, 3}, nil, time.Now())
	if f.value(b.ID) != true {
		t.Fatalf("BOOL = %v", f.value(b.ID))
	}
	if f.value(d.ID) != int32(0x00010002) {
		t.Fatalf("DINT = %v", f.value(d.ID))
	}
	// 一部しか入っていない範囲では反映しない
	f.sync.ApplyRead(ReadBlock{Area: "holding", Address: 11, Count: 1}, []uint16{9}, nil, time.Now())
	if f.value(d.ID) != int32(0x00010002) {
		t.Fatalf("一部だけで反映した: %v", f.value(d.ID))
	}
	_ = f.store.UpdateValue(b.ID, false)
	w := f.sync.PendingWrites()
	if len(w) != 1 || !reflect.DeepEqual(w[0].Image.Bits, []bool{false}) || w[0].Image.Words != nil {
		t.Fatalf("ビットの書き込み: %+v", w)
	}
}

func TestDeviceSync_ReadFailuresBecomeCommError(t *testing.T) {
	f := newSyncFixture(t)
	v := f.variable("v", variable.TypeINT, int16(0), "holding", 0)
	block := ReadBlock{Area: "holding", Count: 1}
	f.sync.ApplyRead(block, []uint16{42}, nil, time.Now())

	f.sync.ReadFailed(block, "タイムアウト", 3)
	f.sync.ReadFailed(block, "タイムアウト", 3)
	if s := f.status(v.ID); s.State != SyncStateOK || s.Error != "タイムアウト" {
		t.Fatalf("2 回ではまだ通信エラーにしない: %+v", s)
	}
	f.sync.ReadFailed(block, "タイムアウト", 3)
	if s := f.status(v.ID); s.State != SyncStateCommError {
		t.Fatalf("3 回で通信エラー: %+v", s)
	}
	if f.value(v.ID) != int16(42) {
		t.Fatal("最後に読めた値を残す")
	}
	f.sync.ApplyRead(block, []uint16{43}, nil, time.Now())
	if s := f.status(v.ID); s.State != SyncStateOK || s.Error != "" || f.value(v.ID) != int16(43) {
		t.Fatalf("読めたら戻る: %+v", s)
	}
}

// マッピングを変えたら（アドレスが変わったら）、機器から読み直すまで書かない
func TestDeviceSync_MappingChangeResets(t *testing.T) {
	f := newSyncFixture(t)
	v := f.variable("v", variable.TypeINT, int16(0), "holding", 0)
	f.sync.ApplyRead(ReadBlock{Area: "holding", Count: 1}, []uint16{1}, nil, time.Now())
	_ = f.store.SetMappings(v.ID, []variable.ProtocolMapping{{ProtocolType: syncKey, MemoryArea: "holding", Address: 5, Endianness: "big"}})
	_ = f.store.UpdateValue(v.ID, int16(9))
	if w := f.sync.PendingWrites(); len(w) != 0 {
		t.Fatalf("新しいアドレスを読む前に書いた: %+v", w)
	}
	// マッピングを外すと、状態も消える
	_ = f.store.SetMappings(v.ID, nil)
	if _, ok := f.sync.Statuses()[v.ID]; ok {
		t.Fatal("マッピングを外した変数の状態が残っている")
	}
}

// 変数の変更と同期が同時に起きても競合しない（-race で確認）
func TestDeviceSync_Concurrent(t *testing.T) {
	f := newSyncFixture(t)
	v := f.variable("v", variable.TypeINT, int16(0), "holding", 0)
	block := ReadBlock{Area: "holding", Count: 1}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = f.store.UpdateValue(v.ID, int16(i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			f.sync.ApplyRead(block, []uint16{uint16(i)}, nil, time.Now())
			for _, w := range f.sync.PendingWrites() {
				f.sync.WriteDone(w)
			}
			f.sync.ReadFailed(block, "x", 3)
			_ = f.sync.Statuses()
			_ = f.sync.PlanReads()
		}
	}()
	wg.Wait()
}
