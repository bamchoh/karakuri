// Package protocoltest はテスト用のフェイクのプロトコル実装（ファクトリー・サーバー・DataStore）を提供する。
// プロトコル固有の実装（internal/protocols）に依存せずに、ホスト側の処理を確認するために使う。
package protocoltest

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

// check はエリアがあるかと、address から points 点（エリアの単位: ビットのエリアは点、ワードのエリアはワード）が
// 範囲に収まるかを確かめ、エリアを返す
func check(area string, address uint64, points uint64) (protocol.MemoryArea, error) {
	for _, a := range Areas {
		if a.ID != area {
			continue
		}
		size := uint64(a.Size)
		if address+points > size {
			return a, fmt.Errorf("area %q: address %d (count %d) out of range", area, address, points)
		}
		return a, nil
	}
	return protocol.MemoryArea{}, fmt.Errorf("unknown area %q", area)
}

// bitsRange は、ビットで読み書きするときの範囲を確かめる（ワードのエリアなら、address はワード × 16 + ビット）
func bitsRange(area string, address uint32, count int) (protocol.MemoryArea, error) {
	for _, a := range Areas {
		if a.ID == area && !a.IsBit {
			if uint64(address)+uint64(count) > uint64(a.Size)*16 {
				return a, fmt.Errorf("area %q: bit %d (count %d) out of range", area, address, count)
			}
			return a, nil
		}
	}
	return check(area, uint64(address), uint64(count))
}

// wordsRange は、ワードで読み書きするときの範囲を確かめる（ビットのエリアなら、address のビットから 16 点ずつ）
func wordsRange(area string, address uint32, count int) (protocol.MemoryArea, error) {
	for _, a := range Areas {
		if a.ID == area && a.IsBit {
			return check(area, uint64(address), uint64(count)*16)
		}
	}
	return check(area, uint64(address), uint64(count))
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

// getBit / setBit は、エリアの n 番目のビット（ワードのエリアなら ワード × 16 + ビット）を読み書きする（ロック済み）
func (d *DataStore) getBit(a protocol.MemoryArea, n uint32) bool {
	if a.IsBit {
		return d.bits[a.ID][n]
	}
	return d.words[a.ID][n/16]&(1<<(n%16)) != 0
}

func (d *DataStore) setBit(a protocol.MemoryArea, n uint32, v bool) {
	if a.IsBit {
		if d.bits[a.ID] == nil {
			d.bits[a.ID] = make(map[uint32]bool)
		}
		d.bits[a.ID][n] = v
		return
	}
	if d.words[a.ID] == nil {
		d.words[a.ID] = make(map[uint32]uint16)
	}
	w := d.words[a.ID][n/16] &^ (1 << (n % 16))
	if v {
		w |= 1 << (n % 16)
	}
	d.words[a.ID][n/16] = w
}

func (d *DataStore) ReadBits(area string, address uint32, count uint16) ([]bool, error) {
	a, err := bitsRange(area, address, int(count))
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]bool, count)
	for i := range result {
		result[i] = d.getBit(a, address+uint32(i))
	}
	return result, nil
}

func (d *DataStore) WriteBits(area string, address uint32, values []bool) error {
	a, err := bitsRange(area, address, len(values))
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, v := range values {
		d.setBit(a, address+uint32(i), v)
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
	a, err := wordsRange(area, address, int(count))
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]uint16, count)
	for i := range result {
		if !a.IsBit {
			result[i] = d.words[area][address+uint32(i)]
			continue
		}
		for b := range 16 {
			if d.getBit(a, address+uint32(i*16+b)) {
				result[i] |= 1 << b
			}
		}
	}
	return result, nil
}

func (d *DataStore) WriteWords(area string, address uint32, values []uint16) error {
	a, err := wordsRange(area, address, len(values))
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, v := range values {
		if !a.IsBit {
			if d.words[area] == nil {
				d.words[area] = make(map[uint32]uint16)
			}
			d.words[area][address+uint32(i)] = v
			continue
		}
		for b := range 16 {
			d.setBit(a, address+uint32(i*16+b), v&(1<<b) != 0)
		}
	}
	return nil
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

func (f *Factory) ProtocolType() protocol.ProtocolType { return f.Type }
func (f *Factory) DisplayName() string                 { return f.Name }

func (f *Factory) CreateServer(config protocol.ProtocolConfig, _ protocol.DataStore) (protocol.ProtocolServer, error) {
	return NewServer(config), nil
}

func (f *Factory) CreateDataStore() protocol.DataStore { return NewDataStore() }

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

// ParseAddress は、フェイクのアドレスの書き方「エリアの ID:番号[.ビット]」（"holdingRegisters:10.3"、"coils:5"）を読む。
// 番号は 0 始まり。ビットはワードのエリアにだけ付けられる（0〜15）
func (f *Factory) ParseAddress(text string) (protocol.DeviceAddress, error) {
	area, rest, ok := strings.Cut(text, ":")
	if !ok {
		return protocol.DeviceAddress{}, fmt.Errorf("アドレスの形が違います: %q", text)
	}
	var def *protocol.MemoryArea
	for i := range Areas {
		if Areas[i].ID == area {
			def = &Areas[i]
		}
	}
	if def == nil {
		return protocol.DeviceAddress{}, fmt.Errorf("エリアがありません: %q", area)
	}
	numText, bitText, hasBit := strings.Cut(rest, ".")
	n, err := strconv.ParseUint(numText, 10, 32)
	if err != nil {
		return protocol.DeviceAddress{}, fmt.Errorf("番号が読めません: %q", text)
	}
	if !hasBit {
		return protocol.DeviceAddress{Area: area, Address: uint32(n), Bit: def.IsBit}, nil
	}
	b, err := strconv.ParseUint(bitText, 10, 8)
	if err != nil || b > 15 || def.IsBit {
		return protocol.DeviceAddress{}, fmt.Errorf("ビットの位置が違います: %q", text)
	}
	return protocol.DeviceAddress{Area: area, Address: uint32(n)*16 + uint32(b), Bit: true}, nil
}
