package rtu

import (
	"fmt"
	"sync"
	"time"
)

// ASCIISerialManager はASCIIモード用のシリアルポート管理を行う
type ASCIISerialManager struct {
	mu          sync.Mutex
	port        Port
	open        PortOpener
	config      SerialConfig
	readTimeout time.Duration
	closed      bool
}

// asciiReadSlice は、1 回の読み取りで待つ時間の上限
const asciiReadSlice = 100 * time.Millisecond

// NewASCIISerialManager は新しいASCIISerialManagerを作成する
func NewASCIISerialManager(config SerialConfig) *ASCIISerialManager {
	return newASCIISerialManager(config, OpenSerialPort)
}

// newASCIISerialManager は、ポートを open で開く ASCIISerialManager を作る（テストではフェイクのポートを開く）
func newASCIISerialManager(config SerialConfig, open PortOpener) *ASCIISerialManager {
	return &ASCIISerialManager{
		open:        open,
		config:      config,
		readTimeout: 1000 * time.Millisecond,
	}
}

// Open はシリアルポートを開く
func (sm *ASCIISerialManager) Open() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.port != nil {
		return nil
	}

	port, err := sm.open(sm.config)
	if err != nil {
		return err
	}

	sm.port = port
	// Close のあとにもう一度開いたとき（サーバーの Stop → Start）も読めるようにする
	sm.closed = false
	return nil
}

// Reopen は、読めなくなって閉じたポートを開き直す。Close したあとは開かない
func (sm *ASCIISerialManager) Reopen() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.closed {
		return fmt.Errorf("serial port closed")
	}
	if sm.port != nil {
		return nil
	}
	port, err := sm.open(sm.config)
	if err != nil {
		return err
	}
	sm.port = port
	return nil
}

// lose は、読めなくなったポートを閉じる（Reopen で開き直す）
func (sm *ASCIISerialManager) lose(port Port) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.port == port {
		sm.port.Close()
		sm.port = nil
	}
}

// PortName は、設定のポートの名前（COM4 など）
func (sm *ASCIISerialManager) PortName() string {
	return sm.config.Port
}

// Shutdown は読み取りをやめさせる（ReadFrame は、待っている読み取りが終わったところで戻る）。ポートは閉じない（Close で閉じる）
func (sm *ASCIISerialManager) Shutdown() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.closed = true
}

// Close はシリアルポートを閉じる
func (sm *ASCIISerialManager) Close() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.closed = true

	if sm.port == nil {
		return nil
	}

	err := sm.port.Close()
	sm.port = nil
	return err
}

// ReadFrame はASCIIフレームを読み取る（':'で開始、CR LFで終了）
func (sm *ASCIISerialManager) ReadFrame() ([]byte, error) {
	sm.mu.Lock()
	if sm.closed {
		sm.mu.Unlock()
		return nil, fmt.Errorf("serial port closed")
	}
	if sm.port == nil {
		sm.mu.Unlock()
		return nil, fmt.Errorf("serial port not open")
	}

	// 読み取りタイムアウトを設定
	// 1 回の読み取りは短くする（Stop が、待っている読み取りの終わりを待つため）。フレームのタイムアウトは readTimeout
	sm.port.SetReadTimeout(min(sm.readTimeout, asciiReadSlice))
	port := sm.port
	sm.mu.Unlock()

	buffer := make([]byte, 1)
	frame := make([]byte, 0, 513) // 最大フレーム長
	inFrame := false
	startTime := time.Now()

	for {
		// 閉じられたかチェック
		sm.mu.Lock()
		if sm.closed {
			sm.mu.Unlock()
			return nil, fmt.Errorf("serial port closed")
		}
		readTimeout := sm.readTimeout
		sm.mu.Unlock()

		// タイムアウトチェック
		if time.Since(startTime) > readTimeout {
			if len(frame) > 0 {
				return nil, ErrTimeout
			}
			return nil, nil
		}

		n, err := port.Read(buffer)
		if err != nil {
			// ポートが閉じられた場合
			sm.mu.Lock()
			if sm.closed {
				sm.mu.Unlock()
				return nil, fmt.Errorf("serial port closed")
			}
			sm.mu.Unlock()

			// ポートが使えなくなった（変換器を抜いたなど）。閉じて、開き直すのはサーバーに任せる
			sm.lose(port)
			return nil, fmt.Errorf("%w: %v", errPortLost, err)
		}

		if n == 0 {
			continue
		}

		b := buffer[0]

		if !inFrame {
			// フレーム開始文字を待つ
			if b == ASCIIFrameStart {
				inFrame = true
				frame = append(frame, b)
				startTime = time.Now() // タイムアウトをリセット
			}
			continue
		}

		// フレーム内
		frame = append(frame, b)

		// CR LFで終了チェック
		if len(frame) >= 2 && frame[len(frame)-2] == ASCIIFrameCR && frame[len(frame)-1] == ASCIIFrameLF {
			return frame, nil
		}

		// 最大フレーム長チェック
		if len(frame) >= 513 {
			return nil, fmt.Errorf("frame too long")
		}
	}
}

// Write はデータを書き込む
func (sm *ASCIISerialManager) Write(data []byte) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.port == nil {
		return fmt.Errorf("serial port not open")
	}

	_, err := sm.port.Write(data)
	return err
}

// SetReadTimeout は読み取りタイムアウトを設定する
func (sm *ASCIISerialManager) SetReadTimeout(timeout time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.readTimeout = timeout
}
