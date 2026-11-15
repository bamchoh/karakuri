package slmp

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
)

// SLMP のクライアント。外部の機器（PLC）につないで、SLMP のコマンドを送る。
//
// リクエストは「コマンド(2) + サブコマンド(2) + データ」で受け取り、その前の部分
// （サブヘッダー・シリアル番号・要求先の指定・データ長・監視タイマ）を付けて送る。
// 送ったバイト列と受け取ったバイト列は、そのまま結果に入れる。

const (
	defaultClientHost      = "127.0.0.1"
	defaultClientTimeoutMs = 1000

	frame3E = "3e"
	frame4E = "4e"

	// 要求先の指定の既定値（自局の CPU）
	defaultStation  = 0xFF
	defaultModuleIO = 0x03FF
	// 監視タイマの既定値（250 ms 単位。16 = 4 秒）
	defaultTimer = 16
)

var (
	_ protocol.ClientFactory       = (*ClientFactory)(nil)
	_ protocol.RequestHelpProvider = (*ClientFactory)(nil)
)

// ClientFactory は SLMP クライアントのファクトリー
type ClientFactory struct{}

func (f *ClientFactory) ProtocolType() protocol.ProtocolType { return ProtocolSLMP }
func (f *ClientFactory) DisplayName() string                 { return "SLMP" }

// ConnectionFields は接続先の設定項目を返す
func (f *ClientFactory) ConnectionFields() []protocol.ConfigField {
	return []protocol.ConfigField{
		{Name: "host", Label: "ホスト", Description: "接続先の PLC の IP アドレスまたはホスト名。", Type: "text", Required: true, Default: defaultClientHost},
		{Name: "port", Label: "ポート", Description: "接続先のポート番号（PLC のパラメーターで決めた番号）。", Type: "number", Required: true, Default: defaultPort, Min: fields.IntPtr(1), Max: fields.IntPtr(65535)},
		{Name: "transport", Label: "通信方式", Description: "TCP か UDP。", Type: "select", Required: true, Default: transportTCP, Options: []protocol.FieldOption{
			{Value: transportTCP, Label: "TCP"},
			{Value: transportUDP, Label: "UDP"},
		}},
		{Name: "frame", Label: "フレーム", Description: "3E フレームか 4E フレーム（どちらもバイナリコード）。4E はシリアル番号を持ち、遅れて届いた応答を見分けられます。", Type: "select", Required: true, Default: frame3E, Options: []protocol.FieldOption{
			{Value: frame3E, Label: "3E フレーム"},
			{Value: frame4E, Label: "4E フレーム"},
		}},
		{Name: "network", Label: "ネットワーク番号", Description: "要求先のネットワーク番号（0〜255）。自局なら 0。", Type: "number", Required: true, Default: 0, Min: fields.IntPtr(0), Max: fields.IntPtr(255)},
		{Name: "station", Label: "局番", Description: "要求先の局番（0〜255）。自局なら 255（0xFF）。", Type: "number", Required: true, Default: defaultStation, Min: fields.IntPtr(0), Max: fields.IntPtr(255)},
		{Name: "moduleIO", Label: "要求先ユニット I/O 番号", Description: "0〜65535。自局の CPU なら 1023（0x03FF）。", Type: "number", Required: true, Default: defaultModuleIO, Min: fields.IntPtr(0), Max: fields.IntPtr(65535)},
		{Name: "multidrop", Label: "要求先マルチドロップ局番", Description: "0〜255。マルチドロップ接続でなければ 0。", Type: "number", Required: true, Default: 0, Min: fields.IntPtr(0), Max: fields.IntPtr(255)},
		{Name: "timer", Label: "監視タイマ", Description: "PLC が処理の完了を待つ時間（250 ms 単位。0〜65535）。0 なら、完了するまで待ちます。", Type: "number", Required: true, Default: defaultTimer, Min: fields.IntPtr(0), Max: fields.IntPtr(65535)},
		{Name: "timeoutMs", Label: "タイムアウト（ms）", Description: "接続と応答を待つ時間。過ぎるとタイムアウトにして接続を閉じます。", Type: "number", Required: true, Default: defaultClientTimeoutMs, Min: fields.IntPtr(10), Max: fields.IntPtr(60000)},
	}
}

type clientSettings struct {
	Host      string
	Port      int
	Transport string
	Frame     string
	Network   int
	Station   int
	ModuleIO  int
	Multidrop int
	Timer     int
	TimeoutMs int
}

func parseClientSettings(settings map[string]interface{}) (clientSettings, error) {
	s := clientSettings{
		Host: defaultClientHost, Port: defaultPort, Transport: transportTCP, Frame: frame3E,
		Station: defaultStation, ModuleIO: defaultModuleIO, Timer: defaultTimer, TimeoutMs: defaultClientTimeoutMs,
	}
	fields.SetString(settings, "host", &s.Host)
	fields.SetInt(settings, "port", &s.Port)
	fields.SetString(settings, "transport", &s.Transport)
	fields.SetString(settings, "frame", &s.Frame)
	fields.SetInt(settings, "network", &s.Network)
	fields.SetInt(settings, "station", &s.Station)
	fields.SetInt(settings, "moduleIO", &s.ModuleIO)
	fields.SetInt(settings, "multidrop", &s.Multidrop)
	fields.SetInt(settings, "timer", &s.Timer)
	fields.SetInt(settings, "timeoutMs", &s.TimeoutMs)
	s.Host = strings.TrimSpace(s.Host)
	switch {
	case s.Host == "":
		return s, fmt.Errorf("ホストを入力してください")
	case s.Port < 1 || s.Port > 65535:
		return s, fmt.Errorf("ポートは 1〜65535 で指定してください")
	case s.Transport != transportUDP && s.Transport != transportTCP:
		return s, fmt.Errorf("通信方式は TCP か UDP にしてください")
	case s.Frame != frame3E && s.Frame != frame4E:
		return s, fmt.Errorf("フレームは 3E か 4E にしてください")
	case s.Network < 0 || s.Network > 255:
		return s, fmt.Errorf("ネットワーク番号は 0〜255 で指定してください")
	case s.Station < 0 || s.Station > 255:
		return s, fmt.Errorf("局番は 0〜255 で指定してください")
	case s.ModuleIO < 0 || s.ModuleIO > 65535:
		return s, fmt.Errorf("要求先ユニット I/O 番号は 0〜65535 で指定してください")
	case s.Multidrop < 0 || s.Multidrop > 255:
		return s, fmt.Errorf("要求先マルチドロップ局番は 0〜255 で指定してください")
	case s.Timer < 0 || s.Timer > 65535:
		return s, fmt.Errorf("監視タイマは 0〜65535 で指定してください")
	case s.TimeoutMs < 10 || s.TimeoutMs > 60000:
		return s, fmt.Errorf("タイムアウトは 10〜60000 ms で指定してください")
	}
	return s, nil
}

// NormalizeSettings は接続先の設定を検証し、ない項目を既定値で埋めたものを返す
func (f *ClientFactory) NormalizeSettings(settings map[string]interface{}) (map[string]interface{}, error) {
	s, err := parseClientSettings(settings)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"host": s.Host, "port": s.Port, "transport": s.Transport, "frame": s.Frame,
		"network": s.Network, "station": s.Station, "moduleIO": s.ModuleIO, "multidrop": s.Multidrop,
		"timer": s.Timer, "timeoutMs": s.TimeoutMs,
	}, nil
}

// NewClient は接続先の設定からクライアントを作る
func (f *ClientFactory) NewClient(settings map[string]interface{}) (protocol.Client, error) {
	s, err := parseClientSettings(settings)
	if err != nil {
		return nil, err
	}
	kind := byte(sub3ERequest)
	if s.Frame == frame4E {
		kind = sub4ERequest
	}
	return &Client{
		address:   net.JoinHostPort(s.Host, strconv.Itoa(s.Port)),
		transport: s.Transport,
		kind:      kind,
		route:     [routeLength]byte{byte(s.Network), byte(s.Station), byte(s.ModuleIO), byte(s.ModuleIO >> 8), byte(s.Multidrop)},
		timer:     uint16(s.Timer),
		timeout:   time.Duration(s.TimeoutMs) * time.Millisecond,
	}, nil
}

// RequestHelp は、16 進で入れるリクエストの説明を返す
func (f *ClientFactory) RequestHelp() protocol.RequestHelp {
	return protocol.RequestHelp{
		Description: "SLMP のコマンド（コマンド 2 バイト + サブコマンド 2 バイト + データ。数値はリトルエンディアン）を入れます。その前の部分（サブヘッダー・要求先の指定・データ長・監視タイマ）はクライアントが付けます。",
		Example:     "01 04 00 00 64 00 00 A8 0A 00",
	}
}

// --- コマンド ---

const (
	commandRead     = "read"
	commandWrite    = "write"
	commandTypeName = "typeName"
	commandSelfTest = "selfTest"
)

func deviceField() protocol.ConfigField {
	options := make([]protocol.FieldOption, len(deviceDefs))
	for i, d := range deviceDefs {
		options[i] = protocol.FieldOption{Value: d.ID, Label: d.Name}
	}
	return protocol.ConfigField{Name: "device", Label: "デバイス", Description: "ビットデバイス（M・X・Y など）はビット単位、ワードデバイス（D・W など）はワード単位で読み書きします。", Type: "select", Required: true, Default: "D", Options: options}
}

func addressField() protocol.ConfigField {
	return protocol.ConfigField{Name: "address", Label: "デバイス番号", Description: "デバイスの書き方のまま入れます（D100 なら 100）。X・Y・B・W・SB・SW・DX・DY・ZR は 16 進（X1F なら 1F）、ほかは 10 進。", Type: "text", Required: true, Default: "0"}
}

func commandTitle(cmd uint16) string {
	return fmt.Sprintf("%04X %s", cmd, commandNames[cmd])
}

// Commands は組み立てられるコマンドの一覧を返す
func (f *ClientFactory) Commands() []protocol.CommandDefinition {
	cmds := []protocol.CommandDefinition{
		{ID: commandRead, DisplayName: commandTitle(cmdBatchRead), Pollable: true, WordValues: true, Fields: []protocol.ConfigField{
			deviceField(), addressField(),
			{Name: "count", Label: "点数", Description: fmt.Sprintf("読み出す点数（ワードデバイスは 1〜%d、ビットデバイスは 1〜%d）。", maxWordPoints, maxBitPoints), Type: "number", Required: true, Default: 1, Min: fields.IntPtr(1), Max: fields.IntPtr(maxBitPoints)},
		}},
		{ID: commandWrite, DisplayName: commandTitle(cmdBatchWrite), Fields: []protocol.ConfigField{
			deviceField(), addressField(),
			{Name: "values", Label: "値", Description: fmt.Sprintf("値をカンマか空白で区切って並べる（ワードの例: 100, 0x10, -1。ビットの例: 1,0,1）。ワードは 1〜%d 個、ビットは 1〜%d 個。", maxWordPoints, maxBitPoints), Type: "text", Required: true, Default: "0"},
		}},
	}
	cmds = append(cmds, randomCommands()...) // 0403 ランダム読み出し・1402 ランダム書き込み（client_random.go）
	return append(cmds,
		protocol.CommandDefinition{ID: commandTypeName, DisplayName: commandTitle(cmdTypeName)},
		protocol.CommandDefinition{ID: commandSelfTest, DisplayName: commandTitle(cmdSelfTest), Fields: []protocol.ConfigField{
			{Name: "data", Label: "データ", Description: "送る文字（英数字。1〜960 文字）。同じものが返れば正常です。", Type: "text", Required: true, Default: "ABCDE"},
		}},
	)
}

// parseDeviceNumber は、デバイス番号の入力を読む（16 進で書くデバイスは 16 進、ほかは 10 進）
func parseDeviceNumber(def deviceDef, s string) (uint32, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("デバイス番号を入力してください")
	}
	var n uint64
	var err error
	if def.Hex {
		body := strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
		n, err = strconv.ParseUint(body, 16, 64)
		if err != nil {
			return 0, fmt.Errorf("デバイス番号 %q を 16 進として読めません（%s は 16 進で書きます）", s, def.ID)
		}
	} else {
		n, err = strconv.ParseUint(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("デバイス番号 %q を 10 進として読めません", s)
		}
	}
	if n >= maxPoints {
		return 0, fmt.Errorf("デバイス番号は %s まで指定できます", target{def: def, number: maxPoints - 1}.label(0))
	}
	return uint32(n), nil
}

// buildTarget は、デバイスとデバイス番号の項目から、デバイスを決める
func buildTarget(params map[string]interface{}) (target, error) {
	def, ok := deviceByID(fields.String(params, "device"))
	if !ok {
		return target{}, fmt.Errorf("デバイスを選んでください")
	}
	// デバイス番号は文字で入れる欄だが、数値で渡されたとき（保存したコマンドなど）も読む
	text := fields.String(params, "address")
	if n, isNumber := fields.Number(params["address"]); isNumber && text == "" {
		text = strconv.FormatInt(n, 10)
	}
	number, err := parseDeviceNumber(def, text)
	if err != nil {
		return target{}, err
	}
	return target{def: def, number: number}, nil
}

func appendCommand(dst []byte, command, sub uint16) []byte {
	dst = binary.LittleEndian.AppendUint16(dst, command)
	return binary.LittleEndian.AppendUint16(dst, sub)
}

// BuildRequest は、コマンドと項目の値からリクエスト（コマンド + サブコマンド + データ）を作る
func (f *ClientFactory) BuildRequest(commandID string, params map[string]interface{}) ([]byte, error) {
	switch commandID {
	case commandRead:
		t, err := buildTarget(params)
		if err != nil {
			return nil, err
		}
		count, err := fields.Int(params, "count", "点数", 1, maxBitPoints)
		if err != nil {
			return nil, err
		}
		return readRequest(t, count)

	case commandWrite:
		t, err := buildTarget(params)
		if err != nil {
			return nil, err
		}
		if t.def.IsBit {
			bits, err := fields.ParseBitList(fields.String(params, "values"))
			if err != nil {
				return nil, fmt.Errorf("値: %w", err)
			}
			return writeRequest(t, nil, bits)
		}
		words, err := fields.ParseWordList(fields.String(params, "values"))
		if err != nil {
			return nil, fmt.Errorf("値: %w", err)
		}
		return writeRequest(t, words, nil)

	case commandRandomRead:
		return buildRandomRead(params)

	case commandRandomWrite:
		return buildRandomWrite(params)

	case commandTypeName:
		return appendCommand(nil, cmdTypeName, 0), nil

	case commandSelfTest:
		data := fields.String(params, "data")
		if len(data) < 1 || len(data) > maxWordPoints {
			return nil, fmt.Errorf("データは 1〜%d 文字にしてください", maxWordPoints)
		}
		for _, c := range []byte(data) {
			if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
				return nil, fmt.Errorf("データは英数字にしてください")
			}
		}
		req := appendCommand(nil, cmdSelfTest, 0)
		req = binary.LittleEndian.AppendUint16(req, uint16(len(data)))
		return append(req, data...), nil
	}
	return nil, fmt.Errorf("不明なコマンドです: %s", commandID)
}

// maxCount は、1 回の要求で読み書きできる点数（ビットデバイスはビット単位、ワードデバイスはワード単位）
func maxCount(def deviceDef) int {
	if def.IsBit {
		return maxBitPoints
	}
	return maxWordPoints
}

func unitSub(def deviceDef) uint16 {
	if def.IsBit {
		return subBitUnits
	}
	return 0
}

func readRequest(t target, count int) ([]byte, error) {
	if count < 1 || count > maxCount(t.def) {
		return nil, fmt.Errorf("%s を読む点数は 1〜%d にしてください（%d）", t.def.ID, maxCount(t.def), count)
	}
	if count > t.remaining() {
		return nil, fmt.Errorf("デバイス番号と点数の合計が、デバイスの範囲を超えています")
	}
	req := appendCommand(nil, cmdBatchRead, unitSub(t.def))
	req = t.appendTo(req)
	return binary.LittleEndian.AppendUint16(req, uint16(count)), nil
}

func writeRequest(t target, words []uint16, bits []bool) ([]byte, error) {
	count := len(words)
	if t.def.IsBit {
		count = len(bits)
	}
	if count < 1 || count > maxCount(t.def) {
		return nil, fmt.Errorf("%s に書く値は 1〜%d 個にしてください（%d 個あります）", t.def.ID, maxCount(t.def), count)
	}
	if count > t.remaining() {
		return nil, fmt.Errorf("デバイス番号と点数の合計が、デバイスの範囲を超えています")
	}
	req := appendCommand(nil, cmdBatchWrite, unitSub(t.def))
	req = t.appendTo(req)
	req = binary.LittleEndian.AppendUint16(req, uint16(count))
	if t.def.IsBit {
		return append(req, packNibbles(bits)...), nil
	}
	return appendWords(req, words), nil
}

// --- 解釈 ---

// batchRequest は、一括読み出し・一括書き込みのリクエストの先頭（デバイスの指定 + 点数）を読む
func batchRequest(req []byte) (t target, count int, bitUnits bool, ok bool) {
	if len(req) < 4 {
		return t, 0, false, false
	}
	sub := binary.LittleEndian.Uint16(req[2:])
	if sub&^(subBitUnits|subLong) != 0 {
		return t, 0, false, false
	}
	long := sub&subLong != 0
	n := deviceLength(long)
	if len(req) < 4+n+2 {
		return t, 0, false, false
	}
	t, end := parseTarget(req[4:], long)
	if end != endOK {
		return t, 0, false, false
	}
	return t, int(binary.LittleEndian.Uint16(req[4+n:])), sub&subBitUnits != 0, true
}

// DescribeRequest はリクエストの内容を 1 行の要約にする
func (f *ClientFactory) DescribeRequest(request []byte) string {
	return describeRequest(request)
}

func describeRequest(req []byte) string {
	if len(req) < 4 {
		return "（コマンドとサブコマンドがありません）"
	}
	cmd := binary.LittleEndian.Uint16(req)
	if _, known := commandNames[cmd]; !known {
		return fmt.Sprintf("コマンド %04X（%d バイト）", cmd, len(req))
	}
	head := commandTitle(cmd)
	switch cmd {
	case cmdBatchRead, cmdBatchWrite:
		t, count, bitUnits, ok := batchRequest(req)
		if !ok {
			return head + "（デバイスを読めません）"
		}
		unit := "ワード単位"
		if bitUnits {
			unit = "ビット単位"
		}
		return fmt.Sprintf("%s %s から %d 点（%s）", head, t.label(0), count, unit)
	case cmdRandomRead:
		return describeRandomRead(req)
	case cmdRandomWrite:
		return describeRandomWrite(req)
	}
	return head
}

func bitText(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func wordText(v uint16) string {
	return fmt.Sprintf("%d (0x%04X)", v, v)
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

// decodeResponse は応答を解釈する。req はこちらが送ったリクエスト（コマンド + サブコマンド + データ）、resp は応答のフレーム
func decodeResponse(req []byte, resp frame) (protocol.CommandStatus, []protocol.ResultValue, string) {
	if resp.isRequest() {
		return protocol.CommandError, nil, "応答ではないフレームを受け取りました"
	}
	if len(resp.Body) < 2 {
		return protocol.CommandError, nil, fmt.Sprintf("応答が短すぎます（終了コードがありません。%d バイト）", len(resp.Body))
	}
	end := binary.LittleEndian.Uint16(resp.Body)
	data := resp.Body[2:]
	if end != endOK {
		text := endCodeText(end)
		return protocol.CommandException, []protocol.ResultValue{{Label: "終了コード", Value: text}}, "異常終了: " + text
	}

	switch cmd := binary.LittleEndian.Uint16(req); cmd {
	case cmdBatchRead:
		t, count, bitUnits, ok := batchRequest(req)
		if !ok {
			break
		}
		if bitUnits {
			if len(data) != (count+1)/2 {
				return protocol.CommandError, nil, fmt.Sprintf("応答のデータが %d 点分ではありません（%d バイト）", count, len(data))
			}
			values := make([]protocol.ResultValue, count)
			for i, on := range unpackNibbles(data, count) {
				values[i] = protocol.ResultValue{Label: t.label(i), Value: bitText(on), Bit: &on}
			}
			return protocol.CommandOK, values, ""
		}
		if len(data) != count*2 {
			return protocol.CommandError, nil, fmt.Sprintf("応答のデータが %d ワードではありません（%d バイト）", count, len(data))
		}
		values := make([]protocol.ResultValue, count)
		for i, w := range parseWords(data) {
			label := t.label(i)
			if t.def.IsBit {
				// ビットデバイスをワード単位で読んだとき（16 進で入れたリクエスト）: 1 ワードが 16 点
				label = fmt.Sprintf("%s から 16 点", t.label(i*16))
			}
			values[i] = protocol.ResultValue{Label: label, Value: wordText(w), Word: &w}
		}
		return protocol.CommandOK, values, ""

	case cmdRandomRead:
		if values, msg, ok := decodeRandomRead(req, data); ok {
			if msg != "" {
				return protocol.CommandError, nil, msg
			}
			return protocol.CommandOK, values, ""
		}

	case cmdBatchWrite, cmdRandomWrite:
		return protocol.CommandOK, []protocol.ResultValue{{Label: "結果", Value: "正常終了"}}, ""

	case cmdTypeName:
		if len(data) >= 18 {
			return protocol.CommandOK, []protocol.ResultValue{
				{Label: "形名", Value: strings.TrimRight(string(data[:16]), " \x00")},
				{Label: "形名コード", Value: fmt.Sprintf("0x%04X", binary.LittleEndian.Uint16(data[16:]))},
			}, ""
		}

	case cmdSelfTest:
		if len(data) >= 2 && len(req) >= 6 {
			result := "送ったデータと同じ"
			if string(data) != string(req[4:]) {
				result = "送ったデータと違う"
			}
			return protocol.CommandOK, []protocol.ResultValue{
				{Label: "結果", Value: result},
				{Label: "データ", Value: string(data[2:])},
			}, ""
		}
	}
	// 解釈を持たないコマンド（16 進で入れたもの）は、データをそのまま見せる
	return protocol.CommandOK, []protocol.ResultValue{{Label: "データ", Value: hexBytes(data)}}, ""
}
