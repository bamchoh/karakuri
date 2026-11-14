package fins

import (
	"errors"
	"strings"
	"sync"

	"karakuri/internal/domain/protocol"
)

var (
	errAreaNotFound      = errors.New("area not found")
	errAddressOutOfRange = errors.New("address out of range")
)

var _ protocol.DataStore = (*DataStore)(nil)

// pageWords は、メモリを確保する単位（ワード数）
const pageWords = 1024

// region は、1 つのエリアのメモリ（ワードの並び）。
//
// エリアは FINS で指せる最大の大きさ（65536 ワード）で、EM のバンクも合わせると 30 個ある。
// ほとんどは使われないので、最初は何も確保せず、0 以外の値が書かれたページだけを確保する（遅延確保）。
// 確保していないページは、0 として読める。読み出しと、0 の書き込みでは確保しない
// （クライアントがエリアを端から端まで読んでも、メモリは増えない）
type region struct {
	size  int        // ワード数
	pages [][]uint16 // ページごとのワード。確保していないページは nil
}

func newRegion(size int) *region {
	return &region{size: size, pages: make([][]uint16, (size+pageWords-1)/pageWords)}
}

func (r *region) get(i uint32) uint16 {
	page := r.pages[i/pageWords]
	if page == nil {
		return 0
	}
	return page[i%pageWords]
}

func (r *region) set(i uint32, v uint16) {
	page := r.pages[i/pageWords]
	if page == nil {
		if v == 0 {
			return
		}
		page = make([]uint16, pageWords)
		r.pages[i/pageWords] = page
	}
	page[i%pageWords] = v
}

func (r *region) bit(n uint32) bool {
	return r.get(n/16)&(1<<(n%16)) != 0
}

func (r *region) setBit(n uint32, v bool) {
	w := r.get(n / 16)
	if v {
		w |= 1 << (n % 16)
	} else {
		w &^= 1 << (n % 16)
	}
	r.set(n/16, w)
}

// DataStore は FINS のメモリ（エリアごとのワードの並び）。
//
// ビットは、ワードのエリアの ID でもビットのエリアの ID（"CIO_BIT"）でも読み書きできる。
// どちらも同じメモリを指し、アドレスは「ワード × 16 + ビット」。
// タイマ・カウンタのアップフラグ（"T_FLAG"）は、現在値（"T"）とは別のメモリで、アドレスは番号
type DataStore struct {
	mu    sync.RWMutex
	words map[string]*region
}

// NewDataStore は、すべて 0 のメモリを作る（メモリは、値が書かれたときに確保する）
func NewDataStore() *DataStore {
	s := &DataStore{words: make(map[string]*region, len(areaDefs))}
	for _, a := range areaDefs {
		s.words[a.ID] = newRegion(a.Words)
		if a.Flags {
			// フラグは 1 ビットずつ。16 個を 1 ワードに詰めて持つ
			s.words[a.ID+flagSuffix] = newRegion((a.Words + 15) / 16)
		}
	}
	return s
}

// allocatedWords は、確保しているメモリの大きさ（ワード数）を返す（テスト用）
func (s *DataStore) allocatedWords() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, r := range s.words {
		for _, page := range r.pages {
			n += len(page)
		}
	}
	return n
}

// GetAreas は利用可能なメモリエリアの一覧を返す
func (s *DataStore) GetAreas() []protocol.MemoryArea {
	return memoryAreas()
}

// resolve はエリアの ID から、メモリと、ビットのエリア（フラグのエリアを含む）の ID だったかどうかを返す
func (s *DataStore) resolve(area string) (*region, bool, error) {
	isBit := strings.HasSuffix(area, bitSuffix) || strings.HasSuffix(area, flagSuffix)
	regs, ok := s.words[strings.TrimSuffix(area, bitSuffix)]
	if !ok {
		return nil, false, errAreaNotFound
	}
	return regs, isBit, nil
}

func (s *DataStore) ReadBit(area string, address uint32) (bool, error) {
	bits, err := s.ReadBits(area, address, 1)
	if err != nil {
		return false, err
	}
	return bits[0], nil
}

func (s *DataStore) WriteBit(area string, address uint32, value bool) error {
	return s.WriteBits(area, address, []bool{value})
}

func (s *DataStore) ReadBits(area string, address uint32, count uint16) ([]bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	regs, _, err := s.resolve(area)
	if err != nil {
		return nil, err
	}
	if uint64(address)+uint64(count) > uint64(regs.size)*16 {
		return nil, errAddressOutOfRange
	}
	bits := make([]bool, count)
	for i := range bits {
		n := address + uint32(i)
		bits[i] = regs.bit(n)
	}
	return bits, nil
}

func (s *DataStore) WriteBits(area string, address uint32, values []bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	regs, _, err := s.resolve(area)
	if err != nil {
		return err
	}
	if uint64(address)+uint64(len(values)) > uint64(regs.size)*16 {
		return errAddressOutOfRange
	}
	for i, v := range values {
		n := address + uint32(i)
		regs.setBit(n, v)
	}
	return nil
}

func (s *DataStore) ReadWord(area string, address uint32) (uint16, error) {
	words, err := s.ReadWords(area, address, 1)
	if err != nil {
		return 0, err
	}
	return words[0], nil
}

func (s *DataStore) WriteWord(area string, address uint32, value uint16) error {
	return s.WriteWords(area, address, []uint16{value})
}

// ReadWords はワードを読む。ビットのエリアの ID のときは、address のビットから 16 ビットずつをワードにまとめる
func (s *DataStore) ReadWords(area string, address uint32, count uint16) ([]uint16, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	regs, isBit, err := s.resolve(area)
	if err != nil {
		return nil, err
	}
	out := make([]uint16, count)
	if !isBit {
		if uint64(address)+uint64(count) > uint64(regs.size) {
			return nil, errAddressOutOfRange
		}
		for i := range out {
			out[i] = regs.get(address + uint32(i))
		}
		return out, nil
	}
	if uint64(address)+uint64(count)*16 > uint64(regs.size)*16 {
		return nil, errAddressOutOfRange
	}
	for i := range out {
		for b := uint32(0); b < 16; b++ {
			n := address + uint32(i)*16 + b
			if regs.bit(n) {
				out[i] |= 1 << b
			}
		}
	}
	return out, nil
}

// WriteWords はワードを書く。ビットのエリアの ID のときは、address のビットから 16 ビットずつに分けて書く
func (s *DataStore) WriteWords(area string, address uint32, values []uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	regs, isBit, err := s.resolve(area)
	if err != nil {
		return err
	}
	if !isBit {
		if uint64(address)+uint64(len(values)) > uint64(regs.size) {
			return errAddressOutOfRange
		}
		for i, v := range values {
			regs.set(address+uint32(i), v)
		}
		return nil
	}
	if uint64(address)+uint64(len(values))*16 > uint64(regs.size)*16 {
		return errAddressOutOfRange
	}
	for i, w := range values {
		for b := uint32(0); b < 16; b++ {
			n := address + uint32(i)*16 + b
			regs.setBit(n, w&(1<<b) != 0)
		}
	}
	return nil
}
