package valuecodec

import (
	"fmt"
	"reflect"
	"testing"
)

// roundTrip は値を MessagePack にして、型を指定せずに読み戻す（ホストとプラグインが実際に行う変換）
func roundTrip(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := Marshal(v)
	if err != nil {
		t.Fatalf("Marshal(%v): %v", v, err)
	}
	var out interface{}
	if err := Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal(%v): %v", v, err)
	}
	return out
}

func TestRoundTrip_Types(t *testing.T) {
	cases := []struct {
		in   interface{}
		want interface{}
	}{
		{true, true},
		{"hello", "hello"}, // []byte にならない
		{"日本語", "日本語"},
		{"", ""},
		{int16(-123), int64(-123)},
		{int16(200), int64(200)},
		{int64(-9000000000000000000), int64(-9000000000000000000)},
		{uint8(5), int64(5)}, // 127 以下の符号なし整数は int64 になる
		{uint8(200), uint64(200)},
		{uint16(60000), uint64(60000)},
		{uint64(18000000000000000000), uint64(18000000000000000000)},
		{float32(1.5), 1.5}, // float32 は float64 になる
		{2.25, 2.25},
		{[]interface{}{int16(1), "a", true}, []interface{}{int64(1), "a", true}},
		// マップは map[string]interface{} になる（入れ子も）
		{
			map[string]interface{}{"x": int16(7), "name": "p", "inner": map[string]interface{}{"ok": true}},
			map[string]interface{}{"x": int64(7), "name": "p", "inner": map[string]interface{}{"ok": true}},
		},
	}
	for _, c := range cases {
		got := roundTrip(t, c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v (%T) → %v (%T), want %v (%T)", c.in, c.in, got, got, c.want, c.want)
		}
	}
}

func TestRoundTrip_Nil(t *testing.T) {
	if got := roundTrip(t, nil); got != nil {
		t.Errorf("nil → %v", got)
	}
}

func TestUnmarshal_BrokenData(t *testing.T) {
	var out interface{}
	// 文字列（長さ 5）と宣言して、中身が足りない
	if err := Unmarshal([]byte{0xA5, 'a'}, &out); err == nil {
		t.Errorf("壊れたデータがエラーにならなかった: %v", fmt.Sprint(out))
	}
}
