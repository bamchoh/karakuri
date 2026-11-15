package modbus

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/protocols/modbus/rtu"
)

// ===== フェイクの機器（シリアルポートの先のスレーブ） =====
//
// クライアントが開くポート（fakeDevicePort）は、実際のポートと同じように動く:
// 届いているバイトがあればすぐ返し、タイムアウトしたら (0, nil)、閉じたら（抜いたら）error。
// 書かれたリクエストには、Karakuri の RTU サーバーと同じ処理（rtu.Processor）で応答する（respond で差し替えられる）。

type fakeDevice struct {
	mu         sync.Mutex
	ports      []*fakeDevicePort
	openErr    error
	writes     []deviceWrite
	respond    func(req []byte) []byte // nil なら processor の応答
	delay      time.Duration           // 応答を返すまでの時間
	chunk      int                     // 応答を何バイトずつ渡すか（0 なら一度に）
	chunkDelay time.Duration
	processor  *rtu.Processor
}

type deviceWrite struct {
	at    time.Time
	frame []byte
}

func newFakeDevice() *fakeDevice {
	store := NewModbusDataStore(65536, 65536, 65536, 65536)
	return &fakeDevice{processor: rtu.NewProcessor(NewRTUDataStoreAdapter(NewDataStoreHandler(store)))}
}

func (d *fakeDevice) open(rtu.SerialConfig) (rtu.Port, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.openErr != nil {
		return nil, d.openErr
	}
	p := &fakeDevicePort{dev: d, changed: make(chan struct{}), timeout: time.Second}
	d.ports = append(d.ports, p)
	return p, nil
}

func (d *fakeDevice) openCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.ports)
}

func (d *fakeDevice) lastPort(t *testing.T) *fakeDevicePort {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.ports) == 0 {
		t.Fatal("ポートが開かれていない")
	}
	return d.ports[len(d.ports)-1]
}

func (d *fakeDevice) writtenFrames() []deviceWrite {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]deviceWrite(nil), d.writes...)
}

// set は、応答の返し方を変える
func (d *fakeDevice) set(f func(d *fakeDevice)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	f(d)
}

// handle は、書かれたリクエストへの応答を、ポートに順に渡す
func (d *fakeDevice) handle(p *fakeDevicePort, req []byte) {
	d.mu.Lock()
	d.writes = append(d.writes, deviceWrite{at: time.Now(), frame: append([]byte(nil), req...)})
	respond, delay, chunk, chunkDelay := d.respond, d.delay, d.chunk, d.chunkDelay
	d.mu.Unlock()

	var resp []byte
	if respond != nil {
		resp = respond(req)
	} else if r, err := rtu.ParseRequest(req); err == nil {
		resp = d.processor.Process(r)
	}
	if len(resp) == 0 {
		return
	}
	go func() {
		time.Sleep(delay)
		if chunk <= 0 {
			p.deliver(resp)
			return
		}
		for i := 0; i < len(resp); i += chunk {
			p.deliver(resp[i:min(i+chunk, len(resp))])
			time.Sleep(chunkDelay)
		}
	}()
}

type fakeDevicePort struct {
	dev *fakeDevice

	mu      sync.Mutex
	rx      []byte
	changed chan struct{}
	timeout time.Duration
	closed  bool
	lost    bool // 抜いた（Read / Write がエラー）
}

func (p *fakeDevicePort) notifyLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *fakeDevicePort) deliver(b []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return // 閉じたポートに遅れて届いたバイトは、捨てられる
	}
	p.rx = append(p.rx, b...)
	p.notifyLocked()
}

func (p *fakeDevicePort) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	deadline := time.Now().Add(p.timeout)
	for {
		if p.closed || p.lost {
			return 0, errors.New("port closed")
		}
		if len(p.rx) > 0 {
			n := copy(b, p.rx)
			p.rx = p.rx[n:]
			return n, nil
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			return 0, nil
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-changed:
		case <-time.After(wait):
		}
		p.mu.Lock()
	}
}

func (p *fakeDevicePort) Write(b []byte) (int, error) {
	p.mu.Lock()
	if p.closed || p.lost {
		p.mu.Unlock()
		return 0, errors.New("port closed")
	}
	p.mu.Unlock()
	p.dev.handle(p, b)
	return len(b), nil
}

func (p *fakeDevicePort) SetReadTimeout(t time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timeout = t
	return nil
}

func (p *fakeDevicePort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		p.notifyLocked()
	}
	return nil
}

func (p *fakeDevicePort) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *fakeDevicePort) unplug() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lost = true
	p.notifyLocked()
}

// ===== ヘルパー =====

func hexb(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

func withCRC(t *testing.T, s string) []byte {
	t.Helper()
	return rtu.AppendCRC(hexb(t, s))
}

// newRTUTestClient は、フェイクの機器につながったクライアントを作る
func newRTUTestClient(t *testing.T, dev *fakeDevice, settings map[string]interface{}) *RTUClient {
	t.Helper()
	f := &RTUClientFactory{open: dev.open}
	base := map[string]interface{}{"serialPort": "COM_FAKE", "baudRate": 115200, "unitId": 1, "timeoutMs": 300}
	for k, v := range settings {
		base[k] = v
	}
	c, err := f.NewClient(base)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c.(*RTUClient)
}

func sendHex(t *testing.T, c *RTUClient, pdu string) protocol.CommandResult {
	t.Helper()
	return c.Send(context.Background(), hexb(t, pdu))
}

func expectStatus(t *testing.T, r protocol.CommandResult, want protocol.CommandStatus) {
	t.Helper()
	if r.Status != want {
		t.Fatalf("status = %s（%s）, want %s", r.Status, r.Error, want)
	}
}

// ===== 設定 =====

func TestRTUClientFactory_NormalizeSettings(t *testing.T) {
	f := &RTUClientFactory{}
	got, err := f.NormalizeSettings(map[string]interface{}{"serialPort": " COM4 ", "parity": "e"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"serialPort": "COM4", "baudRate": 115200, "dataBits": 8, "stopBits": 1, "parity": "E", "unitId": 1, "timeoutMs": 1000,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}

	// 項目の既定値と、ConnectionFields の既定値が合っている（画面で追加した直後の設定がそのまま通る）
	defaults := map[string]interface{}{}
	for _, fd := range f.ConnectionFields() {
		defaults[fd.Name] = fd.Default
	}
	if _, err := f.NormalizeSettings(defaults); err != nil {
		t.Errorf("既定値の設定が通らない: %v", err)
	}

	for _, bad := range []map[string]interface{}{
		{"serialPort": ""},
		{"baudRate": 0},
		{"dataBits": 9},
		{"stopBits": 3},
		{"parity": "X"},
		{"unitId": 256},
		{"timeoutMs": 5},
	} {
		if _, err := f.NormalizeSettings(bad); err == nil {
			t.Errorf("%v: エラーにならない", bad)
		}
	}
}

func TestRTUClientFactory_SharesCommandsWithTCP(t *testing.T) {
	rtuF, tcpF := &RTUClientFactory{}, &TCPClientFactory{}
	if len(rtuF.Commands()) != len(tcpF.Commands()) {
		t.Errorf("コマンドの数が TCP と違う")
	}
	params := map[string]interface{}{"address": 10, "count": 3}
	a, err1 := rtuF.BuildRequest("3", params)
	b, err2 := tcpF.BuildRequest("3", params)
	if err1 != nil || err2 != nil || !bytes.Equal(a, b) {
		t.Errorf("PDU が TCP と違う: % X / % X (%v %v)", a, b, err1, err2)
	}
	if len(rtuF.MappingAreas()) != 4 {
		t.Errorf("マッピングのエリア = %d", len(rtuF.MappingAreas()))
	}
}

func TestRTUResponseLength(t *testing.T) {
	cases := []struct {
		frame string
		want  int
	}{
		{"", 0},
		{"01", 0},
		{"01 83", 5},
		{"01 03", 0},
		{"01 03 04", 9},
		{"01 01 01", 6},
		{"01 06", 8},
		{"01 10", 8},
		{"01 11", -1},
	}
	for _, c := range cases {
		if got := rtuResponseLength(hexb(t, c.frame)); got != c.want {
			t.Errorf("rtuResponseLength(%s) = %d, want %d", c.frame, got, c.want)
		}
	}
}

// ===== 送受信 =====

func TestRTUClient_WriteThenRead(t *testing.T) {
	dev := newFakeDevice()
	c := newRTUTestClient(t, dev, nil)

	r := sendHex(t, c, "06 00 10 12 34")
	expectStatus(t, r, protocol.CommandOK)
	if want := withCRC(t, "01 06 00 10 12 34"); !bytes.Equal(r.Sent, want) {
		t.Errorf("送ったもの % X, want % X", r.Sent, want)
	}
	if !bytes.Equal(r.Received, r.Sent) {
		t.Errorf("FC06 の応答はリクエストと同じはず: % X", r.Received)
	}

	r = sendHex(t, c, "03 00 10 00 02")
	expectStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 2 || r.Values[0].Word == nil || *r.Values[0].Word != 0x1234 || *r.Values[1].Word != 0 {
		t.Errorf("読んだ値: %+v", r.Values)
	}
	if !c.Connected() {
		t.Error("送ったあと Connected が false")
	}
	if dev.openCount() != 1 {
		t.Errorf("ポートを開いた回数 = %d, want 1", dev.openCount())
	}
}

// 応答が何回かに分かれて届いても（USB シリアル変換器）、長さの分が届くまで待って 1 つの応答にする
func TestRTUClient_ResponseInChunks(t *testing.T) {
	dev := newFakeDevice()
	dev.set(func(d *fakeDevice) { d.chunk, d.chunkDelay = 3, 20*time.Millisecond })
	c := newRTUTestClient(t, dev, nil)

	sendHex(t, c, "10 00 00 00 03 06 00 01 00 02 00 03")
	r := sendHex(t, c, "03 00 00 00 03")
	expectStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 3 || *r.Values[2].Word != 3 {
		t.Errorf("読んだ値: %+v", r.Values)
	}
}

// 例外応答は exception。ポートは閉じない
func TestRTUClient_Exception(t *testing.T) {
	dev := newFakeDevice()
	c := newRTUTestClient(t, dev, nil)

	r := sendHex(t, c, "08 00 00 12 34") // 対応していないファンクション
	expectStatus(t, r, protocol.CommandException)
	if !strings.Contains(r.Error, "0x01") {
		t.Errorf("Error = %q", r.Error)
	}
	expectStatus(t, sendHex(t, c, "03 00 00 00 01"), protocol.CommandOK)
	if dev.openCount() != 1 {
		t.Errorf("例外応答でポートを開き直した（%d 回開いた）", dev.openCount())
	}
}

// 応答がなければタイムアウト。ポートを閉じ、次の送信で開き直す。
// タイムアウトしたリクエストへの遅れた応答は、次のリクエストの応答と取り違えない
func TestRTUClient_TimeoutThenLateResponse(t *testing.T) {
	dev := newFakeDevice()
	c := newRTUTestClient(t, dev, map[string]interface{}{"timeoutMs": 100})

	sendHex(t, c, "06 00 00 00 07")
	dev.set(func(d *fakeDevice) { d.delay = 200 * time.Millisecond })
	r := sendHex(t, c, "03 00 00 00 01")
	expectStatus(t, r, protocol.CommandTimeout)
	if c.Connected() {
		t.Error("タイムアウトしたのに、ポートが開いたまま")
	}
	first := dev.lastPort(t)
	if !first.isClosed() {
		t.Error("タイムアウトしたのに、ポートを閉じていない")
	}

	dev.set(func(d *fakeDevice) { d.delay = 0 })
	time.Sleep(150 * time.Millisecond) // 遅れた応答が届くころ（閉じたポートに届くので捨てられる）
	r = sendHex(t, c, "03 00 01 00 02")
	expectStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 2 || r.Values[0].Label != "アドレス 1" {
		t.Errorf("遅れた応答と取り違えた: %+v", r.Values)
	}
	if dev.openCount() != 2 {
		t.Errorf("ポートを開いた回数 = %d, want 2", dev.openCount())
	}
}

func TestRTUClient_BrokenResponses(t *testing.T) {
	cases := []struct {
		name    string
		respond func(req []byte) []byte
		want    string
	}{
		{"CRC が合わない", func([]byte) []byte {
			b := rtu.AppendCRC([]byte{0x01, 0x03, 0x02, 0x00, 0x05})
			b[len(b)-1] ^= 0xFF
			return b
		}, "CRC"},
		{"UnitID が違う", func([]byte) []byte { return rtu.AppendCRC([]byte{0x02, 0x03, 0x02, 0x00, 0x05}) }, "UnitID"},
		{"データが足りない", func([]byte) []byte { return rtu.AppendCRC([]byte{0x01, 0x03, 0x00}) }, "ワード"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dev := newFakeDevice()
			dev.set(func(d *fakeDevice) { d.respond = tc.respond })
			c := newRTUTestClient(t, dev, nil)
			r := sendHex(t, c, "03 00 00 00 01")
			expectStatus(t, r, protocol.CommandError)
			if !strings.Contains(r.Error, tc.want) {
				t.Errorf("Error = %q, want %q を含む", r.Error, tc.want)
			}
			if c.Connected() {
				t.Error("壊れた応答のあと、ポートが開いたまま")
			}
		})
	}
}

// 長さの決まらないファンクションコードの応答は、バイトが届かなくなった時点で終わりにする
func TestRTUClient_UnknownFunctionCode(t *testing.T) {
	dev := newFakeDevice()
	dev.set(func(d *fakeDevice) {
		d.respond = func([]byte) []byte { return rtu.AppendCRC([]byte{0x01, 0x11, 0x03, 0xAA, 0xBB, 0xCC}) }
		d.chunk, d.chunkDelay = 4, 10*time.Millisecond
	})
	c := newRTUTestClient(t, dev, nil)

	r := sendHex(t, c, "11")
	expectStatus(t, r, protocol.CommandOK)
	if len(r.Values) != 1 || r.Values[0].Value != "03 AA BB CC" {
		t.Errorf("Values = %+v", r.Values)
	}
}

// UnitID 0（ブロードキャスト）は、応答を待たない
func TestRTUClient_Broadcast(t *testing.T) {
	dev := newFakeDevice()
	dev.set(func(d *fakeDevice) { d.respond = func([]byte) []byte { return nil } })
	c := newRTUTestClient(t, dev, map[string]interface{}{"unitId": 0, "timeoutMs": 1000})

	start := time.Now()
	r := sendHex(t, c, "06 00 00 00 01")
	expectStatus(t, r, protocol.CommandOK)
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("ブロードキャストで応答を待った（%v）", time.Since(start))
	}
	if w := dev.writtenFrames(); len(w) != 1 || !bytes.Equal(w[0].frame, withCRC(t, "00 06 00 00 00 01")) {
		t.Errorf("機器に届いたもの: %+v", w)
	}
}

// 続けて送るとき、フレームの間を 3.5 文字時間空ける
func TestRTUClient_SilenceBetweenFrames(t *testing.T) {
	dev := newFakeDevice()
	// 1200bps の 3.5 文字時間は約 29ms
	c := newRTUTestClient(t, dev, map[string]interface{}{"baudRate": 1200})
	for range 3 {
		expectStatus(t, sendHex(t, c, "03 00 00 00 01"), protocol.CommandOK)
	}
	w := dev.writtenFrames()
	for i := 1; i < len(w); i++ {
		if gap := w[i].at.Sub(w[i-1].at); gap < 29*time.Millisecond {
			t.Errorf("%d 回目と %d 回目の間が %v（3.5 文字時間より短い）", i, i+1, gap)
		}
	}
}

func TestRTUClient_OpenError(t *testing.T) {
	dev := newFakeDevice()
	dev.set(func(d *fakeDevice) { d.openErr = errors.New("ポートがない") })
	c := newRTUTestClient(t, dev, nil)

	r := sendHex(t, c, "03 00 00 00 01")
	expectStatus(t, r, protocol.CommandError)
	if !strings.Contains(r.Error, "COM_FAKE を開けませんでした") {
		t.Errorf("Error = %q", r.Error)
	}
	if c.Connected() {
		t.Error("開けなかったのに Connected が true")
	}

	// 開けるようになれば、次の送信で開く
	dev.set(func(d *fakeDevice) { d.openErr = nil })
	expectStatus(t, sendHex(t, c, "03 00 00 00 01"), protocol.CommandOK)
}

// 送っている間にポートが使えなくなったら（変換器を抜いた）エラー。次の送信で開き直す
func TestRTUClient_PortLost(t *testing.T) {
	dev := newFakeDevice()
	dev.set(func(d *fakeDevice) { d.delay = 100 * time.Millisecond })
	c := newRTUTestClient(t, dev, nil)

	done := make(chan protocol.CommandResult, 1)
	go func() { done <- c.Send(context.Background(), hexb(t, "03 00 00 00 01")) }()
	waitPort := time.Now().Add(time.Second)
	for dev.openCount() == 0 && time.Now().Before(waitPort) {
		time.Sleep(time.Millisecond)
	}
	dev.lastPort(t).unplug()
	r := <-done
	expectStatus(t, r, protocol.CommandError)
	if !strings.Contains(r.Error, "使えなくなりました") {
		t.Errorf("Error = %q", r.Error)
	}

	dev.set(func(d *fakeDevice) { d.delay = 0 })
	expectStatus(t, sendHex(t, c, "03 00 00 00 01"), protocol.CommandOK)
	if dev.openCount() != 2 {
		t.Errorf("ポートを開いた回数 = %d, want 2", dev.openCount())
	}
}

// 応答を待っている間に取り消されたら、すぐ戻る
func TestRTUClient_Cancel(t *testing.T) {
	dev := newFakeDevice()
	dev.set(func(d *fakeDevice) { d.respond = func([]byte) []byte { return nil } })
	c := newRTUTestClient(t, dev, map[string]interface{}{"timeoutMs": 10000})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	r := c.Send(ctx, hexb(t, "03 00 00 00 01"))
	if time.Since(start) > time.Second {
		t.Errorf("取り消してから戻るまで %v", time.Since(start))
	}
	if r.Status == protocol.CommandOK {
		t.Errorf("取り消したのに ok")
	}
}

func TestRTUClient_RejectsBadPDU(t *testing.T) {
	c := newRTUTestClient(t, newFakeDevice(), nil)
	expectStatus(t, c.Send(context.Background(), nil), protocol.CommandError)
	expectStatus(t, c.Send(context.Background(), make([]byte, 254)), protocol.CommandError)
}
