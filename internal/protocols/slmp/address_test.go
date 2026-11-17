package slmp

import (
	"testing"

	"karakuri/internal/domain/protocol"
)

func TestParseAddress(t *testing.T) {
	ok := []struct {
		text string
		want protocol.DeviceAddress
	}{
		{"D10", protocol.DeviceAddress{Area: "D", Address: 10}},
		{"d10", protocol.DeviceAddress{Area: "D", Address: 10}},
		{" D10.3 ", protocol.DeviceAddress{Area: "D", Address: 163, Bit: true}},
		{"D10.F", protocol.DeviceAddress{Area: "D", Address: 175, Bit: true}},
		{"D16777215.F", protocol.DeviceAddress{Area: "D", Address: 16777215*16 + 15, Bit: true}},
		{"X1F", protocol.DeviceAddress{Area: "X", Address: 0x1F, Bit: true}},
		{"XFFFFFF", protocol.DeviceAddress{Area: "X", Address: 0xFFFFFF, Bit: true}},
		{"M100", protocol.DeviceAddress{Area: "M", Address: 100, Bit: true}},
		{"W1F.A", protocol.DeviceAddress{Area: "W", Address: 0x1F*16 + 10, Bit: true}},
		{"DX10", protocol.DeviceAddress{Area: "DX", Address: 0x10, Bit: true}}, // D ではなく DX
		{"SD5", protocol.DeviceAddress{Area: "SD", Address: 5}},
		{"SM5", protocol.DeviceAddress{Area: "SM", Address: 5, Bit: true}},
		{"S5", protocol.DeviceAddress{Area: "S", Address: 5, Bit: true}},
		{"ZR100", protocol.DeviceAddress{Area: "ZR", Address: 0x100}},
		{"TN3", protocol.DeviceAddress{Area: "TN", Address: 3}},
	}
	for _, c := range ok {
		got, err := parseAddress(c.text)
		if err != nil || got != c.want {
			t.Errorf("parseAddress(%q) = %+v, %v; want %+v", c.text, got, err, c.want)
		}
	}
	for _, text := range []string{
		"", "Q10", "D", "D1A", "D16777216", "D10.G", "D10.10", "D10.", "X1F.1", "M10.0", "XG", "D-1",
	} {
		if got, err := parseAddress(text); err == nil {
			t.Errorf("parseAddress(%q) = %+v, want エラー", text, got)
		}
	}
}
