package modbus

import (
	"reflect"
	"testing"

	"modbus_simulator/internal/domain/datastore"
)

func TestNewModbusDataStore(t *testing.T) {
	store := NewModbusDataStore(100, 50, 200, 150)
	if store == nil {
		t.Fatal("NewModbusDataStore returned nil")
	}

	if len(store.coils) != 100 {
		t.Errorf("expected 100 coils, got %d", len(store.coils))
	}
	if len(store.discreteInputs) != 50 {
		t.Errorf("expected 50 discrete inputs, got %d", len(store.discreteInputs))
	}
	if len(store.holdingRegs) != 200 {
		t.Errorf("expected 200 holding registers, got %d", len(store.holdingRegs))
	}
	if len(store.inputRegs) != 150 {
		t.Errorf("expected 150 input registers, got %d", len(store.inputRegs))
	}
}

func TestModbusDataStore_GetAreas(t *testing.T) {
	store := NewModbusDataStore(100, 50, 200, 150)
	areas := store.GetAreas()

	if len(areas) != 4 {
		t.Fatalf("expected 4 areas, got %d", len(areas))
	}

	// エリアの確認
	areaMap := make(map[string]bool)
	for _, area := range areas {
		areaMap[area.ID] = true
	}

	expectedAreas := []string{AreaCoils, AreaDiscreteInputs, AreaHoldingRegs, AreaInputRegs}
	for _, expected := range expectedAreas {
		if !areaMap[expected] {
			t.Errorf("expected area %s not found", expected)
		}
	}
}

func TestModbusDataStore_ReadWriteBit(t *testing.T) {
	tests := []struct {
		name    string
		area    string
		address uint32
	}{
		{
			name:    "coil",
			area:    AreaCoils,
			address: 10,
		},
		{
			name:    "discrete input",
			area:    AreaDiscreteInputs,
			address: 5,
		},
		{
			name:    "holding register bit0",
			area:    AreaHoldingRegs,
			address: 0,
		},
		{
			name:    "holding register bit15",
			area:    AreaHoldingRegs,
			address: 15,
		},
		{
			name:    "holding register bit16",
			area:    AreaHoldingRegs,
			address: 16,
		},
		{
			name:    "input register bit0",
			area:    AreaInputRegs,
			address: 0,
		},
		{
			name:    "input register bit31",
			area:    AreaInputRegs,
			address: 31,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewModbusDataStore(100, 50, 200, 150)

			if err := store.WriteBit(tt.area, tt.address, true); err != nil {
				t.Fatalf("WriteBit failed: %v", err)
			}

			got, err := store.ReadBit(tt.area, tt.address)
			if err != nil {
				t.Fatalf("ReadBit failed: %v", err)
			}

			if !got {
				t.Fatal("expected true, got false")
			}
		})
	}
}

func TestHoldingRegisterBitsAreIndependent(t *testing.T) {
	store := NewModbusDataStore(100, 50, 200, 150)

	if err := store.WriteBit(AreaHoldingRegs, 0, true); err != nil {
		t.Fatal(err)
	}

	v, err := store.ReadWord(AreaHoldingRegs, 0)
	if err != nil {
		t.Fatal(err)
	}

	if v != 0x0001 {
		t.Fatalf("expected 0x0001, got 0x%04x", v)
	}
}

func TestModbusDataStore_ReadWriteBit_OutOfRange(t *testing.T) {
	tests := []struct {
		name    string
		area    string
		address uint32
	}{
		{"coil", AreaCoils, 100},
		{"discrete", AreaDiscreteInputs, 50},
		{"holding", AreaHoldingRegs, 200 * 16},
		{"input", AreaInputRegs, 150 * 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewModbusDataStore(100, 50, 200, 150)

			_, err := store.ReadBit(tt.area, tt.address)
			if err != datastore.ErrAddressOutOfRange {
				t.Fatalf("expected ErrAddressOutOfRange, got %v", err)
			}

			err = store.WriteBit(tt.area, tt.address, true)
			if err != datastore.ErrAddressOutOfRange {
				t.Fatalf("expected ErrAddressOutOfRange, got %v", err)
			}
		})
	}
}

func TestModbusDataStore_ReadWriteBit_AreaNotFound(t *testing.T) {
	store := NewModbusDataStore(100, 50, 200, 150)

	// 存在しないエリア
	_, err := store.ReadBit("nonexistent", 0)
	if err != datastore.ErrAreaNotFound {
		t.Errorf("expected ErrAreaNotFound, got %v", err)
	}

	err = store.WriteBit("nonexistent", 0, true)
	if err != datastore.ErrAreaNotFound {
		t.Errorf("expected ErrAreaNotFound, got %v", err)
	}
}

func TestModbusDataStore_ReadWriteBits(t *testing.T) {
	tests := []struct {
		name    string
		area    string
		address uint32
		values  []bool
	}{
		{
			name:    "coils",
			area:    AreaCoils,
			address: 10,
			values:  []bool{true, false, true, true, false},
		},
		{
			name:    "discrete inputs",
			area:    AreaDiscreteInputs,
			address: 10,
			values:  []bool{true, false, true, true, false},
		},
		{
			name:    "holding regs",
			area:    AreaHoldingRegs,
			address: 10,
			values:  []bool{true, false, true, true, false},
		},
		{
			name:    "input regs",
			area:    AreaInputRegs,
			address: 10,
			values:  []bool{true, false, true, true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewModbusDataStore(100, 50, 200, 150)

			err := store.WriteBits(tt.area, tt.address, tt.values)
			if err != nil {
				t.Fatal(err)
			}

			got, err := store.ReadBits(tt.area, tt.address, uint16(len(tt.values)))
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, tt.values) {
				t.Fatalf("expected %v, got %v", tt.values, got)
			}
		})
	}
}

func TestModbusDataStore_RegisterBitBoundary(t *testing.T) {
	store := NewModbusDataStore(100, 50, 200, 150)

	err := store.WriteBits(
		AreaHoldingRegs,
		15,
		[]bool{true, true},
	)
	if err != nil {
		t.Fatal(err)
	}

	v0, err := store.ReadWord(AreaHoldingRegs, 0)
	if err != nil {
		t.Fatal(err)
	}

	v1, err := store.ReadWord(AreaHoldingRegs, 1)
	if err != nil {
		t.Fatal(err)
	}

	if v0 != 0x8000 {
		t.Fatalf("word0 expected 0x8000 got 0x%04x", v0)
	}

	if v1 != 0x0001 {
		t.Fatalf("word1 expected 0x0001 got 0x%04x", v1)
	}
}

func TestModbusDataStore_WriteWordThenReadBits(t *testing.T) {
	store := NewModbusDataStore(100, 50, 200, 150)

	err := store.WriteWord(AreaHoldingRegs, 0, 0xABCD)
	if err != nil {
		t.Fatal(err)
	}

	bits, err := store.ReadBits(AreaHoldingRegs, 0, 16)
	if err != nil {
		t.Fatal(err)
	}

	if packBitsToWord(bits) != 0xABCD {
		t.Fatalf(
			"expected 0xABCD got 0x%04X",
			packBitsToWord(bits),
		)
	}
}

func TestModbusDataStore_WriteBitsThenReadWord(t *testing.T) {
	tests := []struct {
		name     string
		area     string
		bits     []bool
		expected uint16
	}{
		{
			name: "holding register",
			area: AreaHoldingRegs,
			bits: []bool{
				true, false, true, false,
				true, false, true, false,
				true, false, true, false,
				true, false, true, false,
			},
			expected: 0x5555,
		},
		{
			name: "input register",
			area: AreaInputRegs,
			bits: []bool{
				false, true, false, true,
				false, true, false, true,
				false, true, false, true,
				false, true, false, true,
			},
			expected: 0xAAAA,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewModbusDataStore(100, 100, 100, 100)

			err := store.WriteBits(tt.area, 0, tt.bits)
			if err != nil {
				t.Fatalf("WriteBits failed: %v", err)
			}

			word, err := store.ReadWord(tt.area, 0)
			if err != nil {
				t.Fatalf("ReadWord failed: %v", err)
			}

			if word != tt.expected {
				t.Fatalf(
					"expected 0x%04X, got 0x%04X",
					tt.expected,
					word,
				)
			}
		})
	}
}

func TestModbusDataStore_ReadWriteBits_OutOfRange(t *testing.T) {
	tests := []struct {
		name    string
		area    string
		address uint32
		count   uint16
		values  []bool
	}{
		{
			name:    "coils read out of range",
			area:    AreaCoils,
			address: 95,
			count:   10,
		},
		{
			name:    "coils write out of range",
			area:    AreaCoils,
			address: 95,
			values:  []bool{true, true, true, true, true, true},
		},
		{
			name:    "discrete inputs read out of range",
			area:    AreaDiscreteInputs,
			address: 145,
			count:   10,
		},
		{
			name:    "discrete inputs write out of range",
			area:    AreaDiscreteInputs,
			address: 145,
			values:  []bool{true, true, true, true, true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewModbusDataStore(100, 50, 200, 150)

			if tt.count > 0 {
				_, err := store.ReadBits(tt.area, tt.address, tt.count)
				if err != datastore.ErrAddressOutOfRange {
					t.Fatalf("expected ErrAddressOutOfRange, got %v", err)
				}
			}

			if tt.values != nil {
				err := store.WriteBits(tt.area, tt.address, tt.values)
				if err != datastore.ErrAddressOutOfRange {
					t.Fatalf("expected ErrAddressOutOfRange, got %v", err)
				}
			}
		})
	}
}

func TestModbusDataStore_ReadWriteBits_Boundary(t *testing.T) {
	tests := []struct {
		name      string
		area      string
		address   uint32
		count     uint16
		shouldErr bool
	}{
		{
			name:      "coils last valid bit",
			area:      AreaCoils,
			address:   199,
			count:     1,
			shouldErr: false,
		},
		{
			name:      "coils first invalid bit",
			area:      AreaCoils,
			address:   200,
			count:     1,
			shouldErr: true,
		},
		{
			name:      "coils exact end",
			area:      AreaCoils,
			address:   195,
			count:     5,
			shouldErr: false,
		},
		{
			name:      "coils beyond end",
			area:      AreaCoils,
			address:   195,
			count:     6,
			shouldErr: true,
		},
		{
			name:      "discrete inputs exact end",
			area:      AreaDiscreteInputs,
			address:   145,
			count:     5,
			shouldErr: false,
		},
		{
			name:      "discrete inputs beyond end",
			area:      AreaDiscreteInputs,
			address:   145,
			count:     6,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewModbusDataStore(200, 150, 200, 150)

			_, err := store.ReadBits(tt.area, tt.address, tt.count)

			if tt.shouldErr {
				if err != datastore.ErrAddressOutOfRange {
					t.Fatalf("expected ErrAddressOutOfRange, got %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestModbusDataStore_ReadWriteWord(t *testing.T) {
	tests := []struct {
		name    string
		area    string
		address uint32
		value   uint16
	}{
		{
			name:    "holding (any address)",
			area:    AreaHoldingRegs,
			address: 10,
			value:   0x1234,
		},
		{
			name:    "holding (max address)",
			area:    AreaHoldingRegs,
			address: 199,
			value:   0xABCD,
		},
		{
			name:    "input (any address)",
			area:    AreaInputRegs,
			address: 5,
			value:   0xABCD,
		},
		{
			name:    "input (max address)",
			area:    AreaInputRegs,
			address: 149,
			value:   0x1234,
		},
		{
			name:    "coil (any address)",
			area:    AreaCoils,
			address: 10,
			value:   0x2345,
		},
		{
			name:    "coil (max address)",
			area:    AreaCoils,
			address: 84,
			value:   0xBCDE,
		},
		{
			name:    "discrete input (any address)",
			area:    AreaCoils,
			address: 10,
			value:   0x3456,
		},
		{
			name:    "discrete input (max address)",
			area:    AreaCoils,
			address: 34,
			value:   0xCDEF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewModbusDataStore(100, 50, 200, 150)

			err := store.WriteWord(tt.area, tt.address, tt.value)
			if err != nil {
				t.Fatal(err)
			}

			got, err := store.ReadWord(tt.area, tt.address)
			if err != nil {
				t.Fatal(err)
			}

			if got != tt.value {
				t.Errorf("expected 0x%04x, got 0x%04x", tt.value, got)
			}
		})
	}
}

func TestModbusDataStore_ReadWriteWord_Areas_AreIndependent(t *testing.T) {
	tests := []struct {
		name          string
		targetArea    string
		affectedAreas []string
		address       uint32
		value         uint16
	}{
		{
			name:          "holding registers are isolated",
			targetArea:    AreaHoldingRegs,
			affectedAreas: []string{AreaInputRegs, AreaCoils, AreaDiscreteInputs},
			address:       0,
			value:         0x1234,
		},
		{
			name:          "input registers are isolated",
			targetArea:    AreaInputRegs,
			affectedAreas: []string{AreaHoldingRegs, AreaCoils, AreaDiscreteInputs},
			address:       0,
			value:         0x1234,
		},
		{
			name:          "coils are isolated",
			targetArea:    AreaCoils,
			affectedAreas: []string{AreaInputRegs, AreaHoldingRegs, AreaDiscreteInputs},
			address:       0,
			value:         0x1234,
		},
		{
			name:          "descrete inputs are isolated",
			targetArea:    AreaDiscreteInputs,
			affectedAreas: []string{AreaInputRegs, AreaCoils, AreaHoldingRegs},
			address:       0,
			value:         0x1234,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewModbusDataStore(100, 50, 200, 150)

			store.WriteWord(tt.targetArea, tt.address, tt.value)

			for _, affectedArea := range tt.affectedAreas {
				if val, _ := store.ReadWord(affectedArea, 0); val != 0 {
					t.Fatalf(
						"write to %s affected %s",
						tt.targetArea,
						affectedArea,
					)
				}
			}
		})
	}
}

func TestModbusDataStore_ReadWriteWord_OutOfRange(t *testing.T) {
	tests := []struct {
		name    string
		area    string
		address uint32
		value   uint16
	}{
		{
			name:    "holding registers",
			area:    AreaHoldingRegs,
			address: 200,
			value:   0x1234,
		},
		{
			name:    "input registers",
			area:    AreaInputRegs,
			address: 150,
			value:   0x1234,
		},
		{
			name:    "coils",
			area:    AreaCoils,
			address: 85,
			value:   0x1234,
		},
		{
			name:    "descrete",
			area:    AreaDiscreteInputs,
			address: 35,
			value:   0x1234,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			store := NewModbusDataStore(100, 50, 200, 150)

			// 範囲外アクセス
			_, err := store.ReadWord(tt.area, tt.address)
			if err != datastore.ErrAddressOutOfRange {
				t.Errorf("expected ErrAddressOutOfRange, got %v", err)
			}

			err = store.WriteWord(tt.area, tt.address, tt.value)
			if err != datastore.ErrAddressOutOfRange {
				t.Errorf("expected ErrAddressOutOfRange, got %v", err)
			}
		})
	}
}

func TestModbusDataStore_ReadWriteWord_AreaNotFound(t *testing.T) {
	store := NewModbusDataStore(100, 50, 200, 150)

	// 存在しないエリア
	_, err := store.ReadWord("nonexistent", 0)
	if err != datastore.ErrAreaNotFound {
		t.Errorf("expected ErrAreaNotFound, got %v", err)
	}

	err = store.WriteWord("nonexistent", 0, 0x1234)
	if err != datastore.ErrAreaNotFound {
		t.Errorf("expected ErrAreaNotFound, got %v", err)
	}
}

func TestModbusDataStore_ReadWriteWords(t *testing.T) {
	tests := []struct {
		name    string
		area    string
		address uint32
		count   uint16
		values  []uint16
	}{
		{
			name:    "holding registers (any address)",
			area:    AreaHoldingRegs,
			address: 10,
			count:   5,
			values:  []uint16{0x1111, 0x2222, 0x3333, 0x4444, 0x5555},
		},
		{
			name:    "holding registers (max address)",
			area:    AreaHoldingRegs,
			address: 195,
			count:   5,
			values:  []uint16{0x1111, 0x2222, 0x3333, 0x4444, 0x5555},
		},
		{
			name:    "input registers (any address)",
			area:    AreaInputRegs,
			address: 20,
			count:   5,
			values:  []uint16{0x1111, 0x2222, 0x3333, 0x4444, 0x5555},
		},
		{
			name:    "input registers (max address)",
			area:    AreaInputRegs,
			address: 145,
			count:   5,
			values:  []uint16{0x1111, 0x2222, 0x3333, 0x4444, 0x5555},
		},
		{
			name:    "coils (any address)",
			area:    AreaCoils,
			address: 16,
			count:   5,
			values:  []uint16{0x1111, 0x2222, 0x3333, 0x4444, 0x5555},
		},
		{
			name:    "coils (max address)",
			area:    AreaCoils,
			address: 20,
			count:   5,
			values:  []uint16{0x1111, 0x2222, 0x3333, 0x4444, 0x5555},
		},
		{
			name:    "descrete (any address)",
			area:    AreaDiscreteInputs,
			address: 0,
			count:   2,
			values:  []uint16{0x1111, 0x2222},
		},
		{
			name:    "descrete (max address)",
			area:    AreaDiscreteInputs,
			address: 18,
			count:   2,
			values:  []uint16{0x1111, 0x2222},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			store := NewModbusDataStore(100, 50, 200, 150)

			// 複数ワードの書き込み
			err := store.WriteWords(tt.area, tt.address, tt.values)
			if err != nil {
				t.Fatalf("WriteWords failed: %v", err)
			}

			// 複数ワードの読み取り
			got, err := store.ReadWords(tt.area, tt.address, tt.count)
			if err != nil {
				t.Fatalf("ReadWords failed: %v", err)
			}

			for i, v := range tt.values {
				if got[i] != v {
					t.Errorf("word %d: expected 0x%04x, got 0x%04x", i, v, got[i])
				}
			}
		})
	}
}

func TestModbusDataStore_ReadWriteWords_OutOfRange(t *testing.T) {
	tests := []struct {
		name    string
		area    string
		address uint32
		count   uint16
		values  []uint16
	}{
		{
			name:    "holding registers (max address)",
			area:    AreaHoldingRegs,
			address: 195,
			count:   6,
			values:  []uint16{0x1111, 0x2222, 0x3333, 0x4444, 0x5555, 0x6666},
		},
		{
			name:    "input registers (max address)",
			area:    AreaInputRegs,
			address: 145,
			count:   6,
			values:  []uint16{0x1111, 0x2222, 0x3333, 0x4444, 0x5555, 0x6666},
		},
		{
			name:    "coils (max address)",
			area:    AreaCoils,
			address: 20,
			count:   6,
			values:  []uint16{0x1111, 0x2222, 0x3333, 0x4444, 0x5555, 0x6666},
		},
		{
			name:    "descrete (max address)",
			area:    AreaDiscreteInputs,
			address: 18,
			count:   3,
			values:  []uint16{0x1111, 0x2222, 0x3333},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewModbusDataStore(100, 50, 200, 150)

			// 範囲外アクセス
			_, err := store.ReadWords(tt.area, tt.address, tt.count)
			if err != datastore.ErrAddressOutOfRange {
				t.Errorf("expected ErrAddressOutOfRange, got %v", err)
			}

			err = store.WriteWords(tt.area, tt.address, tt.values)
			if err != datastore.ErrAddressOutOfRange {
				t.Errorf("expected ErrAddressOutOfRange, got %v", err)
			}
		})
	}
}

func TestModbusDataStore_Snapshot(t *testing.T) {
	store := NewModbusDataStore(10, 10, 10, 10)

	// データを設定
	_ = store.WriteBit(AreaCoils, 0, true)
	_ = store.WriteBit(AreaDiscreteInputs, 1, true)
	_ = store.WriteWord(AreaHoldingRegs, 2, 0x1234)
	_ = store.WriteWord(AreaInputRegs, 3, 0x5678)

	// スナップショット取得
	snapshot := store.Snapshot()

	// スナップショットの確認
	coils, ok := snapshot[AreaCoils].([]bool)
	if !ok {
		t.Fatal("coils not found in snapshot")
	}
	if !coils[0] {
		t.Error("expected coil[0] to be true")
	}

	discreteInputs, ok := snapshot[AreaDiscreteInputs].([]bool)
	if !ok {
		t.Fatal("discreteInputs not found in snapshot")
	}
	if !discreteInputs[1] {
		t.Error("expected discreteInput[1] to be true")
	}

	holdingRegs, ok := snapshot[AreaHoldingRegs].([]uint16)
	if !ok {
		t.Fatal("holdingRegs not found in snapshot")
	}
	if holdingRegs[2] != 0x1234 {
		t.Errorf("expected 0x1234, got 0x%04x", holdingRegs[2])
	}

	inputRegs, ok := snapshot[AreaInputRegs].([]uint16)
	if !ok {
		t.Fatal("inputRegs not found in snapshot")
	}
	if inputRegs[3] != 0x5678 {
		t.Errorf("expected 0x5678, got 0x%04x", inputRegs[3])
	}
}

func TestModbusDataStore_Restore(t *testing.T) {
	store := NewModbusDataStore(10, 10, 10, 10)

	// 復元データを作成
	data := map[string]interface{}{
		AreaCoils:          []bool{true, false, true},
		AreaDiscreteInputs: []bool{false, true, false},
		AreaHoldingRegs:    []uint16{0x1111, 0x2222, 0x3333},
		AreaInputRegs:      []uint16{0x4444, 0x5555, 0x6666},
	}

	// 復元
	err := store.Restore(data)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// 確認
	val, _ := store.ReadBit(AreaCoils, 0)
	if !val {
		t.Error("expected coil[0] to be true")
	}

	val, _ = store.ReadBit(AreaDiscreteInputs, 1)
	if !val {
		t.Error("expected discreteInput[1] to be true")
	}

	word, _ := store.ReadWord(AreaHoldingRegs, 1)
	if word != 0x2222 {
		t.Errorf("expected 0x2222, got 0x%04x", word)
	}

	word, _ = store.ReadWord(AreaInputRegs, 2)
	if word != 0x6666 {
		t.Errorf("expected 0x6666, got 0x%04x", word)
	}
}

func TestModbusDataStore_ClearAll(t *testing.T) {
	store := NewModbusDataStore(10, 10, 10, 10)

	// データを設定
	_ = store.WriteBit(AreaCoils, 0, true)
	_ = store.WriteBit(AreaDiscreteInputs, 0, true)
	_ = store.WriteWord(AreaHoldingRegs, 0, 0x1234)
	_ = store.WriteWord(AreaInputRegs, 0, 0x5678)

	// クリア
	store.ClearAll()

	// 確認
	val, _ := store.ReadBit(AreaCoils, 0)
	if val {
		t.Error("expected coil[0] to be false after clear")
	}

	val, _ = store.ReadBit(AreaDiscreteInputs, 0)
	if val {
		t.Error("expected discreteInput[0] to be false after clear")
	}

	word, _ := store.ReadWord(AreaHoldingRegs, 0)
	if word != 0 {
		t.Errorf("expected 0, got 0x%04x after clear", word)
	}

	word, _ = store.ReadWord(AreaInputRegs, 0)
	if word != 0 {
		t.Errorf("expected 0, got 0x%04x after clear", word)
	}
}
