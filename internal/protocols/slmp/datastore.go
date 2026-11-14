package slmp

import (
	"errors"
	"sync"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/sparse"
)

var (
	errAreaNotFound      = errors.New("area not found")
	errAddressOutOfRange = errors.New("address out of range")
)

var _ protocol.DataStore = (*DataStore)(nil)

// DataStore は SLMP のメモリ（デバイスごとの並び）。
//
// デバイスは 1 つ 16,777,216 点あるので、値が書かれたところだけを確保する（sparse.Words）。
// ビットデバイスは、16 点を 1 ワードに詰めて持つ。
//
// アドレスは、エリアの種類で決まる:
//   - ビットデバイス: ビットの番号。ワードとして読み書きすると、そのビットから 16 点ずつをワードにまとめる
//   - ワードデバイス: ワードの番号。ビットとして読み書きするときは「ワード × 16 + ビット」
type DataStore struct {
	mu      sync.RWMutex
	devices map[string]*device
}

type device struct {
	isBit bool
	words *sparse.Words
}

// NewDataStore は、すべて 0 のメモリを作る（メモリは、値が書かれたときに確保する）
func NewDataStore() *DataStore {
	s := &DataStore{devices: make(map[string]*device, len(deviceDefs))}
	for _, d := range deviceDefs {
		if d.IsBit {
			s.devices[d.ID] = &device{isBit: true, words: sparse.NewBits(maxPoints)}
		} else {
			s.devices[d.ID] = &device{words: sparse.NewWords(maxPoints)}
		}
	}
	return s
}

// allocatedWords は、確保しているメモリの大きさ（ワード数）を返す（テスト用）
func (s *DataStore) allocatedWords() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, d := range s.devices {
		n += d.words.Allocated()
	}
	return n
}

// GetAreas は利用可能なメモリエリアの一覧を返す
func (s *DataStore) GetAreas() []protocol.MemoryArea {
	return memoryAreas()
}

// bits は、デバイスが持つビットの数を返す
func (d *device) bits() uint64 {
	return uint64(d.words.Len()) * 16
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
	d, ok := s.devices[area]
	if !ok {
		return nil, errAreaNotFound
	}
	if uint64(address)+uint64(count) > d.bits() {
		return nil, errAddressOutOfRange
	}
	return d.words.ReadBits(address, int(count)), nil
}

func (s *DataStore) WriteBits(area string, address uint32, values []bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[area]
	if !ok {
		return errAreaNotFound
	}
	if uint64(address)+uint64(len(values)) > d.bits() {
		return errAddressOutOfRange
	}
	d.words.WriteBits(address, values)
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

// ReadWords はワードを読む。ビットデバイスのときは、address のビットから 16 点ずつをワードにまとめる
func (s *DataStore) ReadWords(area string, address uint32, count uint16) ([]uint16, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[area]
	if !ok {
		return nil, errAreaNotFound
	}
	if d.isBit {
		if uint64(address)+uint64(count)*16 > d.bits() {
			return nil, errAddressOutOfRange
		}
		return d.words.ReadWordsAtBit(address, int(count)), nil
	}
	if uint64(address)+uint64(count) > uint64(d.words.Len()) {
		return nil, errAddressOutOfRange
	}
	return d.words.Read(address, int(count)), nil
}

// WriteWords はワードを書く。ビットデバイスのときは、address のビットから 16 点ずつに分けて書く
func (s *DataStore) WriteWords(area string, address uint32, values []uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[area]
	if !ok {
		return errAreaNotFound
	}
	if d.isBit {
		if uint64(address)+uint64(len(values))*16 > d.bits() {
			return errAddressOutOfRange
		}
		d.words.WriteWordsAtBit(address, values)
		return nil
	}
	if uint64(address)+uint64(len(values)) > uint64(d.words.Len()) {
		return errAddressOutOfRange
	}
	d.words.Write(address, values)
	return nil
}
