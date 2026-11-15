package serialport

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Loop は、サーバーの読み取りのループ。ポートを開いて読み続け、届いたバイトを OnData に渡す。
//
//   - 読み取りがエラーになったら（USB シリアル変換器を抜いたなど）、ポートを閉じて ReopenInterval おきに開き直す。
//     閉じずに読み直すと、エラーがすぐ返り続けて CPU を使い切り、挿し直しても同じハンドルでは読めない
//   - Stop は、待っている読み取りが終わってから（最長 ReadTimeout）ポートを閉じる。読み取りの途中で閉じると、
//     Windows ではポートが 50〜200ms ほど使用中のまま残り、止めてすぐの Start が失敗する
//
// Start / Stop は何度呼んでもよい（Stop のあとにもう一度 Start できる）。
type Loop struct {
	Config Config
	Open   Opener // nil なら実際のシリアルポートを開く
	Name   string // ログの出どころ（"[名前] " を付けて出す）

	// OnData は、届いたバイトを渡す（読み取りのゴルーチンから 1 つずつ呼ぶ）。write でポートに書ける
	OnData func(data []byte, write func([]byte) error)
	// OnReset は、ポートを開いた・開き直したときに呼ぶ（読みかけのフレームを捨てるため）
	OnReset func()

	ReadTimeout    time.Duration // 1 回の読み取りで待つ時間（既定 100ms）
	ReopenInterval time.Duration // 使えなくなったポートを開き直す間隔（既定 1 秒）

	mu      sync.Mutex
	port    Port
	running bool
	closing bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func (l *Loop) opener() Opener {
	if l.Open != nil {
		return l.Open
	}
	return Open
}

// Start はポートを開き、読み取りを始める。開けなければ error を返す
func (l *Loop) Start() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running {
		return fmt.Errorf("already running")
	}
	port, err := l.opener()(l.Config)
	if err != nil {
		return err
	}
	if l.ReadTimeout <= 0 {
		l.ReadTimeout = 100 * time.Millisecond
	}
	if l.ReopenInterval <= 0 {
		l.ReopenInterval = time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.port, l.running, l.closing, l.cancel = port, true, false, cancel
	if l.OnReset != nil {
		l.OnReset()
	}
	l.wg.Add(1)
	go l.run(ctx, port)
	return nil
}

// Stop は読み取りをやめ、待っている読み取りが終わってからポートを閉じる
func (l *Loop) Stop() {
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return
	}
	l.running = false
	l.closing = true
	l.cancel()
	l.mu.Unlock()

	l.wg.Wait()

	l.mu.Lock()
	if l.port != nil {
		_ = l.port.Close()
		l.port = nil
	}
	l.mu.Unlock()
}

// write は、いま開いているポートに書く
func (l *Loop) write(b []byte) error {
	l.mu.Lock()
	port := l.port
	l.mu.Unlock()
	if port == nil {
		return fmt.Errorf("serial port not open")
	}
	_, err := port.Write(b)
	return err
}

func (l *Loop) stopping() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closing
}

func (l *Loop) run(ctx context.Context, port Port) {
	defer l.wg.Done()
	buf := make([]byte, 1024)
	for {
		if l.stopping() {
			return
		}
		_ = port.SetReadTimeout(l.ReadTimeout)
		n, err := port.Read(buf)
		if err != nil {
			if l.stopping() {
				return
			}
			port = l.reopen(ctx, err)
			if port == nil {
				return
			}
			continue
		}
		if n > 0 && l.OnData != nil {
			l.OnData(append([]byte(nil), buf[:n]...), l.write)
		}
	}
}

// reopen は、使えなくなったポートを閉じ、開けるか Stop されるまで開き直す。開けたポートを返す（Stop されたら nil）
func (l *Loop) reopen(ctx context.Context, cause error) Port {
	l.mu.Lock()
	if l.port != nil {
		_ = l.port.Close()
		l.port = nil
	}
	l.mu.Unlock()
	log.Printf("[%s] %s が使えなくなりました。%v おきに開き直します（error: %v）", l.Name, l.Config.Port, l.ReopenInterval, cause)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(l.ReopenInterval):
		}
		port, err := l.opener()(l.Config)
		if err != nil {
			continue
		}
		l.mu.Lock()
		if l.closing {
			l.mu.Unlock()
			_ = port.Close()
			return nil
		}
		l.port = port
		if l.OnReset != nil {
			l.OnReset()
		}
		l.mu.Unlock()
		log.Printf("[%s] %s を開き直しました", l.Name, l.Config.Port)
		return port
	}
}
