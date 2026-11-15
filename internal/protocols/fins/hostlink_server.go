package fins

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
	"karakuri/internal/protocols/serialport"
)

// FINS の Host Link（シリアル）のサーバー。シリアルポートで Host Link のフレームを受け取り、
// 載っている FINS のコマンドを、FINS/UDP・TCP と同じ処理（handler）で実行して応答する。
//
//   - 号機番号が違うフレームには応答しない（1 本の線に何台もつながっているとき、自分宛てのものだけに応答する）
//   - FCS が合わない・形が違うフレームには、Host Link の終了コード（13 / 14）で応答する。"FA" 以外のヘッダーコードには "IC"
//   - 応答は、長くても 1 つのフレームで返す（分けて送らない）
//   - 応答待ち時間（コマンドの "FA" の直後の 1 桁）は使わない（すぐ応答する）

// ProtocolFINSHostLink は FINS の Host Link（シリアル）のプロトコルタイプ（サーバーとクライアントで共通）
const ProtocolFINSHostLink protocol.ProtocolType = "fins-hostlink"

const (
	variantHostLink = "hostlink"
	// maxHostLinkBuffer は、CR が来ないまま溜めておく文字の上限（超えたら捨てる）
	maxHostLinkBuffer = maxHostLinkText + 64
)

// HostLinkConfig は FINS の Host Link のサーバーの設定
type HostLinkConfig struct {
	Serial serialport.Config
	Unit   int // 号機番号（0〜31）
}

// DefaultHostLinkConfig は既定の設定（CS / CJ の Host Link の既定と同じ 9600bps・7E2、号機番号 0）
func DefaultHostLinkConfig() *HostLinkConfig {
	return &HostLinkConfig{
		Serial: serialport.Config{Port: "COM1", BaudRate: 9600, DataBits: 7, StopBits: 2, Parity: "E"},
		Unit:   0,
	}
}

func (c *HostLinkConfig) ProtocolType() protocol.ProtocolType { return ProtocolFINSHostLink }
func (c *HostLinkConfig) Variant() string                     { return variantHostLink }

func (c *HostLinkConfig) Validate() error {
	if err := validateSerial(c.Serial); err != nil {
		return err
	}
	if c.Unit < 0 || c.Unit > maxHostLinkUnit {
		return fmt.Errorf("号機番号は 0〜%d で指定してください", maxHostLinkUnit)
	}
	return nil
}

func (c *HostLinkConfig) Clone() protocol.ProtocolConfig {
	clone := *c
	return &clone
}

func validateSerial(s serialport.Config) error {
	switch {
	case strings.TrimSpace(s.Port) == "":
		return fmt.Errorf("シリアルポートを選んでください")
	case s.BaudRate < 300 || s.BaudRate > 921600:
		return fmt.Errorf("ボーレートは 300〜921600 で指定してください")
	case s.DataBits != 7 && s.DataBits != 8:
		return fmt.Errorf("データビットは 7 か 8 にしてください")
	case s.StopBits != 1 && s.StopBits != 2:
		return fmt.Errorf("ストップビットは 1 か 2 にしてください")
	case s.Parity != "N" && s.Parity != "E" && s.Parity != "O":
		return fmt.Errorf("パリティは N / E / O のどれかにしてください")
	}
	return nil
}

// serialFields は、シリアルポートの設定項目（サーバーと接続先で共通）
func serialFields(def serialport.Config, portDescription string) []protocol.ConfigField {
	return []protocol.ConfigField{
		{Name: "serialPort", Label: "シリアルポート", Description: portDescription, Type: "serialport", Required: true, Default: def.Port},
		{Name: "baudRate", Label: "ボーレート", Description: "シリアル通信の速度（bps）。相手と一致させてください。", Type: "select", Required: true, Default: def.BaudRate, Options: []protocol.FieldOption{
			{Value: "4800", Label: "4800"},
			{Value: "9600", Label: "9600"},
			{Value: "19200", Label: "19200"},
			{Value: "38400", Label: "38400"},
			{Value: "57600", Label: "57600"},
			{Value: "115200", Label: "115200"},
		}},
		{Name: "dataBits", Label: "データビット", Description: "1 文字のデータビット数。Host Link は通常 7 ビットです。", Type: "select", Required: true, Default: def.DataBits, Options: []protocol.FieldOption{
			{Value: "7", Label: "7"},
			{Value: "8", Label: "8"},
		}},
		{Name: "stopBits", Label: "ストップビット", Description: "1 文字の終わりのストップビット数。Host Link は通常 2 ビットです。", Type: "select", Required: true, Default: def.StopBits, Options: []protocol.FieldOption{
			{Value: "1", Label: "1"},
			{Value: "2", Label: "2"},
		}},
		{Name: "parity", Label: "パリティ", Description: "誤り検出のパリティ。Host Link は通常 Even です。", Type: "select", Required: true, Default: def.Parity, Options: []protocol.FieldOption{
			{Value: "N", Label: "None"},
			{Value: "E", Label: "Even"},
			{Value: "O", Label: "Odd"},
		}},
	}
}

// applySerial は、map にあるシリアルの設定を s に入れる
func applySerial(settings map[string]interface{}, s *serialport.Config) {
	fields.SetString(settings, "serialPort", &s.Port)
	fields.SetInt(settings, "baudRate", &s.BaudRate)
	fields.SetInt(settings, "dataBits", &s.DataBits)
	fields.SetInt(settings, "stopBits", &s.StopBits)
	fields.SetString(settings, "parity", &s.Parity)
	s.Port = strings.TrimSpace(s.Port)
	s.Parity = strings.ToUpper(strings.TrimSpace(s.Parity))
}

func serialToMap(s serialport.Config, m map[string]interface{}) map[string]interface{} {
	m["serialPort"] = s.Port
	m["baudRate"] = s.BaudRate
	m["dataBits"] = s.DataBits
	m["stopBits"] = s.StopBits
	m["parity"] = s.Parity
	return m
}

// HostLinkFactory は FINS の Host Link のサーバーのファクトリー。メモリエリアは FINS/UDP・TCP と同じ
type HostLinkFactory struct {
	// open はポートを開く関数。nil なら実際のシリアルポートを開く（テストでは仮想のケーブルを渡す）
	open serialport.Opener
}

func (f *HostLinkFactory) ProtocolType() protocol.ProtocolType { return ProtocolFINSHostLink }
func (f *HostLinkFactory) DisplayName() string                 { return "FINS（Host Link）" }

func (f *HostLinkFactory) CreateServer(config protocol.ProtocolConfig, store protocol.DataStore) (protocol.ProtocolServer, error) {
	c, ok := config.(*HostLinkConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type: expected fins.HostLinkConfig")
	}
	return newHostLinkServer(c, store, f.open), nil
}

func (f *HostLinkFactory) CreateDataStore() protocol.DataStore { return NewDataStore() }

func (f *HostLinkFactory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{{ID: variantHostLink, DisplayName: f.DisplayName()}}
}

func (f *HostLinkFactory) CreateConfigFromVariant(_ string) protocol.ProtocolConfig {
	return DefaultHostLinkConfig()
}

func (f *HostLinkFactory) GetConfigFields(_ string) []protocol.ConfigField {
	def := DefaultHostLinkConfig()
	return append(serialFields(def.Serial, "通信に使うシリアルポート（例: COM1、COM3）。"),
		protocol.ConfigField{Name: "unit", Label: "号機番号", Description: "この PLC の Host Link の号機番号（0〜31）。違う号機番号宛てのコマンドには応答しません。", Type: "number", Required: true, Default: def.Unit, Min: fields.IntPtr(0), Max: fields.IntPtr(maxHostLinkUnit)},
	)
}

func (f *HostLinkFactory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	return protocol.ProtocolCapabilities{}
}

func (f *HostLinkFactory) ConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	c, ok := config.(*HostLinkConfig)
	if !ok {
		return nil
	}
	return serialToMap(c.Serial, map[string]interface{}{"unit": c.Unit})
}

// MapToConfig は map から設定を作る（map にない項目は既定値のまま）
func (f *HostLinkFactory) MapToConfig(_ string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	c := DefaultHostLinkConfig()
	applySerial(settings, &c.Serial)
	fields.SetInt(settings, "unit", &c.Unit)
	return c, nil
}

// --- サーバー ---

// HostLinkServer は FINS の Host Link のサーバー
type HostLinkServer struct {
	store   protocol.DataStore
	open    serialport.Opener
	handler *handler

	mu     sync.Mutex
	config *HostLinkConfig
	status protocol.ServerStatus
	loop   *serialport.Loop

	// buf は、まだ CR が来ていない受信中の文字（読み取りのゴルーチンだけが触る）
	buf []byte
}

func newHostLinkServer(config *HostLinkConfig, store protocol.DataStore, open serialport.Opener) *HostLinkServer {
	return &HostLinkServer{config: config, store: store, open: open, handler: newHandler(store), status: protocol.StatusStopped}
}

func (s *HostLinkServer) ProtocolType() protocol.ProtocolType { return ProtocolFINSHostLink }

func (s *HostLinkServer) Status() protocol.ServerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *HostLinkServer) Config() protocol.ProtocolConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func (s *HostLinkServer) UpdateConfig(config protocol.ProtocolConfig) error {
	c, ok := config.(*HostLinkConfig)
	if !ok {
		return fmt.Errorf("invalid config type: expected fins.HostLinkConfig")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == protocol.StatusRunning {
		return fmt.Errorf("cannot update config while server is running")
	}
	s.config = c
	return nil
}

// Start はシリアルポートを開いて、コマンドを待つ
func (s *HostLinkServer) Start(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == protocol.StatusRunning {
		return fmt.Errorf("server is already running")
	}
	if err := s.config.Validate(); err != nil {
		s.status = protocol.StatusError
		return err
	}
	unit := s.config.Unit
	loop := &serialport.Loop{
		Config:  s.config.Serial,
		Open:    s.open,
		Name:    "FINS Host Link",
		OnData:  func(data []byte, write func([]byte) error) { s.receive(unit, data, write) },
		OnReset: func() { s.buf = s.buf[:0] },
	}
	if err := loop.Start(); err != nil {
		s.status = protocol.StatusError
		return fmt.Errorf("failed to start server: %w", err)
	}
	s.loop = loop
	s.status = protocol.StatusRunning
	return nil
}

// Stop はコマンドを待つのをやめ、シリアルポートを閉じる
func (s *HostLinkServer) Stop() error {
	s.mu.Lock()
	loop := s.loop
	s.loop = nil
	s.status = protocol.StatusStopped
	s.mu.Unlock()
	if loop != nil {
		loop.Stop()
	}
	return nil
}

// receive は、届いた文字を溜め、CR までを 1 つのフレームとして処理する
func (s *HostLinkServer) receive(unit int, data []byte, write func([]byte) error) {
	s.buf = append(s.buf, data...)
	for {
		i := bytes.IndexByte(s.buf, hostLinkCR)
		if i < 0 {
			if len(s.buf) > maxHostLinkBuffer {
				s.buf = s.buf[:0]
			}
			return
		}
		frame := s.buf[:i+1]
		// '@' より前に届いたもの（前のフレームの残りなど）は読み飛ばす
		if at := bytes.IndexByte(frame, hostLinkStart); at > 0 {
			frame = frame[at:]
		}
		if resp := s.respond(unit, frame); resp != nil {
			_ = write(resp)
		}
		s.buf = append(s.buf[:0], s.buf[i+1:]...)
	}
}

// respond は、Host Link のフレーム 1 つ（CR まで）への応答を返す。応答しないときは nil
func (s *HostLinkServer) respond(unit int, frame []byte) []byte {
	if len(frame) < 3 || frame[0] != hostLinkStart {
		return nil
	}
	if target, err := strconv.Atoi(string(frame[1:3])); err != nil || target != unit {
		return nil // 号機番号が読めない・ほかの号機宛て
	}
	f, err := parseHostLinkFrame(frame)
	switch {
	case err == errHostLinkFCS:
		return encodeHostLinkError(unit, f.code, hostLinkEndFCS)
	case err != nil:
		// 分けて送られたコマンド（'*' のない途中のフレーム）も、ここで形式の誤りになる
		return encodeHostLinkError(unit, hostLinkHeaderFA, hostLinkEndFormat)
	case f.code != hostLinkHeaderFA:
		return encodeHostLinkError(unit, hostLinkHeaderIC, "")
	}
	if len(f.text) < 1 {
		return encodeHostLinkError(unit, hostLinkHeaderFA, hostLinkEndFormat)
	}
	raw, err := decodeHex(f.text[1:]) // 先頭の 1 桁は応答待ち時間
	if err != nil {
		return encodeHostLinkError(unit, hostLinkHeaderFA, hostLinkEndFormat)
	}
	fins, err := finsFromHostLink(raw)
	if err != nil || len(fins) < headerLength+2 {
		return encodeHostLinkError(unit, hostLinkHeaderFA, hostLinkEndFormat)
	}
	short := fins[0]&icfGateway == 0
	resp := s.handler.handle(fins)
	if resp == nil {
		return nil // 応答不要のコマンド
	}
	if short {
		resp[0] &^= icfGateway // 短い形の応答の ICF は 40
	}
	return encodeHostLinkResponse(unit, resp)
}
