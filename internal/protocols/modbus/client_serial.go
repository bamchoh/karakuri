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

// シリアルポートの先の機器（スレーブ）へ送るクライアント（マスター）で、Modbus RTU と Modbus ASCII に共通の部分。
//
// リクエストは TCP と同じ PDU（ファンクションコード + データ）で受け取る。コマンドの定義・PDU の組み立て・
// 応答の解釈・変数マッピングは TCP と共通（pduClient）。RTU と ASCII で違うのはフレームの形だけで、
// それは serialFramer が受け持つ（RTU: client_rtu.go、ASCII: client_ascii.go）。

const (
	defaultSerialClientPort      = "COM1"
	defaultSerialClientTimeoutMs = 1000

	// maxSerialPDU は PDU の最大の長さ（RTU の ADU 256 バイト - UnitID 1 - CRC 2。ASCII も同じ）
	maxSerialPDU = 253
	// serialUnknownLengthGap は、長さの決まらない応答を、最後のバイトからこの時間届かなければ終わりとみなす間隔
	// （3.5 文字時間より長くするのは、USB シリアル変換器がバイトを 16ms ごとにまとめて渡すため）
	serialUnknownLengthGap = 50 * time.Millisecond
	// serialReadSlice は、応答を待つ間に取り消し（ctx）を確かめる間隔
	serialReadSlice = 50 * time.Millisecond
)

// serialFramer は、シリアルの Modbus のフレームの形（RTU か ASCII か）
type serialFramer interface {
	// encode は、UnitID と PDU から送るフレームを作る
	encode(unitID byte, pdu []byte) []byte
	// responseLength は、届いたところまでの応答から、応答のフレームの長さを返す。
	// まだ決まらなければ 0、形から決まらなければ -1（バイトが届かなくなった時点を終わりにする）
	responseLength(buf []byte) int
	// maxFrame は、応答のフレームの最大の長さ
	maxFrame() int
	// decode は、応答のフレームの検査（CRC / LRC など）をして、UnitID と PDU を返す。誤りは利用者に見せる文で返す
	decode(frame []byte) (unitID byte, pdu []byte, err error)
	// frameGap は、前のフレームから空ける時間（RTU は 3.5 文字時間。ASCII は 0）
	frameGap() time.Duration
}

// serialVariant は、RTU と ASCII で違う設定（既定値・フレームの形・説明）
type serialVariant struct {
	protocolType protocol.ProtocolType
	displayName  string
	defaults     rtu.SerialConfig // Port 以外の既定値（サーバーの既定と同じ）
	checkName    string           // 誤り検出の名前（CRC / LRC。説明の文に使う）
	newFramer    func(rtu.SerialConfig) serialFramer
}

// connectionFields は接続先の設定項目を返す（シリアルの項目はサーバーと同じ）
func (v serialVariant) connectionFields() []protocol.ConfigField {
	def := &ModbusConfig{
		SerialPort: defaultSerialClientPort,
		BaudRate:   v.defaults.BaudRate,
		DataBits:   v.defaults.DataBits,
		StopBits:   v.defaults.StopBits,
		Parity:     v.defaults.Parity,
	}
	out := serialConfigFields(v.displayName, def)
	for i := range out {
		out[i].Category = ""
	}
	out[0].Description = "接続先の機器につながっているシリアルポート（例: COM1、COM3）。"
	return append(out,
		protocol.ConfigField{Name: "unitId", Label: "UnitID", Description: "送り先の機器のスレーブアドレス（1〜247）。0 はブロードキャスト（全部の機器が受け取り、応答しない。書き込みだけに使う）。", Type: "number", Required: true, Default: defaultClientUnitID, Min: fields.IntPtr(0), Max: fields.IntPtr(255)},
		protocol.ConfigField{Name: "timeoutMs", Label: "タイムアウト（ms）", Description: "応答を待つ時間。過ぎるとタイムアウトにしてポートを閉じます（次の送信で開き直します）。", Type: "number", Required: true, Default: defaultSerialClientTimeoutMs, Min: fields.IntPtr(10), Max: fields.IntPtr(60000)},
	)
}

type serialClientSettings struct {
	serial    rtu.SerialConfig
	UnitID    int
	TimeoutMs int
}

func (v serialVariant) parseSettings(settings map[string]interface{}) (serialClientSettings, error) {
	s := serialClientSettings{serial: v.defaults, UnitID: defaultClientUnitID, TimeoutMs: defaultSerialClientTimeoutMs}
	s.serial.Port = defaultSerialClientPort
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

// normalizeSettings は接続先の設定を検証し、ない項目を既定値で埋めたものを返す
func (v serialVariant) normalizeSettings(settings map[string]interface{}) (map[string]interface{}, error) {
	s, err := v.parseSettings(settings)
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

// requestHelp は、16 進で入れるリクエストの説明を返す
func (v serialVariant) requestHelp() protocol.RequestHelp {
	return protocol.RequestHelp{
		Description: fmt.Sprintf("PDU（ファンクションコード + データ）を入れます。UnitID（スレーブアドレス）と %s はクライアントが付けます。", v.checkName),
		Example:     "03 00 00 00 0A",
	}
}

// newClient は接続先の設定からクライアントを作る。ポートを開くのは最初の送信のとき。open が nil なら実際のシリアルポートを開く
func (v serialVariant) newClient(open rtu.PortOpener, settings map[string]interface{}) (protocol.Client, error) {
	s, err := v.parseSettings(settings)
	if err != nil {
		return nil, err
	}
	if open == nil {
		open = rtu.OpenSerialPort
	}
	return &serialClient{
		config:  s.serial,
		open:    open,
		framer:  v.newFramer(s.serial),
		unitID:  byte(s.UnitID),
		timeout: time.Duration(s.TimeoutMs) * time.Millisecond,
	}, nil
}

// --- 送受信 ---

// serialClient は、シリアルの Modbus のクライアント。ポートは最初の Send で開き、タイムアウト・通信エラーで閉じる（次の Send で開き直す）。
// 閉じると、遅れて届いた応答や途中のバイトが捨てられるので、次の応答と取り違えない
type serialClient struct {
	config  rtu.SerialConfig
	open    rtu.PortOpener
	framer  serialFramer
	unitID  byte
	timeout time.Duration

	mu        sync.Mutex // Send を 1 つずつにする。port と lastIO を守る
	port      rtu.Port
	lastIO    time.Time // 最後にフレームを送った・受け取った時刻（次のフレームまで frameGap を空ける）
	connected atomic.Bool
}

// Connected はポートを開いているかどうかを返す
func (c *serialClient) Connected() bool {
	return c.connected.Load()
}

// Close はポートを閉じる
func (c *serialClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
	return nil
}

func (c *serialClient) closeLocked() {
	if c.port != nil {
		_ = c.port.Close()
		c.port = nil
	}
	c.connected.Store(false)
}

// Send は PDU をフレームにして送り、応答を待つ
func (c *serialClient) Send(ctx context.Context, pdu []byte) protocol.CommandResult {
	result := protocol.CommandResult{Summary: describeRequest(pdu)}
	if len(pdu) < 1 || len(pdu) > maxSerialPDU {
		result.Status = protocol.CommandError
		result.Error = fmt.Sprintf("PDU は 1〜%d バイトにしてください（%d バイトあります）", maxSerialPDU, len(pdu))
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

	// 前のフレームから決まった時間を空ける（RTU は、短いと機器が前のフレームの続きと見なす）
	if wait := time.Until(c.lastIO.Add(c.framer.frameGap())); wait > 0 {
		time.Sleep(wait)
	}

	frame := c.framer.encode(c.unitID, pdu)
	result.Sent = frame
	if _, err := port.Write(frame); err != nil {
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

	resp, status, msg := c.receive(ctx, port, deadline)
	result.Received = resp
	if status != protocol.CommandOK {
		return fail(status, msg)
	}
	c.lastIO = time.Now()
	result.Elapsed = time.Since(start)

	unitID, respPDU, err := c.framer.decode(resp)
	if err != nil {
		return fail(protocol.CommandError, err.Error())
	}
	if unitID != c.unitID {
		return fail(protocol.CommandError, fmt.Sprintf("応答の UnitID（%d）が送り先（%d）と違います", unitID, c.unitID))
	}
	status, values, msg := decodeResponse(pdu, respPDU)
	result.Status = status
	result.Values = values
	result.Error = msg
	if status == protocol.CommandError {
		// 応答の中身が壊れているときは、続きのバイトが残っているかもしれないので閉じる
		c.closeLocked()
	}
	return result
}

// receive は応答のフレームを受け取る。失敗したら status にタイムアウトかエラーを返す
func (c *serialClient) receive(ctx context.Context, port rtu.Port, deadline time.Time) ([]byte, protocol.CommandStatus, string) {
	maxFrame := c.framer.maxFrame()
	buf := make([]byte, maxFrame)
	var frame []byte
	var lastByte time.Time
	gap := max(serialUnknownLengthGap, 2*c.framer.frameGap())

	for {
		want := c.framer.responseLength(frame)
		switch {
		case want > 0 && len(frame) >= want:
			// 後ろに余分なバイトがあっても、応答はここまで（余りは、閉じたときに捨てられる）
			return frame[:want], protocol.CommandOK, ""
		case want < 0 && len(frame) > 0 && time.Since(lastByte) >= gap:
			return frame, protocol.CommandOK, ""
		case len(frame) >= maxFrame:
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

		slice := min(remaining, serialReadSlice)
		if want < 0 && len(frame) > 0 {
			slice = min(slice, gap-time.Since(lastByte))
		}
		_ = port.SetReadTimeout(max(slice, time.Millisecond))
		n, err := port.Read(buf[:min(len(buf), maxFrame-len(frame))])
		if err != nil {
			return frame, protocol.CommandError, fmt.Sprintf("%s が使えなくなりました: %v", c.config.Port, err)
		}
		if n > 0 {
			frame = append(frame, buf[:n]...)
			lastByte = time.Now()
		}
	}
}
