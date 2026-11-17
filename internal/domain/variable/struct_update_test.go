package variable

import (
	"reflect"
	"sync"
	"testing"
)

type recordingListener struct {
	mu      sync.Mutex
	changed []string
}

func (l *recordingListener) OnVariableChanged(v *Variable, _ []ProtocolMapping) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.changed = append(l.changed, v.Name)
}

func (l *recordingListener) names() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.changed...)
}

func registerStruct(t *testing.T, s *VariableStore, name string, fields ...StructField) {
	t.Helper()
	def, err := NewStructTypeDef(name, fields, s.TypeResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterStructType(def); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateStructType_KeepsVariablesAndMatchingValues(t *testing.T) {
	s := NewVariableStore()
	registerStruct(t, s, "Motor",
		StructField{Name: "speed", DataType: "INT"},
		StructField{Name: "on", DataType: "BOOL"},
		StructField{Name: "old", DataType: "DINT"},
	)
	registerStruct(t, s, "Line",
		StructField{Name: "id", DataType: "INT"},
		StructField{Name: "motor", DataType: "Motor"},
		StructField{Name: "tail", DataType: "INT"},
	)
	m, err := s.CreateVariable("M1", "Motor", map[string]interface{}{"speed": 100, "on": true, "old": 5})
	if err != nil {
		t.Fatal(err)
	}
	arr, _ := s.CreateVariable("Motors", "ARRAY[1..2] OF Motor", nil)
	if err := s.UpdateFieldValue(arr.ID, "[2].speed", 7); err != nil {
		t.Fatal(err)
	}
	line, _ := s.CreateVariable("L1", "Line", nil)
	if err := s.UpdateFieldValue(line.ID, "motor.speed", 9); err != nil {
		t.Fatal(err)
	}
	other, _ := s.CreateVariable("Plain", "INT", 3)
	before := s.LayoutVersion()
	l := &recordingListener{}
	s.AddListener(l)

	// speed を DINT に広げ、old をなくし、limit を足し、順番も変える
	def, err := s.UpdateStructType("Motor", []StructField{
		{Name: "limit", DataType: "ARRAY[0..1] OF INT"},
		{Name: "on", DataType: "BOOL"},
		{Name: "speed", DataType: "DINT"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if def.WordCount != 5 || def.Fields[2].Offset != 3 {
		t.Fatalf("新しい定義: %+v", def)
	}
	if s.LayoutVersion() == before {
		t.Fatal("索引を作り直すよう、LayoutVersion を増やす")
	}

	// 名前の同じフィールドは値が残り、増えたフィールドは既定値、なくなったフィールドは消える
	got, _ := s.GetVariable(m.ID)
	want := map[string]interface{}{"limit": []interface{}{int16(0), int16(0)}, "on": true, "speed": int32(100)}
	if !reflect.DeepEqual(got.Value, want) {
		t.Fatalf("M1 = %#v, want %#v", got.Value, want)
	}
	// 配列の要素・ほかの構造体のフィールドとして使っている変数も直る
	if v, err := s.ReadFieldValue("Motors", "[2].speed"); err != nil || v != int32(7) {
		t.Fatalf("Motors[2].speed = %#v, %v", v, err)
	}
	if v, err := s.ReadFieldValue("L1", "motor.speed"); err != nil || v != int32(9) {
		t.Fatalf("L1.motor.speed = %#v, %v", v, err)
	}
	// この型を中に持つ型は、オフセットとワード数が変わる（Motor が 4 → 5 ワード）
	lineDef, _ := s.GetStructType("Line")
	if lineDef.WordCount != 7 || lineDef.Fields[2].Offset != 6 {
		t.Fatalf("Line の定義: %+v", lineDef)
	}
	// 使っている変数だけを知らせる
	changed := map[string]bool{}
	for _, n := range l.names() {
		changed[n] = true
	}
	if !changed["M1"] || !changed["Motors"] || !changed["L1"] || changed["Plain"] || len(changed) != 3 {
		t.Fatalf("知らせた変数: %v", l.names())
	}
	if v, _ := s.GetVariable(other.ID); v.Value != int16(3) {
		t.Fatalf("関係のない変数は変わらない: %#v", v.Value)
	}
}

func TestUpdateStructType_ValueThatNoLongerFitsBecomesDefault(t *testing.T) {
	s := NewVariableStore()
	registerStruct(t, s, "T", StructField{Name: "a", DataType: "DINT"}, StructField{Name: "b", DataType: "INT"})
	v, _ := s.CreateVariable("V", "T", map[string]interface{}{"a": 100000, "b": 2})

	// a を INT に狭める（100000 は収まらない）、b を文字列にする
	if _, err := s.UpdateStructType("T", []StructField{{Name: "a", DataType: "INT"}, {Name: "b", DataType: "STRING[4]"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetVariable(v.ID)
	m := got.Value.(map[string]interface{})
	if m["a"] != int16(0) {
		t.Fatalf("収まらない値は既定値: a = %#v", m["a"])
	}
	if _, ok := m["b"].(string); !ok {
		t.Fatalf("b は文字列になる: %#v", m["b"])
	}
}

func TestUpdateStructType_RejectsBadDefinitionsWithoutChanging(t *testing.T) {
	s := NewVariableStore()
	registerStruct(t, s, "Inner", StructField{Name: "x", DataType: "INT"})
	registerStruct(t, s, "Outer", StructField{Name: "inner", DataType: "Inner"})
	v, _ := s.CreateVariable("V", "Inner", map[string]interface{}{"x": 4})

	cases := map[string][]StructField{
		"フィールドなし":   {},
		"名前が重なる":    {{Name: "x", DataType: "INT"}, {Name: "x", DataType: "INT"}},
		"ない型":       {{Name: "x", DataType: "Nope"}},
		"自分を持つ":     {{Name: "self", DataType: "Inner"}},
		"循環（外側を持つ）": {{Name: "outer", DataType: "Outer"}},
		"循環（配列で）":   {{Name: "outer", DataType: "ARRAY[0..1] OF Outer"}},
	}
	for name, fields := range cases {
		if _, err := s.UpdateStructType("Inner", fields); err == nil {
			t.Fatalf("%s: エラーにならない", name)
		}
	}
	if _, err := s.UpdateStructType("Nope", []StructField{{Name: "x", DataType: "INT"}}); err == nil {
		t.Fatal("ない型の更新はエラー")
	}
	def, _ := s.GetStructType("Inner")
	if len(def.Fields) != 1 || def.Fields[0].Name != "x" {
		t.Fatalf("断ったあとも定義は変わらない: %+v", def)
	}
	if got, _ := s.GetVariable(v.ID); !reflect.DeepEqual(got.Value, map[string]interface{}{"x": int16(4)}) {
		t.Fatalf("断ったあとも値は変わらない: %#v", got.Value)
	}
}
