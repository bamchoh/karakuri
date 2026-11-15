package serialport

import (
	"fmt"
	"strings"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
)

// シリアルポートの設定項目（サーバーの設定と、クライアントの接続先の設定で共通）。
// 項目の名前は serialPort / baudRate / dataBits / stopBits / parity。

// ConfigFields は、シリアルポートの設定項目を返す。def は既定値、portDescription はポートの欄の説明
func ConfigFields(def Config, portDescription string) []protocol.ConfigField {
	return []protocol.ConfigField{
		{Name: "serialPort", Label: "シリアルポート", Description: portDescription, Type: "serialport", Required: true, Default: def.Port},
		{Name: "baudRate", Label: "ボーレート", Description: "シリアル通信の速度（bps）。相手と一致させてください。", Type: "select", Required: true, Default: def.BaudRate, Options: []protocol.FieldOption{
			{Value: "4800", Label: "4800"},
			{Value: "9600", Label: "9600"},
			{Value: "19200", Label: "19200"},
			{Value: "38400", Label: "38400"},
			{Value: "57600", Label: "57600"},
			{Value: "115200", Label: "115200"},
		}},
		{Name: "dataBits", Label: "データビット", Description: "1 文字のデータビット数。", Type: "select", Required: true, Default: def.DataBits, Options: []protocol.FieldOption{
			{Value: "7", Label: "7"},
			{Value: "8", Label: "8"},
		}},
		{Name: "stopBits", Label: "ストップビット", Description: "1 文字の終わりのストップビット数。", Type: "select", Required: true, Default: def.StopBits, Options: []protocol.FieldOption{
			{Value: "1", Label: "1"},
			{Value: "2", Label: "2"},
		}},
		{Name: "parity", Label: "パリティ", Description: "誤り検出のパリティ。", Type: "select", Required: true, Default: def.Parity, Options: []protocol.FieldOption{
			{Value: "N", Label: "None"},
			{Value: "E", Label: "Even"},
			{Value: "O", Label: "Odd"},
		}},
	}
}

// ApplySettings は、map にあるシリアルポートの設定を c に入れる（ない項目はそのまま）
func ApplySettings(settings map[string]interface{}, c *Config) {
	fields.SetString(settings, "serialPort", &c.Port)
	fields.SetInt(settings, "baudRate", &c.BaudRate)
	fields.SetInt(settings, "dataBits", &c.DataBits)
	fields.SetInt(settings, "stopBits", &c.StopBits)
	fields.SetString(settings, "parity", &c.Parity)
	c.Port = strings.TrimSpace(c.Port)
	c.Parity = strings.ToUpper(strings.TrimSpace(c.Parity))
}

// ToMap は、シリアルポートの設定を m に入れて返す
func ToMap(c Config, m map[string]interface{}) map[string]interface{} {
	m["serialPort"] = c.Port
	m["baudRate"] = c.BaudRate
	m["dataBits"] = c.DataBits
	m["stopBits"] = c.StopBits
	m["parity"] = c.Parity
	return m
}

// Validate は、シリアルポートの設定を確かめる
func Validate(c Config) error {
	switch {
	case strings.TrimSpace(c.Port) == "":
		return fmt.Errorf("シリアルポートを選んでください")
	case c.BaudRate < 300 || c.BaudRate > 921600:
		return fmt.Errorf("ボーレートは 300〜921600 で指定してください")
	case c.DataBits != 7 && c.DataBits != 8:
		return fmt.Errorf("データビットは 7 か 8 にしてください")
	case c.StopBits != 1 && c.StopBits != 2:
		return fmt.Errorf("ストップビットは 1 か 2 にしてください")
	case c.Parity != "N" && c.Parity != "E" && c.Parity != "O":
		return fmt.Errorf("パリティは N / E / O のどれかにしてください")
	}
	return nil
}
