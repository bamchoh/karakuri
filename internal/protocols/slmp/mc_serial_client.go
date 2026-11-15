package slmp

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
	"karakuri/internal/protocols/serialport"
)

// MC プロトコル（シリアル。4C フレームの形式 4 / 形式 5）のクライアント。シリアルポートの先の C24 に要求を送る。
//
// コマンドの定義・リクエストの組み立てと要約・応答の解釈・変数マッピングは、SLMP のクライアント（ClientFactory）と同じ。
// リクエストは同じバイナリの形（コマンド + サブコマンド + データ）で受け取り、形式 4 なら ASCII に直して送る。
// アクセス経路は、つないだ C24 の CPU（自局）宛て（局番・00・FF・03FF・00・00）。
//
//   - ポートは最初の送信で開き、タイムアウト・通信エラー・壊れた応答では閉じる（次の送信で開き直す）
//   - 異常の応答（形式 4 の NAK、形式 5 のエラーコード）は exception（ポートは閉じない）
//   - 形式 4 で ASCII に直せないコマンド（16 進で入れた、対応していないもの）は、送らずにエラーにする

const defaultMCSerialTimeoutMs = 1000

var (
	_ protocol.ClientFactory         = (*MCSerialClientFactory)(nil)
	_ protocol.RequestHelpProvider   = (*MCSerialClientFactory)(nil)
	_ protocol.MappableClientFactory = (*MCSerialClientFactory)(nil)
)

// MCSerialClientFactory は MC プロトコル（シリアル）のクライアントのファクトリー。
// コマンド・組み立て・要約・マッピングは、埋め込んだ ClientFactory（SLMP）のものを使う
type MCSerialClientFactory struct {
	ClientFactory

	// open はポートを開く関数。nil なら実際のシリアルポートを開く（テストでは仮想のケーブルを渡す）
	open serialport.Opener
}

func (f *MCSerialClientFactory) ProtocolType() protocol.ProtocolType { return ProtocolMCSerial }
func (f *MCSerialClientFactory) DisplayName() string                 { return "MC プロトコル（シリアル）" }

// ConnectionFields は接続先の設定項目を返す
func (f *MCSerialClientFactory) ConnectionFields() []protocol.ConfigField {
	def := DefaultMCSerialConfig()
	out := serialport.ConfigFields(def.Serial, "接続先の C24 につながっているシリアルポート（例: COM1、COM3）。")
	out = append(out, mcOptionFields(def.Format, def.SumCheck, def.Station, "送り先の C24 の局番（0〜31）。")...)
	return append(out, protocol.ConfigField{Name: "timeoutMs", Label: "タイムアウト（ms）", Description: "応答を待つ時間。過ぎるとタイムアウトにしてポートを閉じます（次の送信で開き直します）。", Type: "number", Required: true, Default: defaultMCSerialTimeoutMs, Min: fields.IntPtr(10), Max: fields.IntPtr(60000)})
}

type mcSerialClientSettings struct {
	serial    serialport.Config
	Format    int
	SumCheck  bool
	Station   int
	TimeoutMs int
}

func parseMCSerialClientSettings(settings map[string]interface{}) (mcSerialClientSettings, error) {
	def := DefaultMCSerialConfig()
	s := mcSerialClientSettings{serial: def.Serial, Format: def.Format, SumCheck: def.SumCheck, Station: def.Station, TimeoutMs: defaultMCSerialTimeoutMs}
	serialport.ApplySettings(settings, &s.serial)
	applyMCOptions(settings, &s.Format, &s.SumCheck, &s.Station)
	fields.SetInt(settings, "timeoutMs", &s.TimeoutMs)
	if err := serialport.Validate(s.serial); err != nil {
		return s, err
	}
	if err := validateMCOptions(s.Format, s.Station); err != nil {
		return s, err
	}
	if s.TimeoutMs < 10 || s.TimeoutMs > 60000 {
		return s, fmt.Errorf("タイムアウトは 10〜60000 ms で指定してください")
	}
	return s, nil
}

// NormalizeSettings は接続先の設定を検証し、ない項目を既定値で埋めたものを返す
func (f *MCSerialClientFactory) NormalizeSettings(settings map[string]interface{}) (map[string]interface{}, error) {
	s, err := parseMCSerialClientSettings(settings)
	if err != nil {
		return nil, err
	}
	return serialport.ToMap(s.serial, map[string]interface{}{
		"format": s.Format, "sumCheck": sumText(s.SumCheck), "station": s.Station, "timeoutMs": s.TimeoutMs,
	}), nil
}

// NewClient は接続先の設定からクライアントを作る。ポートを開くのは最初の送信のとき
func (f *MCSerialClientFactory) NewClient(settings map[string]interface{}) (protocol.Client, error) {
	s, err := parseMCSerialClientSettings(settings)
	if err != nil {
		return nil, err
	}
	open := f.open
	if open == nil {
		open = serialport.Open
	}
	return &MCSerialClient{
		config:  s.serial,
		open:    open,
		format:  s.Format,
		sum:     s.SumCheck,
		route:   hostRoute(byte(s.Station)),
		timeout: time.Duration(s.TimeoutMs) * time.Millisecond,
	}, nil
}

// RequestHelp は、16 進で入れるリクエストの説明を返す
func (f *MCSerialClientFactory) RequestHelp() protocol.RequestHelp {
	return protocol.RequestHelp{
		Description: "要求データ（コマンド 2 バイト + サブコマンド 2 バイト + データ。数値はリトルエンディアン）を、SLMP と同じバイナリの形で入れます。形式 4 では ASCII に直して送ります（直せるのは 0401・1401・0403・1402・0101・0619 の、サブコマンド 0000 / 0001 だけ）。4C フレームの枠とアクセス経路はクライアントが付けます。",
		Example:     "01 04 00 00 64 00 00 A8 0A 00",
	}
}

// --- 送受信 ---

// MCSerialClient は MC プロトコル（シリアル）のクライアント
type MCSerialClient struct {
	config  serialport.Config
	open    serialport.Opener
	format  int
	sum     bool
	route   mcRoute
	timeout time.Duration

	mu        sync.Mutex // Send を 1 つずつにする。port を守る
	port      serialport.Port
	connected atomic.Bool
}

// Connected はポートを開いているかどうかを返す
func (c *MCSerialClient) Connected() bool {
	return c.connected.Load()
}

// Close はポートを閉じる
func (c *MCSerialClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
	return nil
}

func (c *MCSerialClient) closeLocked() {
	if c.port != nil {
		_ = c.port.Close()
		c.port = nil
	}
	c.connected.Store(false)
}

// Send は、リクエスト（コマンド + サブコマンド + データ）を 4C フレームにして送り、応答を待つ
func (c *MCSerialClient) Send(ctx context.Context, request []byte) protocol.CommandResult {
	result := protocol.CommandResult{Summary: describeRequest(request)}
	if len(request) < 4 || len(request) > maxDataLength {
		result.Status = protocol.CommandError
		result.Error = fmt.Sprintf("リクエストは 4〜%d バイトにしてください（%d バイトあります）", maxDataLength, len(request))
		return result
	}
	var frame []byte
	if c.format == mcFormat4 {
		text, err := requestToASCII(request)
		if err != nil {
			result.Status = protocol.CommandError
			result.Error = err.Error()
			return result
		}
		frame = encodeF4(ctrlENQ, c.route, text, c.sum)
	} else {
		frame = encodeF5(c.route, request, c.sum)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	deadline := start.Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	fail := func(status protocol.CommandStatus, msg string) protocol.CommandResult {
		c.closeLocked()
		result.Status = status
		result.Error = msg
		result.Elapsed = time.Since(start)
		return result
	}

	if c.port == nil {
		port, err := c.open(c.config)
		if err != nil {
			return fail(protocol.CommandError, fmt.Sprintf("%s を開けませんでした: %v", c.config.Port, err))
		}
		c.port = port
		c.connected.Store(true)
	}
	port := c.port

	result.Sent = frame
	if _, err := port.Write(frame); err != nil {
		return fail(protocol.CommandError, fmt.Sprintf("%s に送信できませんでした: %v", c.config.Port, err))
	}

	raw, resp, status, msg := c.receive(ctx, port, deadline)
	result.Received = raw
	if status != protocol.CommandOK {
		return fail(status, msg)
	}
	result.Elapsed = time.Since(start)

	end, data, err := c.decode(request, resp)
	if err != nil {
		return fail(protocol.CommandError, "応答の"+err.Error())
	}
	body := binary.LittleEndian.AppendUint16(nil, end)
	st, values, m := decodeResponse(request, responseFrame(append(body, data...)))
	result.Status = st
	result.Values = values
	result.Error = m
	if st == protocol.CommandError {
		c.closeLocked()
	}
	return result
}

// responseFrame は、終了コード + データを、SLMP の応答のフレームの形にする（解釈は SLMP の decodeResponse を使うため）
func responseFrame(body []byte) frame {
	return frame{Kind: sub3EResponse, Body: body}
}

// decode は、応答のフレームを読み、終了コードと（バイナリの形の）応答データを返す
func (c *MCSerialClient) decode(request, resp []byte) (uint16, []byte, error) {
	if c.format == mcFormat5 {
		route, body, err := parseF5(resp, c.sum)
		if err != nil {
			return 0, nil, err
		}
		if route.Station != c.route.Station {
			return 0, nil, fmt.Errorf("局番（%02X）が送り先（%02X）と違います", route.Station, c.route.Station)
		}
		if len(body) < 4 || body[0] != 0xFF || body[1] != 0xFF {
			return 0, nil, fmt.Errorf("応答 ID（FFFF）と終了コードがありません")
		}
		return binary.LittleEndian.Uint16(body[2:]), body[4:], nil
	}
	f, err := parseF4(resp, c.sum)
	if err != nil {
		return 0, nil, err
	}
	if f.route.Station != c.route.Station {
		return 0, nil, fmt.Errorf("局番（%02X）が送り先（%02X）と違います", f.route.Station, c.route.Station)
	}
	switch f.ctrl {
	case ctrlACK:
		return endOK, nil, nil
	case ctrlNAK:
		code, err := strconv.ParseUint(string(f.text), 16, 16)
		if err != nil || len(f.text) != 4 {
			return 0, nil, fmt.Errorf("エラーコードを読めません（%q）", f.text)
		}
		return uint16(code), nil, nil
	case ctrlSTX:
		data, err := responseFromASCII(request, f.text)
		if err != nil {
			return 0, nil, err
		}
		return endOK, data, nil
	}
	return 0, nil, fmt.Errorf("制御コード（%02X）が応答のものではありません", f.ctrl)
}

// receive は、応答のフレームが揃うまで受け取る。raw は受け取ったバイトそのまま
func (c *MCSerialClient) receive(ctx context.Context, port serialport.Port, deadline time.Time) (raw, frame []byte, status protocol.CommandStatus, msg string) {
	buf := make([]byte, 1024)
	var pending []byte
	for {
		if f, skip, _, ok := nextMCFrame(pending, c.format, c.sum); ok {
			return raw, f, protocol.CommandOK, ""
		} else {
			pending = pending[skip:]
		}
		if len(pending) > maxMCFrame {
			return raw, nil, protocol.CommandError, "応答が長すぎます"
		}
		if ctx.Err() != nil {
			return raw, nil, protocol.CommandError, "取り消されました"
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if len(raw) > 0 {
				return raw, nil, protocol.CommandTimeout, "応答の途中でタイムアウトしました"
			}
			return raw, nil, protocol.CommandTimeout, fmt.Sprintf("%d ms 以内に応答がありませんでした", c.timeout.Milliseconds())
		}
		_ = port.SetReadTimeout(max(min(remaining, 50*time.Millisecond), time.Millisecond))
		n, err := port.Read(buf)
		if err != nil {
			return raw, nil, protocol.CommandError, fmt.Sprintf("%s が使えなくなりました: %v", c.config.Port, err)
		}
		raw = append(raw, buf[:n]...)
		pending = append(pending, buf[:n]...)
	}
}
