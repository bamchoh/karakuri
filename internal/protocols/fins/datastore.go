package fins

import (
	"errors"
	"strings"
	"sync"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/sparse"
)

var (
	errAreaNotFound      = errors.New("area not found")
	errAddressOutOfRange = errors.New("address out of range")
)

var _ protocol.DataStore = (*DataStore)(nil)

// DataStore は FINS のメモリ（エリアごとのワードの並び）。
//
// ビットは、ワードのエリアの ID でもビットのエリアの ID（"CIO_BIT"）でも読み書きできる。
// どちらも同じメモリを指し、アドレスは「ワード × 16 + ビット」。
// タイマ・カウンタのアップフラグ（"T_FLAG"）は、現在値（"T"）とは別のメモリで、アドレスは番号。
//
// エリアは FINS で指せる最大の大きさで、EM のバンクも合わせると 30 個ある。ほとんどは使われないので、
// 値が書かれたところだけを確保する（sparse.Words）
type DataStore struct {
	mu    sync.RWMutex
	words map[string]*sparse.Words
}

// NewDataStore は、すべて 0 のメモリを作る（メモリは、値が書かれたときに確保する）
func NewDataStore() *DataStore {
	s := &DataStore{words: make(map[string]*sparse.Words, len(areaDefs))}
	for _, a := range areaDefs {
		s.words[a.ID] = sparse.NewWords(a.Words)
		if a.Flags {
			// フラグは 1 ビットずつ。16 個を 1 ワードに詰めて持つ
			s.words[a.ID+flagSuffix] = sparse.NewBits(a.Words)
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
		n += r.Allocated()
	}
	return n
}

// GetAreas は利用可能なメモリエリアの一覧を返す
func (s *DataStore) GetAreas() []protocol.MemoryArea {
	return memoryAreas()
}

// resolve はエリアの ID から、メモリと、ビットのエリア（フラグのエリアを含む）の ID だったかどうかを返す
func (s *DataStore) resolve(area string) (*sparse.Words, bool, error) {
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
	if uint64(address)+uint64(count) > uint64(regs.Len())*16 {
		return nil, errAddressOutOfRange
	}
	return regs.ReadBits(address, int(count)), nil
}

func (s *DataStore) WriteBits(area string, address uint32, values []bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	regs, _, err := s.resolve(area)
	if err != nil {
		return err
	}
	if uint64(address)+uint64(len(values)) > uint64(regs.Len())*16 {
		return errAddressOutOfRange
	}
	regs.WriteBits(address, values)
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
	if !isBit {
		if uint64(address)+uint64(count) > uint64(regs.Len()) {
			return nil, errAddressOutOfRange
		}
		return regs.Read(address, int(count)), nil
	}
	if uint64(address)+uint64(count)*16 > uint64(regs.Len())*16 {
		return nil, errAddressOutOfRange
	}
	return regs.ReadWordsAtBit(address, int(count)), nil
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
		if uint64(address)+uint64(len(values)) > uint64(regs.Len()) {
			return errAddressOutOfRange
		}
		regs.Write(address, values)
		return nil
	}
	if uint64(address)+uint64(len(values))*16 > uint64(regs.Len())*16 {
		return errAddressOutOfRange
	}
	regs.WriteWordsAtBit(address, values)
	return nil
}
