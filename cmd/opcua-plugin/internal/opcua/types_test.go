package opcua

import (
	"reflect"
	"testing"

	"github.com/bamchoh/uastack/ua"
)

func TestParseArrayType(t *testing.T) {
	tests := []struct {
		in    string
		elem  string
		lower int
		size  int
		ok    bool
	}{
		{"ARRAY[2..9] OF INT", "INT", 2, 8, true},
		{"ARRAY[0..2, 0..4] OF INT", "ARRAY[0..4] OF INT", 0, 3, true},
		{"ARRAY[INT;10]", "INT", 0, 10, true},
		{"INT", "", 0, 0, false},
		{"ARRAY[5..1] OF INT", "", 0, 0, false},
	}
	for _, tt := range tests {
		elem, lower, size, ok := parseArrayType(tt.in)
		if elem != tt.elem || lower != tt.lower || size != tt.size || ok != tt.ok {
			t.Errorf("parseArrayType(%q) = (%q,%d,%d,%v), want (%q,%d,%d,%v)",
				tt.in, elem, lower, size, ok, tt.elem, tt.lower, tt.size, tt.ok)
		}
	}
}

func TestShapeOf(t *testing.T) {
	tests := []struct {
		in   string
		want nodeShape
	}{
		{"BOOL", nodeShape{builtin: ua.TypeBoolean}},
		{"ULINT", nodeShape{builtin: ua.TypeUInt64}},
		{"STRING[20]", nodeShape{builtin: ua.TypeString}},
		{"TIME", nodeShape{builtin: ua.TypeString}},
		{"MyStruct", nodeShape{builtin: ua.TypeString}},
		{"ARRAY[1..4] OF REAL", nodeShape{builtin: ua.TypeFloat, array: true, size: 4}},
		{"ARRAY[0..1] OF MyStruct", nodeShape{builtin: ua.TypeString, array: true, size: 2}},
	}
	for _, tt := range tests {
		if got := shapeOf(tt.in); got != tt.want {
			t.Errorf("shapeOf(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestToVariant(t *testing.T) {
	// msgpack デコード後の典型的な値（int64 / uint64 / float64 / []interface{} / map）
	if v := toVariant(int64(-5), shapeOf("SINT")); v.Value() != int8(-5) {
		t.Errorf("SINT: %#v", v.Value())
	}
	if v := toVariant(uint64(1<<63), shapeOf("ULINT")); v.Value() != uint64(1<<63) {
		t.Errorf("ULINT: %#v", v.Value())
	}
	if v := toVariant(nil, shapeOf("REAL")); v.Value() != float32(0) {
		t.Errorf("nil REAL: %#v", v.Value())
	}
	v := toVariant([]interface{}{int64(1), int64(2)}, shapeOf("ARRAY[0..3] OF USINT"))
	if v.Type() != ua.TypeByte || !reflect.DeepEqual(v.Value(), []byte{1, 2, 0, 0}) {
		t.Errorf("USINT array: type=%v value=%#v", v.Type(), v.Value())
	}
	v = toVariant(map[string]interface{}{"a": int64(1)}, shapeOf("MyStruct"))
	if v.Value() != `{"a":1}` {
		t.Errorf("struct: %#v", v.Value())
	}
}

func TestFromVariant(t *testing.T) {
	got, err := fromVariant(ua.NewVariant([]int16{1, 2}), "ARRAY[0..1] OF INT")
	if err != nil || !reflect.DeepEqual(got, []interface{}{int16(1), int16(2)}) {
		t.Errorf("INT array: %#v, %v", got, err)
	}
	got, err = fromVariant(ua.NewByteArrayVariant([]byte{3}), "ARRAY[0..0] OF USINT")
	if err != nil || !reflect.DeepEqual(got, []interface{}{uint8(3)}) {
		t.Errorf("USINT array: %#v, %v", got, err)
	}
	got, err = fromVariant(ua.NewVariant(`{"speed":5}`), "Motor")
	if err != nil || !reflect.DeepEqual(got, map[string]interface{}{"speed": float64(5)}) {
		t.Errorf("struct: %#v, %v", got, err)
	}
	if _, err := fromVariant(ua.NewVariant("not json"), "Motor"); err == nil {
		t.Errorf("invalid JSON accepted")
	}
	got, err = fromVariant(ua.NewVariant("T#1s"), "TIME")
	if err != nil || got != "T#1s" {
		t.Errorf("TIME: %#v, %v", got, err)
	}
}
