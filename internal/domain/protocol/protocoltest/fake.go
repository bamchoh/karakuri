// Package protocoltest はテスト用のフェイクのプロトコル実装（ファクトリー・サーバー・DataStore）を提供する。
// プロトコル固有の実装（プラグイン）に依存せずに、ホスト側の処理を確認するために使う。
package protocoltest

import (
	"context"
	"fmt"
	"sync"

	"karakuri/internal/domain/protocol"
)

// ===== Config =====

// Config はフェイクの設定。Settings をそのまま持つ
type Config struct {
	Type      protocol.ProtocolType
	VariantID string
	Settings  map[string]interface{}
}

func (c *Config) ProtocolType() protocol.ProtocolType { return c.Type }
func (c *Config) Variant() string                     { return c.VariantID }
func (c *Config) Validate() error                     { return nil }
func (c *Config) Clone() protocol.ProtocolConfig {
	return &Config{Type: c.Type, VariantID: c.VariantID, Settings: copySettings(c.Settings)}
}

func copySettings(m map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// ===== DataStore =====

// Areas は Modbus と同じ構成のメモリエリア
var Areas = []protocol.MemoryArea{
	{ID: "coils", DisplayName: "Coils", IsBit: true, Size: 9999, OneOrigin: true, ShortName: "0"},
	{ID: "discreteInputs", DisplayName: "Discrete Inputs", IsBit: true, Size: 9999, ReadOnly: true, OneOrigin: true, ShortName: "1"},
	{ID: "holdingRegisters", DisplayName: "Holding Registers", IsBit: false, Size: 9999, OneOrigin: true, ShortName: "4"},
	{ID: "inputRegisters", DisplayName: "Input Registers", IsBit: false, Size: 9999, ReadOnly: true, OneOrigin: true, ShortName: "3"},
}

// DataStore はメモリ上のフェイクの DataStore。
// 存在しないエリア・範囲外のアドレスはエラーにする。
type DataStore struct {
	mu    sync.Mutex
	bits  map[string]map[uint32]bool
	words map[string]map[uint32]uint16
}

// NewDataStore は空の DataStore を作る
func NewDataStore() *DataStore {
	return &DataStore{
		bits:  make(map[string]map[uint32]bool),
		words: make(map[string]map[uint32]uint16),
	}
}

func (d *DataStore) GetAreas() []protocol.MemoryArea { return Areas }

// check はエリアの種類とアドレス範囲を確かめる
func check(area string, isBit bool, address uint32, count int) error {
	for _, a := range Areas {
		if a.ID != area {
			continue
		}
		if a.IsBit != isBit {
			return fmt.Errorf("area %q: wrong access type", area)
		}
		if uint64(address)+uint64(count) > uint64(a.Size) {
			return fmt.Errorf("area %q: address %d (count %d) out of range", area, address, count)
		}
		return nil
	}
	return fmt.Errorf("unknown area %q", area)
}

func (d *DataStore) ReadBit(area string, address uint32) (bool, error) {
	vals, err := d.ReadBits(area, address, 1)
	if err != nil {
		return false, err
	}
	return vals[0], nil
}

func (d *DataStore) WriteBit(area string, address uint32, value bool) error {
	return d.WriteBits(area, address, []bool{value})
}

func (d *DataStore) ReadBits(area string, address uint32, count uint16) ([]bool, error) {
	if err := check(area, true, address, int(count)); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]bool, count)
	for i := range result {
		result[i] = d.bits[area][address+uint32(i)]
	}
	return result, nil
}

func (d *DataStore) WriteBits(area string, address uint32, values []bool) error {
	if err := check(area, true, address, len(values)); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.bits[area] == nil {
		d.bits[area] = make(map[uint32]bool)
	}
	for i, v := range values {
		d.bits[area][address+uint32(i)] = v
	}
	return nil
}

func (d *DataStore) ReadWord(area string, address uint32) (uint16, error) {
	vals, err := d.ReadWords(area, address, 1)
	if err != nil {
		return 0, err
	}
	return vals[0], nil
}

func (d *DataStore) WriteWord(area string, address uint32, value uint16) error {
	return d.WriteWords(area, address, []uint16{value})
}

func (d *DataStore) ReadWords(area string, address uint32, count uint16) ([]uint16, error) {
	if err := check(area, false, address, int(count)); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]uint16, count)
	for i := range result {
		result[i] = d.words[area][address+uint32(i)]
	}
	return result, nil
}

func (d *DataStore) WriteWords(area string, address uint32, values []uint16) error {
	if err := check(area, false, address, len(values)); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.words[area] == nil {
		d.words[area] = make(map[uint32]uint16)
	}
	for i, v := range values {
		d.words[area][address+uint32(i)] = v
	}
	return nil
}

// Snapshot は書き込まれたワードを {エリア: {アドレス: 値}} の形で返す（JSON にできる形）
func (d *DataStore) Snapshot() map[string]interface{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	snap := map[string]interface{}{}
	for area, words := range d.words {
		m := map[string]interface{}{}
		for addr, v := range words {
			m[fmt.Sprint(addr)] = float64(v)
		}
		snap[area] = m
	}
	return snap
}

// Restore は Snapshot の形のデータを書き戻す
func (d *DataStore) Restore(data map[string]interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.words = make(map[string]map[uint32]uint16)
	for area, raw := range data {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		d.words[area] = make(map[uint32]uint16)
		for addrStr, v := range m {
			var addr uint32
			if _, err := fmt.Sscan(addrStr, &addr); err != nil {
				return err
			}
			f, _ := v.(float64)
			d.words[area][addr] = uint16(f)
		}
	}
	return nil
}

func (d *DataStore) ClearAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bits = make(map[string]map[uint32]bool)
	d.words = make(map[string]map[uint32]uint16)
}

// ===== Server =====

// Server はフェイクのサーバー。Start / Stop で状態だけが変わる
type Server struct {
	mu       sync.Mutex
	Cfg      protocol.ProtocolConfig
	status   protocol.ServerStatus
	StartErr error // nil 以外なら Start がこのエラーを返す
	disabled map[uint8]bool
}

// NewServer は停止中のサーバーを作る
func NewServer(cfg protocol.ProtocolConfig) *Server {
	return &Server{Cfg: cfg, status: protocol.StatusStopped}
}

func (s *Server) Start(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.StartErr != nil {
		return s.StartErr
	}
	s.status = protocol.StatusRunning
	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = protocol.StatusStopped
	return nil
}

func (s *Server) Status() protocol.ServerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Server) ProtocolType() protocol.ProtocolType { return s.Cfg.ProtocolType() }
func (s *Server) Config() protocol.ProtocolConfig     { return s.Cfg }
func (s *Server) UpdateConfig(cfg protocol.ProtocolConfig) error {
	s.Cfg = cfg
	return nil
}

// UnitIDConfigurable

func (s *Server) GetDisabledUnitIDs() []uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := []uint8{}
	for id := 0; id <= 255; id++ {
		if s.disabled[uint8(id)] {
			ids = append(ids, uint8(id))
		}
	}
	return ids
}

func (s *Server) SetUnitIdEnabled(unitId uint8, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled == nil {
		s.disabled = make(map[uint8]bool)
	}
	if enabled {
		delete(s.disabled, unitId)
	} else {
		s.disabled[unitId] = true
	}
}

func (s *Server) SetDisabledUnitIDs(ids []uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disabled = make(map[uint8]bool)
	for _, id := range ids {
		s.disabled[id] = true
	}
}

// ===== Factory =====

// Factory はフェイクのファクトリー。設定は「port」を持ち、1〜65535 以外を拒否する
type Factory struct {
	Type    protocol.ProtocolType
	Name    string
	Variant string
}

// NewFactory は Modbus と同じメモリエリア構成のフェイクのファクトリーを作る
func NewFactory(protocolType protocol.ProtocolType, variantID, displayName string) *Factory {
	return &Factory{Type: protocolType, Name: displayName, Variant: variantID}
}

func (f *Factory) Version() string                     { return "1.0.0" }
func (f *Factory) ProtocolType() protocol.ProtocolType { return f.Type }
func (f *Factory) DisplayName() string                 { return f.Name }

func (f *Factory) CreateServer(config protocol.ProtocolConfig, _ protocol.DataStore) (protocol.ProtocolServer, error) {
	return NewServer(config), nil
}

func (f *Factory) CreateDataStore() protocol.DataStore { return NewDataStore() }

func (f *Factory) DefaultConfig() protocol.ProtocolConfig {
	return f.CreateConfigFromVariant(f.Variant)
}

func (f *Factory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{{ID: f.Variant, DisplayName: f.Name}}
}

// DefaultPort は設定「port」の既定値
const DefaultPort = 502

func (f *Factory) CreateConfigFromVariant(variantID string) protocol.ProtocolConfig {
	return &Config{Type: f.Type, VariantID: variantID, Settings: map[string]interface{}{"port": float64(DefaultPort)}}
}

func (f *Factory) GetConfigFields(_ string) []protocol.ConfigField {
	min, max := 1, 65535
	return []protocol.ConfigField{
		{Name: "port", Label: "ポート番号", Type: "number", Required: true, Default: DefaultPort, Min: &min, Max: &max, Category: "ネットワーク"},
	}
}

func (f *Factory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	return protocol.ProtocolCapabilities{SupportsUnitID: true, UnitIDMin: 1, UnitIDMax: 247}
}

func (f *Factory) ConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	c, ok := config.(*Config)
	if !ok {
		return map[string]interface{}{}
	}
	return copySettings(c.Settings)
}

func (f *Factory) MapToConfig(variantID string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	cfg := f.CreateConfigFromVariant(variantID).(*Config)
	for k, v := range settings {
		cfg.Settings[k] = v
	}
	port, ok := cfg.Settings["port"].(float64)
	if !ok || port < 1 || port > 65535 {
		return nil, fmt.Errorf("port は 1〜65535 で指定してください")
	}
	return cfg, nil
}
