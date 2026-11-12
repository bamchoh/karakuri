package modbus

import (
	"reflect"
	"strings"
	"testing"

	"karakuri/internal/domain/protocol"
)

// 設定を map にして戻すと、同じ設定になる（TCP / RTU / ASCII）
func TestFactory_ConfigRoundTrip(t *testing.T) {
	factories := []protocol.ServerFactory{&TCPFactory{}, &RTUFactory{}, &ASCIIFactory{}}
	for _, f := range factories {
		t.Run(string(f.ProtocolType()), func(t *testing.T) {
			def := f.CreateConfigFromVariant("")
			m := f.ConfigToMap(def)

			// 設定の項目（GetConfigFields）と map のキーが一致する
			var names []string
			for _, field := range f.GetConfigFields("") {
				names = append(names, field.Name)
				if _, ok := m[field.Name]; !ok {
					t.Errorf("ConfigToMap に %q がない", field.Name)
				}
			}
			// 設定の項目の既定値は、実際の既定の設定と同じ
			for _, field := range f.GetConfigFields("") {
				if !reflect.DeepEqual(field.Default, m[field.Name]) {
					t.Errorf("%s の既定値 = %v, 既定の設定は %v", field.Name, field.Default, m[field.Name])
				}
			}
			if len(names) != len(m) {
				t.Errorf("項目 %v と map %v の数が違う", names, m)
			}

			got, err := f.MapToConfig("", m)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, def) {
				t.Errorf("戻した設定が違う: got %+v, want %+v", got, def)
			}
			if got.ProtocolType() != f.ProtocolType() {
				t.Errorf("ProtocolType = %q, want %q", got.ProtocolType(), f.ProtocolType())
			}

			// map にない項目は既定値のまま
			empty, err := f.MapToConfig("", map[string]interface{}{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(empty, def) {
				t.Errorf("空の map から作った設定が既定値と違う: %+v", empty)
			}
		})
	}
}

// 数値は float64（JSON から来た値）でも int でも受け取る
func TestFactory_MapToConfig_Numbers(t *testing.T) {
	tcp, _ := (&TCPFactory{}).MapToConfig("", map[string]interface{}{"tcpAddress": "127.0.0.1", "tcpPort": float64(1502)})
	if c := tcp.(*ModbusConfig); c.TCPAddress != "127.0.0.1" || c.TCPPort != 1502 {
		t.Errorf("TCP: %+v", c)
	}

	for _, f := range []protocol.ServerFactory{&RTUFactory{}, &ASCIIFactory{}} {
		cfg, _ := f.MapToConfig("", map[string]interface{}{
			"serialPort": "COM7", "baudRate": float64(19200), "dataBits": 8, "stopBits": float64(2), "parity": "O",
		})
		c := cfg.(*ModbusConfig)
		if c.SerialPort != "COM7" || c.BaudRate != 19200 || c.DataBits != 8 || c.StopBits != 2 || c.Parity != "O" {
			t.Errorf("%s: %+v", f.ProtocolType(), c)
		}
	}
}

// データビットの説明文は、そのプロトコルの名前と既定のビット数になっている
func TestFactory_DataBitsDescription(t *testing.T) {
	cases := []struct {
		factory protocol.ServerFactory
		want    string
	}{
		{&RTUFactory{}, "Modbus RTU は通常 8 ビット"},
		{&ASCIIFactory{}, "Modbus ASCII は通常 7 ビット"},
	}
	for _, c := range cases {
		for _, field := range c.factory.GetConfigFields("") {
			if field.Name == "dataBits" && !strings.Contains(field.Description, c.want) {
				t.Errorf("%s: 説明文 %q に %q がない", c.factory.ProtocolType(), field.Description, c.want)
			}
		}
	}
}

// オフにした UnitID の一覧は、設定した順番にかかわらず番号順に返る
func TestDataStoreHandler_GetDisabledUnitIDs_Sorted(t *testing.T) {
	h := NewDataStoreHandler(NewModbusDataStore(16, 16, 16, 16))
	want := []uint8{1, 3, 4, 9, 20, 100, 247}
	for i := 0; i < 50; i++ {
		h.SetDisabledUnitIDs([]uint8{100, 4, 247, 3, 20, 1, 9})
		if got := h.GetDisabledUnitIDs(); !reflect.DeepEqual(got, want) {
			t.Fatalf("GetDisabledUnitIDs() = %v, want %v", got, want)
		}
	}
}
