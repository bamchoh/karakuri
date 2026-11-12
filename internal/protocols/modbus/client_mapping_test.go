package modbus

import (
	"bytes"
	"strings"
	"testing"
)

func TestMappingAreas(t *testing.T) {
	f := &TCPClientFactory{}
	areas := f.MappingAreas()
	ro := map[string]bool{}
	for _, a := range areas {
		ro[a.ID] = a.ReadOnly
		if !a.OneOrigin || a.Size != 65536 {
			t.Errorf("%s: %+v", a.ID, a)
		}
	}
	if ro[AreaCoils] || !ro[AreaDiscreteInputs] || ro[AreaHoldingRegs] || !ro[AreaInputRegs] {
		t.Fatalf("読むだけのエリア: %v", ro)
	}
	if f.MaxWriteCount(AreaInputRegs) != 0 || f.MaxWriteCount(AreaHoldingRegs) != 123 || f.MaxReadCount(AreaCoils) != 2000 {
		t.Fatal("上限が違う")
	}
}

func TestBuildAreaRequests(t *testing.T) {
	f := &TCPClientFactory{}
	cases := []struct {
		name    string
		build   func() ([]byte, error)
		want    []byte
		wantErr string
	}{
		{"FC03", func() ([]byte, error) { return f.BuildReadRequest(AreaHoldingRegs, 10, 3) }, []byte{0x03, 0, 10, 0, 3}, ""},
		{"FC02", func() ([]byte, error) { return f.BuildReadRequest(AreaDiscreteInputs, 1, 8) }, []byte{0x02, 0, 1, 0, 8}, ""},
		{"読む個数オーバー", func() ([]byte, error) { return f.BuildReadRequest(AreaInputRegs, 0, 126) }, nil, "1〜125"},
		{"範囲外", func() ([]byte, error) { return f.BuildReadRequest(AreaHoldingRegs, 65535, 2) }, nil, "65536"},
		{"FC06（1 ワード）", func() ([]byte, error) { return f.BuildWriteRequest(AreaHoldingRegs, 5, []uint16{0xABCD}, nil) }, []byte{0x06, 0, 5, 0xAB, 0xCD}, ""},
		{"FC16", func() ([]byte, error) { return f.BuildWriteRequest(AreaHoldingRegs, 5, []uint16{1, 2}, nil) }, []byte{0x10, 0, 5, 0, 2, 4, 0, 1, 0, 2}, ""},
		{"FC05（1 ビット）", func() ([]byte, error) { return f.BuildWriteRequest(AreaCoils, 7, nil, []bool{true}) }, []byte{0x05, 0, 7, 0xFF, 0x00}, ""},
		{"FC15", func() ([]byte, error) { return f.BuildWriteRequest(AreaCoils, 0, nil, []bool{true, false, true}) }, []byte{0x0F, 0, 0, 0, 3, 1, 0x05}, ""},
		{"読むだけのエリアには書けない", func() ([]byte, error) { return f.BuildWriteRequest(AreaInputRegs, 0, []uint16{1}, nil) }, nil, "書き込めません"},
		{"不明なエリア", func() ([]byte, error) { return f.BuildReadRequest("nope", 0, 1) }, nil, "不明"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.build()
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil || !bytes.Equal(got, c.want) {
				t.Fatalf("got % X, %v; want % X", got, err, c.want)
			}
		})
	}
}
