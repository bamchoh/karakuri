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

// DataStore は FINS のメモリ（エリアごとのワードの並び）。
//
// ビットは、ワードのエリアの ID でもビットのエリアの ID（"CIO_BIT"）でも読み書きできる。
// どちらも同じメモリを指し、アドレスは「ワード × 16 + ビット」
type DataStore struct {
	mu    sync.RWMutex
	words map[string][]uint16
}

// NewDataStore は、すべて 0 のメモリを作る
func NewDataStore() *DataStore {
	s := &DataStore{words: make(map[string][]uint16, len(areaDefs))}
	for _, a := range areaDefs {
		s.words[a.ID] = make([]uint16, a.Words)
	}
	return s
}

// GetAreas は利用可能なメモリエリアの一覧を返す
func (s *DataStore) GetAreas() []protocol.MemoryArea {
	return memoryAreas()
}

// resolve はエリアの ID から、ワードの並びと、ビットのエリアの ID だったかどうかを返す
func (s *DataStore) resolve(area string) ([]uint16, bool, error) {
	isBit := strings.HasSuffix(area, bitSuffix)
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
	if uint64(address)+uint64(count) > uint64(len(regs))*16 {
		return nil, errAddressOutOfRange
	}
	bits := make([]bool, count)
	for i := range bits {
		n := address + uint32(i)
		bits[i] = regs[n/16]&(1<<(n%16)) != 0
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
	if uint64(address)+uint64(len(values)) > uint64(len(regs))*16 {
		return errAddressOutOfRange
	}
	for i, v := range values {
		n := address + uint32(i)
		if v {
			regs[n/16] |= 1 << (n % 16)
		} else {
			regs[n/16] &^= 1 << (n % 16)
		}
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
		if uint64(address)+uint64(count) > uint64(len(regs)) {
			return nil, errAddressOutOfRange
		}
		copy(out, regs[address:])
		return out, nil
	}
	if uint64(address)+uint64(count)*16 > uint64(len(regs))*16 {
		return nil, errAddressOutOfRange
	}
	for i := range out {
		for b := uint32(0); b < 16; b++ {
			n := address + uint32(i)*16 + b
			if regs[n/16]&(1<<(n%16)) != 0 {
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
		if uint64(address)+uint64(len(values)) > uint64(len(regs)) {
			return errAddressOutOfRange
		}
		copy(regs[address:], values)
		return nil
	}
	if uint64(address)+uint64(len(values))*16 > uint64(len(regs))*16 {
		return errAddressOutOfRange
	}
	for i, w := range values {
		for b := uint32(0); b < 16; b++ {
			n := address + uint32(i)*16 + b
			if w&(1<<b) != 0 {
				regs[n/16] |= 1 << (n % 16)
			} else {
				regs[n/16] &^= 1 << (n % 16)
			}
		}
	}
	return nil
}
