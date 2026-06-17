package plugin

import (
	"modbus_simulator/cmd/modbus-plugin/internal/modbus"
	"modbus_simulator/internal/domain/protocol"
)

func ResolveFactory(protocolType string) protocol.ServerFactory {
	switch protocolType {
	case "modbus-rtu":
		return modbus.NewModbusRTUServerFactory()

	case "modbus-ascii":
		return modbus.NewModbusASCIIServerFactory()

	default:
		return modbus.NewModbusTCPServerFactory()
	}
}
