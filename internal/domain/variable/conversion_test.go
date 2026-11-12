package variable

import (
	"math"
	"strings"
	"testing"
)

// =====================================================================
// ConvertValue
// =====================================================================

func TestConvertValue_BOOL(t *testing.T) {
	// bool → bool
	v, err := ConvertValue(true, TypeBOOL)
	if err != nil || v != true {
		t.Errorf("ConvertValue(true, BOOL) = (%v, %v)", v, err)
	}
	// float64 → bool
	v, err = ConvertValue(float64(1), TypeBOOL)
	if err != nil || v != true {
		t.Errorf("ConvertValue(1.0, BOOL) = (%v, %v)", v, err)
	}
	v, err = ConvertValue(float64(0), TypeBOOL)
	if err != nil || v != false {
		t.Errorf("ConvertValue(0.0, BOOL) = (%v, %v)", v, err)
	}
}

func TestConvertValue_IntegerTypes(t *testing.T) {
	tests := []struct {
		input    interface{}
		dataType DataType
		want     interface{}
	}{
		// SINT
		{int8(-1), TypeSINT, int8(-1)},
		{float64(42), TypeSINT, int8(42)},
		// INT
		{int16(-100), TypeINT, int16(-100)},
		{float64(500), TypeINT, int16(500)},
		// DINT
		{int32(-100000), TypeDINT, int32(-100000)},
		{float64(100000), TypeDINT, int32(100000)},
		// USINT
		{uint8(200), TypeUSINT, uint8(200)},
		{float64(200), TypeUSINT, uint8(200)},
		// UINT
		{uint16(60000), TypeUINT, uint16(60000)},
		{float64(60000), TypeUINT, uint16(60000)},
		// UDINT
		{uint32(100000), TypeUDINT, uint32(100000)},
		{float64(100000), TypeUDINT, uint32(100000)},
		// REAL
		{float32(3.14), TypeREAL, float32(3.14)},
		{float64(3.14), TypeREAL, float32(float64(3.14))},
		// LREAL
		{float64(3.14), TypeLREAL, float64(3.14)},
		{float32(3.14), TypeLREAL, float64(float32(3.14))},
	}

	for _, tc := range tests {
		got, err := ConvertValue(tc.input, tc.dataType)
		if err != nil {
			t.Errorf("ConvertValue(%v, %s) error: %v", tc.input, tc.dataType, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ConvertValue(%v, %s) = %v (%T), want %v (%T)",
				tc.input, tc.dataType, got, got, tc.want, tc.want)
		}
	}
}

func TestConvertValue_STRING(t *testing.T) {
	// 通常文字列
	v, err := ConvertValue("hello", TypeSTRING)
	if err != nil || v != "hello" {
		t.Errorf("ConvertValue(hello, STRING) = (%v, %v)", v, err)
	}
}

func TestConvertValue_StringFixed(t *testing.T) {
	// 範囲内
	v, err := ConvertValue("hello", NewStringType(10))
	if err != nil || v != "hello" {
		t.Errorf("ConvertValue(hello, STRING[10]) = (%v, %v)", v, err)
	}
	// 切り捨て
	v, err = ConvertValue("hello world!", NewStringType(5))
	if err != nil || v != "hello" {
		t.Errorf("ConvertValue(hello world!, STRING[5]) = (%v, %v)", v, err)
	}
}

func TestConvertValue_TIME_DATE(t *testing.T) {
	// 時間・日付型は文字列のまま
	timeTypes := []DataType{TypeTIME, TypeDATE, TypeTIME_OF_DAY, TypeDATE_AND_TIME}
	for _, dt := range timeTypes {
		v, err := ConvertValue("somestring", dt)
		if err != nil || v != "somestring" {
			t.Errorf("ConvertValue(somestring, %s) = (%v, %v)", dt, v, err)
		}
	}
}

func TestConvertValue_Error(t *testing.T) {
	// 型不一致はエラー
	_, err := ConvertValue("not-a-bool", TypeBOOL)
	if err == nil {
		t.Error("ConvertValue(string, BOOL) should return error")
	}
	_, err = ConvertValue(true, TypeINT)
	if err == nil {
		t.Error("ConvertValue(bool, INT) should return error")
	}
}

// =====================================================================
// ValueToWords / WordsToValue ラウンドトリップ
// =====================================================================

func TestValueToWords_WordsToValue_Roundtrip(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		dt    DataType
	}{
		{"BOOL true", true, TypeBOOL},
		{"BOOL false", false, TypeBOOL},
		{"SINT -1", int8(-1), TypeSINT},
		{"SINT 127", int8(127), TypeSINT},
		{"INT -100", int16(-100), TypeINT},
		{"INT 32767", int16(32767), TypeINT},
		{"USINT 200", uint8(200), TypeUSINT},
		{"UINT 60000", uint16(60000), TypeUINT},
		{"DINT -100000", int32(-100000), TypeDINT},
		{"DINT 100000", int32(100000), TypeDINT},
		{"UDINT 100000", uint32(100000), TypeUDINT},
		{"REAL 3.14", float32(3.14), TypeREAL},
		{"REAL -1.5", float32(-1.5), TypeREAL},
		{"LREAL 3.14", float64(3.14), TypeLREAL},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			words := ValueToWords(tc.value, tc.dt, "big")
			got, err := WordsToValue(words, tc.dt, "big")
			if err != nil {
				t.Fatalf("WordsToValue error: %v", err)
			}
			if got != tc.value {
				t.Errorf("roundtrip: got %v (%T), want %v (%T)", got, got, tc.value, tc.value)
			}
		})
	}
}

// =====================================================================
// エンディアン（32ビット）
// =====================================================================

func TestValueToWords_Endianness_DINT(t *testing.T) {
	val := int32(0x12345678)
	wordsBig := ValueToWords(val, TypeDINT, "big")
	wordsLittle := ValueToWords(val, TypeDINT, "little")

	// big: 上位ワード先
	if wordsBig[0] != 0x1234 || wordsBig[1] != 0x5678 {
		t.Errorf("DINT big endian: got [0x%04X, 0x%04X], want [0x1234, 0x5678]", wordsBig[0], wordsBig[1])
	}
	// little: 下位ワード先
	if wordsLittle[0] != 0x5678 || wordsLittle[1] != 0x1234 {
		t.Errorf("DINT little endian: got [0x%04X, 0x%04X], want [0x5678, 0x1234]", wordsLittle[0], wordsLittle[1])
	}

	// 復元確認
	gotBig, _ := WordsToValue(wordsBig, TypeDINT, "big")
	gotLittle, _ := WordsToValue(wordsLittle, TypeDINT, "little")
	if gotBig != val {
		t.Errorf("DINT big roundtrip: got %v, want %v", gotBig, val)
	}
	if gotLittle != val {
		t.Errorf("DINT little roundtrip: got %v, want %v", gotLittle, val)
	}
}

func TestValueToWords_Endianness_REAL(t *testing.T) {
	val := float32(math.Pi)
	wordsBig := ValueToWords(val, TypeREAL, "big")
	wordsLittle := ValueToWords(val, TypeREAL, "little")

	// big と little でワード順が逆になっているはず
	if wordsBig[0] == wordsLittle[0] && wordsBig[1] == wordsLittle[1] {
		t.Error("REAL big/little endian words should differ")
	}

	// それぞれのエンディアンで復元できること
	gotBig, _ := WordsToValue(wordsBig, TypeREAL, "big")
	gotLittle, _ := WordsToValue(wordsLittle, TypeREAL, "little")
	if gotBig != val {
		t.Errorf("REAL big roundtrip: got %v, want %v", gotBig, val)
	}
	if gotLittle != val {
		t.Errorf("REAL little roundtrip: got %v, want %v", gotLittle, val)
	}
}

// =====================================================================
// エンディアン（64ビット）
// =====================================================================

func TestValueToWords_Endianness_LREAL(t *testing.T) {
	val := float64(math.Pi)
	wordsBig := ValueToWords(val, TypeLREAL, "big")
	wordsLittle := ValueToWords(val, TypeLREAL, "little")

	// 4ワード
	if len(wordsBig) != 4 || len(wordsLittle) != 4 {
		t.Fatalf("LREAL should produce 4 words, got %d/%d", len(wordsBig), len(wordsLittle))
	}

	// 復元確認
	gotBig, _ := WordsToValue(wordsBig, TypeLREAL, "big")
	gotLittle, _ := WordsToValue(wordsLittle, TypeLREAL, "little")
	if gotBig != val {
		t.Errorf("LREAL big roundtrip: got %v, want %v", gotBig, val)
	}
	if gotLittle != val {
		t.Errorf("LREAL little roundtrip: got %v, want %v", gotLittle, val)
	}
}

// =====================================================================
// STRING[n] ValueToWords / WordsToValue
// =====================================================================

func TestValueToWords_StringFixed(t *testing.T) {
	dt := NewStringType(6)
	val := "hello"
	words := ValueToWords(val, dt, "big")

	got, err := WordsToValue(words, dt, "big")
	if err != nil {
		t.Fatalf("WordsToValue error: %v", err)
	}
	if got != val {
		t.Errorf("STRING[6] roundtrip: got %q, want %q", got, val)
	}
}

func TestValueToWords_StringFixed_Padding(t *testing.T) {
	// 短い文字列 → ヌルパディング → 復元
	dt := NewStringType(10)
	val := "hi"
	words := ValueToWords(val, dt, "big")
	if len(words) != 5 { // ceil(10/2)=5
		t.Errorf("STRING[10] should produce 5 words, got %d", len(words))
	}
	got, err := WordsToValue(words, dt, "big")
	if err != nil || got != val {
		t.Errorf("STRING[10] roundtrip: got %q err %v, want %q", got, err, val)
	}
}

// =====================================================================
// ParseTIME / FormatTIME
// =====================================================================

func TestParseTIME(t *testing.T) {
	tests := []struct {
		input   string
		wantMs  int32
		wantErr bool
	}{
		{"T#0ms", 0, false},
		{"T#100ms", 100, false},
		{"T#1s", 1000, false},
		{"T#1m", 60000, false},
		{"T#1h", 3600000, false},
		{"T#1d", 86400000, false},
		{"T#1h30m", 5400000, false},
		{"T#1h30m45s500ms", 5445500, false},
		// 無効なフォーマット
		{"1h30m", 0, true},
		{"", 0, true},
		{"T#", 0, true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseTIME(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseTIME(%q) expected error, got nil (result=%d)", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTIME(%q) error: %v", tc.input, err)
			}
			if got != tc.wantMs {
				t.Errorf("ParseTIME(%q) = %d ms, want %d ms", tc.input, got, tc.wantMs)
			}
		})
	}
}

func TestFormatTIME(t *testing.T) {
	tests := []struct {
		ms   int32
		want string
	}{
		{0, "T#0ms"},
		{100, "T#100ms"},
		{1000, "T#1s"},
		{60000, "T#1m"},
		{3600000, "T#1h"},
		{5400000, "T#1h30m"},
		{5445500, "T#1h30m45s500ms"},
		{86400000, "T#1d"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			got := FormatTIME(tc.ms)
			if got != tc.want {
				t.Errorf("FormatTIME(%d) = %q, want %q", tc.ms, got, tc.want)
			}
		})
	}
}

func TestTIME_Roundtrip(t *testing.T) {
	// TIME型のValueToWords → WordsToValue ラウンドトリップ
	cases := []string{"T#0ms", "T#100ms", "T#1d", "T#1h30m", "T#1h30m45s500ms"}
	for _, s := range cases {
		words := ValueToWords(s, TypeTIME, "big")
		got, err := WordsToValue(words, TypeTIME, "big")
		if err != nil || got != s {
			t.Errorf("TIME roundtrip %q: got %q, err %v", s, got, err)
		}
	}
}

// =====================================================================
// ParseDATE / FormatDATE
// =====================================================================

func TestParseDATE(t *testing.T) {
	// Unix epoch
	ts, err := ParseDATE("D#1970-01-01")
	if err != nil || ts != 0 {
		t.Errorf("ParseDATE(D#1970-01-01) = (%d, %v), want (0, nil)", ts, err)
	}

	// 特定の日付
	ts2, err2 := ParseDATE("D#2024-01-15")
	if err2 != nil || ts2 == 0 {
		t.Errorf("ParseDATE(D#2024-01-15) error: %v", err2)
	}

	// 無効なフォーマット
	_, err3 := ParseDATE("2024-01-15")
	if err3 == nil {
		t.Error("ParseDATE without D# prefix should fail")
	}
	_, err4 := ParseDATE("D#invalid")
	if err4 == nil {
		t.Error("ParseDATE with invalid date should fail")
	}
}

func TestFormatDATE(t *testing.T) {
	// Unix epoch
	got := FormatDATE(0)
	if got != "D#1970-01-01" {
		t.Errorf("FormatDATE(0) = %q, want D#1970-01-01", got)
	}
}

func TestDATE_Roundtrip(t *testing.T) {
	cases := []string{"D#1970-01-01", "D#2024-01-15", "D#2000-12-31"}
	for _, s := range cases {
		words := ValueToWords(s, TypeDATE, "big")
		got, err := WordsToValue(words, TypeDATE, "big")
		if err != nil || got != s {
			t.Errorf("DATE roundtrip %q: got %q, err %v", s, got, err)
		}
	}
}

// =====================================================================
// ParseTIME_OF_DAY / FormatTIME_OF_DAY
// =====================================================================

func TestParseTIME_OF_DAY(t *testing.T) {
	tests := []struct {
		input   string
		wantMs  uint32
		wantErr bool
	}{
		{"TOD#00:00:00", 0, false},
		{"TOD#12:30:15", 45015000, false},
		{"TOD#23:59:59", 86399000, false},
		// 無効
		{"12:30:15", 0, true},
		{"TOD#25:00:00", 0, true},
		{"TOD#00:60:00", 0, true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseTIME_OF_DAY(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseTIME_OF_DAY(%q) expected error", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTIME_OF_DAY(%q) error: %v", tc.input, err)
			}
			if got != tc.wantMs {
				t.Errorf("ParseTIME_OF_DAY(%q) = %d ms, want %d ms", tc.input, got, tc.wantMs)
			}
		})
	}
}

func TestTIME_OF_DAY_Roundtrip(t *testing.T) {
	cases := []string{"TOD#00:00:00", "TOD#12:30:15", "TOD#23:59:59"}
	for _, s := range cases {
		words := ValueToWords(s, TypeTIME_OF_DAY, "big")
		got, err := WordsToValue(words, TypeTIME_OF_DAY, "big")
		if err != nil || got != s {
			t.Errorf("TIME_OF_DAY roundtrip %q: got %q, err %v", s, got, err)
		}
	}
}

// =====================================================================
// ParseDATE_AND_TIME / FormatDATE_AND_TIME
// =====================================================================

func TestParseDATE_AND_TIME(t *testing.T) {
	// Unix epoch
	ts, err := ParseDATE_AND_TIME("DT#1970-01-01-00:00:00")
	if err != nil || ts != 0 {
		t.Errorf("ParseDATE_AND_TIME(epoch) = (%d, %v), want (0, nil)", ts, err)
	}

	// 特定の日時
	ts2, err2 := ParseDATE_AND_TIME("DT#2024-01-15-10:30:00")
	if err2 != nil || ts2 == 0 {
		t.Errorf("ParseDATE_AND_TIME error: %v", err2)
	}

	// 無効なフォーマット
	_, err3 := ParseDATE_AND_TIME("2024-01-15-10:30:00")
	if err3 == nil {
		t.Error("ParseDATE_AND_TIME without DT# prefix should fail")
	}
}

func TestDATE_AND_TIME_Roundtrip(t *testing.T) {
	cases := []string{
		"DT#1970-01-01-00:00:00",
		"DT#2024-01-15-10:30:00",
		"DT#2000-06-15-12:00:00",
	}
	for _, s := range cases {
		words := ValueToWords(s, TypeDATE_AND_TIME, "big")
		got, err := WordsToValue(words, TypeDATE_AND_TIME, "big")
		if err != nil || got != s {
			t.Errorf("DATE_AND_TIME roundtrip %q: got %q, err %v", s, got, err)
		}
	}
}

// =====================================================================
// ValueToBool
// =====================================================================

func TestValueToBool(t *testing.T) {
	if !ValueToBool(true, TypeBOOL) {
		t.Error("ValueToBool(true, BOOL) = false, want true")
	}
	if ValueToBool(false, TypeBOOL) {
		t.Error("ValueToBool(false, BOOL) = true, want false")
	}
	// 非BOOLは最初のワードの非ゼロで判定
	if !ValueToBool(int16(1), TypeINT) {
		t.Error("ValueToBool(1, INT) = false, want true")
	}
	if ValueToBool(int16(0), TypeINT) {
		t.Error("ValueToBool(0, INT) = true, want false")
	}
}

// =====================================================================
// ArrayValueToWords / WordsToArrayValue
// =====================================================================

func TestArrayValueToWords_WordsToArrayValue_Roundtrip(t *testing.T) {
	// INT配列: [1, 2, 3]
	arr := []interface{}{int16(1), int16(2), int16(3)}
	elemType := TypeINT
	size := 3

	words := ArrayValueToWords(arr, elemType, size, "big", nil)
	if len(words) != 3 {
		t.Fatalf("ArrayValueToWords: expected 3 words, got %d", len(words))
	}

	got, err := WordsToArrayValue(words, elemType, size, "big", nil)
	if err != nil {
		t.Fatalf("WordsToArrayValue error: %v", err)
	}
	for i, g := range got {
		if g != arr[i] {
			t.Errorf("element[%d]: got %v, want %v", i, g, arr[i])
		}
	}
}

func TestArrayValueToWords_DINT(t *testing.T) {
	// DINT配列: 2ワード/要素 × 2要素 = 4ワード
	arr := []interface{}{int32(0x12345678), int32(-1)}
	words := ArrayValueToWords(arr, TypeDINT, 2, "big", nil)
	if len(words) != 4 {
		t.Fatalf("DINT array: expected 4 words, got %d", len(words))
	}
	// 復元
	got, err := WordsToArrayValue(words, TypeDINT, 2, "big", nil)
	if err != nil || got[0] != arr[0] || got[1] != arr[1] {
		t.Errorf("DINT array roundtrip failed: got %v, err %v", got, err)
	}
}

// =====================================================================
// StructValueToWords / WordsToStructValue
// =====================================================================

func TestStructValueToWords_WordsToStructValue_Roundtrip(t *testing.T) {
	// Point = {x: INT, y: INT} = 2ワード
	structDef := &StructTypeDef{
		Name: "Point",
		Fields: []StructField{
			{Name: "x", DataType: TypeINT, Offset: 0},
			{Name: "y", DataType: TypeINT, Offset: 1},
		},
		WordCount: 2,
	}

	val := map[string]interface{}{
		"x": int16(10),
		"y": int16(20),
	}

	words := StructValueToWords(val, structDef, "big", nil)
	if len(words) != 2 {
		t.Fatalf("Point struct: expected 2 words, got %d", len(words))
	}

	got, err := WordsToStructValue(words, structDef, "big", nil)
	if err != nil {
		t.Fatalf("WordsToStructValue error: %v", err)
	}
	if got["x"] != int16(10) || got["y"] != int16(20) {
		t.Errorf("Point roundtrip: got %v, want {x:10, y:20}", got)
	}
}

func TestStructValueToWords_MultipleTypes(t *testing.T) {
	// Sensor = {id: UINT(1), value: REAL(2)} = 3ワード
	structDef := &StructTypeDef{
		Name: "Sensor",
		Fields: []StructField{
			{Name: "id", DataType: TypeUINT, Offset: 0},
			{Name: "value", DataType: TypeREAL, Offset: 1},
		},
		WordCount: 3,
	}

	val := map[string]interface{}{
		"id":    uint16(42),
		"value": float32(3.14),
	}

	words := StructValueToWords(val, structDef, "big", nil)
	if len(words) != 3 {
		t.Fatalf("Sensor struct: expected 3 words, got %d", len(words))
	}

	got, err := WordsToStructValue(words, structDef, "big", nil)
	if err != nil {
		t.Fatalf("WordsToStructValue error: %v", err)
	}
	if got["id"] != uint16(42) {
		t.Errorf("Sensor.id: got %v, want 42", got["id"])
	}
	if got["value"] != float32(3.14) {
		t.Errorf("Sensor.value: got %v, want 3.14", got["value"])
	}
}

// =====================================================================
// WordsToValue エラーケース
// =====================================================================

func TestWordsToValue_EmptyWords(t *testing.T) {
	// 空のワード列はデフォルト値を返す
	v, err := WordsToValue([]uint16{}, TypeINT, "big")
	if err != nil {
		t.Fatalf("WordsToValue(empty, INT) error: %v", err)
	}
	if v != int16(0) {
		t.Errorf("WordsToValue(empty, INT) = %v, want 0", v)
	}
}

func TestWordsToValue_UnsupportedType(t *testing.T) {
	_, err := WordsToValue([]uint16{1}, DataType("Unknown"), "big")
	if err == nil {
		t.Error("WordsToValue with unknown type should return error")
	}
}

// MessagePack から読んだ符号なし整数は uint64 で届く（プラグインからの書き込み）
func TestConvertValue_FromUint64(t *testing.T) {
	cases := []struct {
		dataType DataType
		want     interface{}
	}{
		{TypeSINT, int8(100)},
		{TypeINT, int16(100)},
		{TypeDINT, int32(100)},
		{TypeLINT, int64(100)},
		{TypeUSINT, uint8(100)},
		{TypeUINT, uint16(100)},
		{TypeUDINT, uint32(100)},
		{TypeULINT, uint64(100)},
		{TypeREAL, float32(100)},
		{TypeLREAL, float64(100)},
	}
	for _, c := range cases {
		got, err := ConvertValue(uint64(100), c.dataType)
		if err != nil {
			t.Errorf("%s: %v", c.dataType, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %v (%T), want %v (%T)", c.dataType, got, got, c.want, c.want)
		}
	}
	// 型の最大値まで入る
	if got, _ := ConvertValue(uint64(255), TypeUSINT); got != uint8(255) {
		t.Errorf("USINT 255 → %v", got)
	}
	if got, _ := ConvertValue(uint64(65535), TypeUINT); got != uint16(65535) {
		t.Errorf("UINT 65535 → %v", got)
	}
	if got, _ := ConvertValue(uint64(4294967295), TypeUDINT); got != uint32(4294967295) {
		t.Errorf("UDINT 4294967295 → %v", got)
	}
}

// 構造体の値で、変換できないフィールドがあればエラーにする
// （以前は黙って既定値にしていて、書き込みが成功したように見えていた）
func TestConvertValueWithResolver_StructFieldError(t *testing.T) {
	store := NewVariableStore()
	inner, err := NewStructTypeDef("Inner", []StructField{{Name: "n", DataType: TypeINT}}, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterStructType(inner); err != nil {
		t.Fatal(err)
	}
	outer, err := NewStructTypeDef("Outer", []StructField{
		{Name: "count", DataType: TypeINT},
		{Name: "label", DataType: TypeSTRING},
		{Name: "inner", DataType: "Inner"},
		{Name: "list", DataType: "ARRAY[0..1] OF INT"},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterStructType(outer); err != nil {
		t.Fatal(err)
	}

	// 変換できる値はそのまま通る。ないフィールドは既定値になる
	got, err := ConvertValueWithResolver(map[string]interface{}{"count": float64(5), "label": "a"}, "Outer", store)
	if err != nil {
		t.Fatalf("変換できる値がエラーになった: %v", err)
	}
	if m := got.(map[string]interface{}); m["count"] != int16(5) || m["label"] != "a" {
		t.Errorf("変換結果 = %v", m)
	}

	// 変換できないフィールドは、どのフィールドかが分かるエラーになる
	cases := map[string]map[string]interface{}{
		"field count":          {"count": "数値ではない"},
		"field label":          {"label": float64(1)},
		"field inner: field n": {"inner": map[string]interface{}{"n": "x"}},
		"field list":           {"list": []interface{}{float64(1), "x"}},
		"field inner":          {"inner": "構造体ではない"},
	}
	for want, value := range cases {
		_, err := ConvertValueWithResolver(value, "Outer", store)
		if err == nil {
			t.Errorf("%v がエラーにならなかった", value)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%v のエラー = %q, %q を含むはず", value, err, want)
		}
	}
}

// 変数への書き込みでも、変換できないフィールドがあれば失敗し、値は変わらない
func TestUpdateValue_StructFieldErrorKeepsValue(t *testing.T) {
	store := NewVariableStore()
	point, _ := NewStructTypeDef("Point", []StructField{{Name: "x", DataType: TypeINT}, {Name: "y", DataType: TypeINT}}, store)
	_ = store.RegisterStructType(point)
	v, err := store.CreateVariable("P", "Point", map[string]interface{}{"x": float64(1), "y": float64(2)})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.UpdateValue(v.ID, map[string]interface{}{"x": float64(10), "y": "数値ではない"}); err == nil {
		t.Error("変換できないフィールドのある UpdateValue がエラーにならなかった")
	}
	if err := store.UpdateFieldValue(v.ID, "y", "数値ではない"); err == nil {
		t.Error("変換できない値の UpdateFieldValue がエラーにならなかった")
	}
	got, _ := store.GetVariable(v.ID)
	if m := got.Value.(map[string]interface{}); m["x"] != int16(1) || m["y"] != int16(2) {
		t.Errorf("失敗した書き込みで値が変わった: %v", m)
	}
}
