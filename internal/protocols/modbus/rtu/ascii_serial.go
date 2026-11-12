package rtu

import (
	"fmt"
	"sync"
	"time"

	"go.bug.st/serial"
)

// ASCIISerialManager はASCIIモード用のシリアルポート管理を行う
type ASCIISerialManager struct {
	mu          sync.Mutex
	port        serial.Port
	config      SerialConfig
	readTimeout time.Duration
	closed      bool
}

// NewASCIISerialManager は新しいASCIISerialManagerを作成する
func NewASCIISerialManager(config SerialConfig) *ASCIISerialManager {
	return &ASCIISerialManager{
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

	// パリティの変換
	var parity serial.Parity
	switch sm.config.Parity {
	case "N", "none":
		parity = serial.NoParity
	case "E", "even":
		parity = serial.EvenParity
	case "O", "odd":
		parity = serial.OddParity
	default:
		parity = serial.NoParity
	}

	// ストップビットの変換
	var stopBits serial.StopBits
	switch sm.config.StopBits {
	case 1:
		stopBits = serial.OneStopBit
	case 2:
		stopBits = serial.TwoStopBits
	default:
		stopBits = serial.OneStopBit
	}

	mode := &serial.Mode{
		BaudRate: sm.config.BaudRate,
		DataBits: sm.config.DataBits,
		Parity:   parity,
		StopBits: stopBits,
	}

	port, err := serial.Open(sm.config.Port, mode)
	if err != nil {
		return fmt.Errorf("failed to open serial port: %w", err)
	}

	sm.port = port
	return nil
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
	sm.port.SetReadTimeout(sm.readTimeout)
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
			continue
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
