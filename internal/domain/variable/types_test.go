package variable

import (
	"testing"
)

// =====================================================================
// DataType.WordCount
// =====================================================================

func TestDataType_WordCount(t *testing.T) {
	tests := []struct {
		dt   DataType
		want int
	}{
		// 1ワード型
		{TypeBOOL, 1},
		{TypeSINT, 1},
		{TypeUSINT, 1},
		{TypeINT, 1},
		{TypeUINT, 1},
		// 2ワード型
		{TypeDINT, 2},
		{TypeUDINT, 2},
		{TypeREAL, 2},
		{TypeTIME, 2},
		{TypeTIME_OF_DAY, 2},
		// 4ワード型
		{TypeLREAL, 4},
		{TypeDATE, 4},
		{TypeDATE_AND_TIME, 4},
		// STRING（後方互換）
		{TypeSTRING, 1},
		// STRING[n]: ceil(n/2)
		{NewStringType(1), 1},  // ceil(1/2)=1
		{NewStringType(2), 1},  // ceil(2/2)=1
		{NewStringType(3), 2},  // ceil(3/2)=2
		{NewStringType(4), 2},  // ceil(4/2)=2
		{NewStringType(5), 3},  // ceil(5/2)=3
		{NewStringType(10), 5}, // ceil(10/2)=5
	}

	for _, tc := range tests {
		tc := tc
		t.Run(string(tc.dt), func(t *testing.T) {
			got := tc.dt.WordCount()
			if got != tc.want {
				t.Errorf("DataType(%q).WordCount() = %d, want %d", tc.dt, got, tc.want)
			}
		})
	}
}

// =====================================================================
// DataType.IsBitType
// =====================================================================

func TestDataType_IsBitType(t *testing.T) {
	if !TypeBOOL.IsBitType() {
		t.Error("BOOL.IsBitType() = false, want true")
	}
	notBit := []DataType{TypeSINT, TypeINT, TypeDINT, TypeUSINT, TypeUINT, TypeUDINT,
		TypeREAL, TypeLREAL, TypeSTRING, TypeTIME, TypeDATE, TypeTIME_OF_DAY, TypeDATE_AND_TIME,
		NewStringType(10), NewArrayType(TypeINT, 3), DataType("MyStruct")}
	for _, dt := range notBit {
		if dt.IsBitType() {
			t.Errorf("DataType(%q).IsBitType() = true, want false", dt)
		}
	}
}

// =====================================================================
// DataType.IsStringType
// =====================================================================

func TestDataType_IsStringType_Valid(t *testing.T) {
	valid := []DataType{NewStringType(1), NewStringType(10), NewStringType(100), NewStringType(255)}
	for _, dt := range valid {
		if !dt.IsStringType() {
			t.Errorf("DataType(%q).IsStringType() = false, want true", dt)
		}
	}
}

func TestDataType_IsStringType_Invalid(t *testing.T) {
	invalid := []DataType{
		TypeSTRING,    // 長さなし
		"STRING[]",    // 空の括弧
		"STRING[abc]", // 数値でない
		"STRING[10",   // 閉じ括弧なし
		TypeINT,
		DataType("MyStruct"),
	}
	for _, dt := range invalid {
		if dt.IsStringType() {
			t.Errorf("DataType(%q).IsStringType() = true, want false", dt)
		}
	}
	// NOTE: "STRING[-1]" は IsStringType() が true を返す（Atoi("-1")が成功するため）。
	// ただし ParseStringType は maxLen<=0 でエラーを返し、WordCount/DefaultValue では無効として扱われる。
}

// =====================================================================
// ParseStringType
// =====================================================================

func TestParseStringType_Valid(t *testing.T) {
	tests := []struct {
		dt      DataType
		wantLen int
	}{
		{NewStringType(1), 1},
		{NewStringType(10), 10},
		{NewStringType(100), 100},
	}
	for _, tc := range tests {
		n, err := ParseStringType(tc.dt)
		if err != nil || n != tc.wantLen {
			t.Errorf("ParseStringType(%q) = (%d, %v), want (%d, nil)", tc.dt, n, err, tc.wantLen)
		}
	}
}

func TestParseStringType_Invalid(t *testing.T) {
	invalid := []DataType{TypeSTRING, "STRING[]", "STRING[abc]", TypeINT, "STRING[0]"}
	for _, dt := range invalid {
		_, err := ParseStringType(dt)
		if err == nil {
			t.Errorf("ParseStringType(%q) expected error, got nil", dt)
		}
	}
}

// =====================================================================
// NewStringType
// =====================================================================

func TestNewStringType(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "STRING[1]"},
		{20, "STRING[20]"},
		{255, "STRING[255]"},
	}
	for _, tc := range tests {
		dt := NewStringType(tc.n)
		if string(dt) != tc.want {
			t.Errorf("NewStringType(%d) = %q, want %q", tc.n, dt, tc.want)
		}
	}
}

// =====================================================================
// DataType.DefaultValue
// =====================================================================

func TestDataType_DefaultValue(t *testing.T) {
	tests := []struct {
		dt   DataType
		want interface{}
	}{
		{TypeBOOL, false},
		{TypeSINT, int8(0)},
		{TypeINT, int16(0)},
		{TypeDINT, int32(0)},
		{TypeUSINT, uint8(0)},
		{TypeUINT, uint16(0)},
		{TypeUDINT, uint32(0)},
		{TypeREAL, float32(0)},
		{TypeLREAL, float64(0)},
		{TypeSTRING, ""},
		{TypeTIME, "T#0ms"},
		{TypeDATE, "D#1970-01-01"},
		{TypeTIME_OF_DAY, "TOD#00:00:00"},
		{TypeDATE_AND_TIME, "DT#1970-01-01-00:00:00"},
		{NewStringType(10), ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(string(tc.dt), func(t *testing.T) {
			got := tc.dt.DefaultValue()
			if got != tc.want {
				t.Errorf("DataType(%q).DefaultValue() = %v (%T), want %v (%T)",
					tc.dt, got, got, tc.want, tc.want)
			}
		})
	}

	// 不明な型はnil
	if DataType("UnknownType").DefaultValue() != nil {
		t.Error("unknown type DefaultValue() should return nil")
	}
}

// =====================================================================
// DataType.IsValid
// =====================================================================

func TestDataType_IsValid(t *testing.T) {
	// 全スカラー型は有効
	for _, dt := range AllDataTypes() {
		if !dt.IsValid() {
			t.Errorf("DataType(%q).IsValid() = false, want true", dt)
		}
	}
	// STRING[n] も有効
	if !NewStringType(10).IsValid() {
		t.Error("STRING[10].IsValid() = false, want true")
	}
	// 未知の型は無効
	for _, dt := range []DataType{"MyStruct", "UNKNOWN", ""} {
		if dt.IsValid() {
			t.Errorf("DataType(%q).IsValid() = true, want false", dt)
		}
	}
}

// =====================================================================
// DataType.IsArrayType
// =====================================================================

func TestDataType_IsArrayType(t *testing.T) {
	valid := []DataType{
		NewArrayType(TypeINT, 5),
		NewArrayType(TypeDINT, 3),
		NewArrayType(NewStringType(10), 4),
		NewArrayType(DataType("MyStruct"), 2),
	}
	for _, dt := range valid {
		if !dt.IsArrayType() {
			t.Errorf("DataType(%q).IsArrayType() = false, want true", dt)
		}
	}

	invalid := []DataType{TypeINT, NewStringType(10), DataType("MyStruct"), TypeBOOL}
	for _, dt := range invalid {
		if dt.IsArrayType() {
			t.Errorf("DataType(%q).IsArrayType() = true, want false", dt)
		}
	}
}

// =====================================================================
// DataType.IsStructType
// =====================================================================

func TestDataType_IsStructType(t *testing.T) {
	valid := []DataType{"MyStruct", "Point", "MyComplexType"}
	for _, dt := range valid {
		if !dt.IsStructType() {
			t.Errorf("DataType(%q).IsStructType() = false, want true", dt)
		}
	}

	// スカラー、配列、STRINGは構造体ではない
	invalid := []DataType{TypeINT, TypeBOOL, TypeREAL, NewStringType(10), NewArrayType(TypeINT, 3)}
	for _, dt := range invalid {
		if dt.IsStructType() {
			t.Errorf("DataType(%q).IsStructType() = true, want false", dt)
		}
	}
}

// =====================================================================
// ParseArrayType
// =====================================================================

func TestParseArrayType_Valid(t *testing.T) {
	tests := []struct {
		dt       DataType
		wantElem DataType
		wantSize int
	}{
		{NewArrayType(TypeINT, 10), TypeINT, 10},
		{NewArrayType(TypeDINT, 3), TypeDINT, 3},
		{NewArrayType(TypeBOOL, 100), TypeBOOL, 100},
		{NewArrayType(DataType("MyStruct"), 5), DataType("MyStruct"), 5},
	}
	for _, tc := range tests {
		elem, size, err := ParseArrayType(tc.dt)
		if err != nil || elem != tc.wantElem || size != tc.wantSize {
			t.Errorf("ParseArrayType(%q) = (%s, %d, %v), want (%s, %d, nil)",
				tc.dt, elem, size, err, tc.wantElem, tc.wantSize)
		}
	}
}

func TestParseArrayType_Invalid(t *testing.T) {
	invalid := []DataType{
		TypeINT,                // 配列でない
		"ARRAY[]",              // 中身なし
		"ARRAY[0..9]",          // " OF " なし
		"ARRAY[0..abc] OF INT", // 上限が数値でない
		"ARRAY[0..-1] OF INT",  // 上限 < 下限
		// 旧形式の無効ケース
		"ARRAY[INT;abc]", // サイズが数値でない
		"ARRAY[INT;-1]",  // 負のサイズ
		"ARRAY[INT;0]",   // ゼロサイズ
	}
	for _, dt := range invalid {
		_, _, err := ParseArrayType(dt)
		if err == nil {
			t.Errorf("ParseArrayType(%q) expected error, got nil", dt)
		}
	}
}

// =====================================================================
// NewArrayType
// =====================================================================

func TestNewArrayType(t *testing.T) {
	tests := []struct {
		elem DataType
		size int
		want string
	}{
		{TypeINT, 5, "ARRAY[0..4] OF INT"},
		{TypeDINT, 10, "ARRAY[0..9] OF DINT"},
		{DataType("MyStruct"), 3, "ARRAY[0..2] OF MyStruct"},
		// 多次元: 内側配列をフラット化
		{NewArrayType(TypeINT, 5), 3, "ARRAY[0..2, 0..4] OF INT"},
	}
	for _, tc := range tests {
		dt := NewArrayType(tc.elem, tc.size)
		if string(dt) != tc.want {
			t.Errorf("NewArrayType(%s, %d) = %q, want %q", tc.elem, tc.size, dt, tc.want)
		}
	}
}

// =====================================================================
// DataType.WordCountWithResolver
// =====================================================================

func TestDataType_WordCountWithResolver_Scalar(t *testing.T) {
	tests := []struct {
		dt   DataType
		want int
	}{
		{TypeBOOL, 1},
		{TypeINT, 1},
		{TypeDINT, 2},
		{TypeLREAL, 4},
	}
	for _, tc := range tests {
		got := tc.dt.WordCountWithResolver(nil)
		if got != tc.want {
			t.Errorf("DataType(%q).WordCountWithResolver(nil) = %d, want %d", tc.dt, got, tc.want)
		}
	}
}

func TestDataType_WordCountWithResolver_Array(t *testing.T) {
	// ARRAY[0..2] OF DINT = 2 * 3 = 6ワード
	arrType := NewArrayType(TypeDINT, 3)
	got := arrType.WordCountWithResolver(nil)
	if got != 6 {
		t.Errorf("%q.WordCountWithResolver = %d, want 6", arrType, got)
	}

	// ARRAY[0..1] OF LREAL = 4 * 2 = 8ワード
	arrType2 := NewArrayType(TypeLREAL, 2)
	got2 := arrType2.WordCountWithResolver(nil)
	if got2 != 8 {
		t.Errorf("%q.WordCountWithResolver = %d, want 8", arrType2, got2)
	}
}

func TestDataType_WordCountWithResolver_Struct(t *testing.T) {
	store := NewVariableStore()
	// Point = {x: INT(1), y: INT(1)} = 2ワード
	def, err := NewStructTypeDef("Point", []StructField{
		{Name: "x", DataType: TypeINT},
		{Name: "y", DataType: TypeINT},
	}, store)
	if err != nil {
		t.Fatalf("NewStructTypeDef failed: %v", err)
	}
	if err := store.RegisterStructType(def); err != nil {
		t.Fatalf("RegisterStructType failed: %v", err)
	}

	// resolverありで解決
	got := DataType("Point").WordCountWithResolver(store)
	if got != 2 {
		t.Errorf("Point.WordCountWithResolver(store) = %d, want 2", got)
	}

	// resolverなしの場合、構造体型は WordCount() のデフォルト値 1 を返す
	got2 := DataType("Point").WordCountWithResolver(nil)
	if got2 != 1 {
		t.Errorf("Point.WordCountWithResolver(nil) = %d, want 1 (fallthrough to WordCount())", got2)
	}
}

func TestDataType_WordCountWithResolver_StructArray(t *testing.T) {
	store := NewVariableStore()
	// Vec3 = {x: DINT(2), y: DINT(2), z: DINT(2)} = 6ワード
	def, _ := NewStructTypeDef("Vec3", []StructField{
		{Name: "x", DataType: TypeDINT},
		{Name: "y", DataType: TypeDINT},
		{Name: "z", DataType: TypeDINT},
	}, store)
	_ = store.RegisterStructType(def)

	// ARRAY[0..3] OF Vec3 = 6 * 4 = 24ワード
	arrType := NewArrayType(DataType("Vec3"), 4)
	got := arrType.WordCountWithResolver(store)
	if got != 24 {
		t.Errorf("%q.WordCountWithResolver = %d, want 24", arrType, got)
	}
}
