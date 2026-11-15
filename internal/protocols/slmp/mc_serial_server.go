package slmp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
	"karakuri/internal/protocols/serialport"
)

// MC プロトコル（シリアル。4C フレームの形式 4 / 形式 5）のサーバー。シリアルコミュニケーションユニット（C24）のつもりで、
// シリアルポートで要求を受け取り、SLMP と同じ処理（handler）で実行して応答する。
//
//   - 局番の違う要求には応答しない（1 本の線に何台もつながっているとき、自分宛てのものだけに応答する）。経路のほかの欄は確かめない
//   - サムチェックが合わない・読めない要求、扱えないコマンドには、異常の応答（形式 4 は NAK、形式 5 はエラーコード）を返す。
//     エラーコードは SLMP と同じもの（C05C など）で、実際の C24 が返すコードとは違うことがある
//   - デバイスとメモリは SLMP と同じ（別のプロトコルなので、メモリは SLMP のサーバーとは別）

// ProtocolMCSerial は MC プロトコル（シリアル）のプロトコルタイプ（サーバーとクライアントで共通）
const ProtocolMCSerial protocol.ProtocolType = "mc-serial"

const variantMCSerial = "4c"

// MCSerialConfig は MC プロトコル（シリアル）のサーバーの設定
type MCSerialConfig struct {
	Serial   serialport.Config
	Format   int  // 4 か 5
	SumCheck bool // サムチェックを付ける・確かめる
	Station  int  // 局番（0〜31）
}

// DefaultMCSerialConfig は既定の設定
func DefaultMCSerialConfig() *MCSerialConfig {
	return &MCSerialConfig{
		Serial:   serialport.Config{Port: "COM1", BaudRate: 9600, DataBits: 8, StopBits: 1, Parity: "N"},
		Format:   mcFormat4,
		SumCheck: true,
		Station:  0,
	}
}

func (c *MCSerialConfig) ProtocolType() protocol.ProtocolType { return ProtocolMCSerial }
func (c *MCSerialConfig) Variant() string                     { return variantMCSerial }

func (c *MCSerialConfig) Validate() error {
	if err := serialport.Validate(c.Serial); err != nil {
		return err
	}
	return validateMCOptions(c.Format, c.Station)
}

func validateMCOptions(format, station int) error {
	switch {
	case format != mcFormat4 && format != mcFormat5:
		return fmt.Errorf("形式は 4（ASCII）か 5（バイナリ）にしてください")
	case station < 0 || station > maxMCStation:
		return fmt.Errorf("局番は 0〜%d で指定してください", maxMCStation)
	}
	return nil
}

func (c *MCSerialConfig) Clone() protocol.ProtocolConfig {
	clone := *c
	return &clone
}

// mcOptionFields は、形式・サムチェック・局番の設定項目（サーバーと接続先で共通）
func mcOptionFields(format int, sum bool, station int, stationDescription string) []protocol.ConfigField {
	return []protocol.ConfigField{
		{Name: "format", Label: "形式", Description: "MC プロトコルの 4C フレームの形式。相手の設定と合わせてください。", Type: "select", Required: true, Default: format, Options: []protocol.FieldOption{
			{Value: "4", Label: "形式 4（ASCII コード、CR LF 付き）"},
			{Value: "5", Label: "形式 5（バイナリコード）"},
		}},
		{Name: "sumCheck", Label: "サムチェック", Description: "サムチェックコードを付けるかどうか。相手の設定と合わせてください。", Type: "select", Required: true, Default: sumText(sum), Options: []protocol.FieldOption{
			{Value: "on", Label: "あり"},
			{Value: "off", Label: "なし"},
		}},
		{Name: "station", Label: "局番", Description: stationDescription, Type: "number", Required: true, Default: station, Min: fields.IntPtr(0), Max: fields.IntPtr(maxMCStation)},
	}
}

func sumText(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// applyMCOptions は、map にある形式・サムチェック・局番を入れる
// （形式と局番は、数値でも文字列でも読む。画面は選択肢を数値にして送るが、REST API などからは文字列で来ることがある）
func applyMCOptions(settings map[string]interface{}, format *int, sum *bool, station *int) {
	setIntOrString(settings, "format", format)
	switch v := settings["sumCheck"].(type) {
	case string:
		*sum = v != "off"
	case bool:
		*sum = v
	}
	setIntOrString(settings, "station", station)
}

func setIntOrString(settings map[string]interface{}, key string, dst *int) {
	if s, ok := settings[key].(string); ok {
		if n, err := fields.ParseInteger(s); err == nil {
			*dst = int(n)
		}
		return
	}
	fields.SetInt(settings, key, dst)
}

// MCSerialFactory は MC プロトコル（シリアル）のサーバーのファクトリー。デバイスとメモリは SLMP と同じ
type MCSerialFactory struct {
	// open はポートを開く関数。nil なら実際のシリアルポートを開く（テストでは仮想のケーブルを渡す）
	open serialport.Opener
}

func (f *MCSerialFactory) ProtocolType() protocol.ProtocolType { return ProtocolMCSerial }
func (f *MCSerialFactory) DisplayName() string                 { return "MC プロトコル（シリアル）" }

func (f *MCSerialFactory) CreateServer(config protocol.ProtocolConfig, store protocol.DataStore) (protocol.ProtocolServer, error) {
	c, ok := config.(*MCSerialConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type: expected slmp.MCSerialConfig")
	}
	return newMCSerialServer(c, store, f.open), nil
}

func (f *MCSerialFactory) CreateDataStore() protocol.DataStore { return NewDataStore() }

func (f *MCSerialFactory) ConfigVariants() []protocol.ConfigVariant {
	return []protocol.ConfigVariant{{ID: variantMCSerial, DisplayName: "MC プロトコル（4C フレーム）"}}
}

func (f *MCSerialFactory) CreateConfigFromVariant(_ string) protocol.ProtocolConfig {
	return DefaultMCSerialConfig()
}

func (f *MCSerialFactory) GetConfigFields(_ string) []protocol.ConfigField {
	def := DefaultMCSerialConfig()
	return append(serialport.ConfigFields(def.Serial, "通信に使うシリアルポート（例: COM1、COM3）。"),
		mcOptionFields(def.Format, def.SumCheck, def.Station, "この局（C24）の局番（0〜31）。違う局番宛ての要求には応答しません。")...)
}

func (f *MCSerialFactory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	return protocol.ProtocolCapabilities{}
}

func (f *MCSerialFactory) ConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	c, ok := config.(*MCSerialConfig)
	if !ok {
		return nil
	}
	return serialport.ToMap(c.Serial, map[string]interface{}{"format": c.Format, "sumCheck": sumText(c.SumCheck), "station": c.Station})
}

// MapToConfig は map から設定を作る（map にない項目は既定値のまま）
func (f *MCSerialFactory) MapToConfig(_ string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	c := DefaultMCSerialConfig()
	serialport.ApplySettings(settings, &c.Serial)
	applyMCOptions(settings, &c.Format, &c.SumCheck, &c.Station)
	return c, nil
}

// --- サーバー ---

// MCSerialServer は MC プロトコル（シリアル）のサーバー
type MCSerialServer struct {
	store   protocol.DataStore
	open    serialport.Opener
	handler *handler

	mu     sync.Mutex
	config *MCSerialConfig
	status protocol.ServerStatus
	loop   *serialport.Loop

	// buf は、まだフレームになっていない受信中のバイト（読み取りのゴルーチンだけが触る）
	buf []byte
}

func newMCSerialServer(config *MCSerialConfig, store protocol.DataStore, open serialport.Opener) *MCSerialServer {
	return &MCSerialServer{config: config, store: store, open: open, handler: newHandler(store), status: protocol.StatusStopped}
}

func (s *MCSerialServer) ProtocolType() protocol.ProtocolType { return ProtocolMCSerial }

func (s *MCSerialServer) Status() protocol.ServerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *MCSerialServer) Config() protocol.ProtocolConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func (s *MCSerialServer) UpdateConfig(config protocol.ProtocolConfig) error {
	c, ok := config.(*MCSerialConfig)
	if !ok {
		return fmt.Errorf("invalid config type: expected slmp.MCSerialConfig")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == protocol.StatusRunning {
		return fmt.Errorf("cannot update config while server is running")
	}
	s.config = c
	return nil
}

// Start はシリアルポートを開いて、要求を待つ
func (s *MCSerialServer) Start(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == protocol.StatusRunning {
		return fmt.Errorf("server is already running")
	}
	if err := s.config.Validate(); err != nil {
		s.status = protocol.StatusError
		return err
	}
	cfg := *s.config
	loop := &serialport.Loop{
		Config:  cfg.Serial,
		Open:    s.open,
		Name:    "MC プロトコル（シリアル）",
		OnData:  func(data []byte, write func([]byte) error) { s.receive(cfg, data, write) },
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

// Stop は要求を待つのをやめ、シリアルポートを閉じる
func (s *MCSerialServer) Stop() error {
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

// receive は、届いたバイトを溜め、フレームが揃うたびに処理する
func (s *MCSerialServer) receive(cfg MCSerialConfig, data []byte, write func([]byte) error) {
	s.buf = append(s.buf, data...)
	for {
		frame, skip, used, ok := nextMCFrame(s.buf, cfg.Format, cfg.SumCheck)
		if !ok {
			s.buf = append(s.buf[:0], s.buf[skip:]...)
			if len(s.buf) > maxMCFrame {
				s.buf = s.buf[:0]
			}
			return
		}
		if resp := s.respond(cfg, frame); resp != nil {
			_ = write(resp)
		}
		s.buf = append(s.buf[:0], s.buf[used:]...)
	}
}

// respond は、要求のフレーム 1 つへの応答を返す。応答しないときは nil
func (s *MCSerialServer) respond(cfg MCSerialConfig, frame []byte) []byte {
	if cfg.Format == mcFormat5 {
		return s.respondF5(cfg, frame)
	}
	return s.respondF4(cfg, frame)
}

func (s *MCSerialServer) respondF4(cfg MCSerialConfig, frame []byte) []byte {
	f, err := parseF4(frame, cfg.SumCheck)
	if f.ctrl != ctrlENQ {
		return nil // 要求ではない（応答のフレームなど）
	}
	route := f.route
	if err != nil {
		// 経路まで読めていれば、局番を確かめてから異常の応答を返す
		if r, rerr := parseRouteASCII(frame[3:]); rerr == nil && int(r.Station) == cfg.Station {
			return encodeF4(ctrlNAK, r, fmt.Appendf(nil, "%04X", endRequest), false)
		}
		return nil
	}
	if int(route.Station) != cfg.Station {
		return nil
	}
	req, err := requestFromASCII(f.text)
	if err != nil {
		code := endRequest
		if errors.As(err, new(errASCIICommand)) {
			code = endCommand
		}
		return encodeF4(ctrlNAK, route, fmt.Appendf(nil, "%04X", code), false)
	}
	end, data := s.execute(req)
	if end != endOK {
		return encodeF4(ctrlNAK, route, fmt.Appendf(nil, "%04X", end), false)
	}
	text, err := responseToASCII(req, data)
	if err != nil {
		return encodeF4(ctrlNAK, route, fmt.Appendf(nil, "%04X", endRequest), false)
	}
	if len(text) == 0 {
		return encodeF4(ctrlACK, route, nil, false)
	}
	return encodeF4(ctrlSTX, route, text, cfg.SumCheck)
}

func (s *MCSerialServer) respondF5(cfg MCSerialConfig, frame []byte) []byte {
	route, req, err := parseF5(frame, cfg.SumCheck)
	if err != nil {
		return nil // 経路を確かめられないので、応答しない
	}
	if int(route.Station) != cfg.Station {
		return nil
	}
	body := []byte{0xFF, 0xFF} // 応答 ID
	end, data := s.execute(req)
	body = binary.LittleEndian.AppendUint16(body, end)
	if end == endOK {
		body = append(body, data...)
	}
	return encodeF5(route, body, cfg.SumCheck)
}

// execute は、バイナリの形のリクエスト（コマンド + サブコマンド + データ）を処理する
func (s *MCSerialServer) execute(req []byte) (uint16, []byte) {
	if len(req) < 4 {
		return endLength, nil
	}
	return s.handler.execute(binary.LittleEndian.Uint16(req), binary.LittleEndian.Uint16(req[2:]), req[4:])
}
