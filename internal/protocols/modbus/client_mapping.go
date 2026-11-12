package modbus

import (
	"encoding/binary"
	"fmt"

	"karakuri/internal/domain/protocol"
)

// Modbus TCP クライアントの、変数マッピング（protocol.MappableClientFactory）の実装。
// エリアの ID と表示はサーバー（ModbusDataStore）と同じにする。機器の側で書き込めない
// ディスクリート入力と入力レジスタは ReadOnly にする（読むだけ）。

var _ protocol.MappableClientFactory = (*TCPClientFactory)(nil)

// MappingAreas はマッピングできるメモリエリアを返す
func (f *TCPClientFactory) MappingAreas() []protocol.MemoryArea {
	return []protocol.MemoryArea{
		{ID: AreaCoils, DisplayName: "コイル (0x)", IsBit: true, Size: 65536, OneOrigin: true, ShortName: "0"},
		{ID: AreaDiscreteInputs, DisplayName: "ディスクリート入力 (1x)", IsBit: true, Size: 65536, ReadOnly: true, OneOrigin: true, ShortName: "1"},
		{ID: AreaHoldingRegs, DisplayName: "保持レジスタ (4x)", Size: 65536, OneOrigin: true, ShortName: "4"},
		{ID: AreaInputRegs, DisplayName: "入力レジスタ (3x)", Size: 65536, ReadOnly: true, OneOrigin: true, ShortName: "3"},
	}
}

func readFunction(area string) (byte, int, error) {
	switch area {
	case AreaCoils:
		return fcReadCoils, 2000, nil
	case AreaDiscreteInputs:
		return fcReadDiscreteInputs, 2000, nil
	case AreaHoldingRegs:
		return fcReadHoldingRegisters, 125, nil
	case AreaInputRegs:
		return fcReadInputRegisters, 125, nil
	}
	return 0, 0, fmt.Errorf("不明なエリアです: %s", area)
}

// MaxReadCount は 1 回のリクエストで読める個数
func (f *TCPClientFactory) MaxReadCount(area string) int {
	_, max, err := readFunction(area)
	if err != nil {
		return 0
	}
	return max
}

// MaxWriteCount は 1 回のリクエストで書ける個数（書けないエリアは 0）
func (f *TCPClientFactory) MaxWriteCount(area string) int {
	switch area {
	case AreaCoils:
		return 1968
	case AreaHoldingRegs:
		return 123
	}
	return 0
}

// BuildReadRequest は area の address から count 個を読む PDU を作る
func (f *TCPClientFactory) BuildReadRequest(area string, address uint32, count int) ([]byte, error) {
	fc, max, err := readFunction(area)
	if err != nil {
		return nil, err
	}
	if count < 1 || count > max {
		return nil, fmt.Errorf("読む個数は 1〜%d にしてください（%d）", max, count)
	}
	if int(address)+count > 65536 {
		return nil, fmt.Errorf("アドレスと個数の合計が 65536 を超えています")
	}
	pdu := []byte{fc}
	pdu = binary.BigEndian.AppendUint16(pdu, uint16(address))
	pdu = binary.BigEndian.AppendUint16(pdu, uint16(count))
	return pdu, nil
}

// BuildWriteRequest は書き込みの PDU を作る。1 個なら FC05 / FC06、複数なら FC15 / FC16 を使う
// （1 個ずつの書き込みにしか対応しない機器があるため）
func (f *TCPClientFactory) BuildWriteRequest(area string, address uint32, words []uint16, bits []bool) ([]byte, error) {
	max := f.MaxWriteCount(area)
	if max == 0 {
		return nil, fmt.Errorf("このエリアには書き込めません: %s", area)
	}
	n := len(words)
	if area == AreaCoils {
		n = len(bits)
	}
	if n < 1 || n > max {
		return nil, fmt.Errorf("書く個数は 1〜%d にしてください（%d）", max, n)
	}
	if int(address)+n > 65536 {
		return nil, fmt.Errorf("アドレスと個数の合計が 65536 を超えています")
	}

	if area == AreaCoils {
		if n == 1 {
			v := uint16(0x0000)
			if bits[0] {
				v = 0xFF00
			}
			pdu := []byte{fcWriteSingleCoil}
			pdu = binary.BigEndian.AppendUint16(pdu, uint16(address))
			return binary.BigEndian.AppendUint16(pdu, v), nil
		}
		packed := make([]byte, (n+7)/8)
		for i, b := range bits {
			if b {
				packed[i/8] |= 1 << (i % 8)
			}
		}
		pdu := []byte{fcWriteMultipleCoils}
		pdu = binary.BigEndian.AppendUint16(pdu, uint16(address))
		pdu = binary.BigEndian.AppendUint16(pdu, uint16(n))
		pdu = append(pdu, byte(len(packed)))
		return append(pdu, packed...), nil
	}

	if n == 1 {
		pdu := []byte{fcWriteSingleRegister}
		pdu = binary.BigEndian.AppendUint16(pdu, uint16(address))
		return binary.BigEndian.AppendUint16(pdu, words[0]), nil
	}
	pdu := []byte{fcWriteMultipleRegisters}
	pdu = binary.BigEndian.AppendUint16(pdu, uint16(address))
	pdu = binary.BigEndian.AppendUint16(pdu, uint16(n))
	pdu = append(pdu, byte(n*2))
	for _, w := range words {
		pdu = binary.BigEndian.AppendUint16(pdu, w)
	}
	return pdu, nil
}
