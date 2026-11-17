package modbus

import (
	"fmt"
	"strconv"
	"strings"

	"karakuri/internal/domain/protocol"
)

// maxModbusNumber は、Modbus のアドレスの番号（1 始まり）の最大
const maxModbusNumber = 65536

// parseAddress は、Modbus の書き方のアドレス（先頭の 1 桁がエリア、残りが 1 始まりの番号）を、DataStore のエリアとアドレスに直す。
//
//   - 5 桁（40001〜49999）か 6 桁（400001〜465536）。先頭が 0 はコイル、1 は入力、3 は入力レジスタ、4 は保持レジスタ
//   - レジスタの「.」のあとはビットの位置（10 進で 0〜15。40001.3）。アドレスは ワード × 16 + ビット
func parseAddress(text string) (protocol.DeviceAddress, error) {
	s := strings.TrimSpace(text)
	numText, bitText, hasBit := strings.Cut(s, ".")
	if len(numText) != 5 && len(numText) != 6 {
		return protocol.DeviceAddress{}, fmt.Errorf("Modbus のアドレスは、40001 のように 5 桁か 6 桁で書きます: %q", text)
	}
	n, err := strconv.ParseUint(numText[1:], 10, 32)
	if err != nil || n < 1 || n > maxModbusNumber {
		return protocol.DeviceAddress{}, fmt.Errorf("番号が読めません: %q（1〜%d）", text, maxModbusNumber)
	}
	var area string
	isBit := false
	switch numText[0] {
	case '0':
		area, isBit = AreaCoils, true
	case '1':
		area, isBit = AreaDiscreteInputs, true
	case '3':
		area = AreaInputRegs
	case '4':
		area = AreaHoldingRegs
	default:
		return protocol.DeviceAddress{}, fmt.Errorf("先頭の桁は 0（コイル）・1（入力）・3（入力レジスタ）・4（保持レジスタ）のどれかです: %q", text)
	}
	addr := uint32(n - 1)
	if !hasBit {
		return protocol.DeviceAddress{Area: area, Address: addr, Bit: isBit}, nil
	}
	if isBit {
		return protocol.DeviceAddress{}, fmt.Errorf("コイル・入力には、ビットの位置（.）は付けられません: %q", text)
	}
	b, err := strconv.ParseUint(bitText, 10, 8)
	if err != nil || b > 15 || len(bitText) > 2 {
		return protocol.DeviceAddress{}, fmt.Errorf("ビットの位置は 10 進で 0〜15 です: %q", text)
	}
	return protocol.DeviceAddress{Area: area, Address: addr*16 + uint32(b), Bit: true}, nil
}

// ParseAddress は protocol.AddressParser の実装（TCP / RTU / ASCII で共通。HTTP API で "40001" のようにアドレスを書けるようにする）
func (f *BaseFactory) ParseAddress(text string) (protocol.DeviceAddress, error) {
	return parseAddress(text)
}

var _ protocol.AddressParser = (*TCPFactory)(nil)
