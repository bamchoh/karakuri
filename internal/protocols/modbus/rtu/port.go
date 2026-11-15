package rtu

import "karakuri/internal/protocols/serialport"

// シリアルポートの部品は serialport（プロトコルで共通）にある。ここでは、これまでの名前で使えるよう別名を付ける。

// SerialConfig はシリアルポートの設定
type SerialConfig = serialport.Config

// Port は、サーバーが使うシリアルポートの操作。テストでは、メモリ上のフェイクを渡す
type Port = serialport.Port

// PortOpener は、設定からポートを開く関数
type PortOpener = serialport.Opener

// OpenSerialPort は、実際のシリアルポートを開く（サーバーの既定の PortOpener）
func OpenSerialPort(config SerialConfig) (Port, error) {
	return serialport.Open(config)
}
