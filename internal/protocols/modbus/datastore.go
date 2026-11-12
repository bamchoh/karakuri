package modbus

import (
	"fmt"
	"sync"

	"karakuri/internal/domain/protocol"
)

// ModbusDataStore はModbusプロトコル用のデータストア
type ModbusDataStore struct {
	mu             sync.RWMutex
	coils          []bool
	discreteInputs []bool
	holdingRegs    []uint16
	inputRegs      []uint16
}

// エリアID定数
const (
	AreaCoils          = "coils"
	AreaDiscreteInputs = "discreteInputs"
	AreaHoldingRegs    = "holdingRegisters"
	AreaInputRegs      = "inputRegisters"
)

// NewModbusDataStore は新しいModbusDataStoreを作成する
func NewModbusDataStore(coilCount, discreteCount, holdingCount, inputCount int) *ModbusDataStore {
	return &ModbusDataStore{
		coils:          make([]bool, coilCount),
		discreteInputs: make([]bool, discreteCount),
		holdingRegs:    make([]uint16, holdingCount),
		inputRegs:      make([]uint16, inputCount),
	}
}

// GetAreas は利用可能なメモリエリアの一覧を返す
func (s *ModbusDataStore) GetAreas() []protocol.MemoryArea {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return []protocol.MemoryArea{
		{
			ID:          AreaCoils,
			DisplayName: "コイル (0x)",
			IsBit:       true,
			Size:        uint32(len(s.coils)),
			ReadOnly:    false,
			OneOrigin:   true,
			ShortName:   "0",
		},
		{
			ID:          AreaDiscreteInputs,
			DisplayName: "ディスクリート入力 (1x)",
			IsBit:       true,
			Size:        uint32(len(s.discreteInputs)),
			ReadOnly:    false, // シミュレーターなので書き込み可能
			OneOrigin:   true,
			ShortName:   "1",
		},
		{
			ID:          AreaHoldingRegs,
			DisplayName: "保持レジスタ (4x)",
			IsBit:       false,
			Size:        uint32(len(s.holdingRegs)),
			ReadOnly:    false,
			OneOrigin:   true,
			ShortName:   "4",
		},
		{
			ID:          AreaInputRegs,
			DisplayName: "入力レジスタ (3x)",
			IsBit:       false,
			Size:        uint32(len(s.inputRegs)),
			ReadOnly:    false, // シミュレーターなので書き込み可能
			OneOrigin:   true,
			ShortName:   "3",
		},
	}
}

// ReadBit はビット値を読み込む
func (s *ModbusDataStore) ReadBit(area string, address uint32) (bool, error) {
	values, err := s.ReadBits(area, address, 1)
	if err != nil {
		return false, err
	}

	if len(values) != 1 {
		return false, fmt.Errorf("expected 1 bit, got %d", len(values))
	}

	return values[0], nil
}

// WriteBit はビット値を書き込む
func (s *ModbusDataStore) WriteBit(area string, address uint32, value bool) error {
	return s.WriteBits(area, address, []bool{value})
}

// ReadBits は複数のビット値を読み込む
func (s *ModbusDataStore) ReadBits(area string, address uint32, count uint16) ([]bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch area {
	case AreaCoils:
		if int(address)+int(count) > len(s.coils) {
			return nil, ErrAddressOutOfRange
		}
		result := make([]bool, count)
		copy(result, s.coils[address:address+uint32(count)])
		return result, nil
	case AreaDiscreteInputs:
		if int(address)+int(count) > len(s.discreteInputs) {
			return nil, ErrAddressOutOfRange
		}
		result := make([]bool, count)
		copy(result, s.discreteInputs[address:address+uint32(count)])
		return result, nil

	case AreaHoldingRegs:
		return readRegisterBits(s.holdingRegs, address, count)

	case AreaInputRegs:
		return readRegisterBits(s.inputRegs, address, count)

	default:
		return nil, ErrAreaNotFound
	}
}

func (s *ModbusDataStore) WriteBits(area string, address uint32, values []bool) error {
	s.mu.Lock()

	switch area {
	case AreaCoils:
		if int(address)+len(values) > len(s.coils) {
			s.mu.Unlock()
			return ErrAddressOutOfRange
		}
		copy(s.coils[address:], values)

	case AreaDiscreteInputs:
		if int(address)+len(values) > len(s.discreteInputs) {
			s.mu.Unlock()
			return ErrAddressOutOfRange
		}
		copy(s.discreteInputs[address:], values)

	case AreaHoldingRegs:
		if err := checkRegisterBitRange(s.holdingRegs, address, len(values)); err != nil {
			s.mu.Unlock()
			return err
		}

		for i, bit := range values {
			if err := setRegisterBit(s.holdingRegs, address+uint32(i), bit); err != nil {
				s.mu.Unlock()
				return err
			}
		}

	case AreaInputRegs:
		if err := checkRegisterBitRange(s.inputRegs, address, len(values)); err != nil {
			s.mu.Unlock()
			return err
		}

		for i, bit := range values {
			if err := setRegisterBit(s.inputRegs, address+uint32(i), bit); err != nil {
				s.mu.Unlock()
				return err
			}
		}

	default:
		s.mu.Unlock()
		return ErrAreaNotFound
	}

	s.mu.Unlock()

	return nil
}

// ReadWord はワード値を読み込む
func (s *ModbusDataStore) ReadWord(area string, address uint32) (uint16, error) {
	values, err := s.ReadWords(area, address, 1)
	if err != nil {
		return 0, err
	}

	if len(values) != 1 {
		return 0, fmt.Errorf("expected 1 word, got %d", len(values))
	}

	return values[0], nil
}

// WriteWord はワード値を書き込む
func (s *ModbusDataStore) WriteWord(area string, address uint32, value uint16) error {
	return s.WriteWords(area, address, []uint16{value})
}

// ReadWords は複数のワード値を読み込む
func (s *ModbusDataStore) ReadWords(area string, address uint32, count uint16) ([]uint16, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch area {
	case AreaHoldingRegs:
		if int(address)+int(count) > len(s.holdingRegs) {
			return nil, ErrAddressOutOfRange
		}
		result := make([]uint16, count)
		copy(result, s.holdingRegs[address:address+uint32(count)])
		return result, nil
	case AreaInputRegs:
		if int(address)+int(count) > len(s.inputRegs) {
			return nil, ErrAddressOutOfRange
		}
		result := make([]uint16, count)
		copy(result, s.inputRegs[address:address+uint32(count)])
		return result, nil
	case AreaCoils:
		end := int(address) + int(count)*16
		if end > len(s.coils) {
			return nil, ErrAddressOutOfRange
		}
		result := packBitsToWords(s.coils[address:uint32(end)])
		return result, nil
	case AreaDiscreteInputs:
		end := int(address) + int(count)*16

		if end > len(s.discreteInputs) {
			return nil, ErrAddressOutOfRange
		}
		result := packBitsToWords(s.discreteInputs[address:uint32(end)])
		return result, nil
	default:
		return nil, ErrAreaNotFound
	}
}

// WriteWords は複数のワード値を書き込む
func (s *ModbusDataStore) WriteWords(area string, address uint32, values []uint16) error {
	s.mu.Lock()

	switch area {
	case AreaHoldingRegs:
		if int(address)+len(values) > len(s.holdingRegs) {
			s.mu.Unlock()
			return ErrAddressOutOfRange
		}
		copy(s.holdingRegs[address:], values)
	case AreaInputRegs:
		if int(address)+len(values) > len(s.inputRegs) {
			s.mu.Unlock()
			return ErrAddressOutOfRange
		}
		copy(s.inputRegs[address:], values)

	case AreaCoils:
		if int(address)+len(values)*16 > len(s.coils) {
			s.mu.Unlock()
			return ErrAddressOutOfRange
		}

		copy(s.coils[address:], unpackWordsToBits(values))
	case AreaDiscreteInputs:
		if int(address)+len(values)*16 > len(s.discreteInputs) {
			s.mu.Unlock()
			return ErrAddressOutOfRange
		}

		copy(s.discreteInputs[address:], unpackWordsToBits(values))
	default:
		s.mu.Unlock()
		return ErrAreaNotFound
	}
	s.mu.Unlock()
	return nil
}

func unpackWordsToBits(words []uint16) []bool {
	bits := make([]bool, 0, len(words)*16)

	for _, word := range words {
		bits = append(bits, unpackWordToBits(word)...)
	}

	return bits
}

func unpackWordToBits(word uint16) []bool {
	bits := make([]bool, 16)

	for i := 0; i < 16; i++ {
		bits[i] = (word & (1 << i)) != 0
	}

	return bits
}

func packBitsToWords(bits []bool) []uint16 {
	wordCount := (len(bits) + 15) / 16

	words := make([]uint16, wordCount)

	for i := 0; i < wordCount; i++ {
		start := i * 16
		end := start + 16

		if end > len(bits) {
			end = len(bits)
		}

		words[i] = packBitsToWord(bits[start:end])
	}

	return words
}

func packBitsToWord(bits []bool) uint16 {
	var word uint16

	for i := 0; i < len(bits) && i < 16; i++ {
		if bits[i] {
			word |= 1 << i
		}
	}

	return word
}

func readRegisterBits(regs []uint16, address uint32, count uint16) ([]bool, error) {
	result := make([]bool, count)

	for i := uint16(0); i < count; i++ {
		bit, err := getRegisterBit(regs, address+uint32(i))
		if err != nil {
			return nil, err
		}

		result[i] = bit
	}

	return result, nil
}

func getRegisterBit(regs []uint16, bitAddr uint32) (bool, error) {
	wordIndex := bitAddr / 16
	bitIndex := bitAddr % 16

	if int(wordIndex) >= len(regs) {
		return false, ErrAddressOutOfRange
	}

	return (regs[wordIndex] & (1 << bitIndex)) != 0, nil
}

func setRegisterBit(regs []uint16, bitAddr uint32, value bool) error {
	wordIndex := bitAddr / 16
	bitIndex := bitAddr % 16

	if int(wordIndex) >= len(regs) {
		return ErrAddressOutOfRange
	}

	mask := uint16(1 << bitIndex)

	if value {
		regs[wordIndex] |= mask
	} else {
		regs[wordIndex] &^= mask
	}

	return nil
}

func checkRegisterBitRange(regs []uint16, address uint32, count int) error {
	if int(address)+count > len(regs)*16 {
		return ErrAddressOutOfRange
	}
	return nil
}
