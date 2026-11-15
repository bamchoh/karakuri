package modbus

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/protocols/modbus/rtu"
)

// Modbus ASCII のクライアント。フェイクの機器（client_rtu_test.go）を ASCII で応答させて確かめる。
// 送受信の共通の部分（ポートの開閉・タイムアウト・取り消しなど）は、RTU のテストで確かめている。

func newASCIIDevice() *fakeDevice {
	d := newFakeDevice()
	d.ascii = true
	return d
}

func newASCIITestClient(t *testing.T, dev *fakeDevice, settings map[string]interface{}) *serialClient {
	t.Helper()
	return newSerialTestClient(t, &ASCIIClientFactory{open: dev.open}, 9600, settings)
}

// asciiFrame は、16 進の内容（UnitID + PDU）の ASCII のフレーム
func asciiFrame(t *testing.T, body string) []byte {
	t.Helper()
	return rtu.BuildASCIIFrame(hexb(t, body))
}

func TestASCIIClientFactory_Settings(t *testing.T) {
	f := &ASCIIClientFactory{}
	if f.ProtocolType() != ProtocolModbusASCII || f.DisplayName() != "Modbus ASCII" {
		t.Errorf("種類 = %s / %s", f.ProtocolType(), f.DisplayName())
	}
	// 既定はサーバーと同じ 9600bps・7E1
	got, err := f.NormalizeSettings(map[string]interface{}{"serialPort": "COM3"})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]interface{}{"baudRate": 9600, "dataBits": 7, "stopBits": 1, "parity": "E", "unitId": 1, "timeoutMs": 1000} {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	defaults := map[string]interface{}{}
	for _, fd := range f.ConnectionFields() {
		defaults[fd.Name] = fd.Default
	}
	if _, err := f.NormalizeSettings(defaults); err != nil {
		t.Errorf("既定値の設定が通らない: %v", err)
	}
	if !strings.Contains(f.RequestHelp().Description, "LRC") {
		t.Errorf("説明 = %q", f.RequestHelp().Description)
	}
	if len(f.Commands()) == 0 || len(f.MappingAreas()) != 4 {
		t.Error("コマンドかマッピングのエリアがない")
	}
}

func TestASCIIFramer_ResponseLength(t *testing.T) {
	var f asciiFramer
	for _, c := range []struct {
		buf  string
		want int
	}{
		{"", 0},
		{":0103", 0},
		{":010302000AF0\r", 0},
		{":010302000AF0\r\n", 15},
		{":010302000AF0\r\n:01", 15},
	} {
		if got := f.responseLength([]byte(c.buf)); got != c.want {
			t.Errorf("responseLength(%q) = %d, want %d", c.buf, got, c.want)
		}
	}
}

func TestASCIIClient_WriteThenRead(t *testing.T) {
	dev := newASCIIDevice()
	c := newASCIITestClient(t, dev, nil)

	r := sendHex(t, c, "06 00 10 12 34")
	expectStatus(t, r, protocol.CommandOK)
	if want := asciiFrame(t, "01 06 00 10 12 34"); !bytes.Equal(r.Sent, want) {
		t.Errorf("送ったもの %q, want %q", r.Sent, want)
	}
	if !bytes.Equal(r.Received, r.Sent) {
		t.Errorf("FC06 の応答はリクエストと同じはず: %q", r.Received)
	}

	r = sendHex(t, c, "03 00 10 00 02")
	expectStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 2 || r.Values[0].Word == nil || *r.Values[0].Word != 0x1234 {
		t.Errorf("読んだ値: %+v", r.Values)
	}

	// コイル（ビット）も読み書きできる
	expectStatus(t, sendHex(t, c, "0F 00 00 00 03 01 05"), protocol.CommandOK)
	r = sendHex(t, c, "01 00 00 00 03")
	expectStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 3 || !*r.Values[0].Bit || *r.Values[1].Bit || !*r.Values[2].Bit {
		t.Errorf("コイル: %+v", r.Values)
	}
}

// 1 文字ずつ遅れて届いても、前に余分なバイトがあっても、':' から CR LF までを応答にする
func TestASCIIClient_ChunksAndNoise(t *testing.T) {
	dev := newASCIIDevice()
	c := newASCIITestClient(t, dev, nil)
	expectStatus(t, sendHex(t, c, "06 00 01 00 2A"), protocol.CommandOK)

	dev.set(func(d *fakeDevice) {
		d.chunk, d.chunkDelay = 1, 2*time.Millisecond
		d.respond = func(req []byte) []byte {
			r, err := rtu.ParseASCIIRequest(req)
			if err != nil {
				return nil
			}
			return append([]byte("\x00\n"), d.processor.ProcessASCII(r)...)
		}
	})
	r := sendHex(t, c, "03 00 01 00 01")
	expectStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 1 || *r.Values[0].Word != 42 {
		t.Errorf("読んだ値: %+v", r.Values)
	}
}

// 16 進の小文字の応答も読める
func TestASCIIClient_LowerCaseResponse(t *testing.T) {
	dev := newASCIIDevice()
	dev.set(func(d *fakeDevice) {
		d.respond = func([]byte) []byte {
			return []byte(strings.ToLower(string(asciiFrame(t, "01 03 02 AB CD"))))
		}
	})
	c := newASCIITestClient(t, dev, nil)
	r := sendHex(t, c, "03 00 00 00 01")
	expectStatus(t, r, protocol.CommandOK)
	if *r.Values[0].Word != 0xABCD {
		t.Errorf("読んだ値: %+v", r.Values)
	}
}

func TestASCIIClient_Exception(t *testing.T) {
	dev := newASCIIDevice()
	c := newASCIITestClient(t, dev, nil)
	r := sendHex(t, c, "08 00 00 12 34")
	expectStatus(t, r, protocol.CommandException)
	expectStatus(t, sendHex(t, c, "03 00 00 00 01"), protocol.CommandOK)
	if dev.openCount() != 1 {
		t.Errorf("例外応答でポートを開き直した（%d 回開いた）", dev.openCount())
	}
}

func TestASCIIClient_BrokenResponses(t *testing.T) {
	cases := []struct {
		name string
		resp func(t *testing.T) []byte
		want protocol.CommandStatus
		msg  string
	}{
		{"LRC が合わない", func(t *testing.T) []byte {
			b := asciiFrame(t, "01 03 02 00 05")
			b[len(b)-3] = '0'
			b[len(b)-4] = '0'
			return b
		}, protocol.CommandError, "LRC"},
		{"UnitID が違う", func(t *testing.T) []byte { return asciiFrame(t, "02 03 02 00 05") }, protocol.CommandError, "UnitID"},
		{"16 進ではない", func(*testing.T) []byte { return []byte(":01XX0200\r\n") }, protocol.CommandError, "ASCII のフレームとして読めません"},
		{"':' がない", func(*testing.T) []byte { return []byte("0103020005F5\r\n") }, protocol.CommandError, "':'"},
		{"CR LF が来ない", func(*testing.T) []byte { return []byte(":0103020005F5") }, protocol.CommandTimeout, "途中でタイムアウト"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dev := newASCIIDevice()
			dev.set(func(d *fakeDevice) { d.respond = func([]byte) []byte { return tc.resp(t) } })
			c := newASCIITestClient(t, dev, map[string]interface{}{"timeoutMs": 100})
			r := sendHex(t, c, "03 00 00 00 01")
			expectStatus(t, r, tc.want)
			if !strings.Contains(r.Error, tc.msg) {
				t.Errorf("Error = %q, want %q を含む", r.Error, tc.msg)
			}
			if c.Connected() {
				t.Error("壊れた応答のあと、ポートが開いたまま")
			}
		})
	}
}

// タイムアウトしたらポートを閉じ、遅れた応答は次の応答と取り違えない
func TestASCIIClient_TimeoutThenLateResponse(t *testing.T) {
	dev := newASCIIDevice()
	c := newASCIITestClient(t, dev, map[string]interface{}{"timeoutMs": 100})
	expectStatus(t, sendHex(t, c, "06 00 00 00 07"), protocol.CommandOK)

	dev.set(func(d *fakeDevice) { d.delay = 200 * time.Millisecond })
	expectStatus(t, sendHex(t, c, "03 00 00 00 01"), protocol.CommandTimeout)
	dev.set(func(d *fakeDevice) { d.delay = 0 })
	time.Sleep(150 * time.Millisecond)

	r := sendHex(t, c, "03 00 01 00 02")
	expectStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 2 || r.Values[0].Label != "アドレス 1" {
		t.Errorf("遅れた応答と取り違えた: %+v", r.Values)
	}
}

func TestASCIIClient_Broadcast(t *testing.T) {
	dev := newASCIIDevice()
	dev.set(func(d *fakeDevice) { d.respond = func([]byte) []byte { return nil } })
	c := newASCIITestClient(t, dev, map[string]interface{}{"unitId": 0, "timeoutMs": 1000})
	start := time.Now()
	expectStatus(t, sendHex(t, c, "06 00 00 00 01"), protocol.CommandOK)
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("ブロードキャストで応答を待った（%v）", time.Since(start))
	}
	if w := dev.writtenFrames(); len(w) != 1 || !bytes.Equal(w[0].frame, asciiFrame(t, "00 06 00 00 00 01")) {
		t.Errorf("機器に届いたもの: %+v", w)
	}
}

// 最も長い読み取りの応答（125 ワード。':' + 2 × (UnitID 1 + FC 1 + バイト数 1 + データ 250 + LRC 1) + CR LF = 511 文字）を読める
func TestASCIIClient_MaxRead(t *testing.T) {
	dev := newASCIIDevice()
	c := newASCIITestClient(t, dev, nil)
	r := sendHex(t, c, "03 00 00 00 7D")
	expectStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 125 || len(r.Received) != 511 {
		t.Errorf("%d 個 / %d 文字", len(r.Values), len(r.Received))
	}
}
