package slmp

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/protocols/serialport"
)

// MC プロトコル（シリアル）のサーバーとクライアントを、仮想のヌルモデムケーブル（serialport.Line）でつないで確かめる。
// 片側 0 がサーバー（C24）、片側 1 がクライアント。

type mcPair struct {
	line   *serialport.Line
	store  protocol.DataStore
	server *MCSerialServer
	client *MCSerialClient
}

func startMCPair(t *testing.T, format int, sum bool, station int, clientSettings map[string]interface{}) *mcPair {
	t.Helper()
	line := serialport.NewLine()
	store := NewDataStore()
	cfg := DefaultMCSerialConfig()
	cfg.Format, cfg.SumCheck, cfg.Station = format, sum, station
	srv, err := (&MCSerialFactory{open: line.Opener(0)}).CreateServer(cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	settings := map[string]interface{}{"serialPort": "COM_FAKE", "format": format, "sumCheck": sumText(sum), "station": station, "timeoutMs": 300}
	for k, v := range clientSettings {
		settings[k] = v
	}
	c, err := (&MCSerialClientFactory{open: line.Opener(1)}).NewClient(settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &mcPair{line: line, store: store, server: srv.(*MCSerialServer), client: c.(*MCSerialClient)}
}

func (p *mcPair) send(t *testing.T, commandID string, params map[string]interface{}) protocol.CommandResult {
	t.Helper()
	req, err := (&ClientFactory{}).BuildRequest(commandID, params)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	return p.client.Send(context.Background(), req)
}

func expectMCStatus(t *testing.T, r protocol.CommandResult, want protocol.CommandStatus) {
	t.Helper()
	if r.Status != want {
		t.Fatalf("status = %s（%s）, want %s", r.Status, r.Error, want)
	}
}

var mcVariants = []struct {
	name   string
	format int
	sum    bool
}{
	{"形式4・サムチェックあり", mcFormat4, true},
	{"形式4・サムチェックなし", mcFormat4, false},
	{"形式5・サムチェックあり", mcFormat5, true},
	{"形式5・サムチェックなし", mcFormat5, false},
}

func TestMCSerial_ReadWriteThroughServer(t *testing.T) {
	for _, v := range mcVariants {
		t.Run(v.name, func(t *testing.T) {
			p := startMCPair(t, v.format, v.sum, 3, nil)

			// ワードの書き込みと読み出し（16 = 10H を含む: 形式 5 では DLE を重ねる）
			expectMCStatus(t, p.send(t, commandWrite, map[string]interface{}{"device": "D", "address": "100", "values": "16, 0x1010, 4660"}), protocol.CommandOK)
			if w, _ := p.store.ReadWords("D", 100, 3); w[0] != 16 || w[1] != 0x1010 || w[2] != 4660 {
				t.Errorf("サーバーの D = %v", w)
			}
			r := p.send(t, commandRead, map[string]interface{}{"device": "D", "address": "100", "count": 3})
			expectMCStatus(t, r, protocol.CommandOK)
			if len(r.Values) != 3 || *r.Values[1].Word != 0x1010 || *r.Values[2].Word != 4660 {
				t.Errorf("読んだ値: %+v", r.Values)
			}

			// ビットデバイス（16 進のデバイス番号 X1F）
			expectMCStatus(t, p.send(t, commandWrite, map[string]interface{}{"device": "X", "address": "1F", "values": "1,0,1"}), protocol.CommandOK)
			r = p.send(t, commandRead, map[string]interface{}{"device": "X", "address": "1F", "count": 3})
			expectMCStatus(t, r, protocol.CommandOK)
			if len(r.Values) != 3 || !*r.Values[0].Bit || *r.Values[1].Bit || !*r.Values[2].Bit {
				t.Errorf("ビット: %+v", r.Values)
			}

			// 形名の読み出し・折り返しテスト
			r = p.send(t, commandTypeName, nil)
			expectMCStatus(t, r, protocol.CommandOK)
			if !strings.Contains(fmt.Sprint(r.Values), "Q03UDECPU") {
				t.Errorf("形名: %+v", r.Values)
			}
			r = p.send(t, commandSelfTest, map[string]interface{}{"data": "ABCDE"})
			expectMCStatus(t, r, protocol.CommandOK)

			// 最大の読み出し（960 ワード）
			r = p.send(t, commandRead, map[string]interface{}{"device": "D", "address": "0", "count": 960})
			expectMCStatus(t, r, protocol.CommandOK)
			if len(r.Values) != 960 {
				t.Errorf("960 ワード: %d 個", len(r.Values))
			}

			// 異常（点数の範囲外）は exception で、ポートは閉じない
			r = p.client.Send(context.Background(), []byte{0x01, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0xA8, 0x00, 0x00}) // 0 点
			expectMCStatus(t, r, protocol.CommandException)
			if !strings.Contains(r.Error, "C051") {
				t.Errorf("Error = %q", r.Error)
			}
			if !p.client.Connected() || p.line.Opens(1) != 1 {
				t.Error("異常の応答でポートを閉じた")
			}

			// ランダム読み出し・ランダム書き込み（16 進で入れた要求）
			expectMCStatus(t, p.client.Send(context.Background(), unhexMC(t, "02 14 01 00 01 32 00 00 90 01")), protocol.CommandOK) // M50 を ON
			if bits, _ := p.store.ReadBits("M", 50, 1); !bits[0] {
				t.Error("M50 が ON になっていない")
			}
			r = p.client.Send(context.Background(), unhexMC(t, "03 04 00 00 01 00 64 00 00 A8")) // D100
			expectMCStatus(t, r, protocol.CommandOK)
		})
	}
}

// 局番の違う要求には応答しない（タイムアウト）。ポートを閉じ、次の送信で開き直す
func TestMCSerial_OtherStationTimesOut(t *testing.T) {
	for _, v := range mcVariants[:3:3] {
		t.Run(v.name, func(t *testing.T) {
			p := startMCPair(t, v.format, v.sum, 3, map[string]interface{}{"station": 4, "timeoutMs": 100})
			r := p.send(t, commandTypeName, nil)
			expectMCStatus(t, r, protocol.CommandTimeout)
			if p.client.Connected() {
				t.Error("タイムアウトしたのに、ポートが開いたまま")
			}
			p.client.route = hostRoute(3) // 送り先を合わせる（テストのため直接変える）
			expectMCStatus(t, p.send(t, commandTypeName, nil), protocol.CommandOK)
			if p.line.Opens(1) != 2 {
				t.Errorf("ポートを開いた回数 = %d, want 2", p.line.Opens(1))
			}
		})
	}
}

// rawMC は、クライアントの代わりに片側 1 から送り、サーバーの応答を受け取る（wait の間）
func rawMC(t *testing.T, line *serialport.Line, frame []byte, wait time.Duration) []byte {
	t.Helper()
	port, err := line.Opener(1)(serialport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer port.Close()
	if _, err := port.Write(frame); err != nil {
		t.Fatal(err)
	}
	_ = port.SetReadTimeout(20 * time.Millisecond)
	var got []byte
	buf := make([]byte, 256)
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		n, _ := port.Read(buf)
		got = append(got, buf[:n]...)
	}
	return got
}

func TestMCSerial_ServerErrorResponses(t *testing.T) {
	// 形式 4: サムチェックが合わない → NAK、扱えないコマンド → NAK C059、読めないデバイス → NAK C05C
	p := startMCPair(t, mcFormat4, true, 0, nil)
	good := encodeF4(ctrlENQ, hostRoute(0), []byte("01010000"), true)
	bad := append([]byte(nil), good...)
	bad[len(bad)-3] ^= 0x01
	if got := rawMC(t, p.line, bad, 150*time.Millisecond); !bytes.HasPrefix(got, []byte("\x15F80000FF03FF0000C05C")) {
		t.Errorf("サムチェックの誤りへの応答 %q", got)
	}
	if got := rawMC(t, p.line, encodeF4(ctrlENQ, hostRoute(0), []byte("08010000"), true), 150*time.Millisecond); string(got) != "\x15F80000FF03FF0000C059\r\n" {
		t.Errorf("扱えないコマンドへの応答 %q", got)
	}
	if got := rawMC(t, p.line, encodeF4(ctrlENQ, hostRoute(0), []byte("04010000Q*0000000001"), true), 150*time.Millisecond); string(got) != "\x15F80000FF03FF0000C05C\r\n" {
		t.Errorf("読めないデバイスへの応答 %q", got)
	}
	// 書き込みの応答はデータなし（ACK）
	if got := rawMC(t, p.line, encodeF4(ctrlENQ, hostRoute(0), []byte("14010000D*0000000001ABCD"), true), 150*time.Millisecond); string(got) != "\x06F80000FF03FF0000\r\n" {
		t.Errorf("書き込みの応答 %q", got)
	}
	// 前にごみがあっても、ENQ からを読む
	if got := rawMC(t, p.line, append([]byte("zz\r\n"), good...), 150*time.Millisecond); !bytes.HasPrefix(got, []byte("\x02F80000FF03FF0000Q03UDECPU")) {
		t.Errorf("形名の応答 %q", got)
	}

	// 形式 5: 異常は FFFF + エラーコード
	p5 := startMCPair(t, mcFormat5, true, 0, nil)
	got := rawMC(t, p5.line, encodeF5(hostRoute(0), unhexMC(t, "01 08 00 00"), true), 150*time.Millisecond)
	route, body, err := parseF5(got, true)
	if err != nil || route != hostRoute(0) || !bytes.Equal(body, unhexMC(t, "FF FF 59 C0")) {
		t.Errorf("形式 5 の異常の応答 % X (%v)", got, err)
	}
}

func TestMCSerial_ClientBrokenResponses(t *testing.T) {
	cases := []struct {
		name   string
		format int
		reply  func() []byte
		status protocol.CommandStatus
		msg    string
	}{
		{"形式4 サムチェックが合わない", mcFormat4, func() []byte {
			b := encodeF4(ctrlSTX, hostRoute(0), []byte("0001"), true)
			b[len(b)-3] ^= 0x01
			return b
		}, protocol.CommandError, "サムチェック"},
		{"形式4 局番が違う", mcFormat4, func() []byte { return encodeF4(ctrlSTX, hostRoute(1), []byte("0001"), true) }, protocol.CommandError, "局番"},
		{"形式4 データが足りない", mcFormat4, func() []byte { return encodeF4(ctrlSTX, hostRoute(0), []byte("00"), true) }, protocol.CommandError, "短すぎ"},
		{"形式4 NAK", mcFormat4, func() []byte { return encodeF4(ctrlNAK, hostRoute(0), []byte("7140"), true) }, protocol.CommandException, "7140"},
		{"形式4 CR LF が来ない", mcFormat4, func() []byte { return []byte("\x02F80000FF03FF0000") }, protocol.CommandTimeout, "途中でタイムアウト"},
		{"形式5 応答 ID がない", mcFormat5, func() []byte { return encodeF5(hostRoute(0), []byte{0x00, 0x00}, true) }, protocol.CommandError, "応答 ID"},
		{"形式5 異常", mcFormat5, func() []byte { return encodeF5(hostRoute(0), []byte{0xFF, 0xFF, 0x51, 0xC0}, true) }, protocol.CommandException, "C051"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := serialport.NewLine()
			device, _ := line.Opener(0)(serialport.Config{})
			defer device.Close()
			c, _ := (&MCSerialClientFactory{open: line.Opener(1)}).NewClient(map[string]interface{}{
				"serialPort": "COM_FAKE", "format": tc.format, "sumCheck": "on", "station": 0, "timeoutMs": 150,
			})
			defer c.Close()
			go func() {
				buf := make([]byte, 256)
				_ = device.SetReadTimeout(20 * time.Millisecond)
				for {
					n, err := device.Read(buf)
					if err != nil {
						return
					}
					if n > 0 {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				_, _ = device.Write(tc.reply())
			}()
			r := c.Send(context.Background(), unhexMC(t, "01 04 00 00 00 00 00 A8 01 00"))
			expectMCStatus(t, r, tc.status)
			if !strings.Contains(r.Error, tc.msg) {
				t.Errorf("Error = %q, want %q を含む", r.Error, tc.msg)
			}
		})
	}
}

// 形式 4 で ASCII にできない要求は、送らずにエラー
func TestMCSerial_ClientRejectsUnsupportedASCIICommand(t *testing.T) {
	p := startMCPair(t, mcFormat4, true, 0, nil)
	r := p.client.Send(context.Background(), unhexMC(t, "01 08 00 00"))
	expectMCStatus(t, r, protocol.CommandError)
	if p.line.Opens(1) != 0 {
		t.Error("送れない要求でポートを開いた")
	}
}

func TestMCSerial_ServerStopAndRestart(t *testing.T) {
	p := startMCPair(t, mcFormat5, true, 0, nil)
	expectMCStatus(t, p.send(t, commandTypeName, nil), protocol.CommandOK)
	time.Sleep(30 * time.Millisecond)
	if err := p.server.Stop(); err != nil {
		t.Fatal(err)
	}
	if p.line.ClosedDuringRead(0) {
		t.Error("読み取りの途中でポートを閉じた")
	}
	if err := p.server.Start(context.Background()); err != nil {
		t.Fatalf("止めてすぐの Start: %v", err)
	}
	expectMCStatus(t, p.send(t, commandTypeName, nil), protocol.CommandOK)
}

func TestMCSerialFactories_Settings(t *testing.T) {
	cf := &MCSerialClientFactory{}
	got, err := cf.NormalizeSettings(map[string]interface{}{"serialPort": "COM3", "format": "5", "sumCheck": "off"})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]interface{}{"baudRate": 9600, "dataBits": 8, "parity": "N", "format": 5, "sumCheck": "off", "station": 0, "timeoutMs": 1000} {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	for _, bad := range []map[string]interface{}{{"serialPort": ""}, {"format": 3}, {"station": 32}, {"timeoutMs": 5}} {
		if _, err := cf.NormalizeSettings(bad); err == nil {
			t.Errorf("%v: エラーにならない", bad)
		}
	}
	defaults := map[string]interface{}{}
	for _, fd := range cf.ConnectionFields() {
		defaults[fd.Name] = fd.Default
	}
	if _, err := cf.NormalizeSettings(defaults); err != nil {
		t.Errorf("既定値の設定が通らない: %v", err)
	}
	if len(cf.Commands()) == 0 || len(cf.MappingAreas()) == 0 {
		t.Error("コマンドかマッピングのエリアがない")
	}

	sf := &MCSerialFactory{}
	cfg, _ := sf.MapToConfig("", map[string]interface{}{"serialPort": "COM9", "format": 5, "sumCheck": "off", "station": 12})
	if m := sf.ConfigToMap(cfg); m["serialPort"] != "COM9" || m["format"] != 5 || m["sumCheck"] != "off" || m["station"] != 12 {
		t.Errorf("設定 %v", m)
	}
}
