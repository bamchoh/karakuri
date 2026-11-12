package rtu

import "errors"

// Modbus例外コード
const (
	ExceptionIllegalFunction         byte = 0x01
	ExceptionIllegalDataAddress      byte = 0x02
	ExceptionIllegalDataValue        byte = 0x03
	ExceptionSlaveDeviceFailure      byte = 0x04
	ExceptionAcknowledge             byte = 0x05
	ExceptionSlaveDeviceBusy         byte = 0x06
	ExceptionMemoryParityError       byte = 0x08
	ExceptionGatewayPathUnavailable  byte = 0x0A
	ExceptionGatewayTargetNoResponse byte = 0x0B
)

// エラー定義
var (
	ErrIllegalFunction    = errors.New("illegal function")
	ErrIllegalDataAddress = errors.New("illegal data address")
	ErrIllegalDataValue   = errors.New("illegal data value")
	ErrSlaveDeviceFailure = errors.New("slave device failure")
	ErrInvalidCRC         = errors.New("invalid CRC")
	ErrFrameTooShort      = errors.New("frame too short")
	ErrTimeout            = errors.New("timeout")
)

// ModbusException はModbus例外を表す
type ModbusException struct {
	Code byte
}

func (e *ModbusException) Error() string {
	switch e.Code {
	case ExceptionIllegalFunction:
		return "illegal function"
	case ExceptionIllegalDataAddress:
		return "illegal data address"
	case ExceptionIllegalDataValue:
		return "illegal data value"
	case ExceptionSlaveDeviceFailure:
		return "slave device failure"
	case ExceptionAcknowledge:
		return "acknowledge"
	case ExceptionSlaveDeviceBusy:
		return "slave device busy"
	case ExceptionMemoryParityError:
		return "memory parity error"
	case ExceptionGatewayPathUnavailable:
		return "gateway path unavailable"
	case ExceptionGatewayTargetNoResponse:
		return "gateway target device failed to respond"
	default:
		return "unknown exception"
	}
}

// NewModbusException は新しいModbus例外を作成する
func NewModbusException(code byte) *ModbusException {
	return &ModbusException{Code: code}
}
