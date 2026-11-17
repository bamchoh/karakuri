package adapter

import (
	"fmt"
	"testing"

	"karakuri/internal/domain/variable"
)

// レジスタ → 変数の同期は、マッピングの索引（mappingsIn）を使う。
// マッピング・型・変数の追加と削除のあとに、古い索引のまま同期しない
func TestVariableBackedDataStore_IndexFollowsLayoutChanges(t *testing.T) {
	store := variable.NewVariableStore()
	ds := NewVariableBackedDataStore(newTestDataStore(), store, "p")
	v, err := store.CreateVariable("A", "INT", nil)
	if err != nil {
		t.Fatal(err)
	}
	value := func() interface{} {
		got, err := store.GetVariable(v.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.Value
	}
	mapTo := func(addr uint32) {
		t.Helper()
		if err := store.SetMappings(v.ID, []variable.ProtocolMapping{{ProtocolType: "p", MemoryArea: "holding", Address: addr, Endianness: "big"}}); err != nil {
			t.Fatal(err)
		}
	}

	mapTo(0)
	_ = ds.WriteWords("holding", 0, []uint16{1})
	if got := value(); got != int16(1) {
		t.Fatalf("アドレス 0 に書いたあと A = %#v, want 1", got)
	}

	// マッピングを動かしたら、古いアドレスへの書き込みでは変わらない
	mapTo(50)
	_ = ds.WriteWords("holding", 0, []uint16{2})
	if got := value(); got != int16(1) {
		t.Fatalf("マッピングを動かしたあと、古いアドレスへの書き込みで A = %#v, want 1（変わらない）", got)
	}
	_ = ds.WriteWords("holding", 50, []uint16{3})
	if got := value(); got != int16(3) {
		t.Fatalf("新しいアドレス 50 に書いたあと A = %#v, want 3", got)
	}

	// 型を DINT（2 ワード）に変えたら、2 ワード目への書き込みでも変わる
	// （型を変えると値は 0 に戻り、レジスタ 50・51 にも 0 が書かれる）
	if _, err := store.UpdateMetadata(v.ID, "A", "DINT"); err != nil {
		t.Fatal(err)
	}
	_ = ds.WriteWords("holding", 51, []uint16{7})
	if got := value(); got != int32(7) {
		t.Fatalf("DINT にして 2 ワード目に書いたあと A = %#v, want 7", got)
	}

	// 変数を消したら、書き込んでも何も起きない（消えた変数を引かない）
	if err := store.DeleteVariable(v.ID); err != nil {
		t.Fatal(err)
	}
	_ = ds.WriteWords("holding", 50, []uint16{9, 9})
	b, err := store.CreateVariable("B", "INT", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetMappings(b.ID, []variable.ProtocolMapping{{ProtocolType: "p", MemoryArea: "holding", Address: 60, Endianness: "big"}}); err != nil {
		t.Fatal(err)
	}
	_ = ds.WriteWords("holding", 60, []uint16{4})
	if got, _ := store.GetVariable(b.ID); got.Value != int16(4) {
		t.Fatalf("あとから足した B = %#v, want 4", got.Value)
	}
}

// レジスタへの書き込みは、変数の数に比例する時間で終わる（以前は、書いたアドレスごと・変わった変数ごとに
// 全部のマッピングを探していて、変数の数の 2 乗で遅くなった）。n=100 と n=1000 で、1 変数あたりの時間がほぼ同じになる
func BenchmarkVariableBackedDataStore_WriteWords(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("vars=%d", n), func(b *testing.B) {
			store := variable.NewVariableStore()
			ds := NewVariableBackedDataStore(newTestDataStore(), store, "p")
			other := NewVariableBackedDataStore(newSharedLargeDataStore(), store, "q") // 変数の変更が届く、もう 1 つのサーバー
			defer ds.Detach()
			defer other.Detach()
			for i := range n {
				v, err := store.CreateVariable(fmt.Sprintf("V%d", i), "INT", nil)
				if err != nil {
					b.Fatal(err)
				}
				if err := store.SetMappings(v.ID, []variable.ProtocolMapping{
					{ProtocolType: "p", MemoryArea: "holding", Address: uint32(i), Endianness: "big"},
					{ProtocolType: "q", MemoryArea: "W", Address: uint32(i), Endianness: "big"},
				}); err != nil {
					b.Fatal(err)
				}
			}
			values := make([]uint16, n)
			b.ResetTimer()
			for i := range b.N {
				for j := range values {
					values[j] = uint16(i + j)
				}
				_ = ds.WriteWords("holding", 0, values)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/var")
		})
	}
}

// newSharedLargeDataStore は、ワードのエリア W と、同じメモリをビットで見る W_BIT を持つ、大きい DataStore
func newSharedLargeDataStore() *sharedDataStore {
	return &sharedDataStore{w: make([]uint16, 4096), x: make([]uint16, 16)}
}
