package adapter

import (
	"fmt"
	"sync"
	"testing"

	"karakuri/internal/domain/variable"
)

// 構造体型の登録・削除と、構造体の変数への読み書き（レジスタ側・変数側）を同時に行っても、
// 競合しない（CI は競合検出を有効にして実行する）。
//
// 構造体の変数をレジスタと同期するときは、型の定義を引く。型の一覧を書き換えている最中に
// ロックを取らずに引くと、競合する。
func TestVariableBackedDataStore_StructTypeChangesDuringSync(t *testing.T) {
	adapter, inner, varStore := setupAdapter("test")

	point, err := variable.NewStructTypeDef("Point", []variable.StructField{
		{Name: "x", DataType: variable.TypeINT},
		{Name: "y", DataType: variable.TypeINT},
	}, varStore.TypeResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := varStore.RegisterStructType(point); err != nil {
		t.Fatal(err)
	}
	p, err := varStore.CreateVariable("p", "Point", nil)
	if err != nil {
		t.Fatal(err)
	}
	arr, err := varStore.CreateVariable("arr", "ARRAY[0..1] OF Point", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = varStore.SetMappings(p.ID, []variable.ProtocolMapping{{ProtocolType: "test", MemoryArea: "holding", Address: 10, Endianness: "big"}})
	_ = varStore.SetMappings(arr.ID, []variable.ProtocolMapping{{ProtocolType: "test", MemoryArea: "holding", Address: 20, Endianness: "big"}})

	const rounds = 300
	var wg sync.WaitGroup
	wg.Add(4)
	// 関係のない構造体型を、登録しては削除する
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			name := fmt.Sprintf("Tmp%d", i%8)
			def, err := variable.NewStructTypeDef(name, []variable.StructField{
				{Name: "inner", DataType: "Point"},
				{Name: "n", DataType: variable.TypeDINT},
			}, varStore.TypeResolver())
			if err != nil {
				t.Errorf("NewStructTypeDef: %v", err)
				return
			}
			_ = varStore.RegisterStructType(def)
			_ = varStore.DeleteStructType(name)
		}
	}()
	// レジスタ側から構造体の変数に書く（型の定義を引いて、レジスタの値を変数の値にする）
	go func() {
		defer wg.Done()
		for i := 1; i <= rounds; i++ {
			_ = adapter.WriteWords("holding", 10, []uint16{uint16(i), uint16(i + 1)})
			_ = adapter.WriteWord("holding", 22, uint16(i))
		}
	}()
	// 変数側から構造体の変数に書く（型の定義を引いて、変数の値をレジスタの値にする）
	go func() {
		defer wg.Done()
		for i := 1; i <= rounds; i++ {
			_ = varStore.UpdateFieldValue(arr.ID, "[0].x", float64(i))
		}
	}()
	// 構造体型の一覧と定義を読む
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			_ = varStore.GetAllStructTypes()
			_, _ = varStore.GetStructType("Point")
		}
	}()
	wg.Wait()

	// 最後に書いた値が、変数とレジスタの両方に入っている
	gotP, _ := varStore.GetVariable(p.ID)
	if m := gotP.Value.(map[string]interface{}); m["x"] != int16(rounds) || m["y"] != int16(rounds+1) {
		t.Errorf("p = %v, want x=%d y=%d", m, rounds, rounds+1)
	}
	gotArr, _ := varStore.GetVariable(arr.ID)
	elems := gotArr.Value.([]interface{})
	if e0 := elems[0].(map[string]interface{}); e0["x"] != int16(rounds) {
		t.Errorf("arr[0].x = %v, want %d", e0["x"], rounds)
	}
	if e1 := elems[1].(map[string]interface{}); e1["x"] != int16(rounds) {
		t.Errorf("arr[1].x = %v, want %d", e1["x"], rounds)
	}
	if w, _ := inner.ReadWord("holding", 20); w != rounds {
		t.Errorf("arr[0].x のレジスタ = %d, want %d", w, rounds)
	}
}
