package rtu

import (
	"fmt"
	"time"

	"go.bug.st/serial"
)

// Port は、サーバーが使うシリアルポートの操作（go.bug.st/serial の serial.Port と同じ形）。
// テストでは、メモリ上のフェイクを渡す（COM ポートがなくても、送受信を確かめられるようにするため）。
type Port interface {
	// Read は、届いているバイトを読む。何も届かないまま読み取りのタイムアウトが過ぎたら (0, nil) を返す。
	// ポートを閉じたら error を返す
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	SetReadTimeout(t time.Duration) error
	Close() error
}

// PortOpener は、設定からポートを開く関数。
type PortOpener func(config SerialConfig) (Port, error)

// OpenSerialPort は、実際のシリアルポートを開く（サーバーの既定の PortOpener）。
func OpenSerialPort(config SerialConfig) (Port, error) {
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
