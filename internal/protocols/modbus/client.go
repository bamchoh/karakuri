package modbus

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
)

// Modbus TCP のクライアント。外部の機器（Modbus TCP サーバー）につないで、リクエストを送る。
//
// リクエストは PDU（ファンクションコード + データ）で受け取り、MBAP ヘッダー（トランザクション ID・UnitID）を付けて送る。
// 送ったバイト列と受け取ったバイト列はそのまま結果に入れる（画面で 16 進で見せるため）。

const (
	// maxPDULength は PDU の最大長（Modbus の仕様。ADU 260 バイト - MBAP 7 バイト + UnitID 1 バイト = 253）
	maxPDULength = 253
	mbapLength   = 7

	defaultClientHost      = "127.0.0.1"
	defaultClientPort      = 502
	defaultClientUnitID    = 1
	defaultClientTimeoutMs = 1000
)

// TCPClientFactory は Modbus TCP クライアントのファクトリー
type TCPClientFactory struct{}

// ProtocolType はクライアントのプロトコルの種類を返す
func (f *TCPClientFactory) ProtocolType() protocol.ProtocolType {
	return ProtocolModbusTCP
}

// DisplayName はプロトコルの表示名を返す
func (f *TCPClientFactory) DisplayName() string {
	return "Modbus TCP"
}

// ConnectionFields は接続先の設定項目を返す
func (f *TCPClientFactory) ConnectionFields() []protocol.ConfigField {
	return []protocol.ConfigField{
		{Name: "host", Label: "ホスト", Description: "接続先の機器の IP アドレスまたはホスト名。", Type: "text", Required: true, Default: defaultClientHost},
		{Name: "port", Label: "ポート", Description: "接続先のポート番号。標準ポートは 502 です。", Type: "number", Required: true, Default: defaultClientPort, Min: fields.IntPtr(1), Max: fields.IntPtr(65535)},
		{Name: "unitId", Label: "UnitID", Description: "MBAP ヘッダーに入れる UnitID（0〜255）。", Type: "number", Required: true, Default: defaultClientUnitID, Min: fields.IntPtr(0), Max: fields.IntPtr(255)},
		{Name: "timeoutMs", Label: "タイムアウト（ms）", Description: "接続と応答を待つ時間。過ぎるとタイムアウトにして接続を閉じます。", Type: "number", Required: true, Default: defaultClientTimeoutMs, Min: fields.IntPtr(10), Max: fields.IntPtr(60000)},
	}
}

type tcpClientSettings struct {
	Host      string
	Port      int
	UnitID    int
	TimeoutMs int
}

func parseTCPClientSettings(settings map[string]interface{}) (tcpClientSettings, error) {
	s := tcpClientSettings{Host: defaultClientHost, Port: defaultClientPort, UnitID: defaultClientUnitID, TimeoutMs: defaultClientTimeoutMs}
	fields.SetString(settings, "host", &s.Host)
	fields.SetInt(settings, "port", &s.Port)
	fields.SetInt(settings, "unitId", &s.UnitID)
	fields.SetInt(settings, "timeoutMs", &s.TimeoutMs)
	s.Host = strings.TrimSpace(s.Host)
	switch {
	case s.Host == "":
		return s, fmt.Errorf("ホストを入力してください")
	case s.Port < 1 || s.Port > 65535:
		return s, fmt.Errorf("ポートは 1〜65535 で指定してください")
	case s.UnitID < 0 || s.UnitID > 255:
		return s, fmt.Errorf("UnitID は 0〜255 で指定してください")
	case s.TimeoutMs < 10 || s.TimeoutMs > 60000:
		return s, fmt.Errorf("タイムアウトは 10〜60000 ms で指定してください")
	}
	return s, nil
}

// NormalizeSettings は接続先の設定を検証し、ない項目を既定値で埋めたものを返す
func (f *TCPClientFactory) NormalizeSettings(settings map[string]interface{}) (map[string]interface{}, error) {
	s, err := parseTCPClientSettings(settings)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"host":      s.Host,
		"port":      s.Port,
		"unitId":    s.UnitID,
		"timeoutMs": s.TimeoutMs,
	}, nil
}

// NewClient は接続先の設定からクライアントを作る
func (f *TCPClientFactory) NewClient(settings map[string]interface{}) (protocol.Client, error) {
	s, err := parseTCPClientSettings(settings)
	if err != nil {
		return nil, err
	}
	return &TCPClient{
		address: net.JoinHostPort(s.Host, strconv.Itoa(s.Port)),
		unitID:  byte(s.UnitID),
		timeout: time.Duration(s.TimeoutMs) * time.Millisecond,
	}, nil
}

// --- コマンド（ファンクションコード） ---

const (
	fcReadCoils              = 0x01
	fcReadDiscreteInputs     = 0x02
	fcReadHoldingRegisters   = 0x03
	fcReadInputRegisters     = 0x04
	fcWriteSingleCoil        = 0x05
	fcWriteSingleRegister    = 0x06
	fcWriteMultipleCoils     = 0x0F
	fcWriteMultipleRegisters = 0x10
)

var functionNames = map[byte]string{
	fcReadCoils:              "コイルの読み取り",
	fcReadDiscreteInputs:     "入力ステータスの読み取り",
	fcReadHoldingRegisters:   "保持レジスタの読み取り",
	fcReadInputRegisters:     "入力レジスタの読み取り",
	fcWriteSingleCoil:        "コイル 1 点の書き込み",
	fcWriteSingleRegister:    "保持レジスタ 1 点の書き込み",
	fcWriteMultipleCoils:     "コイルの複数書き込み",
	fcWriteMultipleRegisters: "保持レジスタの複数書き込み",
}

const addressDescription = "PDU に入れるアドレス（0 始まり）。画面の 1 始まりの番号（40001 など）のときは、エリアの先頭からの番号から 1 を引いた値です。"

func addressField() protocol.ConfigField {
	return protocol.ConfigField{Name: "address", Label: "アドレス", Description: addressDescription, Type: "number", Required: true, Default: 0, Min: fields.IntPtr(0), Max: fields.IntPtr(65535)}
}

func countField(max int) protocol.ConfigField {
	return protocol.ConfigField{Name: "count", Label: "個数", Description: fmt.Sprintf("読み取る個数（1〜%d）。", max), Type: "number", Required: true, Default: 1, Min: fields.IntPtr(1), Max: fields.IntPtr(max)}
}

// Commands は組み立てられるコマンドの一覧を返す
func (f *TCPClientFactory) Commands() []protocol.CommandDefinition {
	def := func(fc byte, fields ...protocol.ConfigField) protocol.CommandDefinition {
		return protocol.CommandDefinition{
			ID:          strconv.Itoa(int(fc)),
			DisplayName: fmt.Sprintf("FC%02d %s", fc, functionNames[fc]),
			Fields:      fields,
		}
	}
	return []protocol.CommandDefinition{
		pollable(def(fcReadCoils, addressField(), countField(2000))),
		pollable(def(fcReadDiscreteInputs, addressField(), countField(2000))),
		pollable(def(fcReadHoldingRegisters, addressField(), countField(125))),
		pollable(def(fcReadInputRegisters, addressField(), countField(125))),
		def(fcWriteSingleCoil, addressField(), protocol.ConfigField{
			Name: "value", Label: "値", Type: "select", Required: true, Default: "on",
			Options: []protocol.FieldOption{{Value: "on", Label: "ON（0xFF00）"}, {Value: "off", Label: "OFF（0x0000）"}},
		}),
		def(fcWriteSingleRegister, addressField(), protocol.ConfigField{
			Name: "value", Label: "値", Description: "0〜65535（-32768〜-1 も可）。0x で始めると 16 進、0b で始めると 2 進。", Type: "text", Required: true, Default: "0",
		}),
		def(fcWriteMultipleCoils, addressField(), protocol.ConfigField{
			Name: "values", Label: "値", Description: "1 / 0 をカンマか空白で区切って並べる（例: 1,0,1）。1〜1968 個。", Type: "text", Required: true, Default: "1",
		}),
		def(fcWriteMultipleRegisters, addressField(), protocol.ConfigField{
			Name: "values", Label: "値", Description: "値をカンマか空白で区切って並べる（例: 100, 0x10, -1）。1〜123 個。", Type: "text", Required: true, Default: "0",
		}),
	}
}

// DescribeRequest はリクエスト（PDU）の内容を 1 行の要約にする
func (f *TCPClientFactory) DescribeRequest(pdu []byte) string {
	return describeRequest(pdu)
}

// RequestHelp は、16 進で入れるリクエストの説明を返す
func (f *TCPClientFactory) RequestHelp() protocol.RequestHelp {
	return protocol.RequestHelp{
		Description: "PDU（ファンクションコード + データ）を入れます。MBAP ヘッダーはクライアントが付けます。",
		Example:     "03 00 00 00 0A",
	}
}

// pollable はコマンドを、ポーリングに使える（読み取りの）コマンドにする
func pollable(c protocol.CommandDefinition) protocol.CommandDefinition {
	c.Pollable = true
	c.WordValues = true
	return c
}

// BuildRequest は、コマンドと項目の値から PDU を作る
func (f *TCPClientFactory) BuildRequest(commandID string, params map[string]interface{}) ([]byte, error) {
	fcInt, err := strconv.Atoi(commandID)
	if err != nil {
		return nil, fmt.Errorf("不明なコマンドです: %s", commandID)
	}
	fc := byte(fcInt)
	if _, ok := functionNames[fc]; !ok || fcInt != int(fc) {
		return nil, fmt.Errorf("不明なコマンドです: %s", commandID)
	}

	address, err := fields.Int(params, "address", "アドレス", 0, 65535)
	if err != nil {
		return nil, err
	}
	pdu := []byte{fc}
	pdu = binary.BigEndian.AppendUint16(pdu, uint16(address))

	switch fc {
	case fcReadCoils, fcReadDiscreteInputs, fcReadHoldingRegisters, fcReadInputRegisters:
		max := 125
		if fc == fcReadCoils || fc == fcReadDiscreteInputs {
			max = 2000
		}
		count, err := fields.Int(params, "count", "個数", 1, max)
		if err != nil {
			return nil, err
		}
		if address+count > 65536 {
			return nil, fmt.Errorf("アドレスと個数の合計が 65536 を超えています")
		}
		pdu = binary.BigEndian.AppendUint16(pdu, uint16(count))

	case fcWriteSingleCoil:
		on, err := paramCoil(params, "value")
		if err != nil {
			return nil, err
		}
		v := uint16(0x0000)
		if on {
			v = 0xFF00
		}
		pdu = binary.BigEndian.AppendUint16(pdu, v)

	case fcWriteSingleRegister:
		s, _ := params["value"].(string)
		if s == "" {
			if n, ok := fields.Number(params["value"]); ok {
				s = strconv.FormatInt(n, 10)
			}
		}
		v, err := fields.ParseWord(s)
		if err != nil {
			return nil, fmt.Errorf("値: %w", err)
		}
		pdu = binary.BigEndian.AppendUint16(pdu, v)

	case fcWriteMultipleCoils:
		bits, err := fields.ParseBitList(fields.String(params, "values"))
		if err != nil {
			return nil, fmt.Errorf("値: %w", err)
		}
		if len(bits) < 1 || len(bits) > 1968 {
			return nil, fmt.Errorf("値は 1〜1968 個にしてください（%d 個あります）", len(bits))
		}
		if address+len(bits) > 65536 {
			return nil, fmt.Errorf("アドレスと個数の合計が 65536 を超えています")
		}
		packed := make([]byte, (len(bits)+7)/8)
		for i, b := range bits {
			if b {
				packed[i/8] |= 1 << (i % 8)
			}
		}
		pdu = binary.BigEndian.AppendUint16(pdu, uint16(len(bits)))
		pdu = append(pdu, byte(len(packed)))
		pdu = append(pdu, packed...)

	case fcWriteMultipleRegisters:
		words, err := fields.ParseWordList(fields.String(params, "values"))
		if err != nil {
			return nil, fmt.Errorf("値: %w", err)
		}
		if len(words) < 1 || len(words) > 123 {
			return nil, fmt.Errorf("値は 1〜123 個にしてください（%d 個あります）", len(words))
		}
		if address+len(words) > 65536 {
			return nil, fmt.Errorf("アドレスと個数の合計が 65536 を超えています")
		}
		pdu = binary.BigEndian.AppendUint16(pdu, uint16(len(words)))
		pdu = append(pdu, byte(len(words)*2))
		for _, w := range words {
			pdu = binary.BigEndian.AppendUint16(pdu, w)
		}
	}
	return pdu, nil
}

func paramCoil(params map[string]interface{}, key string) (bool, error) {
	switch v := params[key].(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "on", "1", "true":
			return true, nil
		case "off", "0", "false":
			return false, nil
		}
	default:
		if n, ok := fields.Number(v); ok && (n == 0 || n == 1) {
			return n == 1, nil
		}
	}
	return false, fmt.Errorf("値は ON か OFF にしてください")
}

// --- 送受信 ---

// TCPClient は Modbus TCP のクライアント。接続は最初の Send でつなぎ、失敗・タイムアウトしたら閉じる（次の Send でつなぎ直す）
type TCPClient struct {
	address string
	unitID  byte
	timeout time.Duration

	mu        sync.Mutex // Send を 1 つずつにする。conn と txID を守る
	conn      net.Conn
	txID      uint16
	connected atomic.Bool
}

// Connected は接続中かどうかを返す
func (c *TCPClient) Connected() bool {
	return c.connected.Load()
}

// Close は接続を閉じる
func (c *TCPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
	return nil
}

func (c *TCPClient) closeLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.connected.Store(false)
}

func isTimeout(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// Send は PDU を送って応答を待つ
func (c *TCPClient) Send(ctx context.Context, pdu []byte) protocol.CommandResult {
	result := protocol.CommandResult{Summary: describeRequest(pdu)}
	if len(pdu) < 1 || len(pdu) > maxPDULength {
		result.Status = protocol.CommandError
		result.Error = fmt.Sprintf("PDU は 1〜%d バイトにしてください（%d バイトあります）", maxPDULength, len(pdu))
		return result
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

	if c.conn == nil {
		dialCtx, cancel := context.WithDeadline(ctx, deadline)
		var d net.Dialer
		conn, err := d.DialContext(dialCtx, "tcp", c.address)
		cancel()
		if err != nil {
			if isTimeout(err) {
				return fail(protocol.CommandTimeout, fmt.Sprintf("%s に接続できませんでした（タイムアウト）", c.address))
			}
			return fail(protocol.CommandError, fmt.Sprintf("%s に接続できませんでした: %v", c.address, err))
		}
		c.conn = conn
		c.connected.Store(true)
	}
	conn := c.conn

	// ctx が取り消されたら、待っている読み書きをすぐに終わらせる
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	_ = conn.SetDeadline(deadline)

	c.txID++
	txID := c.txID
	frame := make([]byte, 0, mbapLength+len(pdu))
	frame = binary.BigEndian.AppendUint16(frame, txID)
	frame = binary.BigEndian.AppendUint16(frame, 0) // プロトコル ID（Modbus は 0）
	frame = binary.BigEndian.AppendUint16(frame, uint16(len(pdu)+1))
	frame = append(frame, c.unitID)
	frame = append(frame, pdu...)
	result.Sent = frame

	if _, err := conn.Write(frame); err != nil {
		if isTimeout(err) {
			return fail(protocol.CommandTimeout, "送信がタイムアウトしました")
		}
		return fail(protocol.CommandError, fmt.Sprintf("送信できませんでした: %v", err))
	}

	for {
		header := make([]byte, mbapLength)
		if _, err := io.ReadFull(conn, header); err != nil {
			if ctx.Err() != nil {
				return fail(protocol.CommandError, "取り消されました")
			}
			if isTimeout(err) {
				return fail(protocol.CommandTimeout, fmt.Sprintf("%d ms 以内に応答がありませんでした", c.timeout.Milliseconds()))
			}
			return fail(protocol.CommandError, fmt.Sprintf("応答を受け取れませんでした: %v", err))
		}
		respTxID := binary.BigEndian.Uint16(header[0:2])
		protoID := binary.BigEndian.Uint16(header[2:4])
		length := int(binary.BigEndian.Uint16(header[4:6]))
		if length < 2 || length > maxPDULength+1 {
			result.Received = header
			return fail(protocol.CommandError, fmt.Sprintf("応答の長さ（MBAP の長さ %d）が不正です", length))
		}
		body := make([]byte, length-1)
		if _, err := io.ReadFull(conn, body); err != nil {
			result.Received = header
			if isTimeout(err) {
				return fail(protocol.CommandTimeout, "応答の途中でタイムアウトしました")
			}
			return fail(protocol.CommandError, fmt.Sprintf("応答を受け取れませんでした: %v", err))
		}
		if respTxID != txID {
			// 前のリクエスト（タイムアウトしたもの）への遅れた応答。読み捨てて、待ち続ける
			continue
		}
		result.Received = append(header, body...)
		result.Elapsed = time.Since(start)
		if protoID != 0 {
			return fail(protocol.CommandError, fmt.Sprintf("応答のプロトコル ID が 0 ではありません（%d）", protoID))
		}
		status, values, msg := decodeResponse(pdu, body)
		result.Status = status
		result.Values = values
		result.Error = msg
		if status == protocol.CommandError {
			// 応答の中身が壊れているときは、続きのバイトが残っているかもしれないので閉じる
			c.closeLocked()
		}
		return result
	}
}

// --- 解釈 ---

var exceptionNames = map[byte]string{
	0x01: "不正なファンクション",
	0x02: "不正なデータアドレス",
	0x03: "不正なデータ値",
	0x04: "サーバー機器の故障",
	0x05: "確認応答（処理中）",
	0x06: "サーバー機器がビジー",
	0x08: "メモリのパリティエラー",
	0x0A: "ゲートウェイの経路がない",
	0x0B: "ゲートウェイの先の機器が応答しない",
}

// describeRequest はリクエストの内容を 1 行にする（16 進で入れたリクエストにも使う）
func describeRequest(pdu []byte) string {
	if len(pdu) == 0 {
		return "（空のリクエスト）"
	}
	fc := pdu[0]
	name, known := functionNames[fc]
	if !known {
		return fmt.Sprintf("FC%02d（%d バイト）", fc, len(pdu))
	}
	head := fmt.Sprintf("FC%02d %s", fc, name)
	if len(pdu) < 5 {
		return head + "（長さが足りません）"
	}
	addr := binary.BigEndian.Uint16(pdu[1:3])
	v := binary.BigEndian.Uint16(pdu[3:5])
	switch fc {
	case fcReadCoils, fcReadDiscreteInputs, fcReadHoldingRegisters, fcReadInputRegisters,
		fcWriteMultipleCoils, fcWriteMultipleRegisters:
		return fmt.Sprintf("%s アドレス %d から %d 個", head, addr, v)
	case fcWriteSingleCoil:
		return fmt.Sprintf("%s アドレス %d = %s", head, addr, coilText(v))
	case fcWriteSingleRegister:
		return fmt.Sprintf("%s アドレス %d = %s", head, addr, wordText(v))
	}
	return head
}

func coilText(v uint16) string {
	switch v {
	case 0xFF00:
		return "ON"
	case 0x0000:
		return "OFF"
	}
	return fmt.Sprintf("不正な値 0x%04X", v)
}

func wordText(v uint16) string {
	return fmt.Sprintf("%d (0x%04X)", v, v)
}

func bitText(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// decodeResponse は応答の PDU を解釈する。req はこちらが送った PDU
func decodeResponse(req, resp []byte) (protocol.CommandStatus, []protocol.ResultValue, string) {
	if len(resp) < 1 {
		return protocol.CommandError, nil, "応答の PDU が空です"
	}
	fc := req[0]
	if resp[0] == fc|0x80 && fc < 0x80 {
		if len(resp) < 2 {
			return protocol.CommandError, nil, "例外応答に例外コードがありません"
		}
		code := resp[1]
		name := exceptionNames[code]
		if name == "" {
			name = "不明な例外"
		}
		return protocol.CommandException, []protocol.ResultValue{{Label: "例外コード", Value: fmt.Sprintf("0x%02X %s", code, name)}},
			fmt.Sprintf("例外応答: 0x%02X %s", code, name)
	}
	if resp[0] != fc {
		return protocol.CommandError, nil, fmt.Sprintf("応答のファンクションコード（0x%02X）がリクエスト（0x%02X）と違います", resp[0], fc)
	}

	_, known := functionNames[fc]
	if !known || len(req) < 5 {
		// 組み立てられないリクエスト（16 進で入れたもの）は、データをそのまま見せる
		return protocol.CommandOK, []protocol.ResultValue{{Label: "データ", Value: hexBytes(resp[1:])}}, ""
	}
	start := binary.BigEndian.Uint16(req[1:3])
	qty := int(binary.BigEndian.Uint16(req[3:5]))

	switch fc {
	case fcReadCoils, fcReadDiscreteInputs:
		if len(resp) < 2 || len(resp) != 2+int(resp[1]) {
			return protocol.CommandError, nil, "応答のバイト数が合いません"
		}
		data := resp[2:]
		if len(data) < (qty+7)/8 {
			return protocol.CommandError, nil, fmt.Sprintf("応答のデータが %d 個分ありません", qty)
		}
		values := make([]protocol.ResultValue, qty)
		for i := 0; i < qty; i++ {
			on := data[i/8]&(1<<(i%8)) != 0
			values[i] = protocol.ResultValue{Label: fmt.Sprintf("アドレス %d", int(start)+i), Value: bitText(on), Bit: &on}
		}
		return protocol.CommandOK, values, ""

	case fcReadHoldingRegisters, fcReadInputRegisters:
		if len(resp) < 2 || len(resp) != 2+int(resp[1]) {
			return protocol.CommandError, nil, "応答のバイト数が合いません"
		}
		data := resp[2:]
		if len(data) != qty*2 {
			return protocol.CommandError, nil, fmt.Sprintf("応答のデータが %d ワードではありません（%d バイト）", qty, len(data))
		}
		values := make([]protocol.ResultValue, qty)
		for i := 0; i < qty; i++ {
			w := binary.BigEndian.Uint16(data[i*2:])
			values[i] = protocol.ResultValue{Label: fmt.Sprintf("アドレス %d", int(start)+i), Value: wordText(w), Word: &w}
		}
		return protocol.CommandOK, values, ""

	case fcWriteSingleCoil, fcWriteSingleRegister:
		if len(resp) != 5 {
			return protocol.CommandError, nil, "応答の長さが 5 バイトではありません"
		}
		addr := binary.BigEndian.Uint16(resp[1:3])
		v := binary.BigEndian.Uint16(resp[3:5])
		text := wordText(v)
		if fc == fcWriteSingleCoil {
			text = coilText(v)
		}
		return protocol.CommandOK, []protocol.ResultValue{
			{Label: "アドレス", Value: strconv.Itoa(int(addr))},
			{Label: "値", Value: text},
		}, ""

	case fcWriteMultipleCoils, fcWriteMultipleRegisters:
		if len(resp) != 5 {
			return protocol.CommandError, nil, "応答の長さが 5 バイトではありません"
		}
		return protocol.CommandOK, []protocol.ResultValue{
			{Label: "アドレス", Value: strconv.Itoa(int(binary.BigEndian.Uint16(resp[1:3])))},
			{Label: "個数", Value: strconv.Itoa(int(binary.BigEndian.Uint16(resp[3:5])))},
		}, ""
	}
	return protocol.CommandOK, nil, ""
}

func hexBytes(b []byte) string {
	if len(b) == 0 {
		return "（なし）"
	}
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02X", x)
	}
	return strings.Join(parts, " ")
}
