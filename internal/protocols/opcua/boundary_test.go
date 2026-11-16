package opcua

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/bamchoh/uastack/ua"

	"karakuri/internal/domain/variable"
	"karakuri/internal/infrastructure/adapter"
)

// すべての PLC 型について、型の端の値が、変数ストア → OPC UA（Variant）→ 変数ストアと行き来しても変わらない。
// 組み込み型も、PLC 型に合ったもの（INT なら Int16）になる
func TestBoundary_RoundTripAllTypes(t *testing.T) {
	cases := []struct {
		dataType string
		builtin  ua.TypeID
		values   []interface{} // 変数ストアが持つ値
	}{
		{"BOOL", ua.TypeBoolean, []interface{}{false, true}},
		{"SINT", ua.TypeSByte, []interface{}{int8(math.MinInt8), int8(-1), int8(0), int8(math.MaxInt8)}},
		{"INT", ua.TypeInt16, []interface{}{int16(math.MinInt16), int16(-1), int16(math.MaxInt16)}},
		{"DINT", ua.TypeInt32, []interface{}{int32(math.MinInt32), int32(math.MaxInt32)}},
		{"LINT", ua.TypeInt64, []interface{}{int64(math.MinInt64), int64(-9007199254740993), int64(math.MaxInt64)}},
		{"USINT", ua.TypeByte, []interface{}{uint8(0), uint8(math.MaxUint8)}},
		{"UINT", ua.TypeUInt16, []interface{}{uint16(0), uint16(math.MaxUint16)}},
		{"UDINT", ua.TypeUInt32, []interface{}{uint32(0), uint32(math.MaxUint32)}},
		{"ULINT", ua.TypeUInt64, []interface{}{uint64(0), uint64(9007199254740993), uint64(math.MaxUint64)}},
		{"REAL", ua.TypeFloat, []interface{}{float32(-math.MaxFloat32), float32(math.SmallestNonzeroFloat32), float32(math.MaxFloat32)}},
		{"LREAL", ua.TypeDouble, []interface{}{-math.MaxFloat64, math.SmallestNonzeroFloat64, math.MaxFloat64}},
		{"STRING[6]", ua.TypeString, []interface{}{"", "abcdef", "日本"}},
		{"TIME", ua.TypeString, []interface{}{"T#0ms", "T#24d20h31m23s647ms"}},
		{"DATE", ua.TypeString, []interface{}{"D#1970-01-01", "D#2106-02-07"}},
	}
	for _, c := range cases {
		sh := shapeOf(c.dataType)
		for _, want := range c.values {
			v := toVariant(want, sh)
			if v.Type() != c.builtin {
				t.Errorf("%s %v: 組み込み型 %v, want %v", c.dataType, want, v.Type(), c.builtin)
				continue
			}
			back, err := fromVariant(v, c.dataType)
			if err != nil {
				t.Errorf("%s %v: fromVariant: %v", c.dataType, want, err)
				continue
			}
			got, err := variable.ConvertValue(back, variable.DataType(c.dataType))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("%s: %#v → %#v → %#v (%v)", c.dataType, want, v.Value(), got, err)
			}
		}
	}

	// 配列も、要素の端の値がそのまま行き来する（どの型の配列も）
	for _, c := range cases {
		dataType := fmt.Sprintf("ARRAY[1..%d] OF %s", len(c.values), c.dataType)
		v := toVariant(c.values, shapeOf(dataType))
		back, err := fromVariant(v, dataType)
		if err != nil {
			t.Errorf("%s: fromVariant: %v", dataType, err)
			continue
		}
		got, err := variable.ConvertValue(back, variable.DataType(dataType))
		if err != nil || !reflect.DeepEqual(got, c.values) {
			t.Errorf("%s: %#v → %#v → %#v (%v)", dataType, c.values, v.Value(), got, err)
		}
	}
	// 組み込み型の配列でない値は、配列として読まない
	if _, err := fromVariant(ua.NewVariant([]ua.NodeId{ua.NewNumericNodeId(0, 1)}), "ARRAY[0..0] OF INT"); err == nil {
		t.Error("NodeId の配列を INT の配列として読めた")
	}
}

// 変数ストアの外の、幅の広い型の値（テストのフェイクなどが使う int64 / float64 / 文字列）も、PLC 型に合った組み込み型にする
func TestBoundary_ToVariantFromWideValues(t *testing.T) {
	cases := []struct {
		val      interface{}
		dataType string
		want     interface{}
	}{
		{int64(-5), "SINT", int8(-5)},
		{float64(3), "INT", int16(3)},
		{uint64(65535), "UINT", uint16(65535)},
		{int64(-1), "UDINT", uint32(math.MaxUint32)},
		{float64(1.5), "REAL", float32(1.5)},
		{int64(2), "LREAL", float64(2)},
		{uint64(math.MaxUint64), "LREAL", float64(math.MaxUint64)},
		{true, "INT", int16(1)},
		{false, "REAL", float32(0)},
		{int64(0), "BOOL", false},
		{float64(2), "BOOL", true},
		{"true", "BOOL", true},
		{"no", "BOOL", false},
		{nil, "LINT", int64(0)},
		{nil, "STRING", ""},
		{[]byte("ab"), "STRING", "ab"},
		{int64(7), "STRING", "7"},
	}
	for _, c := range cases {
		if got := toVariant(c.val, shapeOf(c.dataType)).Value(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("toVariant(%#v, %s) = %#v, want %#v", c.val, c.dataType, got, c.want)
		}
	}
	// 配列の要素も同じ（足りない要素はゼロ値）
	cases2 := []struct {
		val      []interface{}
		dataType string
		want     interface{}
	}{
		{[]interface{}{"1", "x", true}, "ARRAY[0..3] OF BOOL", []bool{true, false, true, false}}, // "1" は true、読めない文字列は false
		{[]interface{}{int64(-1)}, "ARRAY[0..1] OF SINT", []int8{-1, 0}},
		{[]interface{}{float64(1)}, "ARRAY[0..0] OF UINT", []uint16{1}},
		{[]interface{}{int64(1)}, "ARRAY[0..0] OF DINT", []int32{1}},
		{[]interface{}{int64(1)}, "ARRAY[0..0] OF UDINT", []uint32{1}},
		{[]interface{}{int64(-1)}, "ARRAY[0..0] OF LINT", []int64{-1}},
		{[]interface{}{uint64(1)}, "ARRAY[0..0] OF ULINT", []uint64{1}},
		{[]interface{}{int64(2)}, "ARRAY[0..0] OF REAL", []float32{2}},
		{[]interface{}{float32(2)}, "ARRAY[0..0] OF LREAL", []float64{2}},
		{[]interface{}{"x", nil}, "ARRAY[0..1] OF STRING[3]", []string{"x", ""}},
	}
	for _, c := range cases2 {
		if got := toVariant(c.val, shapeOf(c.dataType)).Value(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("toVariant(%#v, %s) = %#v, want %#v", c.val, c.dataType, got, c.want)
		}
	}
}

// OPC UA のクライアントから、型の端の値と、型に収まらない値を書く（実際の変数ストアを通す）。
// 構造体のノードは JSON の文字列で書くので、uastack の型の検査を通らない。収まらない値は、変数ストアが断る
func TestBoundary_WritesThroughHostStore(t *testing.T) {
	vars := variable.NewVariableStore()
	motor, err := variable.NewStructTypeDef("Motor", []variable.StructField{
		{Name: "speed", DataType: "INT"},
		{Name: "total", DataType: "LINT"},
		{Name: "count", DataType: "ULINT"},
	}, vars.TypeResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := vars.RegisterStructType(motor); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for name, dataType := range map[string]string{"Small": "SINT", "Word": "UINT", "M": "Motor", "Ms": "ARRAY[0..1] OF Motor"} {
		v, err := vars.CreateVariable(name, variable.DataType(dataType), nil)
		if err != nil {
			t.Fatalf("CreateVariable(%s): %v", name, err)
		}
		vars.SetNodePublishing(v.ID, string(ProtocolOPCUA), &variable.NodePublishing{Enabled: true, AccessMode: "readwrite"})
		ids[name] = v.ID
	}
	srv := NewServer(testConfig(freePort(t)), adapter.NewVariableStoreAccessor(vars))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	c := dialTestClient(t, srv)
	ns := srv.nodes.ns
	ctx := context.Background()
	write := func(node string, v interface{}) error {
		return c.WriteValue(ctx, ua.NewStringNodeId(ns, node), ua.NewVariant(v))
	}
	host := func(name string) interface{} {
		v, err := vars.GetVariable(ids[name])
		if err != nil {
			t.Fatal(err)
		}
		return v.Value
	}

	// 組み込み型の端の値
	for _, v := range []int8{math.MinInt8, math.MaxInt8} {
		if err := write("Small", v); err != nil || host("Small") != v {
			t.Errorf("Small に %d: %v（ホスト %#v）", v, err, host("Small"))
		}
	}
	if err := write("Word", uint16(math.MaxUint16)); err != nil || host("Word") != uint16(math.MaxUint16) {
		t.Errorf("Word に 65535: %v（ホスト %#v）", err, host("Word"))
	}

	// 構造体（JSON）: 端の値は桁を落とさずに届く
	big := `{"speed": -32768, "total": -9223372036854775808, "count": 18446744073709551615}`
	if err := write("M", big); err != nil {
		t.Fatalf("M に端の値: %v", err)
	}
	want := map[string]interface{}{"speed": int16(math.MinInt16), "total": int64(math.MinInt64), "count": uint64(math.MaxUint64)}
	if got := host("M"); !reflect.DeepEqual(got, want) {
		t.Errorf("M（ホスト） = %#v, want %#v", got, want)
	}
	if err := write("M", `{"speed": 1, "total": 9007199254740993, "count": 0}`); err != nil {
		t.Fatalf("M に 2^53 + 1: %v", err)
	}
	if got := host("M").(map[string]interface{})["total"]; got != int64(9007199254740993) {
		t.Errorf("M.total = %#v, want 9007199254740993（JSON の数値で桁を落とした？）", got)
	}

	// 構造体（JSON）: 型に収まらない値・小数は断り、値は変わらない
	before := host("M")
	for _, s := range []string{
		`{"speed": 70000, "total": 0, "count": 0}`,
		`{"speed": 1.5, "total": 0, "count": 0}`,
		`{"speed": 0, "total": 0, "count": 18446744073709551616}`,
		`{"speed": 0, "total": 0, "count": 0} {}`, // JSON のあとに余分な文字
	} {
		if err := write("M", s); err == nil {
			t.Errorf("M に %s を書けた", s)
		}
	}
	if err := write("Ms", []string{`{"speed": 1, "total": 0, "count": 0}`, `{"speed": -32769, "total": 0, "count": 0}`}); err == nil {
		t.Error("構造体の配列の要素に、収まらない値を書けた")
	}
	if got := host("M"); !reflect.DeepEqual(got, before) {
		t.Errorf("断った書き込みで M が変わった: %#v → %#v", before, got)
	}

	// 型の違う組み込み型は、uastack が断る（変数ストアまで届かない）
	if err := write("Small", int16(1)); err == nil || !strings.Contains(err.Error(), "Bad") {
		t.Errorf("SINT（SByte）に Int16 を書けた: %v", err)
	}
}
