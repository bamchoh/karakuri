package adapter

import (
	"fmt"
	"sync"
	"testing"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
)

// 同じメモリを指すエリア（MemoryArea.BitsOf: ワードのエリアと、それをビットごとに見るエリア）の間で、
// 片方に書かれたら、もう片方にマッピングした変数に反映することを確認する。

// sharedDataStore は、ワードのエリア "W" と、同じメモリをビットごとに見る "W_BIT"、
// どちらとも関係のないワードのエリア "X" を持つ DataStore
type sharedDataStore struct {
	mu sync.Mutex
	w  []uint16
	x  []uint16
}

func newSharedDataStore() *sharedDataStore {
	return &sharedDataStore{w: make([]uint16, 16), x: make([]uint16, 16)}
}

func (d *sharedDataStore) GetAreas() []protocol.MemoryArea {
	return []protocol.MemoryArea{
		{ID: "W", Size: 16},
		{ID: "X", Size: 16},
		{ID: "W_BIT", IsBit: true, Size: 16 * 16, BitsOf: "W"},
	}
}

func (d *sharedDataStore) regs(area string) ([]uint16, error) {
	switch area {
	case "W", "W_BIT":
		return d.w, nil
	case "X":
		return d.x, nil
	}
	return nil, fmt.Errorf("no area %s", area)
}

func (d *sharedDataStore) ReadBits(area string, addr uint32, count uint16) ([]bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	regs, err := d.regs(area)
	if err != nil {
		return nil, err
	}
	out := make([]bool, count)
	for i := range out {
		n := addr + uint32(i)
		if int(n/16) >= len(regs) {
			return nil, fmt.Errorf("out of range")
		}
		out[i] = regs[n/16]&(1<<(n%16)) != 0
	}
	return out, nil
}

func (d *sharedDataStore) WriteBits(area string, addr uint32, values []bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	regs, err := d.regs(area)
	if err != nil {
		return err
	}
	for i, v := range values {
		n := addr + uint32(i)
		if int(n/16) >= len(regs) {
			return fmt.Errorf("out of range")
		}
		if v {
			regs[n/16] |= 1 << (n % 16)
		} else {
			regs[n/16] &^= 1 << (n % 16)
		}
	}
	return nil
}

func (d *sharedDataStore) ReadWords(area string, addr uint32, count uint16) ([]uint16, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	regs, err := d.regs(area)
	if err != nil {
		return nil, err
	}
	if int(addr)+int(count) > len(regs) {
		return nil, fmt.Errorf("out of range")
	}
	return append([]uint16(nil), regs[addr:int(addr)+int(count)]...), nil
}

func (d *sharedDataStore) WriteWords(area string, addr uint32, values []uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	regs, err := d.regs(area)
	if err != nil {
		return err
	}
	if int(addr)+len(values) > len(regs) {
		return fmt.Errorf("out of range")
	}
	copy(regs[addr:], values)
	return nil
}

func (d *sharedDataStore) ReadBit(area string, addr uint32) (bool, error) {
	bits, err := d.ReadBits(area, addr, 1)
	if err != nil {
		return false, err
	}
	return bits[0], nil
}

func (d *sharedDataStore) WriteBit(area string, addr uint32, v bool) error {
	return d.WriteBits(area, addr, []bool{v})
}

func (d *sharedDataStore) ReadWord(area string, addr uint32) (uint16, error) {
	words, err := d.ReadWords(area, addr, 1)
	if err != nil {
		return 0, err
	}
	return words[0], nil
}

func (d *sharedDataStore) WriteWord(area string, addr uint32, v uint16) error {
	return d.WriteWords(area, addr, []uint16{v})
}

type sharedFixture struct {
	t     *testing.T
	store *variable.VariableStore
	ds    *VariableBackedDataStore
}

func newSharedFixture(t *testing.T) *sharedFixture {
	t.Helper()
	store := variable.NewVariableStore()
	ds := NewVariableBackedDataStore(newSharedDataStore(), store, "test")
	t.Cleanup(ds.Detach)
	return &sharedFixture{t: t, store: store, ds: ds}
}

// variable は変数を作り、area の address にマッピングして、ID を返す
func (f *sharedFixture) variable(name string, dt variable.DataType, value interface{}, area string, address uint32) string {
	f.t.Helper()
	v, err := f.store.CreateVariable(name, dt, value)
	if err != nil {
		f.t.Fatal(err)
	}
	err = f.store.SetMappings(v.ID, []variable.ProtocolMapping{{ProtocolType: "test", MemoryArea: area, Address: address, Endianness: "big"}})
	if err != nil {
		f.t.Fatal(err)
	}
	return v.ID
}

func (f *sharedFixture) value(id string) interface{} {
	f.t.Helper()
	v, err := f.store.GetVariable(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return v.Value
}

func (f *sharedFixture) expect(what, id string, want interface{}) {
	f.t.Helper()
	if got := f.value(id); got != want {
		f.t.Errorf("%s: 値 = %v (%T), want %v (%T)", what, got, got, want, want)
	}
}

// レジスタへの書き込み（クライアント・画面・スクリプトから）が、同じメモリを見ているもう片方の変数に反映される
func TestSharedAreas_RegisterWrites(t *testing.T) {
	f := newSharedFixture(t)
	word := f.variable("Word", variable.TypeINT, int16(0), "W", 2)
	bit := f.variable("Bit", variable.TypeBOOL, false, "W_BIT", 2*16+5)
	other := f.variable("Other", variable.TypeBOOL, false, "W_BIT", 3*16+5)

	// ワードで書く → 同じ場所のビットの変数
	if err := f.ds.WriteWord("W", 2, 0x0020); err != nil {
		t.Fatal(err)
	}
	f.expect("ワードで書いたあとのビット", bit, true)
	f.expect("ワードで書いたあとのワード", word, int16(0x20))
	f.expect("重ならない場所のビット", other, false)

	// ビットで書く → 同じ場所のワードの変数
	if err := f.ds.WriteBit("W_BIT", 2*16+0, true); err != nil {
		t.Fatal(err)
	}
	f.expect("ビットで書いたあとのワード", word, int16(0x21))
	if err := f.ds.WriteBits("W_BIT", 2*16+4, []bool{true, false}); err != nil {
		t.Fatal(err)
	}
	f.expect("複数のビットで書いたあとのワード", word, int16(0x11))
	f.expect("複数のビットで書いたあとのビット", bit, false)

	// 複数のワードをまとめて書く → 範囲に入るビットの変数は、どれも反映される
	if err := f.ds.WriteWords("W", 2, []uint16{0x0020, 0x0020}); err != nil {
		t.Fatal(err)
	}
	f.expect("まとめて書いたあとのビット", bit, true)
	f.expect("まとめて書いたあとの、次のワードのビット", other, true)

	// 重ならない場所への書き込みでは、変わらない
	if err := f.ds.WriteWord("W", 4, 0xFFFF); err != nil {
		t.Fatal(err)
	}
	f.expect("別のワードを書いたあとのワード", word, int16(0x20))
	f.expect("別のワードを書いたあとのビット", bit, true)
}

// 変数を変えたときも、同じメモリを見ているもう片方の変数に反映される
func TestSharedAreas_VariableChanges(t *testing.T) {
	f := newSharedFixture(t)
	word := f.variable("Word", variable.TypeINT, int16(0), "W", 2)
	bit := f.variable("Bit", variable.TypeBOOL, false, "W_BIT", 2*16+5)

	if err := f.store.UpdateValue(bit, true); err != nil {
		t.Fatal(err)
	}
	f.expect("ビットの変数を ON にしたあとのワード", word, int16(0x20))
	if w, _ := f.ds.ReadWord("W", 2); w != 0x20 {
		t.Errorf("メモリ = %04X, want 0020", w)
	}

	if err := f.store.UpdateValue(word, int16(0)); err != nil {
		t.Fatal(err)
	}
	f.expect("ワードの変数を 0 にしたあとのビット", bit, false)

	// 行き来しない（片方を変えたら、もう片方が 1 回変わるだけ）
	if err := f.store.UpdateValue(word, int16(0x7FFF)); err != nil {
		t.Fatal(err)
	}
	f.expect("ワードの変数を変えたあとのワード", word, int16(0x7FFF))
	f.expect("ワードの変数を変えたあとのビット", bit, true)
}

// 2 ワードの変数は、どちらのワードのビットとも重なる
func TestSharedAreas_MultiWordVariable(t *testing.T) {
	f := newSharedFixture(t)
	big := f.variable("Big", variable.TypeDINT, int32(0), "W", 4)
	hi := f.variable("Hi", variable.TypeBOOL, false, "W_BIT", 4*16+0) // 上位ワード（ビッグエンディアン）のビット 0
	lo := f.variable("Lo", variable.TypeBOOL, false, "W_BIT", 5*16+0) // 下位ワードのビット 0
	next := f.variable("Next", variable.TypeBOOL, false, "W_BIT", 6*16+0)

	if err := f.store.UpdateValue(big, int32(0x00010001)); err != nil {
		t.Fatal(err)
	}
	f.expect("上位ワードのビット", hi, true)
	f.expect("下位ワードのビット", lo, true)
	f.expect("隣のワードのビット", next, false)

	// 下位ワードのビットだけを消す → 2 ワードの変数全体が読み直される
	if err := f.ds.WriteBit("W_BIT", 5*16+0, false); err != nil {
		t.Fatal(err)
	}
	f.expect("ビットを消したあとの DINT", big, int32(0x00010000))
}

// 同じメモリを指すエリアを持たないプロトコル・関係のないエリアでは、これまでどおり
func TestSharedAreas_UnrelatedAreas(t *testing.T) {
	f := newSharedFixture(t)
	x := f.variable("X", variable.TypeINT, int16(7), "X", 2)
	bit := f.variable("Bit", variable.TypeBOOL, false, "W_BIT", 2*16+0)

	// "X" は "W" と別のメモリ。同じアドレスに書いても、W のビットは変わらない
	if err := f.ds.WriteWord("X", 2, 1); err != nil {
		t.Fatal(err)
	}
	f.expect("X に書いたあとの X", x, int16(1))
	f.expect("X に書いたあとの W のビット", bit, false)

	// BitsOf のない DataStore（Modbus など）では、何も変わらない
	plain := NewVariableBackedDataStore(newTestDataStore(), f.store, "plain")
	t.Cleanup(plain.Detach)
	if len(plain.sharedAreas("holding")) != 0 || len(plain.sharedAreas("coils")) != 0 {
		t.Error("BitsOf のないエリアに、同じメモリを指すエリアがある")
	}
	if got := f.ds.sharedAreas("W"); len(got) != 1 || got[0] != "W_BIT" {
		t.Errorf("W と同じメモリを指すエリア = %v", got)
	}
	if got := f.ds.sharedAreas("W_BIT"); len(got) != 1 || got[0] != "W" {
		t.Errorf("W_BIT と同じメモリを指すエリア = %v", got)
	}
	if got := f.ds.sharedAreas("X"); len(got) != 0 {
		t.Errorf("X と同じメモリを指すエリア = %v", got)
	}
}
