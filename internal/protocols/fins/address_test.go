package fins

import (
	"testing"

	"karakuri/internal/domain/protocol"
)

func TestParseAddress(t *testing.T) {
	ok := []struct {
		text string
		want protocol.DeviceAddress
	}{
		{"D100", protocol.DeviceAddress{Area: "DM", Address: 100}},
		{"DM100", protocol.DeviceAddress{Area: "DM", Address: 100}},
		{"d100.3", protocol.DeviceAddress{Area: "DM", Address: 1603, Bit: true}},
		{"CIO 100.05", protocol.DeviceAddress{Area: "CIO", Address: 1605, Bit: true}},
		{"CIO100", protocol.DeviceAddress{Area: "CIO", Address: 100}},
		{"100.15", protocol.DeviceAddress{Area: "CIO", Address: 1615, Bit: true}},
		{"0", protocol.DeviceAddress{Area: "CIO", Address: 0}},
		{"W10.15", protocol.DeviceAddress{Area: "WR", Address: 175, Bit: true}},
		{"WR10", protocol.DeviceAddress{Area: "WR", Address: 10}},
		{"H5", protocol.DeviceAddress{Area: "HR", Address: 5}},
		{"A5", protocol.DeviceAddress{Area: "AR", Address: 5}},
		{"T5", protocol.DeviceAddress{Area: "T", Address: 5}},
		{"C5", protocol.DeviceAddress{Area: "C", Address: 5}},
		{"TF5", protocol.DeviceAddress{Area: "T_FLAG", Address: 5, Bit: true}},
		{"CF32767", protocol.DeviceAddress{Area: "C_FLAG", Address: 32767, Bit: true}},
		{"E0_100", protocol.DeviceAddress{Area: "EM0", Address: 100}},
		{"EM0_100.1", protocol.DeviceAddress{Area: "EM0", Address: 1601, Bit: true}},
		{"E18_65535", protocol.DeviceAddress{Area: "EM18", Address: 65535}},
		{"EA_1", protocol.DeviceAddress{Area: "EMA", Address: 1}},
		{"D65535.15", protocol.DeviceAddress{Area: "DM", Address: 65535*16 + 15, Bit: true}},
	}
	for _, c := range ok {
		got, err := parseAddress(c.text)
		if err != nil || got != c.want {
			t.Errorf("parseAddress(%q) = %+v, %v; want %+v", c.text, got, err, c.want)
		}
	}
	for _, text := range []string{
		"", "X10", "D65536", "D10.16", "D10.100", "D10.", "TF5.1", "T32768", "E19_0", "E0100", "D", "D1A",
	} {
		if got, err := parseAddress(text); err == nil {
			t.Errorf("parseAddress(%q) = %+v, want エラー", text, got)
		}
	}
}
