package rtu

import (
	"fmt"
	"sync"
	"time"

	"go.bug.st/serial"
)

// SerialConfig はシリアルポートの設定を表す
type SerialConfig struct {
	Port     string
	BaudRate int
	DataBits int
	StopBits int
	Parity   string
}

// SerialManager はシリアルポートの管理を行う
type SerialManager struct {
	mu           sync.Mutex
	port         serial.Port
	config       SerialConfig
	silenceTime  time.Duration // 3.5文字時間
	readTimeout  time.Duration
	closed       bool
}

// NewSerialManager は新しいSerialManagerを作成する
func NewSerialManager(config SerialConfig) *SerialManager {
	// 3.5文字時間を計算（1文字 = スタートビット + データビット + パリティビット + ストップビット）
	// 9600bps以下の場合は3.5文字時間を使用、それ以上は固定値1.75ms
	bitsPerChar := 1 + config.DataBits + config.StopBits
	if config.Parity != "N" && config.Parity != "none" {
		bitsPerChar++
	}

	var silenceTime time.Duration
	if config.BaudRate <= 19200 {
		// 3.5文字時間
		silenceTime = time.Duration(float64(bitsPerChar)*3.5/float64(config.BaudRate)*1e9) * time.Nanosecond
	} else {
		// 固定値 1750us
		silenceTime = 1750 * time.Microsecond
	}

	return &SerialManager{
		config:      config,
		silenceTime: silenceTime,
		readTimeout: 100 * time.Millisecond,
	}
}

// Open はシリアルポートを開く
func (sm *SerialManager) Open() error {
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
func (sm *SerialManager) Close() error {
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

// ReadFrame はフレームを読み取る（3.5文字時間の静寂で区切る）
func (sm *SerialManager) ReadFrame() ([]byte, error) {
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
	silenceTime := sm.silenceTime
	sm.mu.Unlock()

	buffer := make([]byte, 256)
	frame := make([]byte, 0, 256)
	lastReadTime := time.Now()

	for {
		// 閉じられたかチェック
		sm.mu.Lock()
		if sm.closed {
			sm.mu.Unlock()
			return nil, fmt.Errorf("serial port closed")
		}
		sm.mu.Unlock()

		n, err := port.Read(buffer)
		if err != nil {
			// ポートが閉じられた場合
			sm.mu.Lock()
			if sm.closed {
				sm.mu.Unlock()
				return nil, fmt.Errorf("serial port closed")
			}
			sm.mu.Unlock()

			// タイムアウトの場合
			if len(frame) > 0 {
				// 既にデータがある場合は、3.5文字時間経過でフレーム完了とみなす
				if time.Since(lastReadTime) >= silenceTime {
					return frame, nil
				}
			}
			continue
		}

		if n > 0 {
			frame = append(frame, buffer[:n]...)
			lastReadTime = time.Now()
		} else {
			// データなし
			if len(frame) > 0 && time.Since(lastReadTime) >= silenceTime {
				return frame, nil
			}
		}

		// 最大フレーム長チェック
		if len(frame) >= 256 {
			return frame, nil
		}
	}
}

// Write はデータを書き込む
func (sm *SerialManager) Write(data []byte) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.port == nil {
		return fmt.Errorf("serial port not open")
	}

	_, err := sm.port.Write(data)
	return err
}

// SetReadTimeout は読み取りタイムアウトを設定する
func (sm *SerialManager) SetReadTimeout(timeout time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.readTimeout = timeout
}

// SilenceTime は3.5文字時間を返す
func (sm *SerialManager) SilenceTime() time.Duration {
	return sm.silenceTime
}
