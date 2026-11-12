package modbus

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"karakuri/internal/domain/protocol"
)

// Modbus のプロトコルタイプ
const (
	ProtocolModbusTCP   protocol.ProtocolType = "modbus-tcp"
	ProtocolModbusRTU   protocol.ProtocolType = "modbus-rtu"
	ProtocolModbusASCII protocol.ProtocolType = "modbus-ascii"
)

// ErrUnitIdDisabled はUnitIDが無効化されている場合のエラー
var ErrUnitIdDisabled = errors.New("unit ID is disabled")

// BaseFactory は、TCP / RTU / ASCII のファクトリーに共通の部分
type BaseFactory struct{}

// CreateServer はサーバーを作成する
func (f *BaseFactory) CreateServer(config protocol.ProtocolConfig, store protocol.DataStore) (protocol.ProtocolServer, error) {
	modbusConfig, ok := config.(*ModbusConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type: expected ModbusConfig")
	}

	return NewModbusServer(modbusConfig, store), nil
}

// CreateDataStore はプロトコル用のデータストアを作成する
func (f *BaseFactory) CreateDataStore() protocol.DataStore {
	return NewModbusDataStore(65536, 65536, 65536, 65536)
}

// GetProtocolCapabilities はプロトコルの機能情報を返す
func (f *BaseFactory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	return protocol.ProtocolCapabilities{
		SupportsUnitID: true,
		UnitIDMin:      1,
		UnitIDMax:      247,
	}
}

// TCPFactory は Modbus TCP のファクトリー
type TCPFactory struct {
	BaseFactory
}

// ProtocolType はファクトリーが作成するプロトコルの種類を返す
func (f *TCPFactory) ProtocolType() protocol.ProtocolType {
	return ProtocolModbusTCP
}

// DisplayName はプロトコルの表示名を返す
func (f *TCPFactory) DisplayName() string {
	return "Modbus TCP"
}

// CreateConfigFromVariant は既定の設定を作成する（バリアントは 1 つだけなので、引数は使わない）
func (f *TCPFactory) CreateConfigFromVariant(_ string) protocol.ProtocolConfig {
	return DefaultTCPConfig()
}

// ConfigVariants は利用可能な設定バリアントを返す（バリアントは固定なので1エントリのみ）
func (f *TCPFactory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{
		{ID: string(VariantTCP), DisplayName: f.DisplayName()},
	}
}

// GetConfigFields は設定フィールドを返す
func (f *TCPFactory) GetConfigFields(_ string) []protocol.ConfigField {
	def := DefaultTCPConfig()
	return []protocol.ConfigField{
		{Name: "tcpAddress", Label: "アドレス", Description: "待ち受けるネットワークアドレス。0.0.0.0 で全インターフェースに対応します。", Type: "text", Required: true, Default: def.TCPAddress},
		{Name: "tcpPort", Label: "ポート", Description: "Modbus TCP の待ち受けポート番号。標準ポートは 502 です。", Type: "number", Required: true, Default: def.TCPPort, Min: intPtr(1), Max: intPtr(65535)},
	}
}

// ConfigToMap は設定をmapに変換する
func (f *TCPFactory) ConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	mc, ok := config.(*ModbusConfig)
	if !ok {
		return nil
	}
	return map[string]interface{}{
		"tcpAddress": mc.TCPAddress,
		"tcpPort":    mc.TCPPort,
	}
}

// MapToConfig はmapから設定を作成する（map にない項目は既定値のまま）
func (f *TCPFactory) MapToConfig(_ string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	config := DefaultTCPConfig()
	setString(settings, "tcpAddress", &config.TCPAddress)
	setInt(settings, "tcpPort", &config.TCPPort)
	return config, nil
}

// RTUFactory は Modbus RTU のファクトリー
type RTUFactory struct {
	BaseFactory
}

// ProtocolType はファクトリーが作成するプロトコルの種類を返す
func (f *RTUFactory) ProtocolType() protocol.ProtocolType {
	return ProtocolModbusRTU
}

// DisplayName はプロトコルの表示名を返す
func (f *RTUFactory) DisplayName() string {
	return "Modbus RTU"
}

// CreateConfigFromVariant は既定の設定を作成する（バリアントは 1 つだけなので、引数は使わない）
func (f *RTUFactory) CreateConfigFromVariant(_ string) protocol.ProtocolConfig {
	return DefaultRTUConfig()
}

// ConfigVariants は利用可能な設定バリアントを返す（バリアントは固定なので1エントリのみ）
func (f *RTUFactory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{
		{ID: string(VariantRTU), DisplayName: f.DisplayName()},
	}
}

// GetConfigFields は設定フィールドを返す
func (f *RTUFactory) GetConfigFields(_ string) []protocol.ConfigField {
	return serialConfigFields(f.DisplayName(), DefaultRTUConfig())
}

// ConfigToMap は設定をmapに変換する
func (f *RTUFactory) ConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	return serialConfigToMap(config)
}

// MapToConfig はmapから設定を作成する（map にない項目は既定値のまま）
func (f *RTUFactory) MapToConfig(_ string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	return applySerialSettings(DefaultRTUConfig(), settings), nil
}

// ASCIIFactory は Modbus ASCII のファクトリー
type ASCIIFactory struct {
	BaseFactory
}

// ProtocolType はファクトリーが作成するプロトコルの種類を返す
func (f *ASCIIFactory) ProtocolType() protocol.ProtocolType {
	return ProtocolModbusASCII
}

// DisplayName はプロトコルの表示名を返す
func (f *ASCIIFactory) DisplayName() string {
	return "Modbus ASCII"
}

// CreateConfigFromVariant は既定の設定を作成する（バリアントは 1 つだけなので、引数は使わない）
func (f *ASCIIFactory) CreateConfigFromVariant(_ string) protocol.ProtocolConfig {
	return DefaultASCIIConfig()
}

// ConfigVariants は利用可能な設定バリアントを返す（バリアントは固定なので1エントリのみ）
func (f *ASCIIFactory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{
		{ID: string(VariantASCII), DisplayName: f.DisplayName()},
	}
}

// GetConfigFields は設定フィールドを返す
func (f *ASCIIFactory) GetConfigFields(_ string) []protocol.ConfigField {
	return serialConfigFields(f.DisplayName(), DefaultASCIIConfig())
}

// ConfigToMap は設定をmapに変換する
func (f *ASCIIFactory) ConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	return serialConfigToMap(config)
}

// MapToConfig はmapから設定を作成する（map にない項目は既定値のまま）
func (f *ASCIIFactory) MapToConfig(_ string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	return applySerialSettings(DefaultASCIIConfig(), settings), nil
}

// ============================================================
// シリアル通信（RTU / ASCII）で共通の設定
// ============================================================

// serialConfigFields はシリアル通信の設定フィールドを返す。
// 各項目の既定値と、データビットの説明文の「通常 n ビット」は、そのプロトコルの既定の設定（def）から取る
func serialConfigFields(displayName string, def *ModbusConfig) []protocol.ConfigField {
	return []protocol.ConfigField{
		{Name: "serialPort", Label: "シリアルポート", Description: "通信に使用するシリアルポート（例: COM1、COM3）。", Type: "serialport", Required: true, Default: def.SerialPort, Category: "基本設定"},
		{Name: "baudRate", Label: "ボーレート", Description: "シリアル通信の速度（bps）。接続先デバイスと一致させてください。", Type: "select", Required: true, Default: def.BaudRate, Category: "基本設定", Options: []protocol.FieldOption{
			{Value: "9600", Label: "9600"},
			{Value: "19200", Label: "19200"},
			{Value: "38400", Label: "38400"},
			{Value: "57600", Label: "57600"},
			{Value: "115200", Label: "115200"},
		}},
		{Name: "dataBits", Label: "データビット", Description: "1フレームあたりのデータビット数。" + displayName + " は通常 " + strconv.Itoa(def.DataBits) + " ビットです。", Type: "select", Required: true, Default: def.DataBits, Category: "フレーム設定", Options: []protocol.FieldOption{
			{Value: "7", Label: "7"},
			{Value: "8", Label: "8"},
		}},
		{Name: "stopBits", Label: "ストップビット", Description: "フレーム末尾のストップビット数。", Type: "select", Required: true, Default: def.StopBits, Category: "フレーム設定", Options: []protocol.FieldOption{
			{Value: "1", Label: "1"},
			{Value: "2", Label: "2"},
		}},
		{Name: "parity", Label: "パリティ", Description: "エラー検出用のパリティビット。None（N）、Even（E）、Odd（O）から選択します。", Type: "select", Required: true, Default: def.Parity, Category: "フレーム設定", Options: []protocol.FieldOption{
			{Value: "N", Label: "None"},
			{Value: "E", Label: "Even"},
			{Value: "O", Label: "Odd"},
		}},
	}
}

// serialConfigToMap はシリアル通信の設定をmapに変換する
func serialConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	mc, ok := config.(*ModbusConfig)
	if !ok {
		return nil
	}
	return map[string]interface{}{
		"serialPort": mc.SerialPort,
		"baudRate":   mc.BaudRate,
		"dataBits":   mc.DataBits,
		"stopBits":   mc.StopBits,
		"parity":     mc.Parity,
	}
}

// applySerialSettings は、map にあるシリアル通信の設定を config に入れて返す
func applySerialSettings(config *ModbusConfig, settings map[string]interface{}) *ModbusConfig {
	setString(settings, "serialPort", &config.SerialPort)
	setInt(settings, "baudRate", &config.BaudRate)
	setInt(settings, "dataBits", &config.DataBits)
	setInt(settings, "stopBits", &config.StopBits)
	setString(settings, "parity", &config.Parity)
	return config
}

// setString は、settings[key] が文字列なら dst に入れる
func setString(settings map[string]interface{}, key string, dst *string) {
	if v, ok := settings[key].(string); ok {
		*dst = v
	}
}

// setInt は、settings[key] が数値なら dst に入れる（JSON から来た値は float64、Go から来た値は int）
func setInt(settings map[string]interface{}, key string, dst *int) {
	switch v := settings[key].(type) {
	case float64:
		*dst = int(v)
	case int:
		*dst = v
	}
}

func intPtr(i int) *int {
	return &i
}

// ModbusVariant はModbusのバリアント
type ModbusVariant string

const (
	VariantTCP   ModbusVariant = "tcp"
	VariantRTU   ModbusVariant = "rtu"
	VariantASCII ModbusVariant = "ascii"
)

// ModbusConfig はModbusサーバーの設定
type ModbusConfig struct {
	variant ModbusVariant

	// TCP設定
	TCPAddress string `json:"tcpAddress"`
	TCPPort    int    `json:"tcpPort"`

	// RTU設定
	SerialPort string `json:"serialPort"`
	BaudRate   int    `json:"baudRate"`
	DataBits   int    `json:"dataBits"`
	StopBits   int    `json:"stopBits"`
	Parity     string `json:"parity"`
}

// ProtocolType はプロトコルの種類を返す
func (c *ModbusConfig) ProtocolType() protocol.ProtocolType {
	switch c.variant {
	case VariantTCP:
		return ProtocolModbusTCP
	case VariantRTU:
		return ProtocolModbusRTU
	case VariantASCII:
		return ProtocolModbusASCII
	default:
		return ProtocolModbusTCP
	}
}

// Variant はバリアント名を返す
func (c *ModbusConfig) Variant() string {
	return string(c.variant)
}

// Validate は設定を検証する
func (c *ModbusConfig) Validate() error {
	switch c.variant {
	case VariantTCP:
		if c.TCPPort < 1 || c.TCPPort > 65535 {
			return fmt.Errorf("invalid TCP port: %d", c.TCPPort)
		}
	case VariantRTU, VariantASCII:
		if c.SerialPort == "" {
			return fmt.Errorf("serial port is required")
		}
		if c.BaudRate <= 0 {
			return fmt.Errorf("invalid baud rate: %d", c.BaudRate)
		}
	default:
		return fmt.Errorf("unknown variant: %s", c.variant)
	}
	return nil
}

// Clone は設定のコピーを作成する
func (c *ModbusConfig) Clone() protocol.ProtocolConfig {
	return &ModbusConfig{
		variant:    c.variant,
		TCPAddress: c.TCPAddress,
		TCPPort:    c.TCPPort,
		SerialPort: c.SerialPort,
		BaudRate:   c.BaudRate,
		DataBits:   c.DataBits,
		StopBits:   c.StopBits,
		Parity:     c.Parity,
	}
}

// GetVariant はバリアントを返す
func (c *ModbusConfig) GetVariant() ModbusVariant {
	return c.variant
}

// DefaultTCPConfig はデフォルトのTCP設定を返す
func DefaultTCPConfig() *ModbusConfig {
	return &ModbusConfig{
		variant:    VariantTCP,
		TCPAddress: "0.0.0.0",
		TCPPort:    502,
	}
}

// DefaultRTUConfig はデフォルトのRTU設定を返す
func DefaultRTUConfig() *ModbusConfig {
	return &ModbusConfig{
		variant:    VariantRTU,
		SerialPort: "COM1",
		BaudRate:   115200,
		DataBits:   8,
		StopBits:   1,
		Parity:     "N",
	}
}

// DefaultASCIIConfig はデフォルトのASCII設定を返す
func DefaultASCIIConfig() *ModbusConfig {
	return &ModbusConfig{
		variant:    VariantASCII,
		SerialPort: "COM1",
		BaudRate:   9600,
		DataBits:   7,
		StopBits:   1,
		Parity:     "E",
	}
}

var _ protocol.UnitIDConfigurable = (*ModbusServer)(nil)

// ModbusServer はModbusプロトコルサーバー
type ModbusServer struct {
	config      *ModbusConfig
	store       protocol.DataStore
	handler     *DataStoreHandler
	innerServer *Server
	status      protocol.ServerStatus
}

// NewModbusServer は新しいModbusServerを作成する
func NewModbusServer(config *ModbusConfig, store protocol.DataStore) *ModbusServer {
	return &ModbusServer{
		config:  config,
		store:   store,
		handler: NewDataStoreHandler(store),
		status:  protocol.StatusStopped,
	}
}

// Start はサーバーを起動する
func (s *ModbusServer) Start(ctx context.Context) error {
	if s.status == protocol.StatusRunning {
		return fmt.Errorf("server is already running")
	}

	// 内部サーバーを作成
	s.innerServer = NewServerWithHandler(s.config, s.handler)

	if err := s.innerServer.Start(); err != nil {
		s.status = protocol.StatusError
		return err
	}

	s.status = protocol.StatusRunning
	return nil
}

// Stop はサーバーを停止する
func (s *ModbusServer) Stop() error {
	if s.innerServer != nil {
		if err := s.innerServer.Stop(); err != nil {
			return err
		}
		s.innerServer = nil
	}
	s.status = protocol.StatusStopped
	return nil
}

// Status はサーバーの状態を返す
func (s *ModbusServer) Status() protocol.ServerStatus {
	return s.status
}

// ProtocolType はプロトコルの種類を返す
func (s *ModbusServer) ProtocolType() protocol.ProtocolType {
	return s.config.ProtocolType()
}

// Config は現在の設定を返す
func (s *ModbusServer) Config() protocol.ProtocolConfig {
	return s.config
}

// UpdateConfig は設定を更新する
func (s *ModbusServer) UpdateConfig(config protocol.ProtocolConfig) error {
	if s.status == protocol.StatusRunning {
		return fmt.Errorf("cannot update config while server is running")
	}

	modbusConfig, ok := config.(*ModbusConfig)
	if !ok {
		return fmt.Errorf("invalid config type: expected ModbusConfig")
	}

	// ハンドラーの無効化UnitIDリストを保持
	disabledIDs := s.handler.GetDisabledUnitIDs()
	s.config = modbusConfig
	s.handler = NewDataStoreHandler(s.store)
	s.handler.SetDisabledUnitIDs(disabledIDs)
	return nil
}

// SetUnitIdEnabled は指定したUnitIdの応答を有効/無効にする
func (s *ModbusServer) SetUnitIdEnabled(unitId uint8, enabled bool) {
	s.handler.SetUnitIdEnabled(unitId, enabled)
}

// IsUnitIdEnabled は指定したUnitIdが応答するかどうかを返す
func (s *ModbusServer) IsUnitIdEnabled(unitId uint8) bool {
	return s.handler.IsUnitIdEnabled(unitId)
}

// GetDisabledUnitIDs は無効化されたUnitIDのリストを返す
func (s *ModbusServer) GetDisabledUnitIDs() []uint8 {
	return s.handler.GetDisabledUnitIDs()
}

// SetDisabledUnitIDs は無効化するUnitIDのリストを設定する
func (s *ModbusServer) SetDisabledUnitIDs(ids []uint8) {
	s.handler.SetDisabledUnitIDs(ids)
}

// DataStoreHandler はDataStoreを使用するModbusハンドラー
type DataStoreHandler struct {
	store protocol.DataStore

	// 応答しない UnitID。設定の変更と通信の処理が別のゴルーチンから触るため mu で保護する
	mu              sync.RWMutex
	disabledUnitIDs map[uint8]bool
}

// NewDataStoreHandler は新しいDataStoreHandlerを作成する
func NewDataStoreHandler(store protocol.DataStore) *DataStoreHandler {
	return &DataStoreHandler{
		store:           store,
		disabledUnitIDs: make(map[uint8]bool),
	}
}

// SetUnitIdEnabled sets whether a unit ID responds
func (h *DataStoreHandler) SetUnitIdEnabled(unitId uint8, enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if enabled {
		delete(h.disabledUnitIDs, unitId)
	} else {
		h.disabledUnitIDs[unitId] = true
	}
}

// IsUnitIdEnabled checks if a unit ID responds
func (h *DataStoreHandler) IsUnitIdEnabled(unitId uint8) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return !h.disabledUnitIDs[unitId]
}

// GetDisabledUnitIDs returns the list of disabled unit IDs
func (h *DataStoreHandler) GetDisabledUnitIDs() []uint8 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]uint8, 0, len(h.disabledUnitIDs))
	for id := range h.disabledUnitIDs {
		ids = append(ids, id)
	}
	// 番号順に返す（map の順番のままだと、呼ぶたびに順番が変わる）
	slices.Sort(ids)
	return ids
}

// SetDisabledUnitIDs sets the list of disabled unit IDs
func (h *DataStoreHandler) SetDisabledUnitIDs(ids []uint8) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.disabledUnitIDs = make(map[uint8]bool)
	for _, id := range ids {
		h.disabledUnitIDs[id] = true
	}
}

// DataStore のインターフェースを満たすことを確認
var _ protocol.DataStore = (*ModbusDataStore)(nil)
