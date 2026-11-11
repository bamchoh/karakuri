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
		{"MyStruct", nodeShape{builtin: ua.TypeString, json: true}},
		{"ARRAY[1..4] OF REAL", nodeShape{builtin: ua.TypeFloat, dims: []int{4}}},
		{"ARRAY[0..1] OF MyStruct", nodeShape{builtin: ua.TypeString, dims: []int{2}, json: true}},
		// 多次元配列は Matrix（次元ごとの要素数）
		{"ARRAY[0..1, 0..2, 0..3] OF INT", nodeShape{builtin: ua.TypeInt16, dims: []int{2, 3, 4}}},
		{"ARRAY[1..2, 5..6] OF MyStruct", nodeShape{builtin: ua.TypeString, dims: []int{2, 2}, json: true}},
		// 旧形式の入れ子表記も同じ
		{"ARRAY[ARRAY[INT;5];3]", nodeShape{builtin: ua.TypeInt16, dims: []int{3, 5}}},
	}
	for _, tt := range tests {
		if got := shapeOf(tt.in); !reflect.DeepEqual(got, tt.want) {
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

func TestToVariant_Matrix(t *testing.T) {
	// ARRAY[0..1, 0..2] OF INT の値（入れ子の []interface{}）
	val := []interface{}{
		[]interface{}{int64(1), int64(2), int64(3)},
		[]interface{}{int64(4), int64(5), int64(6)},
	}
	v := toVariant(val, shapeOf("ARRAY[0..1, 0..2] OF INT"))
	if v.ValueRank() != 2 || !reflect.DeepEqual(v.Dimensions(), []int32{2, 3}) {
		t.Fatalf("rank=%d dims=%v, want 2 / [2 3]", v.ValueRank(), v.Dimensions())
	}
	// 行優先（最後の添字が最も速く変わる）
	if !reflect.DeepEqual(v.Value(), []int16{1, 2, 3, 4, 5, 6}) {
		t.Errorf("value = %#v", v.Value())
	}

	// 足りない要素はゼロ値で埋める
	v = toVariant([]interface{}{[]interface{}{int64(7)}}, shapeOf("ARRAY[0..1, 0..1] OF USINT"))
	if v.Type() != ua.TypeByte || !reflect.DeepEqual(v.Value(), []byte{7, 0, 0, 0}) {
		t.Errorf("padded byte matrix: type=%v value=%#v", v.Type(), v.Value())
	}
}

func TestFromVariant_Matrix(t *testing.T) {
	mv, err := ua.NewMatrixVariant([]int16{1, 2, 3, 4, 5, 6}, []int32{2, 3})
	if err != nil {
		t.Fatal(err)
	}
	got, err := fromVariant(mv, "ARRAY[0..1, 0..2] OF INT")
	if err != nil {
		t.Fatalf("fromVariant: %v", err)
	}
	want := []interface{}{
		[]interface{}{int16(1), int16(2), int16(3)},
		[]interface{}{int16(4), int16(5), int16(6)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}

	// 3 次元: 往復変換で元に戻る
	val3 := []interface{}{
		[]interface{}{[]interface{}{int64(1), int64(2)}, []interface{}{int64(3), int64(4)}},
		[]interface{}{[]interface{}{int64(5), int64(6)}, []interface{}{int64(7), int64(8)}},
	}
	back, err := fromVariant(toVariant(val3, shapeOf("ARRAY[0..1, 0..1, 0..1] OF DINT")), "ARRAY[0..1, 0..1, 0..1] OF DINT")
	if err != nil {
		t.Fatal(err)
	}
	if got := toInt64(back.([]interface{})[1].([]interface{})[0].([]interface{})[1]); got != 6 {
		t.Errorf("[1][0][1] = %d, want 6 (%#v)", got, back)
	}

	// 構造体の Matrix は各要素を JSON として解釈する
	sm, _ := ua.NewMatrixVariant([]string{`{"a":1}`, `{"a":2}`, `{"a":3}`, `{"a":4}`}, []int32{2, 2})
	got, err = fromVariant(sm, "ARRAY[0..1, 0..1] OF MyStruct")
	if err != nil {
		t.Fatalf("struct matrix: %v", err)
	}
	if m := got.([]interface{})[1].([]interface{})[0].(map[string]interface{}); m["a"] != float64(3) {
		t.Errorf("struct matrix [1][0] = %#v", m)
	}

	// 次元数が違う書き込みはエラー
	if _, err := fromVariant(mv, "ARRAY[0..1, 0..2, 0..0] OF INT"); err == nil {
		t.Errorf("rank mismatch accepted")
	}
}
