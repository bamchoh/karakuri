package slmp

import (
	"fmt"
	"strconv"
	"strings"

	"karakuri/internal/domain/protocol"
)

// parseAddress は、三菱の書き方のアドレス（"D10"、"D10.3"、"X1F"、"M100"、"ZR10"）を、DataStore のエリアとアドレスに直す。
//
//   - デバイスの記号は、長いほうを優先する（"DX10" は DX、"D10" は D）。大文字・小文字は区別しない
//   - X・Y・B・W など番号を 16 進で書くデバイスは 16 進、ほかは 10 進
//   - ワードデバイスの「.」のあとはビットの位置（16 進 1 桁、0〜F。D10.F）。アドレスは ワード × 16 + ビット
func parseAddress(text string) (protocol.DeviceAddress, error) {
	s := strings.ToUpper(strings.TrimSpace(text))
	var def deviceDef
	for _, d := range deviceDefs {
		if strings.HasPrefix(s, d.ID) && len(d.ID) > len(def.ID) {
			def = d
		}
	}
	if def.ID == "" {
		return protocol.DeviceAddress{}, fmt.Errorf("デバイスの記号が読めません: %q（D10、X1F、M100 のように書きます）", text)
	}
	numText, bitText, hasBit := strings.Cut(s[len(def.ID):], ".")
	base := 10
	if def.Hex {
		base = 16
	}
	n, err := strconv.ParseUint(numText, base, 32)
	if err != nil || n >= maxPoints {
		kind := "10 進"
		if def.Hex {
			kind = "16 進"
		}
		return protocol.DeviceAddress{}, fmt.Errorf("デバイス番号が読めません: %q（%s は番号を %s で書きます）", text, def.ID, kind)
	}
	if !hasBit {
		return protocol.DeviceAddress{Area: def.ID, Address: uint32(n), Bit: def.IsBit}, nil
	}
	if def.IsBit {
		return protocol.DeviceAddress{}, fmt.Errorf("%s はビットのデバイスなので、ビットの位置（.）は付けられません: %q", def.ID, text)
	}
	b, err := strconv.ParseUint(bitText, 16, 8)
	if err != nil || len(bitText) != 1 {
		return protocol.DeviceAddress{}, fmt.Errorf("ビットの位置は 16 進 1 桁（0〜F）で書きます: %q", text)
	}
	return protocol.DeviceAddress{Area: def.ID, Address: uint32(n)*16 + uint32(b), Bit: true}, nil
}

// ParseAddress は protocol.AddressParser の実装（HTTP API で "D10.3" のようにアドレスを書けるようにする）
func (f *Factory) ParseAddress(text string) (protocol.DeviceAddress, error) {
	return parseAddress(text)
}

// ParseAddress は protocol.AddressParser の実装（デバイスは SLMP と同じ）
func (f *MCSerialFactory) ParseAddress(text string) (protocol.DeviceAddress, error) {
	return parseAddress(text)
}

var (
	_ protocol.AddressParser = (*Factory)(nil)
	_ protocol.AddressParser = (*MCSerialFactory)(nil)
)
