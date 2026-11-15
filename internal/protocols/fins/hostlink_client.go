package fins

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
	"karakuri/internal/protocols/serialport"
)

// FINS の Host Link（シリアル）のクライアント。シリアルポートの先の PLC に、Host Link に載せた FINS のコマンドを送る。
//
// コマンドの定義・リクエストの組み立てと要約・応答の解釈・変数マッピングは、FINS/UDP・TCP のクライアント（ClientFactory）と同じ。
// リクエストは同じ「コマンドコード + パラメーター」で受け取り、FINS のヘッダーを短い形（ICF 00 DA2 00 SA2 00 SID）にして、
// Host Link のフレーム（'@' 号機番号 "FA" ... FCS '*' CR）で送る。
//
//   - ポートは最初の送信で開き、タイムアウト・通信エラー・壊れた応答では閉じる（次の送信で開き直す）。
//     閉じると、遅れて届いた応答と途中の文字が捨てられるので、次の応答と取り違えない
//   - 応答が分けて届いたら（途中のフレームが '*' なしで FCS CR で終わる）、CR を送って続きをもらい、つないで読む
//   - FINS の終了コードが 0 以外なら exception（ポートは閉じない）。Host Link の終了コードが 00 以外・"IC" の応答も exception

const (
	defaultHostLinkTimeoutMs = 1000
	// hostLinkReadSlice は、応答を待つ間に取り消し（ctx）を確かめる間隔
	hostLinkReadSlice = 50 * time.Millisecond
)

var (
	_ protocol.ClientFactory         = (*HostLinkClientFactory)(nil)
	_ protocol.RequestHelpProvider   = (*HostLinkClientFactory)(nil)
	_ protocol.MappableClientFactory = (*HostLinkClientFactory)(nil)
)

// HostLinkClientFactory は FINS の Host Link のクライアントのファクトリー。
// コマンド・組み立て・要約・マッピングは、埋め込んだ ClientFactory（FINS/UDP・TCP）のものを使う
type HostLinkClientFactory struct {
	ClientFactory

	// open はポートを開く関数。nil なら実際のシリアルポートを開く（テストでは仮想のケーブルを渡す）
	open serialport.Opener
}

func (f *HostLinkClientFactory) ProtocolType() protocol.ProtocolType { return ProtocolFINSHostLink }
func (f *HostLinkClientFactory) DisplayName() string                 { return "FINS（Host Link）" }

// ConnectionFields は接続先の設定項目を返す
func (f *HostLinkClientFactory) ConnectionFields() []protocol.ConfigField {
	def := DefaultHostLinkConfig()
	return append(serialport.ConfigFields(def.Serial, "接続先の PLC につながっているシリアルポート（例: COM1、COM3）。"),
		protocol.ConfigField{Name: "unit", Label: "号機番号", Description: "送り先の PLC の Host Link の号機番号（0〜31）。", Type: "number", Required: true, Default: def.Unit, Min: fields.IntPtr(0), Max: fields.IntPtr(maxHostLinkUnit)},
		protocol.ConfigField{Name: "timeoutMs", Label: "タイムアウト（ms）", Description: "応答を待つ時間。過ぎるとタイムアウトにしてポートを閉じます（次の送信で開き直します）。", Type: "number", Required: true, Default: defaultHostLinkTimeoutMs, Min: fields.IntPtr(10), Max: fields.IntPtr(60000)},
	)
}

type hostLinkClientSettings struct {
	serial    serialport.Config
	Unit      int
	TimeoutMs int
}

func parseHostLinkClientSettings(settings map[string]interface{}) (hostLinkClientSettings, error) {
	def := DefaultHostLinkConfig()
	s := hostLinkClientSettings{serial: def.Serial, Unit: def.Unit, TimeoutMs: defaultHostLinkTimeoutMs}
	serialport.ApplySettings(settings, &s.serial)
	fields.SetInt(settings, "unit", &s.Unit)
	fields.SetInt(settings, "timeoutMs", &s.TimeoutMs)
	if err := serialport.Validate(s.serial); err != nil {
		return s, err
	}
	switch {
	case s.Unit < 0 || s.Unit > maxHostLinkUnit:
		return s, fmt.Errorf("号機番号は 0〜%d で指定してください", maxHostLinkUnit)
	case s.TimeoutMs < 10 || s.TimeoutMs > 60000:
		return s, fmt.Errorf("タイムアウトは 10〜60000 ms で指定してください")
	}
	return s, nil
}

// NormalizeSettings は接続先の設定を検証し、ない項目を既定値で埋めたものを返す
func (f *HostLinkClientFactory) NormalizeSettings(settings map[string]interface{}) (map[string]interface{}, error) {
	s, err := parseHostLinkClientSettings(settings)
	if err != nil {
		return nil, err
	}
	return serialport.ToMap(s.serial, map[string]interface{}{"unit": s.Unit, "timeoutMs": s.TimeoutMs}), nil
}

// NewClient は接続先の設定からクライアントを作る。ポートを開くのは最初の送信のとき
func (f *HostLinkClientFactory) NewClient(settings map[string]interface{}) (protocol.Client, error) {
	s, err := parseHostLinkClientSettings(settings)
	if err != nil {
		return nil, err
	}
	open := f.open
	if open == nil {
		open = serialport.Open
	}
	return &HostLinkClient{
		config:  s.serial,
		open:    open,
		unit:    s.Unit,
		timeout: time.Duration(s.TimeoutMs) * time.Millisecond,
	}, nil
}

// RequestHelp は、16 進で入れるリクエストの説明を返す
func (f *HostLinkClientFactory) RequestHelp() protocol.RequestHelp {
	return protocol.RequestHelp{
		Description: "FINS のコマンド（コマンドコード 2 バイト + パラメーター）を入れます。FINS のヘッダー（ICF DA2 SA2 SID）と Host Link のフレーム（'@' 号機番号 FA … FCS '*' CR）はクライアントが付けます。",
		Example:     "01 01 82 00 64 00 00 0A",
	}
}

// --- 送受信 ---

// HostLinkClient は FINS の Host Link のクライアント
type HostLinkClient struct {
	config  serialport.Config
	open    serialport.Opener
	unit    int
	timeout time.Duration

	mu        sync.Mutex // Send を 1 つずつにする。port と sid を守る
	port      serialport.Port
	sid       byte
	connected atomic.Bool
}

// Connected はポートを開いているかどうかを返す
func (c *HostLinkClient) Connected() bool {
	return c.connected.Load()
}

// Close はポートを閉じる
func (c *HostLinkClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
	return nil
}

func (c *HostLinkClient) closeLocked() {
	if c.port != nil {
		_ = c.port.Close()
		c.port = nil
	}
	c.connected.Store(false)
}

// Send は、リクエスト（コマンドコード + パラメーター）を Host Link のフレームにして送り、応答を待つ
func (c *HostLinkClient) Send(ctx context.Context, request []byte) protocol.CommandResult {
	result := protocol.CommandResult{Summary: describeRequest(request)}
	if len(request) < 2 || len(request) > maxFrameLength-headerLength {
		result.Status = protocol.CommandError
		result.Error = fmt.Sprintf("リクエストは 2〜%d バイトにしてください（%d バイトあります）", maxFrameLength-headerLength, len(request))
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
	}
	port := c.port

	c.sid++
	sid := c.sid
	fins := header{ICF: 0x00, GCT: 0x02, SID: sid}.append(nil) // 短い形（シリアルでつながった CPU 宛て）
	fins = append(fins, request...)
	frame := encodeHostLinkCommand(c.unit, 0, fins)
	result.Sent = frame
	if _, err := port.Write(frame); err != nil {
		return fail(protocol.CommandError, fmt.Sprintf("%s に送信できませんでした: %v", c.config.Port, err))
	}

	raw, text, status, msg := c.receive(ctx, port, deadline)
	result.Received = raw
	if status != protocol.CommandOK {
		return fail(status, msg)
	}
	result.Elapsed = time.Since(start)

	switch {
	case text.unit != c.unit:
		return fail(protocol.CommandError, fmt.Sprintf("応答の号機番号（%02d）が送り先（%02d）と違います", text.unit, c.unit))
	case text.code == hostLinkHeaderIC:
		result.Status = protocol.CommandException
		result.Error = "Host Link: 未定義のコマンド（IC）"
		return result
	case text.code != hostLinkHeaderFA:
		return fail(protocol.CommandError, fmt.Sprintf("応答のヘッダーコード（%s）が FA ではありません", text.code))
	case len(text.text) < 2:
		return fail(protocol.CommandError, "応答に終了コードがありません")
	}
	if end := string(text.text[:2]); end != hostLinkEndNormal {
		result.Status = protocol.CommandException
		result.Values = []protocol.ResultValue{{Label: "Host Link の終了コード", Value: hostLinkEndText(end)}}
		result.Error = "Host Link の異常終了: " + hostLinkEndText(end)
		return result
	}
	body, err := decodeHex(text.text[2:])
	if err != nil {
		return fail(protocol.CommandError, "応答の FINS の部分を読めません: "+err.Error())
	}
	resp, err := finsFromHostLink(body)
	if err != nil {
		return fail(protocol.CommandError, "応答の FINS の部分を読めません: "+err.Error())
	}
	if resp[9] != sid {
		return fail(protocol.CommandError, fmt.Sprintf("応答の SID（%d）がリクエスト（%d）と違います", resp[9], sid))
	}
	st, values, m := decodeResponse(request, resp)
	result.Status = st
	result.Values = values
	result.Error = m
	if st == protocol.CommandError {
		c.closeLocked()
	}
	return result
}

// receive は、'*' CR で終わるまで応答を受け取る。途中のフレーム（'*' のない FCS CR）が来たら、CR を送って続きをもらう。
// raw は受け取った文字そのまま、frame は全部のフレームをつないで読んだもの
func (c *HostLinkClient) receive(ctx context.Context, port serialport.Port, deadline time.Time) (raw []byte, frame hostLinkFrame, status protocol.CommandStatus, msg string) {
	buf := make([]byte, 1024)
	var cur []byte    // いま受け取っているフレーム
	var joined []byte // つないだもの（'@' から、最後のフレームの FCS の前まで。FCS は最後に付け直す）
	first := true

	for {
		if i := bytes.IndexByte(cur, hostLinkCR); i >= 0 {
			one := cur[:i+1]
			cur = cur[i+1:]
			if first {
				// '@' より前に届いたもの（前の応答の残りなど）は読み飛ばす
				at := bytes.IndexByte(one, hostLinkStart)
				if at < 0 {
					continue
				}
				one = one[at:]
			}
			if bytes.HasSuffix(one, []byte{'*', hostLinkCR}) {
				if first {
					f, err := parseHostLinkFrame(one)
					if err != nil {
						return raw, f, protocol.CommandError, "応答の " + err.Error()
					}
					return raw, f, protocol.CommandOK, ""
				}
				// 最後のフレーム: 続きの文字・FCS・'*' CR
				part, err := splitHostLinkPart(one[:len(one)-2])
				if err != nil {
					return raw, hostLinkFrame{}, protocol.CommandError, "応答の " + err.Error()
				}
				joined = append(joined, part...)
				f, err := parseHostLinkFrame(appendHostLinkTail(joined, 0))
				if err != nil {
					return raw, f, protocol.CommandError, "応答の " + err.Error()
				}
				return raw, f, protocol.CommandOK, ""
			}
			// 途中のフレーム: 文字・FCS・CR。FCS を確かめて、CR を送って続きをもらう
			part, err := splitHostLinkPart(one[:len(one)-1])
			if err != nil {
				return raw, hostLinkFrame{}, protocol.CommandError, "応答の " + err.Error()
			}
			joined = append(joined, part...)
			first = false
			if len(joined) > maxHostLinkText {
				return raw, hostLinkFrame{}, protocol.CommandError, "応答が長すぎます"
			}
			if _, err := port.Write([]byte{hostLinkCR}); err != nil {
				return raw, hostLinkFrame{}, protocol.CommandError, fmt.Sprintf("%s に送信できませんでした: %v", c.config.Port, err)
			}
			continue
		}
		if len(cur) > maxHostLinkText {
			return raw, hostLinkFrame{}, protocol.CommandError, "応答が長すぎます（CR が来ません）"
		}
		if ctx.Err() != nil {
			return raw, hostLinkFrame{}, protocol.CommandError, "取り消されました"
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if len(raw) > 0 {
				return raw, hostLinkFrame{}, protocol.CommandTimeout, "応答の途中でタイムアウトしました"
			}
			return raw, hostLinkFrame{}, protocol.CommandTimeout, fmt.Sprintf("%d ms 以内に応答がありませんでした", c.timeout.Milliseconds())
		}
		_ = port.SetReadTimeout(max(min(remaining, hostLinkReadSlice), time.Millisecond))
		n, err := port.Read(buf)
		if err != nil {
			return raw, hostLinkFrame{}, protocol.CommandError, fmt.Sprintf("%s が使えなくなりました: %v", c.config.Port, err)
		}
		raw = append(raw, buf[:n]...)
		cur = append(cur, buf[:n]...)
	}
}

// splitHostLinkPart は、分けて届いたフレーム 1 つ（末尾の FCS 2 桁まで。区切りは除いたもの）の FCS を確かめ、FCS の前の文字を返す
func splitHostLinkPart(b []byte) ([]byte, error) {
	if len(b) < 2 {
		return nil, errHostLinkFormat
	}
	body, fcsText := b[:len(b)-2], b[len(b)-2:]
	var fcs byte
	if _, err := fmt.Sscanf(string(fcsText), "%02X", &fcs); err != nil {
		return nil, errHostLinkFormat
	}
	if fcs != hostLinkFCS(body) {
		return nil, errHostLinkFCS
	}
	return body, nil
}
