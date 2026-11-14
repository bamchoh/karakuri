package sparse

import (
	"reflect"
	"testing"
)

func TestWords_AllocatesOnlyWrittenPages(t *testing.T) {
	w := NewWords(1 << 24)
	if w.Len() != 1<<24 || w.Allocated() != 0 {
		t.Fatalf("作った直後: Len = %d, Allocated = %d", w.Len(), w.Allocated())
	}

	// 読み出しと 0 の書き込みでは確保しない
	if got := w.Read(1<<24-10, 10); !reflect.DeepEqual(got, make([]uint16, 10)) {
		t.Errorf("書いていないところ = %v", got)
	}
	w.Set(12345, 0)
	w.SetBit(999999, false)
	w.Write(5000, []uint16{0, 0, 0})
	if w.Allocated() != 0 {
		t.Fatalf("読み出しと 0 の書き込みで %d ワード確保した", w.Allocated())
	}

	// 書いたページだけを確保する。同じページなら増えない
	w.Set(100, 0x1234)
	w.Set(101, 0xFFFF)
	if w.Allocated() != PageWords {
		t.Errorf("1 ページに書いたあと = %d ワード, want %d", w.Allocated(), PageWords)
	}
	// 最後のワード
	w.Set(1<<24-1, 7)
	if w.Get(1<<24-1) != 7 || w.Allocated() != PageWords*2 {
		t.Errorf("最後のワード = %d, Allocated = %d", w.Get(1<<24-1), w.Allocated())
	}

	// ページの境目をまたぐ書き込み
	w.Write(PageWords*3-1, []uint16{0xAAAA, 0xBBBB})
	if got := w.Read(PageWords*3-2, 4); !reflect.DeepEqual(got, []uint16{0, 0xAAAA, 0xBBBB, 0}) {
		t.Errorf("境目の前後 = %04X", got)
	}

	// 確保したページに 0 を書けば、0 に戻る
	w.Set(100, 0)
	if w.Get(100) != 0 || w.Get(101) != 0xFFFF {
		t.Errorf("0 を書いたあと: %04X %04X", w.Get(100), w.Get(101))
	}
}

func TestWords_Bits(t *testing.T) {
	w := NewBits(100)
	if w.Len() != 7 {
		t.Errorf("100 ビット分のワード数 = %d, want 7", w.Len())
	}
	w.SetBit(5, true)
	w.SetBit(16, true)
	w.SetBit(31, true)
	if w.Get(0) != 0x0020 || w.Get(1) != 0x8001 {
		t.Errorf("ワード = %04X %04X, want 0020 8001", w.Get(0), w.Get(1))
	}
	if got := w.ReadBits(4, 3); !reflect.DeepEqual(got, []bool{false, true, false}) {
		t.Errorf("ReadBits = %v", got)
	}
	w.WriteBits(4, []bool{true, false, true})
	if w.Get(0) != 0x0050 {
		t.Errorf("WriteBits のあと = %04X, want 0050", w.Get(0))
	}

	// ワードの境目に合っていないビットから、ワードとして読み書きする
	w.WriteWordsAtBit(20, []uint16{0xF00F, 0x0001})
	if got := w.ReadWordsAtBit(20, 2); !reflect.DeepEqual(got, []uint16{0xF00F, 0x0001}) {
		t.Errorf("ReadWordsAtBit = %04X", got)
	}
	if !w.Bit(20) || w.Bit(24) || !w.Bit(35) || !w.Bit(36) || w.Bit(37) {
		t.Error("WriteWordsAtBit のビットの位置が違う")
	}
	// 前後のビットは変わらない
	if !w.Bit(16) || w.Bit(19) {
		t.Error("前のビットが変わった")
	}
}
