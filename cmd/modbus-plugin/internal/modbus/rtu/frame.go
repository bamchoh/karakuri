package rtu

import (
	"encoding/binary"
)

// 機能コード定義
const (
	FuncReadCoils              byte = 0x01
	FuncReadDiscreteInputs     byte = 0x02
	FuncReadHoldingRegisters   byte = 0x03
	FuncReadInputRegisters     byte = 0x04
	FuncWriteSingleCoil        byte = 0x05
	FuncWriteSingleRegister    byte = 0x06
	FuncWriteMultipleCoils     byte = 0x0F
	FuncWriteMultipleRegisters byte = 0x10
)

// Request はModbus RTUリクエストを表す
type Request struct {
	UnitID       byte
	FunctionCode byte
	Address      uint16
	Quantity     uint16
	Data         []byte
}

// Response はModbus RTUレスポンスを表す
type Response struct {
	UnitID       byte
	FunctionCode byte
	Data         []byte
}

// ParseRequest はバイト列からリクエストを解析する
func ParseRequest(frame []byte) (*Request, error) {
	// 最小フレーム長: UnitID(1) + FunctionCode(1) + CRC(2) = 4
	if len(frame) < 4 {
		return nil, ErrFrameTooShort
	}

	// CRC検証
	if !CheckCRC(frame) {
		return nil, ErrInvalidCRC
	}

	// CRCを除いたデータ部分
	return parseRequestBody(frame[:len(frame)-2])
}

// parseRequestBody は UnitID・機能コード・データ（CRC / LRC を除いたもの）からリクエストを作る。
// RTU と ASCII で共通。
func parseRequestBody(data []byte) (*Request, error) {
	if len(data) < 2 {
		return nil, ErrFrameTooShort
	}

	req := &Request{
		UnitID:       data[0],
		FunctionCode: data[1],
	}

	switch req.FunctionCode {
	case FuncReadCoils, FuncReadDiscreteInputs, FuncReadHoldingRegisters, FuncReadInputRegisters:
		// 読み取りリクエスト: Address(2) + Quantity(2)
		if len(data) < 6 {
			return nil, ErrFrameTooShort
		}
		req.Address = binary.BigEndian.Uint16(data[2:4])
		req.Quantity = binary.BigEndian.Uint16(data[4:6])

	case FuncWriteSingleCoil, FuncWriteSingleRegister:
		// 単一書き込み: Address(2) + Value(2)
		if len(data) < 6 {
			return nil, ErrFrameTooShort
		}
		req.Address = binary.BigEndian.Uint16(data[2:4])
		req.Quantity = 1
		req.Data = data[4:6]

	case FuncWriteMultipleCoils:
		// 複数コイル書き込み: Address(2) + Quantity(2) + ByteCount(1) + Data(N)
		if len(data) < 7 {
			return nil, ErrFrameTooShort
		}
		req.Address = binary.BigEndian.Uint16(data[2:4])
		req.Quantity = binary.BigEndian.Uint16(data[4:6])
		byteCount := int(data[6])
		if len(data) < 7+byteCount {
			return nil, ErrFrameTooShort
		}
		req.Data = data[7 : 7+byteCount]

	case FuncWriteMultipleRegisters:
		// 複数レジスタ書き込み: Address(2) + Quantity(2) + ByteCount(1) + Data(N)
		if len(data) < 7 {
			return nil, ErrFrameTooShort
		}
		req.Address = binary.BigEndian.Uint16(data[2:4])
		req.Quantity = binary.BigEndian.Uint16(data[4:6])
		byteCount := int(data[6])
		if len(data) < 7+byteCount {
			return nil, ErrFrameTooShort
		}
		req.Data = data[7 : 7+byteCount]

	default:
		// 対応していない機能コードはエラーにしない。
		// Processor が例外応答（Illegal Function）を返せるよう、UnitID と機能コードだけを持つリクエストにする
	}

	return req, nil
}

// BuildReadResponse は読み取りレスポンスを構築する（コイル/ディスクリート入力用）
func BuildReadBitsResponse(unitID, funcCode byte, values []bool) []byte {
	byteCount := (len(values) + 7) / 8
	data := make([]byte, 3+byteCount)
	data[0] = unitID
	data[1] = funcCode
	data[2] = byte(byteCount)

	// ビットをバイトにパック
	for i, v := range values {
		if v {
			byteIdx := i / 8
			bitIdx := uint(i % 8)
			data[3+byteIdx] |= 1 << bitIdx
		}
	}

	return AppendCRC(data)
}

// BuildReadRegistersResponse は読み取りレスポンスを構築する（レジスタ用）
func BuildReadRegistersResponse(unitID, funcCode byte, values []uint16) []byte {
	byteCount := len(values) * 2
	data := make([]byte, 3+byteCount)
	data[0] = unitID
	data[1] = funcCode
	data[2] = byte(byteCount)

	for i, v := range values {
		binary.BigEndian.PutUint16(data[3+i*2:], v)
	}

	return AppendCRC(data)
}

// BuildWriteSingleResponse は単一書き込みレスポンスを構築する
func BuildWriteSingleResponse(unitID, funcCode byte, address uint16, value uint16) []byte {
	data := make([]byte, 6)
	data[0] = unitID
	data[1] = funcCode
	binary.BigEndian.PutUint16(data[2:4], address)
	binary.BigEndian.PutUint16(data[4:6], value)

	return AppendCRC(data)
}

// BuildWriteMultipleResponse は複数書き込みレスポンスを構築する
func BuildWriteMultipleResponse(unitID, funcCode byte, address, quantity uint16) []byte {
	data := make([]byte, 6)
	data[0] = unitID
	data[1] = funcCode
	binary.BigEndian.PutUint16(data[2:4], address)
	binary.BigEndian.PutUint16(data[4:6], quantity)

	return AppendCRC(data)
}

// BuildExceptionResponse は例外レスポンスを構築する
func BuildExceptionResponse(unitID, funcCode, exceptionCode byte) []byte {
	data := make([]byte, 3)
	data[0] = unitID
	data[1] = funcCode | 0x80 // 例外フラグ
	data[2] = exceptionCode

	return AppendCRC(data)
}
