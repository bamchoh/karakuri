package fins

import "testing"

// メモリは、0 以外の値が書かれたページだけを確保する
func TestDataStore_AllocatesOnWrite(t *testing.T) {
	s := NewDataStore()
	if n := s.allocatedWords(); n != 0 {
		t.Fatalf("作った直後に %d ワード確保している", n)
	}

	// 全エリアを端から端まで読んでも、0 を書いても、確保しない
	for _, a := range areaDefs {
		for addr := 0; addr < a.Words; addr += 1000 {
			count := min(1000, a.Words-addr)
			words, err := s.ReadWords(a.ID, uint32(addr), uint16(count))
			if err != nil {
				t.Fatalf("%s %d: %v", a.ID, addr, err)
			}
			for _, w := range words {
				if w != 0 {
					t.Fatalf("%s %d: 0 でない値が読めた", a.ID, addr)
				}
			}
		}
		if err := s.WriteWords(a.ID, uint32(a.Words-2), []uint16{0, 0}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.WriteBit(AreaCIO+bitSuffix, 65535*16+15, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := s.ReadBit(AreaT+flagSuffix, 32767); b {
		t.Error("書いていないフラグが ON")
	}
	if n := s.allocatedWords(); n != 0 {
		t.Fatalf("読み出しと 0 の書き込みで %d ワード確保した", n)
	}

	// 書いたページだけを確保する
	if err := s.WriteWord(AreaDM, 100, 0x1234); err != nil {
		t.Fatal(err)
	}
	if n := s.allocatedWords(); n != pageWords {
		t.Errorf("1 ワード書いたあと = %d ワード, want %d", n, pageWords)
	}
	// 同じページへの書き込みでは増えない。ビットの書き込みも同じメモリ
	_ = s.WriteWord(AreaDM, 101, 0xFFFF)
	_ = s.WriteBit(AreaDM+bitSuffix, 102*16+3, true)
	if n := s.allocatedWords(); n != pageWords {
		t.Errorf("同じページに書いたあと = %d ワード, want %d", n, pageWords)
	}
	if w, _ := s.ReadWord(AreaDM, 102); w != 0x0008 {
		t.Errorf("D102 = %04X, want 0008", w)
	}

	// ページの境目をまたぐ書き込みは、両方のページを確保し、値は続けて読める
	if err := s.WriteWords("EM18", pageWords-1, []uint16{0xAAAA, 0xBBBB}); err != nil {
		t.Fatal(err)
	}
	if n := s.allocatedWords(); n != pageWords*3 {
		t.Errorf("境目をまたいで書いたあと = %d ワード, want %d", n, pageWords*3)
	}
	if words, _ := s.ReadWords("EM18", pageWords-2, 4); words[0] != 0 || words[1] != 0xAAAA || words[2] != 0xBBBB || words[3] != 0 {
		t.Errorf("境目の前後 = %04X", words)
	}

	// 確保したページに 0 を書けば、0 に戻る（ページは持ったまま）
	_ = s.WriteWord(AreaDM, 100, 0)
	if w, _ := s.ReadWord(AreaDM, 100); w != 0 {
		t.Errorf("D100 = %04X, want 0", w)
	}

	// 最後のワード・最後のビット・最後のフラグ
	_ = s.WriteWord(AreaCIO, 65535, 0x8000)
	if b, _ := s.ReadBit(AreaCIO+bitSuffix, 65535*16+15); !b {
		t.Error("CIO 65535.15 が ON になっていない")
	}
	_ = s.WriteBit(AreaC+flagSuffix, 32767, true)
	if b, _ := s.ReadBit(AreaC+flagSuffix, 32767); !b {
		t.Error("C32767 のアップフラグが ON になっていない")
	}
	if _, err := s.ReadWords(AreaCIO, 65535, 2); err == nil {
		t.Error("範囲外を読めた")
	}
}
