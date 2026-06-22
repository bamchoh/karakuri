package application

import (
	"context"
	"sync"

	"modbus_simulator/internal/domain/protocol"
)

// ===== fakeConfig =====

type fakeConfig struct {
	protocolType protocol.ProtocolType
	variant      string
}

func (c *fakeConfig) ProtocolType() protocol.ProtocolType { return c.protocolType }
func (c *fakeConfig) Variant() string                     { return c.variant }
func (c *fakeConfig) Validate() error                     { return nil }
func (c *fakeConfig) Clone() protocol.ProtocolConfig {
	cp := *c
	return &cp
}

// ===== fakeDataStore =====

// Modbus 互換のメモリエリア定義
var fakeModbusAreas = []protocol.MemoryArea{
	{ID: "coils", DisplayName: "Coils", IsBit: true, Size: 9999, OneOrigin: true},
	{ID: "discreteInputs", DisplayName: "Discrete Inputs", IsBit: true, Size: 9999, ReadOnly: true, OneOrigin: true},
	{ID: "holdingRegisters", DisplayName: "Holding Registers", IsBit: false, Size: 9999, OneOrigin: true},
	{ID: "inputRegisters", DisplayName: "Input Registers", IsBit: false, Size: 9999, ReadOnly: true, OneOrigin: true},
}

type fakeDataStore struct {
	mu    sync.Mutex
	bits  map[string]map[uint32]bool
	words map[string]map[uint32]uint16
}

func newFakeDataStore() *fakeDataStore {
	return &fakeDataStore{
		bits:  make(map[string]map[uint32]bool),
		words: make(map[string]map[uint32]uint16),
	}
}

func (d *fakeDataStore) GetAreas() []protocol.MemoryArea { return fakeModbusAreas }

func (d *fakeDataStore) getBit(area string, address uint32) bool {
	if d.bits[area] == nil {
		return false
	}
	return d.bits[area][address]
}

func (d *fakeDataStore) setBit(area string, address uint32, value bool) {
	if d.bits[area] == nil {
		d.bits[area] = make(map[uint32]bool)
	}
	d.bits[area][address] = value
}

func (d *fakeDataStore) getWord(area string, address uint32) uint16 {
	if d.words[area] == nil {
		return 0
	}
	return d.words[area][address]
}

func (d *fakeDataStore) setWord(area string, address uint32, value uint16) {
	if d.words[area] == nil {
		d.words[area] = make(map[uint32]uint16)
	}
	d.words[area][address] = value
}

func (d *fakeDataStore) ReadBit(area string, address uint32) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getBit(area, address), nil
}

func (d *fakeDataStore) WriteBit(area string, address uint32, value bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.setBit(area, address, value)
	return nil
}

func (d *fakeDataStore) ReadBits(area string, address uint32, count uint16) ([]bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]bool, count)
	for i := uint16(0); i < count; i++ {
		result[i] = d.getBit(area, address+uint32(i))
	}
	return result, nil
}

func (d *fakeDataStore) WriteBits(area string, address uint32, values []bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, v := range values {
		d.setBit(area, address+uint32(i), v)
	}
	return nil
}

func (d *fakeDataStore) ReadWord(area string, address uint32) (uint16, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getWord(area, address), nil
}

func (d *fakeDataStore) WriteWord(area string, address uint32, value uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.setWord(area, address, value)
	return nil
}

func (d *fakeDataStore) ReadWords(area string, address uint32, count uint16) ([]uint16, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]uint16, count)
	for i := uint16(0); i < count; i++ {
		result[i] = d.getWord(area, address+uint32(i))
	}
	return result, nil
}

func (d *fakeDataStore) WriteWords(area string, address uint32, values []uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, v := range values {
		d.setWord(area, address+uint32(i), v)
	}
	return nil
}

func (d *fakeDataStore) Snapshot() map[string]interface{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return map[string]interface{}{}
}

func (d *fakeDataStore) Restore(data map[string]interface{}) error { return nil }

func (d *fakeDataStore) ClearAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bits = make(map[string]map[uint32]bool)
	d.words = make(map[string]map[uint32]uint16)
}

// ===== fakeServer =====

type fakeServer struct {
	cfg    protocol.ProtocolConfig
	status protocol.ServerStatus
}

func (s *fakeServer) Start(_ context.Context) error {
	s.status = protocol.StatusRunning
	return nil
}

func (s *fakeServer) Stop() error {
	s.status = protocol.StatusStopped
	return nil
}

func (s *fakeServer) Status() protocol.ServerStatus       { return s.status }
func (s *fakeServer) ProtocolType() protocol.ProtocolType { return s.cfg.ProtocolType() }
func (s *fakeServer) Config() protocol.ProtocolConfig     { return s.cfg }
func (s *fakeServer) UpdateConfig(cfg protocol.ProtocolConfig) error {
	s.cfg = cfg
	return nil
}

// ===== fakeServerFactory =====

type fakeServerFactory struct {
	protocolType protocol.ProtocolType
	displayName  string
	variantID    string
}

// newFakeModbusFactory は Modbus 互換のフェイクファクトリーを作成する。
// protocolType: "modbus-tcp" / "modbus-rtu" / "modbus-ascii"
// variantID: "tcp" / "rtu" / "ascii"
func newFakeModbusFactory(protocolType protocol.ProtocolType, variantID, displayName string) *fakeServerFactory {
	return &fakeServerFactory{
		protocolType: protocolType,
		displayName:  displayName,
		variantID:    variantID,
	}
}

func (f *fakeServerFactory) Version() string                     { return "1.0.0" }
func (f *fakeServerFactory) ProtocolType() protocol.ProtocolType { return f.protocolType }
func (f *fakeServerFactory) DisplayName() string                 { return f.displayName }

func (f *fakeServerFactory) CreateServer(config protocol.ProtocolConfig, _ protocol.DataStore) (protocol.ProtocolServer, error) {
	return &fakeServer{cfg: config}, nil
}

func (f *fakeServerFactory) CreateDataStore() protocol.DataStore {
	return newFakeDataStore()
}

func (f *fakeServerFactory) DefaultConfig() protocol.ProtocolConfig {
	return &fakeConfig{protocolType: f.protocolType, variant: f.variantID}
}

func (f *fakeServerFactory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{
		{ID: f.variantID, DisplayName: f.displayName},
	}
}

func (f *fakeServerFactory) CreateConfigFromVariant(variantID string) protocol.ProtocolConfig {
	return &fakeConfig{protocolType: f.protocolType, variant: variantID}
}

func (f *fakeServerFactory) GetConfigFields(_ string) []protocol.ConfigField { return nil }

func (f *fakeServerFactory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	return protocol.ProtocolCapabilities{
		SupportsUnitID: true,
		UnitIDMin:      1,
		UnitIDMax:      247,
	}
}

func (f *fakeServerFactory) ConfigToMap(_ protocol.ProtocolConfig) map[string]interface{} {
	return map[string]interface{}{}
}

func (f *fakeServerFactory) MapToConfig(variantID string, _ map[string]interface{}) (protocol.ProtocolConfig, error) {
	return &fakeConfig{protocolType: f.protocolType, variant: variantID}, nil
}
