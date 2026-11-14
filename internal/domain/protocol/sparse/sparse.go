// Package sparse は、大きいメモリエリアを、使ったところだけ確保して持つための入れもの。
//
// プロトコルのメモリエリアは、プロトコルで指せる最大の大きさにする（FINS は 1 エリア 65536 ワード、
// SLMP は 1 デバイス 16,777,216 点）。ほとんどは使われないので、最初は何も確保せず、
// 0 以外の値が書かれたページだけを確保する（遅延確保）。プロトコルの DataStore の実装で共通に使う。
package sparse

// PageWords は、メモリを確保する単位（ワード数）
const PageWords = 1024

// Words は、ワードの並び（すべて 0 から始まる）。
//
//   - 確保していないページは、0 として読める
//   - 読み出しと、0 の書き込みでは確保しない（クライアントがエリアを端から端まで読んでも、メモリは増えない）
//   - ビットは、16 個を 1 ワードに詰めて持つ（ビット n は、ワード n/16 のビット n%16）
//
// 同時に使うときの排他は、使う側が行う。範囲の検査も使う側が行う（範囲外の番号を渡さない）
type Words struct {
	size  int
	pages map[uint32][]uint16
}

// NewWords は、size ワードの並びを作る
func NewWords(size int) *Words {
	return &Words{size: size, pages: make(map[uint32][]uint16)}
}

// NewBits は、size ビットを入れられる並びを作る
func NewBits(size int) *Words {
	return NewWords((size + 15) / 16)
}

// Len はワード数を返す
func (w *Words) Len() int { return w.size }

// Allocated は、確保しているワード数を返す
func (w *Words) Allocated() int {
	return len(w.pages) * PageWords
}

// Get は i 番目のワードを返す
func (w *Words) Get(i uint32) uint16 {
	page := w.pages[i/PageWords]
	if page == nil {
		return 0
	}
	return page[i%PageWords]
}

// Set は i 番目のワードを書く
func (w *Words) Set(i uint32, v uint16) {
	page := w.pages[i/PageWords]
	if page == nil {
		if v == 0 {
			return
		}
		page = make([]uint16, PageWords)
		w.pages[i/PageWords] = page
	}
	page[i%PageWords] = v
}

// Bit は n 番目のビットを返す
func (w *Words) Bit(n uint32) bool {
	return w.Get(n/16)&(1<<(n%16)) != 0
}

// SetBit は n 番目のビットを書く
func (w *Words) SetBit(n uint32, v bool) {
	word := w.Get(n / 16)
	if v {
		word |= 1 << (n % 16)
	} else {
		word &^= 1 << (n % 16)
	}
	w.Set(n/16, word)
}

// Read は i 番目から count ワードを返す
func (w *Words) Read(i uint32, count int) []uint16 {
	out := make([]uint16, count)
	for k := range out {
		out[k] = w.Get(i + uint32(k))
	}
	return out
}

// Write は i 番目からワードを書く
func (w *Words) Write(i uint32, values []uint16) {
	for k, v := range values {
		w.Set(i+uint32(k), v)
	}
}

// ReadBits は n 番目のビットから count 個を返す
func (w *Words) ReadBits(n uint32, count int) []bool {
	out := make([]bool, count)
	for k := range out {
		out[k] = w.Bit(n + uint32(k))
	}
	return out
}

// WriteBits は n 番目のビットから書く
func (w *Words) WriteBits(n uint32, values []bool) {
	for k, v := range values {
		w.SetBit(n+uint32(k), v)
	}
}

// ReadWordsAtBit は、n 番目のビットから 16 ビットずつを、count 個のワードにまとめて返す（最初のビットが、ワードのビット 0）
func (w *Words) ReadWordsAtBit(n uint32, count int) []uint16 {
	out := make([]uint16, count)
	for k := range out {
		for b := uint32(0); b < 16; b++ {
			if w.Bit(n + uint32(k)*16 + b) {
				out[k] |= 1 << b
			}
		}
	}
	return out
}

// WriteWordsAtBit は、ワードを 16 ビットずつに分けて、n 番目のビットから書く
func (w *Words) WriteWordsAtBit(n uint32, values []uint16) {
	for k, v := range values {
		for b := uint32(0); b < 16; b++ {
			w.SetBit(n+uint32(k)*16+b, v&(1<<b) != 0)
		}
	}
}
