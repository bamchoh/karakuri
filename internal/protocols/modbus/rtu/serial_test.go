package rtu

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

// ===== フェイクのシリアルポート =====
//
// 実際のポート（go.bug.st/serial）と同じように動く:
//   - Read は、届いているバイトがあればすぐ返す。何も届かないまま読み取りのタイムアウトが過ぎたら (0, nil)
//   - Close のあとの Read / Write は error（Read の待ちも、すぐ戻る）

var errFakePortClosed = errors.New("fake port closed")

type fakePort struct {
	mu      sync.Mutex
	rx      []byte        // サーバーがまだ読んでいないバイト（相手から届いたもの）
	changed chan struct{} // rx か closed が変わったら close する
	timeout time.Duration
	closed  bool
	written chan []byte // サーバーが書いたバイト列（Write 1 回ごと）
}

func newFakePort() *fakePort {
	return &fakePort{
		changed: make(chan struct{}),
		timeout: time.Second,
		written: make(chan []byte, 64),
	}
}

func (p *fakePort) notifyLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *fakePort) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	deadline := time.Now().Add(p.timeout)
	for {
		if p.closed {
			return 0, errFakePortClosed
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

func (p *fakePort) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, errFakePortClosed
	}
	p.written <- append([]byte(nil), b...)
	return len(b), nil
}

func (p *fakePort) SetReadTimeout(t time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timeout = t
	return nil
}

func (p *fakePort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		p.notifyLocked()
	}
	return nil
}

func (p *fakePort) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// feed は、相手（マスター）からバイトが届いたことにする
func (p *fakePort) feed(b []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rx = append(p.rx, b...)
	p.notifyLocked()
}

// expectWrite は、サーバーが書いたバイト列を 1 つ受け取る
func (p *fakePort) expectWrite(t *testing.T) []byte {
	t.Helper()
	select {
	case b := <-p.written:
		return b
	case <-time.After(2 * time.Second):
		t.Fatal("サーバーが応答しない")
		return nil
	}
}

// expectNoWrite は、d の間サーバーが何も書かないことを確かめる
func (p *fakePort) expectNoWrite(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case b := <-p.written:
		t.Fatalf("応答しないはずなのに書いた: % X", b)
	case <-time.After(d):
	}
}

// fakeLine は、開くたびに新しい fakePort を作る PortOpener を持つ
type fakeLine struct {
	mu      sync.Mutex
	ports   []*fakePort
	configs []SerialConfig
	openErr error
}

func (l *fakeLine) open(config SerialConfig) (Port, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.configs = append(l.configs, config)
	if l.openErr != nil {
		return nil, l.openErr
	}
	p := newFakePort()
	l.ports = append(l.ports, p)
	return p, nil
}

// port は、最後に開いたポート
func (l *fakeLine) port(t *testing.T) *fakePort {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ports) == 0 {
		t.Fatal("ポートが開かれていない")
	}
	return l.ports[len(l.ports)-1]
}

func (l *fakeLine) openCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.ports)
}

func serialConfig(baud int) SerialConfig {
	return SerialConfig{Port: "COM_FAKE", BaudRate: baud, DataBits: 8, StopBits: 1, Parity: "N"}
}

// startRTU は、フェイクのポートでつながった RTU サーバーを起動する
func startRTU(t *testing.T, baud int, h RequestHandler) (*RTUServer, *fakeLine) {
	t.Helper()
	line := &fakeLine{}
	srv := newRTUServer(serialConfig(baud), h, line.open)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { srv.Stop() })
	return srv, line
}

// startASCII は、フェイクのポートでつながった ASCII サーバーを起動する
func startASCII(t *testing.T, h RequestHandler) (*ASCIIServer, *fakeLine) {
	t.Helper()
	line := &fakeLine{}
	srv := newASCIIServer(serialConfig(9600), h, line.open)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { srv.Stop() })
	return srv, line
}

// expectRTUResponse は、RTU の応答を受け取り、CRC を除いた中身が want と一致することを確かめる
func expectRTUResponse(t *testing.T, p *fakePort, want string) {
	t.Helper()
	got := p.expectWrite(t)
	if !CheckCRC(got) {
		t.Fatalf("応答の CRC が正しくない: % X", got)
	}
	if body := got[:len(got)-2]; !bytes.Equal(body, unhex(t, want)) {
		t.Errorf("応答 % X, want %s", body, want)
	}
}

// expectASCIIResponse は、ASCII の応答を受け取り、LRC を除いた中身が want と一致することを確かめる
func expectASCIIResponse(t *testing.T, p *fakePort, want string) {
	t.Helper()
	got := p.expectWrite(t)
	body, err := ParseASCIIFrame(got)
	if err != nil {
		t.Fatalf("応答を ASCII のフレームとして読めない（%q）: %v", got, err)
	}
	if !bytes.Equal(body, unhex(t, want)) {
		t.Errorf("応答 % X, want %s", body, want)
	}
}

// ===== RTU =====

func TestRTUServer_OpensPortWithConfig(t *testing.T) {
	_, line := startRTU(t, 19200, newMemHandler())
	if len(line.configs) != 1 || line.configs[0] != serialConfig(19200) {
		t.Errorf("ポートを開いた設定: %+v", line.configs)
	}
}

func TestRTUServer_WriteThenRead(t *testing.T) {
	_, line := startRTU(t, 9600, newMemHandler())
	p := line.port(t)

	p.feed(rtuFrame(t, "01 06 00 10 12 34"))
	expectRTUResponse(t, p, "01 06 00 10 12 34")

	p.feed(rtuFrame(t, "01 03 00 10 00 01"))
	expectRTUResponse(t, p, "01 03 02 12 34")
}

// 1 つのフレームが何回かに分かれて届いても（無音の時間より短い間隔なら）、1 つのフレームとして処理する
func TestRTUServer_FrameSplitAcrossReads(t *testing.T) {
	// 600bps の 3.5 文字時間は約 58ms
	_, line := startRTU(t, 600, newMemHandler())
	p := line.port(t)

	frame := rtuFrame(t, "01 06 00 20 AB CD")
	for _, b := range frame {
		p.feed([]byte{b})
		time.Sleep(5 * time.Millisecond)
	}
	expectRTUResponse(t, p, "01 06 00 20 AB CD")
}

// 無音の時間（3.5 文字時間）を空けて届いたフレームは、別のフレームとして処理する
func TestRTUServer_SilenceSeparatesFrames(t *testing.T) {
	// 9600bps の 3.5 文字時間は約 3.6ms。間隔は、それより十分長く、読み取りのタイムアウト（100ms）より短くする
	_, line := startRTU(t, 9600, newMemHandler())
	p := line.port(t)

	p.feed(rtuFrame(t, "01 06 00 01 00 0A"))
	time.Sleep(30 * time.Millisecond)
	p.feed(rtuFrame(t, "01 06 00 02 00 0B"))

	expectRTUResponse(t, p, "01 06 00 01 00 0A")
	expectRTUResponse(t, p, "01 06 00 02 00 0B")
}

// 2 つのフレームが 1 回の読み取りで届いても、それぞれに応答する（後ろのフレームは次に回す）
func TestRTUServer_TwoFramesInOneRead(t *testing.T) {
	_, line := startRTU(t, 9600, newMemHandler())
	p := line.port(t)

	both := append(rtuFrame(t, "01 10 00 05 00 02 04 00 01 00 02"), rtuFrame(t, "01 03 00 05 00 02")...)
	p.feed(both)
	expectRTUResponse(t, p, "01 10 00 05 00 02")
	expectRTUResponse(t, p, "01 03 04 00 01 00 02")
}

// 長さの決まらないファンクションコードのフレームも、届かなくなった時点で 1 つのフレームとして処理する
func TestRTUServer_UnknownFunctionCodeFallsBackToSilence(t *testing.T) {
	_, line := startRTU(t, 9600, newMemHandler())
	p := line.port(t)

	// FC08（診断）は対応していないので、例外応答（不正なファンクション）が返る
	p.feed(rtuFrame(t, "01 08 00 00 12 34"))
	expectRTUResponse(t, p, "01 88 01")
}

func TestRequestLength(t *testing.T) {
	cases := []struct {
		frame string
		want  int
	}{
		{"", 0},
		{"01", 0},
		{"01 03", 8},
		{"01 06 00 01", 8},
		{"01 10 00 00 00 02", 0}, // バイト数がまだ届いていない
		{"01 10 00 00 00 02 04", 13},
		{"01 0F 00 00 00 0A 02", 11},
		{"01 08", 0},
	}
	for _, c := range cases {
		if got := requestLength(unhex(t, c.frame)); got != c.want {
			t.Errorf("requestLength(%s) = %d, want %d", c.frame, got, c.want)
		}
	}
}

// CRC が合わないフレームには応答せず、次の正しいフレームには応答する
func TestRTUServer_IgnoresBrokenFrame(t *testing.T) {
	_, line := startRTU(t, 9600, newMemHandler())
	p := line.port(t)

	broken := rtuFrame(t, "01 03 00 00 00 01")
	broken[len(broken)-1] ^= 0xFF
	p.feed(broken)
	p.expectNoWrite(t, 200*time.Millisecond)

	p.feed(rtuFrame(t, "01 03 00 00 00 01"))
	expectRTUResponse(t, p, "01 03 02 00 00")
}

// 応答しない UnitID 宛てには何も返さない
func TestRTUServer_DisabledUnitID(t *testing.T) {
	h := newMemHandler()
	h.disabled[0x02] = true
	_, line := startRTU(t, 9600, h)
	p := line.port(t)

	p.feed(rtuFrame(t, "02 03 00 00 00 01"))
	p.expectNoWrite(t, 200*time.Millisecond)

	p.feed(rtuFrame(t, "01 03 00 00 00 01"))
	expectRTUResponse(t, p, "01 03 02 00 00")
}

// Stop はポートを閉じて読み取りの待ちを終わらせる。もう一度 Start すると、ポートを開き直して応答する
func TestRTUServer_StopAndRestart(t *testing.T) {
	srv, line := startRTU(t, 9600, newMemHandler())
	first := line.port(t)

	stopped := make(chan struct{})
	go func() {
		srv.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop が戻らない")
	}
	if !first.isClosed() {
		t.Error("Stop のあと、ポートが閉じていない")
	}
	if srv.IsRunning() {
		t.Error("Stop のあとも IsRunning が true")
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("2 回目の Start: %v", err)
	}
	if line.openCount() != 2 {
		t.Fatalf("ポートを開いた回数 = %d, want 2", line.openCount())
	}
	second := line.port(t)
	second.feed(rtuFrame(t, "01 03 00 00 00 01"))
	expectRTUResponse(t, second, "01 03 02 00 00")
}

func TestRTUServer_OpenError(t *testing.T) {
	line := &fakeLine{openErr: errors.New("ポートがない")}
	srv := newRTUServer(serialConfig(9600), newMemHandler(), line.open)
	if err := srv.Start(); err == nil {
		srv.Stop()
		t.Fatal("ポートを開けないのに Start が成功した")
	}
	if srv.IsRunning() {
		t.Error("Start に失敗したのに IsRunning が true")
	}
}

// ===== ASCII =====

func TestASCIIServer_WriteThenRead(t *testing.T) {
	_, line := startASCII(t, newMemHandler())
	p := line.port(t)

	p.feed(BuildASCIIFrame(unhex(t, "01 06 00 10 12 34")))
	expectASCIIResponse(t, p, "01 06 00 10 12 34")

	p.feed(BuildASCIIFrame(unhex(t, "01 03 00 10 00 01")))
	expectASCIIResponse(t, p, "01 03 02 12 34")
}

// 1 文字ずつ届いても、フレームの前にごみがあっても、':' から CR LF までを 1 つのフレームとして処理する
func TestASCIIServer_ByteByByteWithNoise(t *testing.T) {
	_, line := startASCII(t, newMemHandler())
	p := line.port(t)

	data := append([]byte("xx\r\n"), BuildASCIIFrame(unhex(t, "01 06 00 20 AB CD"))...)
	for _, b := range data {
		p.feed([]byte{b})
		time.Sleep(time.Millisecond)
	}
	expectASCIIResponse(t, p, "01 06 00 20 AB CD")
}

// 続けて届いた 2 つのフレームに、それぞれ応答する
func TestASCIIServer_BackToBackFrames(t *testing.T) {
	_, line := startASCII(t, newMemHandler())
	p := line.port(t)

	p.feed(append(BuildASCIIFrame(unhex(t, "01 06 00 01 00 0A")), BuildASCIIFrame(unhex(t, "01 06 00 02 00 0B"))...))
	expectASCIIResponse(t, p, "01 06 00 01 00 0A")
	expectASCIIResponse(t, p, "01 06 00 02 00 0B")
}

// LRC が合わないフレームには応答せず、次の正しいフレームには応答する
func TestASCIIServer_IgnoresBrokenFrame(t *testing.T) {
	_, line := startASCII(t, newMemHandler())
	p := line.port(t)

	broken := BuildASCIIFrame(unhex(t, "01 03 00 00 00 01"))
	// LRC（CR LF の前の 2 文字）を変える
	broken[len(broken)-3] = '0'
	broken[len(broken)-4] = '0'
	p.feed(broken)
	p.expectNoWrite(t, 200*time.Millisecond)

	p.feed(BuildASCIIFrame(unhex(t, "01 03 00 00 00 01")))
	expectASCIIResponse(t, p, "01 03 02 00 00")
}

func TestASCIIServer_StopAndRestart(t *testing.T) {
	srv, line := startASCII(t, newMemHandler())
	first := line.port(t)

	stopped := make(chan struct{})
	go func() {
		srv.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop が戻らない")
	}
	if !first.isClosed() {
		t.Error("Stop のあと、ポートが閉じていない")
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("2 回目の Start: %v", err)
	}
	second := line.port(t)
	second.feed(BuildASCIIFrame(unhex(t, "01 03 00 00 00 01")))
	expectASCIIResponse(t, second, "01 03 02 00 00")
}
