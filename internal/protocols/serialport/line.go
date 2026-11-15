package serialport

import (
	"errors"
	"sync"
	"time"
)

// Line は、メモリ上の仮想のヌルモデムケーブル（クロスでつないだ 2 つのポート）。テストで、サーバーとクライアントを
// COM ポートなしでつなぐ。片側（0 か 1）を Opener で開くと、そのポートに書いたバイトが、もう片側で開いたポートに届く。
//
// ポートは実際のポートと同じように動く: 届いているバイトがあればすぐ返し、タイムアウトしたら (0, nil)、
// 閉じたら（Unplug したら）error。同じ側を閉じずに開き直すと、実際のポートと同じく「使用中」のエラー。
type Line struct {
	mu    sync.Mutex
	ends  [2]*lineEnd
	opens [2]int
	fail  [2]error // nil 以外なら、その側は開けない（Unplug している）
	// closedDuringRead は、読み取りを待っている最中に閉じられたことがあるか
	// （実際のポートでは、しばらく使用中のまま残って開き直せない。Loop の Stop がそうしないことを確かめる）
	closedDuringRead [2]bool
}

// ClosedDuringRead は、片側のポートが、読み取りを待っている最中に閉じられたことがあるかを返す
func (l *Line) ClosedDuringRead(side int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closedDuringRead[side]
}

// NewLine は、仮想のヌルモデムケーブルを作る
func NewLine() *Line {
	return &Line{}
}

var (
	errLineBusy   = errors.New("serial port busy")
	errLineClosed = errors.New("port closed")
)

// Opener は、片側（0 か 1）のポートを開く Opener を返す
func (l *Line) Opener(side int) Opener {
	return func(Config) (Port, error) {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.fail[side] != nil {
			return nil, l.fail[side]
		}
		if e := l.ends[side]; e != nil && !e.isClosed() {
			return nil, errLineBusy
		}
		e := &lineEnd{line: l, side: side, changed: make(chan struct{}), timeout: time.Second}
		l.ends[side] = e
		l.opens[side]++
		return e, nil
	}
}

// Opens は、片側を開いた回数
func (l *Line) Opens(side int) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.opens[side]
}

// Unplug は、片側の USB シリアル変換器を抜いたことにする（開いているポートは読み書きがエラーになり、Replug まで開けない）
func (l *Line) Unplug(side int) {
	l.mu.Lock()
	e := l.ends[side]
	l.fail[side] = errors.New("port not found")
	l.mu.Unlock()
	if e != nil {
		e.mu.Lock()
		e.lost = true
		e.notifyLocked()
		e.mu.Unlock()
	}
}

// Replug は、片側を挿し直したことにする（次から開ける）
func (l *Line) Replug(side int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fail[side] = nil
}

// peer は、もう片側でいま開いているポート（なければ nil）
func (l *Line) peer(side int) *lineEnd {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ends[1-side]
}

type lineEnd struct {
	line *Line
	side int

	mu      sync.Mutex
	rx      []byte
	changed chan struct{}
	timeout time.Duration
	closed  bool
	lost    bool
	reading int // いま待っている Read の数
}

func (e *lineEnd) notifyLocked() {
	close(e.changed)
	e.changed = make(chan struct{})
}

func (e *lineEnd) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

func (e *lineEnd) Read(b []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reading++
	defer func() { e.reading-- }()
	deadline := time.Now().Add(e.timeout)
	for {
		if e.closed || e.lost {
			return 0, errLineClosed
		}
		if len(e.rx) > 0 {
			n := copy(b, e.rx)
			e.rx = e.rx[n:]
			return n, nil
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			return 0, nil
		}
		changed := e.changed
		e.mu.Unlock()
		select {
		case <-changed:
		case <-time.After(wait):
		}
		e.mu.Lock()
	}
}

func (e *lineEnd) Write(b []byte) (int, error) {
	e.mu.Lock()
	bad := e.closed || e.lost
	e.mu.Unlock()
	if bad {
		return 0, errLineClosed
	}
	// 相手が開いていなければ、バイトは届かずに消える（実際のケーブルと同じ）
	if p := e.line.peer(e.side); p != nil {
		p.mu.Lock()
		if !p.closed && !p.lost {
			p.rx = append(p.rx, b...)
			p.notifyLocked()
		}
		p.mu.Unlock()
	}
	return len(b), nil
}

func (e *lineEnd) SetReadTimeout(t time.Duration) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.timeout = t
	return nil
}

func (e *lineEnd) Close() error {
	e.mu.Lock()
	duringRead := false
	if !e.closed {
		e.closed = true
		duringRead = e.reading > 0 && !e.lost
		e.notifyLocked()
	}
	e.mu.Unlock()
	if duringRead {
		e.line.mu.Lock()
		e.line.closedDuringRead[e.side] = true
		e.line.mu.Unlock()
	}
	return nil
}
