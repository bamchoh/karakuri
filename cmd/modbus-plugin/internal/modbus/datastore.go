package modbus

import (
	"fmt"
	"sync"

	"modbus_simulator/internal/domain/datastore"
	"modbus_simulator/internal/domain/protocol"
	"modbus_simulator/internal/pluginruntime"
)

// ModbusDataStore はModbusプロトコル用のデータストア
type ModbusDataStore struct {
	mu             sync.RWMutex
	coils          []bool
	discreteInputs []bool
	holdingRegs    []uint16
	inputRegs      []uint16

	hookMu     sync.RWMutex
	changeHook pluginruntime.DataChangeHook
}

// エリアID定数
const (
	AreaCoils          = "coils"
	AreaDiscreteInputs = "discreteInputs"
	AreaHoldingRegs    = "holdingRegisters"
	AreaInputRegs      = "inputRegisters"
)

// SetChangeHook はデータ変更時に呼ばれるフックを設定する。
// nil を渡すとフックを解除する。
// フックは Modbus クライアントの書き込み時にのみ呼び出すこと（ホストからの書き込み時は呼び出さない）。
func (s *ModbusDataStore) SetChangeHook(hook pluginruntime.DataChangeHook) {
	s.hookMu.Lock()
	s.changeHook = hook
	s.hookMu.Unlock()
}

// callChangeHook はフックを安全に呼び出す（ロック外で呼ぶこと）
func (s *ModbusDataStore) callChangeHook(area string, address uint32, values []uint16, isBit bool, bitValues []bool) {
	s.hookMu.RLock()
	hook := s.changeHook
	s.hookMu.RUnlock()
	if hook != nil {
		hook(area, address, values, isBit, bitValues)
	}
}

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
		},
		{
			ID:          AreaDiscreteInputs,
			DisplayName: "ディスクリート入力 (1x)",
			IsBit:       true,
			Size:        uint32(len(s.discreteInputs)),
			ReadOnly:    false, // シミュレーターなので書き込み可能
			OneOrigin:   true,
		},
		{
			ID:          AreaHoldingRegs,
			DisplayName: "保持レジスタ (4x)",
			IsBit:       false,
			Size:        uint32(len(s.holdingRegs)),
			ReadOnly:    false,
			OneOrigin:   true,
		},
		{
			ID:          AreaInputRegs,
			DisplayName: "入力レジスタ (3x)",
			IsBit:       false,
			Size:        uint32(len(s.inputRegs)),
			ReadOnly:    false, // シミュレーターなので書き込み可能
			OneOrigin:   true,
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
			return nil, datastore.ErrAddressOutOfRange
		}
		result := make([]bool, count)
		copy(result, s.coils[address:address+uint32(count)])
		return result, nil
	case AreaDiscreteInputs:
		if int(address)+int(count) > len(s.discreteInputs) {
			return nil, datastore.ErrAddressOutOfRange
		}
		result := make([]bool, count)
		copy(result, s.discreteInputs[address:address+uint32(count)])
		return result, nil

	case AreaHoldingRegs:
		return readRegisterBits(s.holdingRegs, address, count)

	case AreaInputRegs:
		return readRegisterBits(s.inputRegs, address, count)

	default:
		return nil, datastore.ErrAreaNotFound
	}
}

func (s *ModbusDataStore) WriteBits(area string, address uint32, values []bool) error {
	s.mu.Lock()

	switch area {
	case AreaCoils:
		if int(address)+len(values) > len(s.coils) {
			s.mu.Unlock()
			return datastore.ErrAddressOutOfRange
		}
		copy(s.coils[address:], values)

	case AreaDiscreteInputs:
		if int(address)+len(values) > len(s.discreteInputs) {
			s.mu.Unlock()
			return datastore.ErrAddressOutOfRange
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
		return datastore.ErrAreaNotFound
	}

	s.mu.Unlock()

	s.callChangeHook(area, address, nil, true, values)

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
			return nil, datastore.ErrAddressOutOfRange
		}
		result := make([]uint16, count)
		copy(result, s.holdingRegs[address:address+uint32(count)])
		return result, nil
	case AreaInputRegs:
		if int(address)+int(count) > len(s.inputRegs) {
			return nil, datastore.ErrAddressOutOfRange
		}
		result := make([]uint16, count)
		copy(result, s.inputRegs[address:address+uint32(count)])
		return result, nil
	case AreaCoils:
		end := int(address) + int(count)*16
		if end > len(s.coils) {
			return nil, datastore.ErrAddressOutOfRange
		}
		result := packBitsToWords(s.coils[address:uint32(end)])
		return result, nil
	case AreaDiscreteInputs:
		end := int(address) + int(count)*16

		if end > len(s.discreteInputs) {
			return nil, datastore.ErrAddressOutOfRange
		}
		result := packBitsToWords(s.discreteInputs[address:uint32(end)])
		return result, nil
	default:
		return nil, datastore.ErrAreaNotFound
	}
}

// WriteWords は複数のワード値を書き込む
func (s *ModbusDataStore) WriteWords(area string, address uint32, values []uint16) error {
	s.mu.Lock()

	var changeBits []bool

	switch area {
	case AreaHoldingRegs:
		if int(address)+len(values) > len(s.holdingRegs) {
			s.mu.Unlock()
			return datastore.ErrAddressOutOfRange
		}
		copy(s.holdingRegs[address:], values)
	case AreaInputRegs:
		if int(address)+len(values) > len(s.inputRegs) {
			s.mu.Unlock()
			return datastore.ErrAddressOutOfRange
		}
		copy(s.inputRegs[address:], values)

	case AreaCoils:
		if int(address)+len(values)*16 > len(s.coils) {
			s.mu.Unlock()
			return datastore.ErrAddressOutOfRange
		}

		changeBits = unpackWordsToBits(values)
		copy(s.coils[address:], changeBits)
	case AreaDiscreteInputs:
		if int(address)+len(values)*16 > len(s.discreteInputs) {
			s.mu.Unlock()
			return datastore.ErrAddressOutOfRange
		}

		changeBits = unpackWordsToBits(values)
		copy(s.discreteInputs[address:], changeBits)
	default:
		s.mu.Unlock()
		return datastore.ErrAreaNotFound
	}
	s.mu.Unlock()

	switch area {
	case AreaHoldingRegs, AreaInputRegs:
		s.callChangeHook(area, address, values, false, nil)

	case AreaCoils, AreaDiscreteInputs:
		s.callChangeHook(area, address, nil, true, changeBits)
	}
	return nil
}

// Snapshot はデータストアのスナップショットを作成する
func (s *ModbusDataStore) Snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	coils := make([]bool, len(s.coils))
	copy(coils, s.coils)

	discreteInputs := make([]bool, len(s.discreteInputs))
	copy(discreteInputs, s.discreteInputs)

	holdingRegs := make([]uint16, len(s.holdingRegs))
	copy(holdingRegs, s.holdingRegs)

	inputRegs := make([]uint16, len(s.inputRegs))
	copy(inputRegs, s.inputRegs)

	return map[string]interface{}{
		AreaCoils:          coils,
		AreaDiscreteInputs: discreteInputs,
		AreaHoldingRegs:    holdingRegs,
		AreaInputRegs:      inputRegs,
	}
}

// Restore はスナップショットからデータを復元する
func (s *ModbusDataStore) Restore(data map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if coils, ok := data[AreaCoils]; ok {
		if bools, ok := coils.([]bool); ok {
			count := len(bools)
			if count > len(s.coils) {
				count = len(s.coils)
			}
			copy(s.coils, bools[:count])
		}
	}

	if discreteInputs, ok := data[AreaDiscreteInputs]; ok {
		if bools, ok := discreteInputs.([]bool); ok {
			count := len(bools)
			if count > len(s.discreteInputs) {
				count = len(s.discreteInputs)
			}
			copy(s.discreteInputs, bools[:count])
		}
	}

	if holdingRegs, ok := data[AreaHoldingRegs]; ok {
		if words, ok := holdingRegs.([]uint16); ok {
			count := len(words)
			if count > len(s.holdingRegs) {
				count = len(s.holdingRegs)
			}
			copy(s.holdingRegs, words[:count])
		}
	}

	if inputRegs, ok := data[AreaInputRegs]; ok {
		if words, ok := inputRegs.([]uint16); ok {
			count := len(words)
			if count > len(s.inputRegs) {
				count = len(s.inputRegs)
			}
			copy(s.inputRegs, words[:count])
		}
	}

	return nil
}

// ClearAll は全てのデータをクリアする
func (s *ModbusDataStore) ClearAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.coils {
		s.coils[i] = false
	}
	for i := range s.discreteInputs {
		s.discreteInputs[i] = false
	}
	for i := range s.holdingRegs {
		s.holdingRegs[i] = 0
	}
	for i := range s.inputRegs {
		s.inputRegs[i] = 0
	}
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
		return false, datastore.ErrAddressOutOfRange
	}

	return (regs[wordIndex] & (1 << bitIndex)) != 0, nil
}

func setRegisterBit(regs []uint16, bitAddr uint32, value bool) error {
	wordIndex := bitAddr / 16
	bitIndex := bitAddr % 16

	if int(wordIndex) >= len(regs) {
		return datastore.ErrAddressOutOfRange
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
		return datastore.ErrAddressOutOfRange
	}
	return nil
}
