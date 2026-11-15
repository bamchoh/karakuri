package rtu

import (
	"fmt"
	"sync"
	"time"
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
	mu          sync.Mutex
	port        Port
	open        PortOpener
	config      SerialConfig
	silenceTime time.Duration // 3.5文字時間
	readTimeout time.Duration
	closed      bool
	pending     []byte // 前のフレームのあとに続けて届いていたバイト（次の ReadFrame で使う）
}

// maxRTUFrameSize は RTU のフレーム（ADU）の最大の長さ
const maxRTUFrameSize = 256

// NewSerialManager は新しいSerialManagerを作成する
func NewSerialManager(config SerialConfig) *SerialManager {
	return newSerialManager(config, OpenSerialPort)
}

// newSerialManager は、ポートを open で開く SerialManager を作る（テストではフェイクのポートを開く）
func newSerialManager(config SerialConfig, open PortOpener) *SerialManager {
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
		open:        open,
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

	port, err := sm.open(sm.config)
	if err != nil {
		return err
	}

	sm.port = port
	// Close のあとにもう一度開いたとき（サーバーの Stop → Start）も読めるようにする
	sm.closed = false
	sm.pending = nil
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

// requestLength は、フレームの先頭からわかるリクエストの長さ（CRC を含む）を返す。
// ファンクションコードが長さの決まらないもの、まだ長さを決めるところまで届いていないときは 0。
func requestLength(frame []byte) int {
	if len(frame) < 2 {
		return 0
	}
	switch frame[1] {
	case FuncReadCoils, FuncReadDiscreteInputs, FuncReadHoldingRegisters, FuncReadInputRegisters,
		FuncWriteSingleCoil, FuncWriteSingleRegister:
		// UnitID・FC・アドレス 2・個数（値）2・CRC 2
		return 8
	case FuncWriteMultipleCoils, FuncWriteMultipleRegisters:
		// UnitID・FC・アドレス 2・個数 2・バイト数 1・データ・CRC 2
		if len(frame) < 7 {
			return 0
		}
		return 9 + int(frame[6])
	}
	return 0
}

// ReadFrame はフレームを 1 つ読み取る。
//
//   - リクエストの長さがファンクションコードからわかる（requestLength）ときは、その長さが届いて CRC が合った時点で返す。
//     そのあとに続けて届いていたバイトは、次のフレームに回す
//   - 長さがわからない・CRC が合わないときは、バイトが届かなくなるまで（読み取りのタイムアウトが過ぎ、
//     最後に届いてから 3.5 文字時間が過ぎるまで）を 1 つのフレームにする
//
// 無音の時間だけで厳密に区切らないのは、USB シリアル変換器が受信したバイトをまとめて渡すため
// （1 つのフレームの途中に、3.5 文字時間より長い間が空くことがある）。
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
	frame := append(make([]byte, 0, maxRTUFrameSize), sm.pending...)
	sm.pending = nil
	sm.mu.Unlock()

	buffer := make([]byte, maxRTUFrameSize)
	lastReadTime := time.Now()

	for {
		if n := requestLength(frame); n > 0 && n <= len(frame) && CheckCRC(frame[:n]) {
			if n < len(frame) {
				sm.mu.Lock()
				sm.pending = append([]byte(nil), frame[n:]...)
				sm.mu.Unlock()
			}
			return frame[:n], nil
		}
		if len(frame) >= maxRTUFrameSize {
			return frame, nil
		}

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
