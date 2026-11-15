package serialport

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s にならない", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ===== Line（仮想のヌルモデムケーブル） =====

func TestLine_BytesCrossToTheOtherSide(t *testing.T) {
	line := NewLine()
	a, err := line.Opener(0)(Config{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := line.Opener(1)(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	n, err := b.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("届いたもの %q, %v", buf[:n], err)
	}

	// 何も届かなければ、タイムアウトで (0, nil)
	_ = b.SetReadTimeout(20 * time.Millisecond)
	if n, err := b.Read(buf); n != 0 || err != nil {
		t.Errorf("タイムアウトの読み取り = %d, %v", n, err)
	}

	// 開いたまま同じ側を開くと使用中。閉じれば開ける
	if _, err := line.Opener(0)(Config{}); err == nil {
		t.Error("開いている側をもう一度開けた")
	}
	_ = a.Close()
	if _, err := a.Read(buf); err == nil {
		t.Error("閉じたポートが読めた")
	}
	if _, err := line.Opener(0)(Config{}); err != nil {
		t.Errorf("閉じたあとに開けない: %v", err)
	}
}

func TestLine_UnplugAndReplug(t *testing.T) {
	line := NewLine()
	a, _ := line.Opener(0)(Config{})
	line.Unplug(0)
	if _, err := a.Read(make([]byte, 1)); err == nil {
		t.Error("抜いたポートが読めた")
	}
	_ = a.Close()
	if _, err := line.Opener(0)(Config{}); err == nil {
		t.Error("抜いている間に開けた")
	}
	line.Replug(0)
	if _, err := line.Opener(0)(Config{}); err != nil {
		t.Errorf("挿し直したのに開けない: %v", err)
	}
}

// ===== Loop =====

// echoLoop は、届いたバイトをそのまま送り返すループ（片側 0）
func echoLoop(line *Line, resets *int, mu *sync.Mutex) *Loop {
	return &Loop{
		Config:         Config{Port: "COM_TEST"},
		Open:           line.Opener(0),
		Name:           "test",
		ReopenInterval: 20 * time.Millisecond,
		OnData:         func(data []byte, write func([]byte) error) { _ = write(data) },
		OnReset: func() {
			mu.Lock()
			*resets++
			mu.Unlock()
		},
	}
}

func readAll(t *testing.T, p Port, want string) {
	t.Helper()
	_ = p.SetReadTimeout(50 * time.Millisecond)
	var got []byte
	buf := make([]byte, 64)
	deadline := time.Now().Add(2 * time.Second)
	for len(got) < len(want) && time.Now().Before(deadline) {
		n, err := p.Read(buf)
		if err != nil {
			t.Fatalf("読み取り: %v", err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, []byte(want)) {
		t.Fatalf("届いたもの %q, want %q", got, want)
	}
}

func TestLoop_EchoStopAndRestart(t *testing.T) {
	line := NewLine()
	var mu sync.Mutex
	resets := 0
	loop := echoLoop(line, &resets, &mu)
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	peer, _ := line.Opener(1)(Config{})
	_, _ = peer.Write([]byte("abc"))
	readAll(t, peer, "abc")

	// 読み取りを待っている最中に止めても、読み取りが終わってから閉じる
	time.Sleep(30 * time.Millisecond)
	start := time.Now()
	loop.Stop()
	if time.Since(start) > time.Second {
		t.Errorf("Stop に %v かかった", time.Since(start))
	}
	if line.ClosedDuringRead(0) {
		t.Error("読み取りの途中でポートを閉じた")
	}

	// 止めてすぐ起動し直せる
	if err := loop.Start(); err != nil {
		t.Fatalf("2 回目の Start: %v", err)
	}
	defer loop.Stop()
	_, _ = peer.Write([]byte("xyz"))
	readAll(t, peer, "xyz")
	mu.Lock()
	defer mu.Unlock()
	if resets != 2 {
		t.Errorf("OnReset の回数 = %d, want 2", resets)
	}
}

func TestLoop_ReopensAfterUnplug(t *testing.T) {
	line := NewLine()
	var mu sync.Mutex
	resets := 0
	loop := echoLoop(line, &resets, &mu)
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	defer loop.Stop()
	peer, _ := line.Opener(1)(Config{})

	line.Unplug(0)
	time.Sleep(100 * time.Millisecond)
	if line.Opens(0) != 1 {
		t.Errorf("抜いている間に開けた（%d 回）", line.Opens(0))
	}
	line.Replug(0)
	waitFor(t, "開き直す", func() bool { return line.Opens(0) == 2 })

	_, _ = peer.Write([]byte("again"))
	readAll(t, peer, "again")
	mu.Lock()
	defer mu.Unlock()
	if resets != 2 {
		t.Errorf("OnReset の回数 = %d, want 2", resets)
	}
}

func TestLoop_StopWhileReopening(t *testing.T) {
	line := NewLine()
	var mu sync.Mutex
	resets := 0
	loop := echoLoop(line, &resets, &mu)
	loop.ReopenInterval = time.Hour
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	line.Unplug(0)
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() { loop.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("開き直すのを待っている間の Stop が戻らない")
	}
}

func TestLoop_StartFailsWhenPortCannotOpen(t *testing.T) {
	line := NewLine()
	line.Unplug(0)
	var mu sync.Mutex
	resets := 0
	loop := echoLoop(line, &resets, &mu)
	if err := loop.Start(); err == nil {
		loop.Stop()
		t.Fatal("開けないのに Start が成功した")
	}
	loop.Stop() // 動いていなければ何もしない
}
