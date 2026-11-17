package modbus

import (
	"testing"

	"karakuri/internal/domain/protocol"
)

func TestParseAddress(t *testing.T) {
	ok := []struct {
		text string
		want protocol.DeviceAddress
	}{
		{"40001", protocol.DeviceAddress{Area: AreaHoldingRegs, Address: 0}},
		{"400001", protocol.DeviceAddress{Area: AreaHoldingRegs, Address: 0}},
		{"465536", protocol.DeviceAddress{Area: AreaHoldingRegs, Address: 65535}},
		{"40010.3", protocol.DeviceAddress{Area: AreaHoldingRegs, Address: 9*16 + 3, Bit: true}},
		{"30002", protocol.DeviceAddress{Area: AreaInputRegs, Address: 1}},
		{"00001", protocol.DeviceAddress{Area: AreaCoils, Address: 0, Bit: true}},
		{"10005", protocol.DeviceAddress{Area: AreaDiscreteInputs, Address: 4, Bit: true}},
		{" 49999.15 ", protocol.DeviceAddress{Area: AreaHoldingRegs, Address: 9998*16 + 15, Bit: true}},
	}
	for _, c := range ok {
		got, err := parseAddress(c.text)
		if err != nil || got != c.want {
			t.Errorf("parseAddress(%q) = %+v, %v; want %+v", c.text, got, err, c.want)
		}
	}
	for _, text := range []string{
		"", "4001", "4000001", "40000", "400000", "465537", "20001", "4000A", "40001.16", "00001.0", "40001.",
	} {
		if got, err := parseAddress(text); err == nil {
			t.Errorf("parseAddress(%q) = %+v, want エラー", text, got)
		}
	}
}
