package fins

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

// FINS のクライアント。外部の機器（PLC）につないで、FINS のコマンドを送る。
//
// リクエストは「コマンドコード（MRC SRC）+ パラメーター」で受け取り、FINS ヘッダー（宛先・送信元・SID）と、
// FINS/TCP のときはそのヘッダーを付けて送る。送ったバイト列と受け取ったバイト列は、そのまま結果に入れる。

const (
	defaultClientHost      = "127.0.0.1"
	defaultClientTimeoutMs = 1000
)

var (
	_ protocol.ClientFactory       = (*ClientFactory)(nil)
	_ protocol.RequestHelpProvider = (*ClientFactory)(nil)
)

// ClientFactory は FINS クライアントのファクトリー
type ClientFactory struct{}

func (f *ClientFactory) ProtocolType() protocol.ProtocolType { return ProtocolFINS }
func (f *ClientFactory) DisplayName() string                 { return "FINS" }

// ConnectionFields は接続先の設定項目を返す
func (f *ClientFactory) ConnectionFields() []protocol.ConfigField {
	return []protocol.ConfigField{
		{Name: "host", Label: "ホスト", Description: "接続先の PLC の IP アドレスまたはホスト名。", Type: "text", Required: true, Default: defaultClientHost},
		{Name: "port", Label: "ポート", Description: "接続先のポート番号。標準ポートは 9600 です。", Type: "number", Required: true, Default: defaultPort, Min: fields.IntPtr(1), Max: fields.IntPtr(65535)},
		{Name: "transport", Label: "通信方式", Description: "FINS/UDP か FINS/TCP。", Type: "select", Required: true, Default: transportUDP, Options: []protocol.FieldOption{
			{Value: transportUDP, Label: "FINS/UDP"},
			{Value: transportTCP, Label: "FINS/TCP"},
		}},
		{Name: "destNode", Label: "相手のノードアドレス", Description: "FINS ヘッダーの DA1（0〜254）。0 なら自動（UDP は接続先の IP アドレスの最後の数字、TCP は接続したときに PLC が知らせる値）。", Type: "number", Required: true, Default: 0, Min: fields.IntPtr(0), Max: fields.IntPtr(254)},
		{Name: "srcNode", Label: "自分のノードアドレス", Description: "FINS ヘッダーの SA1（0〜254）。0 なら自動（UDP はこの PC の IP アドレスの最後の数字、TCP は PLC が割り当てる値）。", Type: "number", Required: true, Default: 0, Min: fields.IntPtr(0), Max: fields.IntPtr(254)},
		{Name: "timeoutMs", Label: "タイムアウト（ms）", Description: "接続と応答を待つ時間。過ぎるとタイムアウトにして接続を閉じます。", Type: "number", Required: true, Default: defaultClientTimeoutMs, Min: fields.IntPtr(10), Max: fields.IntPtr(60000)},
	}
}

type clientSettings struct {
	Host      string
	Port      int
	Transport string
	DestNode  int
	SrcNode   int
	TimeoutMs int
}

func parseClientSettings(settings map[string]interface{}) (clientSettings, error) {
	s := clientSettings{Host: defaultClientHost, Port: defaultPort, Transport: transportUDP, TimeoutMs: defaultClientTimeoutMs}
	fields.SetString(settings, "host", &s.Host)
	fields.SetInt(settings, "port", &s.Port)
	fields.SetString(settings, "transport", &s.Transport)
	fields.SetInt(settings, "destNode", &s.DestNode)
	fields.SetInt(settings, "srcNode", &s.SrcNode)
	fields.SetInt(settings, "timeoutMs", &s.TimeoutMs)
	s.Host = strings.TrimSpace(s.Host)
	switch {
	case s.Host == "":
		return s, fmt.Errorf("ホストを入力してください")
	case s.Port < 1 || s.Port > 65535:
		return s, fmt.Errorf("ポートは 1〜65535 で指定してください")
	case s.Transport != transportUDP && s.Transport != transportTCP:
		return s, fmt.Errorf("通信方式は FINS/UDP か FINS/TCP にしてください")
	case s.DestNode < 0 || s.DestNode > 254:
		return s, fmt.Errorf("相手のノードアドレスは 0〜254 で指定してください")
	case s.SrcNode < 0 || s.SrcNode > 254:
		return s, fmt.Errorf("自分のノードアドレスは 0〜254 で指定してください")
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
		"host": s.Host, "port": s.Port, "transport": s.Transport,
		"destNode": s.DestNode, "srcNode": s.SrcNode, "timeoutMs": s.TimeoutMs,
	}, nil
}

// NewClient は接続先の設定からクライアントを作る
func (f *ClientFactory) NewClient(settings map[string]interface{}) (protocol.Client, error) {
	s, err := parseClientSettings(settings)
	if err != nil {
		return nil, err
	}
	return &Client{
		address:   net.JoinHostPort(s.Host, strconv.Itoa(s.Port)),
		transport: s.Transport,
		destNode:  byte(s.DestNode),
		srcNode:   byte(s.SrcNode),
		timeout:   time.Duration(s.TimeoutMs) * time.Millisecond,
	}, nil
}

// RequestHelp は、16 進で入れるリクエストの説明を返す
func (f *ClientFactory) RequestHelp() protocol.RequestHelp {
	return protocol.RequestHelp{
		Description: "FINS のコマンド（コマンドコード 2 バイト + パラメーター）を入れます。FINS ヘッダー（宛先・送信元・SID）と FINS/TCP のヘッダーはクライアントが付けます。",
		Example:     "01 01 82 00 64 00 00 0A",
	}
}

// --- コマンド ---

const (
	commandRead   = "read"
	commandWrite  = "write"
	commandFill   = "fill"
	commandCPU    = "cpuUnitData"
	commandStatus = "cpuUnitStatus"
	commandClock  = "clock"

	unitWord = "word"
	unitBit  = "bit"
)

func areaField() protocol.ConfigField {
	options := make([]protocol.FieldOption, len(areaDefs))
	for i, a := range areaDefs {
		options[i] = protocol.FieldOption{Value: a.ID, Label: a.Name}
	}
	return protocol.ConfigField{Name: "area", Label: "エリア", Type: "select", Required: true, Default: AreaDM, Options: options}
}

func unitField() protocol.ConfigField {
	return protocol.ConfigField{Name: "unit", Label: "単位", Description: "T（タイマ）と C（カウンタ）は、ワードが現在値、ビットがアップフラグです。", Type: "select", Required: true, Default: unitWord, Options: []protocol.FieldOption{
		{Value: unitWord, Label: "ワード"},
		{Value: unitBit, Label: "ビット"},
	}}
}

func addressField() protocol.ConfigField {
	return protocol.ConfigField{Name: "address", Label: "アドレス", Description: "ワードの番号（D100 なら 100、CIO 100.05 なら 100）。T と C は、タイマ・カウンタの番号。", Type: "number", Required: true, Default: 0, Min: fields.IntPtr(0), Max: fields.IntPtr(32767)}
}

func bitField() protocol.ConfigField {
	return protocol.ConfigField{Name: "bit", Label: "ビット", Description: "ワードの中のビットの番号（CIO 100.05 なら 5）。T と C のアップフラグでは使いません。", Type: "number", Required: true, Default: 0, Min: fields.IntPtr(0), Max: fields.IntPtr(15),
		Condition: &protocol.FieldCondition{Field: "unit", Value: unitBit}}
}

func commandTitle(cmd uint16) string {
	return fmt.Sprintf("%04X %s", cmd, commandNames[cmd])
}

// Commands は組み立てられるコマンドの一覧を返す
func (f *ClientFactory) Commands() []protocol.CommandDefinition {
	return []protocol.CommandDefinition{
		{ID: commandRead, DisplayName: commandTitle(cmdMemoryRead), Pollable: true, WordValues: true, Fields: []protocol.ConfigField{
			areaField(), unitField(), addressField(), bitField(),
			{Name: "count", Label: "個数", Description: fmt.Sprintf("読み出す個数（1〜%d）。", maxReadCount), Type: "number", Required: true, Default: 1, Min: fields.IntPtr(1), Max: fields.IntPtr(maxReadCount)},
		}},
		{ID: commandWrite, DisplayName: commandTitle(cmdMemoryWrite), Fields: []protocol.ConfigField{
			areaField(), unitField(), addressField(), bitField(),
			{Name: "values", Label: "値", Description: fmt.Sprintf("値をカンマか空白で区切って並べる（ワードの例: 100, 0x10, -1。ビットの例: 1,0,1）。1〜%d 個。", maxWriteCount), Type: "text", Required: true, Default: "0"},
		}},
		{ID: commandFill, DisplayName: commandTitle(cmdMemoryFill), Fields: []protocol.ConfigField{
			areaField(), addressField(),
			{Name: "count", Label: "個数", Description: "同じ値を書くワードの数。", Type: "number", Required: true, Default: 1, Min: fields.IntPtr(1), Max: fields.IntPtr(32768)},
			{Name: "value", Label: "値", Description: "0〜65535（-32768〜-1 も可）。0x で始めると 16 進。", Type: "text", Required: true, Default: "0"},
		}},
		{ID: commandCPU, DisplayName: commandTitle(cmdCPUUnitData)},
		{ID: commandStatus, DisplayName: commandTitle(cmdCPUUnitStatus)},
		{ID: commandClock, DisplayName: commandTitle(cmdClockRead)},
	}
}

// buildTarget は、エリア・単位・アドレス・ビットの項目から、エリア種別とアドレスを決める
func buildTarget(params map[string]interface{}, allowBit bool) (target, error) {
	def, ok := areaByID(fields.String(params, "area"))
	if !ok {
		return target{}, fmt.Errorf("エリアを選んでください")
	}
	t := target{def: def}
	var err error
	if t.word, err = fields.Int(params, "address", "アドレス", 0, def.Words-1); err != nil {
		return t, err
	}
	if allowBit && fields.String(params, "unit") == unitBit {
		t.isBit = true
		if def.Flags {
			return t, nil // アップフラグは番号ごとに 1 つ（ビットの番号はない）
		}
		if t.bit, err = fields.Int(params, "bit", "ビット", 0, 15); err != nil {
			return t, err
		}
	}
	return t, nil
}

// BuildRequest は、コマンドと項目の値からリクエスト（コマンドコード + パラメーター）を作る
func (f *ClientFactory) BuildRequest(commandID string, params map[string]interface{}) ([]byte, error) {
	switch commandID {
	case commandRead:
		t, err := buildTarget(params, true)
		if err != nil {
			return nil, err
		}
		count, err := fields.Int(params, "count", "個数", 1, maxReadCount)
		if err != nil {
			return nil, err
		}
		return readRequest(t, count)

	case commandWrite:
		t, err := buildTarget(params, true)
		if err != nil {
			return nil, err
		}
		if t.isBit {
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

	case commandFill:
		t, err := buildTarget(params, false)
		if err != nil {
			return nil, err
		}
		count, err := fields.Int(params, "count", "個数", 1, t.remaining())
		if err != nil {
			return nil, err
		}
		value, err := fields.ParseWord(fields.String(params, "value"))
		if err != nil {
			return nil, fmt.Errorf("値: %w", err)
		}
		req := binary.BigEndian.AppendUint16(nil, cmdMemoryFill)
		req = t.appendTo(req)
		req = binary.BigEndian.AppendUint16(req, uint16(count))
		return binary.BigEndian.AppendUint16(req, value), nil

	case commandCPU:
		return binary.BigEndian.AppendUint16(nil, cmdCPUUnitData), nil
	case commandStatus:
		return binary.BigEndian.AppendUint16(nil, cmdCPUUnitStatus), nil
	case commandClock:
		return binary.BigEndian.AppendUint16(nil, cmdClockRead), nil
	}
	return nil, fmt.Errorf("不明なコマンドです: %s", commandID)
}

func readRequest(t target, count int) ([]byte, error) {
	if count < 1 || count > maxReadCount {
		return nil, fmt.Errorf("読む個数は 1〜%d にしてください（%d）", maxReadCount, count)
	}
	if count > t.remaining() {
		return nil, fmt.Errorf("アドレスと個数の合計が、エリアの範囲を超えています")
	}
	req := binary.BigEndian.AppendUint16(nil, cmdMemoryRead)
	req = t.appendTo(req)
	return binary.BigEndian.AppendUint16(req, uint16(count)), nil
}

func writeRequest(t target, words []uint16, bits []bool) ([]byte, error) {
	count := len(words)
	if t.isBit {
		count = len(bits)
	}
	if count < 1 || count > maxWriteCount {
		return nil, fmt.Errorf("値は 1〜%d 個にしてください（%d 個あります）", maxWriteCount, count)
	}
	if count > t.remaining() {
		return nil, fmt.Errorf("アドレスと個数の合計が、エリアの範囲を超えています")
	}
	req := binary.BigEndian.AppendUint16(nil, cmdMemoryWrite)
	req = t.appendTo(req)
	req = binary.BigEndian.AppendUint16(req, uint16(count))
	if t.isBit {
		for _, b := range bits {
			if b {
				req = append(req, 1)
			} else {
				req = append(req, 0)
			}
		}
		return req, nil
	}
	for _, w := range words {
		req = binary.BigEndian.AppendUint16(req, w)
	}
	return req, nil
}

// --- 解釈 ---

// requestTarget は、リクエストのパラメーターの先頭（エリア種別 + アドレス + 個数）を読む
func requestTarget(req []byte) (t target, count int, ok bool) {
	if len(req) < 8 {
		return target{}, 0, false
	}
	t, end := targetFromWire(req[2], int(binary.BigEndian.Uint16(req[3:5])), int(req[5]))
	if end != endNormal {
		return target{}, 0, false
	}
	return t, int(binary.BigEndian.Uint16(req[6:8])), true
}

// DescribeRequest はリクエストの内容を 1 行の要約にする
func (f *ClientFactory) DescribeRequest(request []byte) string {
	return describeRequest(request)
}

func describeRequest(req []byte) string {
	if len(req) < 2 {
		return "（コマンドコードがありません）"
	}
	cmd := binary.BigEndian.Uint16(req)
	if _, known := commandNames[cmd]; !known {
		return fmt.Sprintf("コマンド %04X（%d バイト）", cmd, len(req))
	}
	head := commandTitle(cmd)
	switch cmd {
	case cmdMemoryRead, cmdMemoryWrite, cmdMemoryFill:
		t, count, ok := requestTarget(req)
		if !ok {
			return head + "（エリアを読めません）"
		}
		return fmt.Sprintf("%s %s から %d 個", head, t.label(0), count)
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

// decodeResponse は応答の FINS フレームを解釈する。req はこちらが送ったリクエスト（コマンドコード + パラメーター）
func decodeResponse(req, frame []byte) (protocol.CommandStatus, []protocol.ResultValue, string) {
	if len(frame) < headerLength+4 {
		return protocol.CommandError, nil, fmt.Sprintf("応答が短すぎます（%d バイト）", len(frame))
	}
	if frame[0]&icfResponse == 0 {
		return protocol.CommandError, nil, "応答ではないフレームを受け取りました"
	}
	cmd := binary.BigEndian.Uint16(req)
	if got := binary.BigEndian.Uint16(frame[headerLength:]); got != cmd {
		return protocol.CommandError, nil, fmt.Sprintf("応答のコマンドコード（%04X）がリクエスト（%04X）と違います", got, cmd)
	}
	// 終了コードのうち、中継のエラー（最上位ビット）と CPU の異常の有無（下位バイトの上位 2 ビット）は、結果の判定から外す
	end := binary.BigEndian.Uint16(frame[headerLength+2:]) &^ 0x80C0
	data := frame[headerLength+4:]
	if end != endNormal {
		text := endCodeText(end)
		return protocol.CommandException, []protocol.ResultValue{{Label: "終了コード", Value: text}}, "異常終了: " + text
	}

	switch cmd {
	case cmdMemoryRead:
		t, count, ok := requestTarget(req)
		if !ok {
			break
		}
		values := make([]protocol.ResultValue, count)
		if t.isBit {
			if len(data) != count {
				return protocol.CommandError, nil, fmt.Sprintf("応答のデータが %d ビット分ではありません（%d バイト）", count, len(data))
			}
			for i := range values {
				on := data[i]&1 != 0
				values[i] = protocol.ResultValue{Label: t.label(i), Value: bitText(on), Bit: &on}
			}
			return protocol.CommandOK, values, ""
		}
		if len(data) != count*2 {
			return protocol.CommandError, nil, fmt.Sprintf("応答のデータが %d ワードではありません（%d バイト）", count, len(data))
		}
		for i := range values {
			w := binary.BigEndian.Uint16(data[i*2:])
			values[i] = protocol.ResultValue{Label: t.label(i), Value: wordText(w), Word: &w}
		}
		return protocol.CommandOK, values, ""

	case cmdMemoryWrite, cmdMemoryFill:
		return protocol.CommandOK, []protocol.ResultValue{{Label: "結果", Value: "正常終了"}}, ""

	case cmdCPUUnitData:
		if len(data) >= 40 {
			return protocol.CommandOK, []protocol.ResultValue{
				{Label: "形式", Value: strings.TrimRight(string(data[:20]), " \x00")},
				{Label: "バージョン", Value: strings.TrimRight(string(data[20:40]), " \x00")},
			}, ""
		}

	case cmdCPUUnitStatus:
		if len(data) >= 2 {
			status := "停止中"
			if data[0]&1 != 0 {
				status = "運転中"
			}
			mode := map[byte]string{0x00: "プログラム", 0x02: "モニター", 0x04: "運転"}[data[1]]
			if mode == "" {
				mode = fmt.Sprintf("0x%02X", data[1])
			}
			return protocol.CommandOK, []protocol.ResultValue{{Label: "状態", Value: status}, {Label: "運転モード", Value: mode}}, ""
		}

	case cmdClockRead:
		if len(data) >= 6 {
			return protocol.CommandOK, []protocol.ResultValue{
				{Label: "時計", Value: fmt.Sprintf("20%02X-%02X-%02X %02X:%02X:%02X", data[0], data[1], data[2], data[3], data[4], data[5])},
			}, ""
		}
	}
	// 解釈を持たないコマンド（16 進で入れたもの）は、データをそのまま見せる
	return protocol.CommandOK, []protocol.ResultValue{{Label: "データ", Value: hexBytes(data)}}, ""
}
