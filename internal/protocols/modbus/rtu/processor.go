package rtu

import (
	"encoding/binary"
)

// RequestHandler はリクエストを処理するためのインターフェース
type RequestHandler interface {
	// HandleReadCoils はコイル読み取りを処理する (FC 01)
	HandleReadCoils(unitID byte, address, quantity uint16) ([]bool, error)
	// HandleReadDiscreteInputs はディスクリート入力読み取りを処理する (FC 02)
	HandleReadDiscreteInputs(unitID byte, address, quantity uint16) ([]bool, error)
	// HandleReadHoldingRegisters は保持レジスタ読み取りを処理する (FC 03)
	HandleReadHoldingRegisters(unitID byte, address, quantity uint16) ([]uint16, error)
	// HandleReadInputRegisters は入力レジスタ読み取りを処理する (FC 04)
	HandleReadInputRegisters(unitID byte, address, quantity uint16) ([]uint16, error)
	// HandleWriteSingleCoil は単一コイル書き込みを処理する (FC 05)
	HandleWriteSingleCoil(unitID byte, address uint16, value bool) error
	// HandleWriteSingleRegister は単一レジスタ書き込みを処理する (FC 06)
	HandleWriteSingleRegister(unitID byte, address, value uint16) error
	// HandleWriteMultipleCoils は複数コイル書き込みを処理する (FC 15)
	HandleWriteMultipleCoils(unitID byte, address uint16, values []bool) error
	// HandleWriteMultipleRegisters は複数レジスタ書き込みを処理する (FC 16)
	HandleWriteMultipleRegisters(unitID byte, address uint16, values []uint16) error
	// IsUnitIDEnabled は指定したUnitIDが応答するかどうかを返す
	IsUnitIDEnabled(unitID byte) bool
}

// Processor はModbus RTUリクエストを処理する
type Processor struct {
	handler RequestHandler
}

// NewProcessor は新しいProcessorを作成する
func NewProcessor(handler RequestHandler) *Processor {
	return &Processor{handler: handler}
}

// ProcessASCII はリクエストを処理して、ASCII フレームのレスポンスを返す。
// 処理は RTU と同じで、フレームの形式（CRC の代わりに LRC、16 進の文字列）だけが違う
func (p *Processor) ProcessASCII(req *Request) []byte {
	res := p.Process(req)
	if res == nil {
		return nil
	}
	return BuildASCIIFrame(res[:len(res)-2]) // CRC を除いた中身
}

// Process はリクエストを処理してレスポンス（RTU フレーム）を返す。
// 応答しない場合（UnitID がオフ）は nil を返す
func (p *Processor) Process(req *Request) []byte {
	// UnitIDが無効な場合は応答しない
	if !p.handler.IsUnitIDEnabled(req.UnitID) {
		return nil
	}

	switch req.FunctionCode {
	case FuncReadCoils:
		return p.processReadCoils(req)
	case FuncReadDiscreteInputs:
		return p.processReadDiscreteInputs(req)
	case FuncReadHoldingRegisters:
		return p.processReadHoldingRegisters(req)
	case FuncReadInputRegisters:
		return p.processReadInputRegisters(req)
	case FuncWriteSingleCoil:
		return p.processWriteSingleCoil(req)
	case FuncWriteSingleRegister:
		return p.processWriteSingleRegister(req)
	case FuncWriteMultipleCoils:
		return p.processWriteMultipleCoils(req)
	case FuncWriteMultipleRegisters:
		return p.processWriteMultipleRegisters(req)
	default:
		return BuildExceptionResponse(req.UnitID, req.FunctionCode, ExceptionIllegalFunction)
	}
}

func (p *Processor) processReadCoils(req *Request) []byte {
	values, err := p.handler.HandleReadCoils(req.UnitID, req.Address, req.Quantity)
	if err != nil {
		return p.buildExceptionFromError(req.UnitID, req.FunctionCode, err)
	}
	return BuildReadBitsResponse(req.UnitID, req.FunctionCode, values)
}

func (p *Processor) processReadDiscreteInputs(req *Request) []byte {
	values, err := p.handler.HandleReadDiscreteInputs(req.UnitID, req.Address, req.Quantity)
	if err != nil {
		return p.buildExceptionFromError(req.UnitID, req.FunctionCode, err)
	}
	return BuildReadBitsResponse(req.UnitID, req.FunctionCode, values)
}

func (p *Processor) processReadHoldingRegisters(req *Request) []byte {
	values, err := p.handler.HandleReadHoldingRegisters(req.UnitID, req.Address, req.Quantity)
	if err != nil {
		return p.buildExceptionFromError(req.UnitID, req.FunctionCode, err)
	}
	return BuildReadRegistersResponse(req.UnitID, req.FunctionCode, values)
}

func (p *Processor) processReadInputRegisters(req *Request) []byte {
	values, err := p.handler.HandleReadInputRegisters(req.UnitID, req.Address, req.Quantity)
	if err != nil {
		return p.buildExceptionFromError(req.UnitID, req.FunctionCode, err)
	}
	return BuildReadRegistersResponse(req.UnitID, req.FunctionCode, values)
}

func (p *Processor) processWriteSingleCoil(req *Request) []byte {
	if len(req.Data) < 2 {
		return BuildExceptionResponse(req.UnitID, req.FunctionCode, ExceptionIllegalDataValue)
	}

	value := binary.BigEndian.Uint16(req.Data)
	var boolValue bool
	if value == 0xFF00 {
		boolValue = true
	} else if value == 0x0000 {
		boolValue = false
	} else {
		return BuildExceptionResponse(req.UnitID, req.FunctionCode, ExceptionIllegalDataValue)
	}

	if err := p.handler.HandleWriteSingleCoil(req.UnitID, req.Address, boolValue); err != nil {
		return p.buildExceptionFromError(req.UnitID, req.FunctionCode, err)
	}

	return BuildWriteSingleResponse(req.UnitID, req.FunctionCode, req.Address, value)
}

func (p *Processor) processWriteSingleRegister(req *Request) []byte {
	if len(req.Data) < 2 {
		return BuildExceptionResponse(req.UnitID, req.FunctionCode, ExceptionIllegalDataValue)
	}

	value := binary.BigEndian.Uint16(req.Data)
	if err := p.handler.HandleWriteSingleRegister(req.UnitID, req.Address, value); err != nil {
		return p.buildExceptionFromError(req.UnitID, req.FunctionCode, err)
	}

	return BuildWriteSingleResponse(req.UnitID, req.FunctionCode, req.Address, value)
}

func (p *Processor) processWriteMultipleCoils(req *Request) []byte {
	values := unpackBools(req.Data, int(req.Quantity))
	if err := p.handler.HandleWriteMultipleCoils(req.UnitID, req.Address, values); err != nil {
		return p.buildExceptionFromError(req.UnitID, req.FunctionCode, err)
	}
	return BuildWriteMultipleResponse(req.UnitID, req.FunctionCode, req.Address, req.Quantity)
}

func (p *Processor) processWriteMultipleRegisters(req *Request) []byte {
	values := unpackUint16s(req.Data)
	if err := p.handler.HandleWriteMultipleRegisters(req.UnitID, req.Address, values); err != nil {
		return p.buildExceptionFromError(req.UnitID, req.FunctionCode, err)
	}
	return BuildWriteMultipleResponse(req.UnitID, req.FunctionCode, req.Address, req.Quantity)
}

func (p *Processor) buildExceptionFromError(unitID, funcCode byte, err error) []byte {
	var exCode byte
	switch err {
	case ErrIllegalFunction:
		exCode = ExceptionIllegalFunction
	case ErrIllegalDataAddress:
		exCode = ExceptionIllegalDataAddress
	case ErrIllegalDataValue:
		exCode = ExceptionIllegalDataValue
	default:
		if me, ok := err.(*ModbusException); ok {
			exCode = me.Code
		} else {
			exCode = ExceptionSlaveDeviceFailure
		}
	}
	return BuildExceptionResponse(unitID, funcCode, exCode)
}

// unpackBools はバイト列をbool配列に展開する
func unpackBools(data []byte, count int) []bool {
	result := make([]bool, count)
	for i := 0; i < count; i++ {
		byteIdx := i / 8
		bitIdx := uint(i % 8)
		if byteIdx < len(data) {
			result[i] = (data[byteIdx] & (1 << bitIdx)) != 0
		}
	}
	return result
}

// unpackUint16s はバイト列をuint16配列に展開する
func unpackUint16s(data []byte) []uint16 {
	count := len(data) / 2
	result := make([]uint16, count)
	for i := 0; i < count; i++ {
		result[i] = binary.BigEndian.Uint16(data[i*2:])
	}
	return result
}
