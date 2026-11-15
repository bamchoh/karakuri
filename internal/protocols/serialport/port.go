// Package serialport は、シリアルポートを使うプロトコル（Modbus RTU / ASCII、FINS の Host Link）で共通の部品。
//
//   - Port / Opener / Open: ポートを開く処理を差し替えられるようにする（テストではメモリ上のフェイクを渡す）
//   - Loop: サーバーの読み取りのループ（使えなくなったら開き直す。Stop は読み取りが終わってから閉じる）
//   - NewPipe: つながった 2 つのフェイクのポート（サーバーとクライアントを、COM ポートなしでつなぐテスト用）
//
// プロトコルの実装同士は import しないので、プロトコルをまたいで使うものはここに置く。
package serialport

import (
	"fmt"
	"time"

	"go.bug.st/serial"
)

// Config は、シリアルポートの設定
type Config struct {
	Port     string
	BaudRate int
	DataBits int
	StopBits int
	Parity   string // "N" / "E" / "O"（"none" / "even" / "odd" も可）
}

// Port は、プロトコルが使うシリアルポートの操作（go.bug.st/serial の serial.Port と同じ形）。
type Port interface {
	// Read は、届いているバイトを読む。何も届かないまま読み取りのタイムアウトが過ぎたら (0, nil) を返す。
	// ポートを閉じたら（使えなくなったら）error を返す
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	SetReadTimeout(t time.Duration) error
	Close() error
}

// Opener は、設定からポートを開く関数
type Opener func(config Config) (Port, error)

// Open は、実際のシリアルポートを開く（既定の Opener）
func Open(config Config) (Port, error) {
	var parity serial.Parity
	switch config.Parity {
	case "E", "even":
		parity = serial.EvenParity
	case "O", "odd":
		parity = serial.OddParity
	default: // "N", "none"
		parity = serial.NoParity
	}

	stopBits := serial.OneStopBit
	if config.StopBits == 2 {
		stopBits = serial.TwoStopBits
	}

	mode := &serial.Mode{
		BaudRate: config.BaudRate,
		DataBits: config.DataBits,
		Parity:   parity,
		StopBits: stopBits,
	}

	port, err := serial.Open(config.Port, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port: %w", err)
	}
	return port, nil
}
