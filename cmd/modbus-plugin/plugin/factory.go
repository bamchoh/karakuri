package plugin

import (
	"karakuri/cmd/modbus-plugin/internal/modbus"
	"karakuri/internal/domain/protocol"
)

// ResolveFactory はプロトコルタイプに対応するファクトリーを返す（不明な値は Modbus TCP）
func ResolveFactory(protocolType string) protocol.ServerFactory {
	switch protocolType {
	case string(modbus.ProtocolModbusRTU):
		return &modbus.RTUFactory{}

	case string(modbus.ProtocolModbusASCII):
		return &modbus.ASCIIFactory{}

	default:
		return &modbus.TCPFactory{}
	}
}
