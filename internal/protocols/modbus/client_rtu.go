package modbus

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
	"karakuri/internal/protocols/modbus/rtu"
)

// Modbus RTU のクライアント（マスター）。シリアルポートの先の機器（スレーブ）にリクエストを送る。
//
// リクエストは TCP と同じ PDU（ファンクションコード + データ）で受け取り、UnitID（スレーブアドレス）と CRC を付けて送る。
// コマンドの定義・PDU の組み立て・応答の解釈・変数マッピングは TCP と共通（pduClient）。
//
// 応答の終わりは、ファンクションコードから決まる長さで判断する（無音の時間だけで区切らない。
// USB シリアル変換器は受信したバイトをまとめて渡すので、1 つの応答の途中に間が空くことがある）。
// 長さの決まらないファンクションコード（16 進で入れたもの）は、バイトが届かなくなった時点を終わりにする。

const (
	defaultRTUClientPort      = "COM1"
	defaultRTUClientBaudRate  = 115200
	defaultRTUClientDataBits  = 8
	defaultRTUClientStopBits  = 1
	defaultRTUClientParity    = "N"
	defaultRTUClientTimeoutMs = 1000

	// maxRTUADU は RTU のフレーム（UnitID + PDU + CRC）の最大の長さ
	maxRTUADU = 256
	// rtuUnknownLengthGap は、長さの決まらない応答を、最後のバイトからこの時間届かなければ終わりとみなす間隔
	// （3.5 文字時間より長くするのは、USB シリアル変換器がバイトを 16ms ごとにまとめて渡すため）
	rtuUnknownLengthGap = 50 * time.Millisecond
	// rtuReadSlice は、応答を待つ間に取り消し（ctx）を確かめる間隔
	rtuReadSlice = 50 * time.Millisecond
)

// RTUClientFactory は Modbus RTU クライアントのファクトリー。コマンドとマッピングは pduClient（TCP と共通）
type RTUClientFactory struct {
	pduClient

	// open はポートを開く関数。nil なら実際のシリアルポートを開く（テストではフェイクのポートを渡す）
	open rtu.PortOpener
}

// ProtocolType はクライアントのプロトコルの種類を返す
func (f *RTUClientFactory) ProtocolType() protocol.ProtocolType {
	return ProtocolModbusRTU
}

// DisplayName はプロトコルの表示名を返す
func (f *RTUClientFactory) DisplayName() string {
	return "Modbus RTU"
}

// ConnectionFields は接続先の設定項目を返す（シリアルの項目はサーバーと同じ）
func (f *RTUClientFactory) ConnectionFields() []protocol.ConfigField {
	def := &ModbusConfig{
		SerialPort: defaultRTUClientPort,
		BaudRate:   defaultRTUClientBaudRate,
		DataBits:   defaultRTUClientDataBits,
		StopBits:   defaultRTUClientStopBits,
		Parity:     defaultRTUClientParity,
	}
	out := serialConfigFields(f.DisplayName(), def)
	for i := range out {
		out[i].Category = ""
	}
	out[0].Description = "接続先の機器につながっているシリアルポート（例: COM1、COM3）。"
	return append(out,
		protocol.ConfigField{Name: "unitId", Label: "UnitID", Description: "送り先の機器のスレーブアドレス（1〜247）。0 はブロードキャスト（全部の機器が受け取り、応答しない。書き込みだけに使う）。", Type: "number", Required: true, Default: defaultClientUnitID, Min: fields.IntPtr(0), Max: fields.IntPtr(255)},
		protocol.ConfigField{Name: "timeoutMs", Label: "タイムアウト（ms）", Description: "応答を待つ時間。過ぎるとタイムアウトにしてポートを閉じます（次の送信で開き直します）。", Type: "number", Required: true, Default: defaultRTUClientTimeoutMs, Min: fields.IntPtr(10), Max: fields.IntPtr(60000)},
	)
}

type rtuClientSettings struct {
	serial    rtu.SerialConfig
	UnitID    int
	TimeoutMs int
}

func parseRTUClientSettings(settings map[string]interface{}) (rtuClientSettings, error) {
	s := rtuClientSettings{
		serial: rtu.SerialConfig{
			Port:     defaultRTUClientPort,
			BaudRate: defaultRTUClientBaudRate,
			DataBits: defaultRTUClientDataBits,
			StopBits: defaultRTUClientStopBits,
			Parity:   defaultRTUClientParity,
		},
		UnitID:    defaultClientUnitID,
		TimeoutMs: defaultRTUClientTimeoutMs,
	}
	fields.SetString(settings, "serialPort", &s.serial.Port)
	fields.SetInt(settings, "baudRate", &s.serial.BaudRate)
	fields.SetInt(settings, "dataBits", &s.serial.DataBits)
	fields.SetInt(settings, "stopBits", &s.serial.StopBits)
	fields.SetString(settings, "parity", &s.serial.Parity)
	fields.SetInt(settings, "unitId", &s.UnitID)
	fields.SetInt(settings, "timeoutMs", &s.TimeoutMs)
	s.serial.Port = strings.TrimSpace(s.serial.Port)
	s.serial.Parity = strings.ToUpper(strings.TrimSpace(s.serial.Parity))
	switch {
	case s.serial.Port == "":
		return s, fmt.Errorf("シリアルポートを選んでください")
	case s.serial.BaudRate < 300 || s.serial.BaudRate > 921600:
		return s, fmt.Errorf("ボーレートは 300〜921600 で指定してください")
	case s.serial.DataBits != 7 && s.serial.DataBits != 8:
		return s, fmt.Errorf("データビットは 7 か 8 にしてください")
	case s.serial.StopBits != 1 && s.serial.StopBits != 2:
		return s, fmt.Errorf("ストップビットは 1 か 2 にしてください")
	case s.serial.Parity != "N" && s.serial.Parity != "E" && s.serial.Parity != "O":
		return s, fmt.Errorf("パリティは N / E / O のどれかにしてください")
	case s.UnitID < 0 || s.UnitID > 255:
		return s, fmt.Errorf("UnitID は 0〜255 で指定してください")
	case s.TimeoutMs < 10 || s.TimeoutMs > 60000:
		return s, fmt.Errorf("タイムアウトは 10〜60000 ms で指定してください")
	}
	return s, nil
}

// NormalizeSettings は接続先の設定を検証し、ない項目を既定値で埋めたものを返す
func (f *RTUClientFactory) NormalizeSettings(settings map[string]interface{}) (map[string]interface{}, error) {
	s, err := parseRTUClientSettings(settings)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"serialPort": s.serial.Port,
		"baudRate":   s.serial.BaudRate,
		"dataBits":   s.serial.DataBits,
		"stopBits":   s.serial.StopBits,
		"parity":     s.serial.Parity,
		"unitId":     s.UnitID,
		"timeoutMs":  s.TimeoutMs,
	}, nil
}

// RequestHelp は、16 進で入れるリクエストの説明を返す
func (f *RTUClientFactory) RequestHelp() protocol.RequestHelp {
	return protocol.RequestHelp{
		Description: "PDU（ファンクションコード + データ）を入れます。UnitID（スレーブアドレス）と CRC はクライアントが付けます。",
		Example:     "03 00 00 00 0A",
	}
}

// NewClient は接続先の設定からクライアントを作る。ポートを開くのは最初の送信のとき
func (f *RTUClientFactory) NewClient(settings map[string]interface{}) (protocol.Client, error) {
	s, err := parseRTUClientSettings(settings)
	if err != nil {
		return nil, err
	}
	open := f.open
	if open == nil {
		open = rtu.OpenSerialPort
	}
	return &RTUClient{
		config:  s.serial,
		open:    open,
		unitID:  byte(s.UnitID),
		timeout: time.Duration(s.TimeoutMs) * time.Millisecond,
		silence: rtuSilenceTime(s.serial),
	}, nil
}

// rtuSilenceTime は、フレームの間に空ける時間（3.5 文字時間。19200bps より速ければ 1.75ms。Modbus の仕様）
func rtuSilenceTime(c rtu.SerialConfig) time.Duration {
	if c.BaudRate > 19200 {
		return 1750 * time.Microsecond
	}
	bits := 1 + c.DataBits + c.StopBits
	if c.Parity != "N" {
		bits++
	}
	return time.Duration(float64(bits) * 3.5 / float64(c.BaudRate) * float64(time.Second))
}

// --- 送受信 ---

// RTUClient は Modbus RTU のクライアント。ポートは最初の Send で開き、タイムアウト・通信エラーで閉じる（次の Send で開き直す）。
// 閉じると、遅れて届いた応答や途中のバイトが捨てられるので、次の応答と取り違えない
type RTUClient struct {
	config  rtu.SerialConfig
	open    rtu.PortOpener
	unitID  byte
	timeout time.Duration
	silence time.Duration

	mu        sync.Mutex // Send を 1 つずつにする。port と lastIO を守る
	port      rtu.Port
	lastIO    time.Time // 最後にフレームを送った・受け取った時刻（次のフレームまで 3.5 文字時間を空ける）
	connected atomic.Bool
}

// Connected はポートを開いているかどうかを返す
func (c *RTUClient) Connected() bool {
	return c.connected.Load()
}

// Close はポートを閉じる
func (c *RTUClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
	return nil
}

func (c *RTUClient) closeLocked() {
	if c.port != nil {
		_ = c.port.Close()
		c.port = nil
	}
	c.connected.Store(false)
}

// Send は PDU に UnitID と CRC を付けて送り、応答を待つ
func (c *RTUClient) Send(ctx context.Context, pdu []byte) protocol.CommandResult {
	result := protocol.CommandResult{Summary: describeRequest(pdu)}
	if len(pdu) < 1 || len(pdu) > maxRTUADU-3 {
		result.Status = protocol.CommandError
		result.Error = fmt.Sprintf("PDU は 1〜%d バイトにしてください（%d バイトあります）", maxRTUADU-3, len(pdu))
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

	if c.port == nil {
		port, err := c.open(c.config)
		if err != nil {
			return fail(protocol.CommandError, fmt.Sprintf("%s を開けませんでした: %v", c.config.Port, err))
		}
		c.port = port
		c.connected.Store(true)
		c.lastIO = time.Time{}
	}
	port := c.port

	// 前のフレームから 3.5 文字時間を空ける（短いと、機器が前のフレームの続きと見なす）
	if wait := time.Until(c.lastIO.Add(c.silence)); wait > 0 {
		time.Sleep(wait)
	}

	adu := rtu.AppendCRC(append([]byte{c.unitID}, pdu...))
	result.Sent = adu
	if _, err := port.Write(adu); err != nil {
		return fail(protocol.CommandError, fmt.Sprintf("%s に送信できませんでした: %v", c.config.Port, err))
	}
	c.lastIO = time.Now()

	if c.unitID == 0 {
		// ブロードキャストには、どの機器も応答しない
		result.Status = protocol.CommandOK
		result.Values = []protocol.ResultValue{{Label: "応答", Value: "ブロードキャスト（UnitID 0）なので、応答を待ちません"}}
		result.Elapsed = time.Since(start)
		return result
	}

	frame, status, msg := c.receive(ctx, port, deadline)
	result.Received = frame
	if status != protocol.CommandOK {
		return fail(status, msg)
	}
	c.lastIO = time.Now()
	result.Elapsed = time.Since(start)

	if !rtu.CheckCRC(frame) {
		return fail(protocol.CommandError, "応答の CRC が合いません")
	}
	if frame[0] != c.unitID {
		return fail(protocol.CommandError, fmt.Sprintf("応答の UnitID（%d）が送り先（%d）と違います", frame[0], c.unitID))
	}
	status, values, msg := decodeResponse(pdu, frame[1:len(frame)-2])
	result.Status = status
	result.Values = values
	result.Error = msg
	if status == protocol.CommandError {
		// 応答の中身が壊れているときは、続きのバイトが残っているかもしれないので閉じる
		c.closeLocked()
	}
	return result
}

// receive は応答のフレーム（UnitID から CRC まで）を受け取る。失敗したら status にタイムアウトかエラーを返す
func (c *RTUClient) receive(ctx context.Context, port rtu.Port, deadline time.Time) ([]byte, protocol.CommandStatus, string) {
	buf := make([]byte, maxRTUADU)
	var frame []byte
	var lastByte time.Time
	gap := max(rtuUnknownLengthGap, 2*c.silence)

	for {
		want := rtuResponseLength(frame)
		switch {
		case want > 0 && len(frame) >= want:
			// 後ろに余分なバイトがあっても、応答はここまで（余りは、閉じたときに捨てられる）
			return frame[:want], protocol.CommandOK, ""
		case want < 0 && len(frame) > 0 && time.Since(lastByte) >= gap:
			return frame, protocol.CommandOK, ""
		case len(frame) >= maxRTUADU:
			return frame, protocol.CommandOK, ""
		}
		if ctx.Err() != nil {
			return frame, protocol.CommandError, "取り消されました"
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if len(frame) > 0 {
				return frame, protocol.CommandTimeout, "応答の途中でタイムアウトしました"
			}
			return frame, protocol.CommandTimeout, fmt.Sprintf("%d ms 以内に応答がありませんでした", c.timeout.Milliseconds())
		}

		slice := min(remaining, rtuReadSlice)
		if want < 0 && len(frame) > 0 {
			slice = min(slice, gap-time.Since(lastByte))
		}
		_ = port.SetReadTimeout(max(slice, time.Millisecond))
		n, err := port.Read(buf)
		if err != nil {
			return frame, protocol.CommandError, fmt.Sprintf("%s が使えなくなりました: %v", c.config.Port, err)
		}
		if n > 0 {
			frame = append(frame, buf[:n]...)
			lastByte = time.Now()
		}
	}
}

// rtuResponseLength は、届いたところまでの応答から、応答の長さ（UnitID から CRC まで）を返す。
// まだ決まらなければ 0、ファンクションコードから決まらなければ -1
func rtuResponseLength(frame []byte) int {
	if len(frame) < 2 {
		return 0
	}
	fc := frame[1]
	if fc&0x80 != 0 {
		return 5 // UnitID・FC・例外コード・CRC 2
	}
	switch fc {
	case fcReadCoils, fcReadDiscreteInputs, fcReadHoldingRegisters, fcReadInputRegisters:
		if len(frame) < 3 {
			return 0
		}
		return 5 + int(frame[2]) // UnitID・FC・バイト数・データ・CRC 2
	case fcWriteSingleCoil, fcWriteSingleRegister, fcWriteMultipleCoils, fcWriteMultipleRegisters:
		return 8 // UnitID・FC・アドレス 2・値（個数）2・CRC 2
	}
	return -1
}
