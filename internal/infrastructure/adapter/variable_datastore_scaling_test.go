package adapter

import (
	"fmt"
	"testing"

	"karakuri/internal/domain/variable"
)

// レジスタ → 変数の同期の量を、時間ではなく回数で確かめる（CI の実行環境は速さがぶれるので、時間では比べない）。
// 以前は、書き込みのたびに全部のマッピングを探していて、変数の数の 2 乗で遅くなった。
// その作りに戻ると、ここで数えるマッピングの数が変数の数に比例して増える
func TestVariableBackedDataStore_SyncWorkDoesNotGrowWithVariables(t *testing.T) {
	const n = 1000
	store := variable.NewVariableStore()
	ds := NewVariableBackedDataStore(newTestDataStore(), store, "p")
	shared := NewVariableBackedDataStore(newSharedLargeDataStore(), store, "q") // 同じメモリを指すエリアを持つ、もう 1 つのサーバー
	defer ds.Detach()
	defer shared.Detach()
	ids := make([]string, n)
	for i := range n {
		v, err := store.CreateVariable(fmt.Sprintf("V%d", i), "INT", nil)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = v.ID
		if err := store.SetMappings(v.ID, []variable.ProtocolMapping{
			{ProtocolType: "p", MemoryArea: "holding", Address: uint32(i), Endianness: "big"},
			{ProtocolType: "q", MemoryArea: "W", Address: uint32(i), Endianness: "big"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// 大きい変数（構造体くらいの大きさ）が 1 つあっても、ほかの書き込みで調べる数は増えすぎない
	big, err := store.CreateVariable("Big", "ARRAY[0..19] OF INT", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetMappings(big.ID, []variable.ProtocolMapping{{ProtocolType: "p", MemoryArea: "holding", Address: 2000, Endianness: "big"}}); err != nil {
		t.Fatal(err)
	}

	measure := func(a *VariableBackedDataStore, do func()) (checked, builds uint64) {
		c0, b0 := a.stats.checked.Load(), a.stats.indexBuilds.Load()
		do()
		return a.stats.checked.Load() - c0, a.stats.indexBuilds.Load() - b0
	}
	_ = ds.WriteWord("holding", 0, 0) // 索引を作っておく

	// 1 ワード書いたら、調べるのは書いたところの近くだけ（大きい変数の長さ 20 ワードの分まで）
	checked, builds := measure(ds, func() { _ = ds.WriteWord("holding", 500, 5) })
	if checked > 21 {
		t.Errorf("変数 %d 個のとき、1 ワードの書き込みで %d 個のマッピングを調べた（want 21 以下）", n, checked)
	}
	if builds != 0 {
		t.Errorf("値を書いただけで、索引を %d 回作り直した（want 0）", builds)
	}
	if v, _ := store.GetVariable(ids[500]); v.Value != int16(5) {
		t.Fatalf("V500 = %#v, want 5", v.Value)
	}

	// n ワードまとめて書いたら、調べるのは n 個の近く（書いたアドレスごとに全部を探さない）
	values := make([]uint16, n)
	for i := range values {
		values[i] = uint16(i + 1)
	}
	checked, _ = measure(ds, func() { _ = ds.WriteWords("holding", 0, values) })
	if checked > n+21 {
		t.Errorf("%d ワードの書き込みで %d 個のマッピングを調べた（want %d 以下）", n, checked, n+21)
	}

	// 変数を 1 つ書いたら、もう 1 つのサーバーで調べるのは、同じメモリを指すエリアの近くだけ
	checked, builds = measure(shared, func() {
		if err := store.UpdateValue(ids[700], int16(-7)); err != nil {
			t.Fatal(err)
		}
	})
	if checked > 21 {
		t.Errorf("変数を 1 つ書いて、もう 1 つのサーバーで %d 個のマッピングを調べた（want 21 以下）", checked)
	}
	if builds > 1 { // まだ索引がなければ、最初の 1 回だけ作る
		t.Errorf("変数を 1 つ書いて、索引を %d 回作った（want 1 以下）", builds)
	}

	// マッピングを変えたら、次の書き込みで 1 回だけ作り直す
	if err := store.SetMappings(ids[0], []variable.ProtocolMapping{{ProtocolType: "p", MemoryArea: "holding", Address: 3000, Endianness: "big"}}); err != nil {
		t.Fatal(err)
	}
	_, builds = measure(ds, func() {
		_ = ds.WriteWord("holding", 3000, 1)
		_ = ds.WriteWord("holding", 3000, 2)
	})
	if builds != 1 {
		t.Errorf("マッピングを変えたあと、2 回の書き込みで索引を %d 回作った（want 1）", builds)
	}
	if v, _ := store.GetVariable(ids[0]); v.Value != int16(2) {
		t.Fatalf("動かした V0 = %#v, want 2", v.Value)
	}
}

// 範囲の始まりより前から始まる大きい変数（配列）も、途中のワードへの書き込みで更新する
// （索引を始まりの順に並べて探すので、手前から始まるマッピングを取りこぼさない）
func TestVariableBackedDataStore_OverlapFindsEarlierLongMapping(t *testing.T) {
	store := variable.NewVariableStore()
	ds := NewVariableBackedDataStore(newTestDataStore(), store, "p")
	defer ds.Detach()
	arr, err := store.CreateVariable("Arr", "ARRAY[0..9] OF INT", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetMappings(arr.ID, []variable.ProtocolMapping{{ProtocolType: "p", MemoryArea: "holding", Address: 10, Endianness: "big"}}); err != nil {
		t.Fatal(err)
	}
	for i := range 30 { // 配列のあとに、1 ワードの変数をたくさん
		v, err := store.CreateVariable(fmt.Sprintf("S%d", i), "INT", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetMappings(v.ID, []variable.ProtocolMapping{{ProtocolType: "p", MemoryArea: "holding", Address: uint32(11 + i), Endianness: "big"}}); err != nil {
			t.Fatal(err)
		}
	}
	_ = ds.WriteWord("holding", 19, 42) // 配列の最後の要素
	v, _ := store.GetVariable(arr.ID)
	if got := v.Value.([]interface{})[9]; got != int16(42) {
		t.Fatalf("Arr[9] = %#v, want 42", got)
	}
}
