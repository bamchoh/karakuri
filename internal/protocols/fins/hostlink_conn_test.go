package fins

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/protocols/serialport"
)

// FINS の Host Link のサーバーとクライアントを、仮想のヌルモデムケーブル（serialport.Line）でつないで確かめる。
// 片側 0 がサーバー、片側 1 がクライアント。

type hostLinkPair struct {
	line   *serialport.Line
	store  protocol.DataStore
	server *HostLinkServer
	client *HostLinkClient
}

func startHostLinkPair(t *testing.T, unit int, clientSettings map[string]interface{}) *hostLinkPair {
	t.Helper()
	line := serialport.NewLine()
	store := NewDataStore()
	cfg := DefaultHostLinkConfig()
	cfg.Unit = unit
	srv, err := (&HostLinkFactory{open: line.Opener(0)}).CreateServer(cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	settings := map[string]interface{}{"serialPort": "COM_FAKE", "unit": unit, "timeoutMs": 300}
	for k, v := range clientSettings {
		settings[k] = v
	}
	c, err := (&HostLinkClientFactory{open: line.Opener(1)}).NewClient(settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &hostLinkPair{line: line, store: store, server: srv.(*HostLinkServer), client: c.(*HostLinkClient)}
}

func (p *hostLinkPair) send(t *testing.T, commandID string, params map[string]interface{}) protocol.CommandResult {
	t.Helper()
	req, err := (&ClientFactory{}).BuildRequest(commandID, params)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	return p.client.Send(context.Background(), req)
}

func expectHostLinkStatus(t *testing.T, r protocol.CommandResult, want protocol.CommandStatus) {
	t.Helper()
	if r.Status != want {
		t.Fatalf("status = %s（%s）, want %s", r.Status, r.Error, want)
	}
}

// rawExchange は、クライアントの代わりに片側 1 から文字を送り、サーバーの応答を CR まで受け取る（応答がなければ空）
func rawExchange(t *testing.T, line *serialport.Line, frame string) string {
	t.Helper()
	port, err := line.Opener(1)(serialport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer port.Close()
	if _, err := port.Write([]byte(frame)); err != nil {
		t.Fatal(err)
	}
	_ = port.SetReadTimeout(50 * time.Millisecond)
	var got []byte
	buf := make([]byte, 256)
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) && !bytes.HasSuffix(got, []byte{'\r'}) {
		n, _ := port.Read(buf)
		got = append(got, buf[:n]...)
	}
	return string(got)
}

func TestHostLink_WriteAndReadThroughServer(t *testing.T) {
	p := startHostLinkPair(t, 5, nil)

	r := p.send(t, commandWrite, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": 100, "values": "11, 0x16, 33"})
	expectHostLinkStatus(t, r, protocol.CommandOK)
	if !strings.HasPrefix(string(r.Sent), "@05FA0") || !strings.HasSuffix(string(r.Sent), "*\r") {
		t.Errorf("送ったフレーム %q", r.Sent)
	}
	if w, _ := p.store.ReadWords(AreaDM, 100, 3); w[0] != 11 || w[1] != 0x16 || w[2] != 33 {
		t.Errorf("サーバーの DM = %v", w)
	}

	r = p.send(t, commandRead, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": 100, "count": 3})
	expectHostLinkStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 3 || *r.Values[1].Word != 0x16 {
		t.Errorf("読んだ値: %+v", r.Values)
	}
	// 短い形の応答（終了コード 00、ICF 40）
	if !strings.HasPrefix(string(r.Received), "@05FA0040") {
		t.Errorf("受け取ったフレーム %q", r.Received)
	}

	// ビットの書き込みと読み出し（CIO 10.03）
	expectHostLinkStatus(t, p.send(t, commandWrite, map[string]interface{}{"area": AreaCIO, "unit": unitBit, "address": 10, "bit": 3, "values": "1"}), protocol.CommandOK)
	r = p.send(t, commandRead, map[string]interface{}{"area": AreaCIO, "unit": unitBit, "address": 10, "bit": 2, "count": 3})
	expectHostLinkStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 3 || *r.Values[0].Bit || !*r.Values[1].Bit || *r.Values[2].Bit {
		t.Errorf("ビット: %+v", r.Values)
	}

	// 長い応答（999 ワード。1 フレームで返る）
	r = p.send(t, commandRead, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": 0, "count": 999})
	expectHostLinkStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 999 {
		t.Errorf("999 ワード: %d 個", len(r.Values))
	}

	// FINS の異常終了（範囲外）は exception で、ポートは閉じない
	req := []byte{0x01, 0x01, 0x82, 0xFF, 0xFF, 0x00, 0x00, 0x02} // DM 65535 から 2 ワード
	r = p.client.Send(context.Background(), req)
	expectHostLinkStatus(t, r, protocol.CommandException)
	if !p.client.Connected() || p.line.Opens(1) != 1 {
		t.Error("異常終了でポートを閉じた")
	}
}

// 実機の例のコマンド（ヘッダー 10 バイトの形で HR 7.00 から 4 ビットを読む）に、実機と同じ応答を返す
func TestHostLink_ServerAnswersExampleLikeTheRealPLC(t *testing.T) {
	p := startHostLinkPair(t, 0, nil)
	if err := p.store.WriteWord(AreaHR, 7, 0x0005); err != nil { // H7.00 と H7.02 が ON
		t.Fatal(err)
	}
	if got := rawExchange(t, p.line, exampleHostLinkCommand); got != exampleHostLinkResponse {
		t.Errorf("応答 %q, want %q", got, exampleHostLinkResponse)
	}
}

func TestHostLink_ServerErrorResponses(t *testing.T) {
	p := startHostLinkPair(t, 0, nil)

	// FCS が合わない → 終了コード 13
	bad := []byte(exampleHostLinkCommand)
	bad[len(bad)-4] = '1'
	if got := rawExchange(t, p.line, string(bad)); !strings.HasPrefix(got, "@00FA13") {
		t.Errorf("FCS の誤りへの応答 %q", got)
	}
	// "FA" 以外のヘッダーコード → IC
	rd := appendHostLinkTail(appendHostLinkHead(nil, 0, "RD"), 0)
	if got := rawExchange(t, p.line, string(rd)); got != "@00IC4A*\r" {
		t.Errorf("未定義のコマンドへの応答 %q", got)
	}
	// 16 進ではない FINS → 終了コード 14
	zz := appendHostLinkTail(append(appendHostLinkHead(nil, 0, "FA"), "0ZZ"...), 0)
	if got := rawExchange(t, p.line, string(zz)); !strings.HasPrefix(got, "@00FA14") {
		t.Errorf("形式の誤りへの応答 %q", got)
	}
	// ほかの号機宛てには応答しない
	other := encodeHostLinkCommand(1, 0, append(header{GCT: 2}.append(nil), 0x05, 0x01))
	if got := rawExchange(t, p.line, string(other)); got != "" {
		t.Errorf("ほかの号機宛てに応答した: %q", got)
	}
	// 前にごみがあっても、'@' からを読む
	cmd := encodeHostLinkCommand(0, 0, append(header{GCT: 2, SID: 7}.append(nil), 0x07, 0x01))
	if got := rawExchange(t, p.line, "\x00\r"+string(cmd)); !strings.HasPrefix(got, "@00FA0040000007"+"0701"+"0000") {
		t.Errorf("時計の読み出しへの応答 %q", got)
	}
}

// 号機番号が違えば応答がなく、タイムアウト。ポートを閉じ、次の送信で開き直す
func TestHostLink_ClientTimeoutAndReopen(t *testing.T) {
	p := startHostLinkPair(t, 3, map[string]interface{}{"unit": 4, "timeoutMs": 100})
	r := p.send(t, commandCPU, nil)
	expectHostLinkStatus(t, r, protocol.CommandTimeout)
	if p.client.Connected() {
		t.Error("タイムアウトしたのに、ポートが開いたまま")
	}
	p.client.unit = 3 // 送り先を合わせる（テストのため直接変える）
	expectHostLinkStatus(t, p.send(t, commandCPU, nil), protocol.CommandOK)
	if p.line.Opens(1) != 2 {
		t.Errorf("ポートを開いた回数 = %d, want 2", p.line.Opens(1))
	}
}

// 応答が 2 つのフレームに分けて届いたら、CR を送って続きをもらい、つないで読む
func TestHostLink_ClientReceivesMultiFrameResponse(t *testing.T) {
	line := serialport.NewLine()
	device, _ := line.Opener(0)(serialport.Config{})
	defer device.Close()
	c, err := (&HostLinkClientFactory{open: line.Opener(1)}).NewClient(map[string]interface{}{"serialPort": "COM_FAKE", "unit": 0, "timeoutMs": 1000})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// DM 0 から 2 ワード（0x1234, 0xABCD）を読んだ応答を、途中で分ける
	resp := append(header{ICF: 0x40, GCT: 2, SID: 1}.append(nil), 0x01, 0x01, 0x00, 0x00, 0x12, 0x34, 0xAB, 0xCD)
	whole := encodeHostLinkResponse(0, resp)
	text := whole[:len(whole)-4] // '@' から FCS の前まで
	part1, part2 := text[:20], text[20:]
	frame1 := appendFrameFCS(part1, "\r")
	frame2 := appendFrameFCS(part2, "*\r")

	acks := make(chan string, 4)
	go func() {
		buf := make([]byte, 256)
		var got []byte
		_ = device.SetReadTimeout(20 * time.Millisecond)
		for !bytes.HasSuffix(got, []byte("*\r")) {
			n, err := device.Read(buf)
			if err != nil {
				return
			}
			got = append(got, buf[:n]...)
		}
		_, _ = device.Write(frame1)
		var ack []byte
		for len(ack) == 0 {
			n, err := device.Read(buf)
			if err != nil {
				return
			}
			ack = append(ack, buf[:n]...)
		}
		acks <- string(ack)
		_, _ = device.Write(frame2)
	}()

	req, _ := (&ClientFactory{}).BuildRequest(commandRead, map[string]interface{}{"area": AreaDM, "unit": unitWord, "address": 0, "count": 2})
	r := c.Send(context.Background(), req)
	expectHostLinkStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 2 || *r.Values[0].Word != 0x1234 || *r.Values[1].Word != 0xABCD {
		t.Errorf("読んだ値: %+v", r.Values)
	}
	select {
	case ack := <-acks:
		if ack != "\r" {
			t.Errorf("続きを頼む文字 %q, want CR", ack)
		}
	default:
		t.Error("続きを頼む CR を送っていない")
	}
	if string(r.Received) != string(frame1)+string(frame2) {
		t.Errorf("受け取ったもの %q", r.Received)
	}
}

// appendFrameFCS は、文字に FCS と区切り（CR か '*' CR）を付ける
func appendFrameFCS(text []byte, tail string) []byte {
	out := append([]byte(nil), text...)
	out = append(out, []byte(strings.ToUpper(hexByte(hostLinkFCS(text))))...)
	return append(out, tail...)
}

func hexByte(b byte) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[b>>4], digits[b&0x0F]})
}

func TestHostLink_ClientBrokenResponses(t *testing.T) {
	cases := []struct {
		name   string
		reply  func(sid byte) string
		status protocol.CommandStatus
		msg    string
	}{
		{"FCS が合わない", func(byte) string {
			b := []byte(exampleHostLinkResponse)
			b[len(b)-4] = '0'
			return string(b)
		}, protocol.CommandError, "FCS"},
		{"Host Link の異常終了", func(byte) string { return string(encodeHostLinkError(0, "FA", "14")) }, protocol.CommandException, "14"},
		{"IC", func(byte) string { return "@00IC4A*\r" }, protocol.CommandException, "IC"},
		{"SID が違う", func(sid byte) string {
			return string(encodeHostLinkResponse(0, append(header{ICF: 0x40, GCT: 2, SID: sid + 1}.append(nil), 0x05, 0x01, 0x00, 0x00)))
		}, protocol.CommandError, "SID"},
		{"号機番号が違う", func(sid byte) string {
			return string(encodeHostLinkResponse(1, append(header{ICF: 0x40, GCT: 2, SID: sid}.append(nil), 0x05, 0x01, 0x00, 0x00)))
		}, protocol.CommandError, "号機番号"},
		{"'*' CR が来ない", func(byte) string { return "@00FA0040" }, protocol.CommandTimeout, "途中でタイムアウト"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := serialport.NewLine()
			device, _ := line.Opener(0)(serialport.Config{})
			defer device.Close()
			c, _ := (&HostLinkClientFactory{open: line.Opener(1)}).NewClient(map[string]interface{}{"serialPort": "COM_FAKE", "unit": 0, "timeoutMs": 150})
			defer c.Close()
			go func() {
				buf := make([]byte, 256)
				var got []byte
				_ = device.SetReadTimeout(20 * time.Millisecond)
				for !bytes.HasSuffix(got, []byte("*\r")) {
					n, err := device.Read(buf)
					if err != nil {
						return
					}
					got = append(got, buf[:n]...)
				}
				_, _ = device.Write([]byte(tc.reply(1)))
			}()
			r := c.Send(context.Background(), []byte{0x05, 0x01})
			expectHostLinkStatus(t, r, tc.status)
			if !strings.Contains(r.Error, tc.msg) {
				t.Errorf("Error = %q, want %q を含む", r.Error, tc.msg)
			}
		})
	}
}

func TestHostLink_ServerStopAndRestart(t *testing.T) {
	p := startHostLinkPair(t, 0, nil)
	expectHostLinkStatus(t, p.send(t, commandCPU, nil), protocol.CommandOK)
	time.Sleep(30 * time.Millisecond) // 読み取りを待っている最中に止める
	if err := p.server.Stop(); err != nil {
		t.Fatal(err)
	}
	if p.line.ClosedDuringRead(0) {
		t.Error("読み取りの途中でポートを閉じた")
	}
	if err := p.server.Start(context.Background()); err != nil {
		t.Fatalf("止めてすぐの Start: %v", err)
	}
	expectHostLinkStatus(t, p.send(t, commandCPU, nil), protocol.CommandOK)
}

func TestHostLinkFactories_Settings(t *testing.T) {
	cf := &HostLinkClientFactory{}
	got, err := cf.NormalizeSettings(map[string]interface{}{"serialPort": "COM3"})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]interface{}{"baudRate": 9600, "dataBits": 7, "stopBits": 2, "parity": "E", "unit": 0, "timeoutMs": 1000} {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	for _, bad := range []map[string]interface{}{{"serialPort": ""}, {"unit": 32}, {"parity": "X"}, {"timeoutMs": 5}} {
		if _, err := cf.NormalizeSettings(bad); err == nil {
			t.Errorf("%v: エラーにならない", bad)
		}
	}
	if len(cf.Commands()) == 0 || len(cf.MappingAreas()) == 0 {
		t.Error("コマンドかマッピングのエリアがない")
	}

	sf := &HostLinkFactory{}
	cfg, _ := sf.MapToConfig("", map[string]interface{}{"serialPort": "COM9", "unit": 12})
	if m := sf.ConfigToMap(cfg); m["serialPort"] != "COM9" || m["unit"] != 12 || m["baudRate"] != 9600 {
		t.Errorf("設定 %v", m)
	}
	bad := DefaultHostLinkConfig()
	bad.Unit = 40
	if bad.Validate() == nil {
		t.Error("号機番号 40 が通った")
	}
}
