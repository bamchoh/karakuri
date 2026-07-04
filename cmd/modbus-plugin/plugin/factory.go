package plugin

import (
	"karakuri/cmd/modbus-plugin/internal/modbus"
	"karakuri/internal/domain/protocol"
)

func ResolveFactory(protocolType string) protocol.ServerFactory {
	switch protocolType {
	case "modbus-rtu":
		return &modbus.RTUFactory{}

	case "modbus-ascii":
		return &modbus.ASCIIFactory{}

	default:
		return &modbus.TCPFactory{}
	}
}
