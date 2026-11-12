package variable

import (
	"encoding/json"
	"fmt"
	"testing"
)

// =====================================================================
// テストヘルパー
// =====================================================================

// mockChangeListener はChangeListenerのモック実装
type mockChangeListener struct {
	calls     int
	lastVar   *Variable
	lastMappings []ProtocolMapping
}

func (m *mockChangeListener) OnVariableChanged(v *Variable, mappings []ProtocolMapping) {
	m.calls++
	m.lastVar = v
	m.lastMappings = mappings
}

// mockTypeResolver はTypeResolverのモック実装（循環参照テスト用）
type mockTypeResolver struct {
	defs map[string]*StructTypeDef
}

func (m *mockTypeResolver) ResolveStructWordCount(name string) (int, error) {
	d, ok := m.defs[name]
	if !ok {
		return 0, fmt.Errorf("not found: %s", name)
	}
	return d.WordCount, nil
}

func (m *mockTypeResolver) ResolveStructDef(name string) (*StructTypeDef, error) {
	d, ok := m.defs[name]
	if !ok {
		return nil, fmt.Errorf("not found: %s", name)
	}
	return d, nil
}

// snapshotRoundtrip はスナップショットをJSON経由でRoundtripする（Restoreが []interface{} を期待するため）
func snapshotRoundtrip(data map[string]interface{}) (map[string]interface{}, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// =====================================================================
// NewVariableStore
// =====================================================================

func TestNewVariableStore(t *testing.T) {
	s := NewVariableStore()
	if s == nil {
		t.Fatal("NewVariableStore returned nil")
	}
	if len(s.GetAllVariables()) != 0 {
		t.Error("new store should have no variables")
	}
	if len(s.GetAllStructTypes()) != 0 {
		t.Error("new store should have no struct types")
	}
}

// =====================================================================
// CreateVariable - スカラー型
// =====================================================================

func TestVariableStore_CreateVariable_Scalar(t *testing.T) {
	s := NewVariableStore()

	v, err := s.CreateVariable("x", TypeINT, int16(42))
	if err != nil {
		t.Fatalf("CreateVariable error: %v", err)
	}
	if v.Name != "x" || v.DataType != TypeINT || v.Value != int16(42) {
		t.Errorf("variable: %+v", v)
	}
	if v.ID == "" {
		t.Error("variable ID should not be empty")
	}
}

func TestVariableStore_CreateVariable_DefaultValue(t *testing.T) {
	s := NewVariableStore()
	// initialValue=nil はデフォルト値を使う
	v, err := s.CreateVariable("flag", TypeBOOL, nil)
	if err != nil {
		t.Fatalf("CreateVariable error: %v", err)
	}
	if v.Value != false {
		t.Errorf("default BOOL value: got %v, want false", v.Value)
	}
}

func TestVariableStore_CreateVariable_DuplicateName(t *testing.T) {
	s := NewVariableStore()
	_, _ = s.CreateVariable("x", TypeINT, nil)
	_, err := s.CreateVariable("x", TypeINT, nil)
	if err == nil {
		t.Error("duplicate variable name should return error")
	}
}

func TestVariableStore_CreateVariable_InvalidType(t *testing.T) {
	s := NewVariableStore()
	_, err := s.CreateVariable("x", DataType("NonExistentType"), nil)
	if err == nil {
		t.Error("invalid data type should return error")
	}
}

// =====================================================================
// CreateVariable - 配列型
// =====================================================================

func TestVariableStore_CreateVariable_Array(t *testing.T) {
	s := NewVariableStore()
	dt := NewArrayType(TypeINT, 3)
	v, err := s.CreateVariable("arr", dt, nil)
	if err != nil {
		t.Fatalf("CreateVariable array error: %v", err)
	}
	arr, ok := v.Value.([]interface{})
	if !ok || len(arr) != 3 {
		t.Errorf("array value: got %T len=%d, want []interface{} len=3", v.Value, len(arr))
	}
	// デフォルト値はint16(0)
	for i, elem := range arr {
		if elem != int16(0) {
			t.Errorf("arr[%d] = %v, want int16(0)", i, elem)
		}
	}
}

// =====================================================================
// CreateVariable - 構造体型
// =====================================================================

func TestVariableStore_CreateVariable_Struct(t *testing.T) {
	s := NewVariableStore()
	def, _ := NewStructTypeDef("Point", []StructField{
		{Name: "x", DataType: TypeINT},
		{Name: "y", DataType: TypeINT},
	}, s)
	_ = s.RegisterStructType(def)

	v, err := s.CreateVariable("p", DataType("Point"), nil)
	if err != nil {
		t.Fatalf("CreateVariable struct error: %v", err)
	}
	m, ok := v.Value.(map[string]interface{})
	if !ok {
		t.Fatalf("struct value should be map[string]interface{}, got %T", v.Value)
	}
	if m["x"] != int16(0) || m["y"] != int16(0) {
		t.Errorf("struct default values: %v", m)
	}
}

func TestVariableStore_CreateVariable_UnknownStruct(t *testing.T) {
	s := NewVariableStore()
	_, err := s.CreateVariable("p", DataType("NoSuchStruct"), nil)
	if err == nil {
		t.Error("unknown struct type should return error")
	}
}

// =====================================================================
// GetVariable / GetVariableByName
// =====================================================================

func TestVariableStore_GetVariable(t *testing.T) {
	s := NewVariableStore()
	v, _ := s.CreateVariable("x", TypeINT, int16(10))

	// IDで取得
	got, err := s.GetVariable(v.ID)
	if err != nil || got.Name != "x" {
		t.Errorf("GetVariable: err=%v, got=%v", err, got)
	}

	// 存在しないID
	_, err2 := s.GetVariable("nonexistent-id")
	if err2 == nil {
		t.Error("GetVariable with unknown ID should return error")
	}
}

func TestVariableStore_GetVariableByName(t *testing.T) {
	s := NewVariableStore()
	_, _ = s.CreateVariable("myVar", TypeINT, nil)

	got, err := s.GetVariableByName("myVar")
	if err != nil || got.Name != "myVar" {
		t.Errorf("GetVariableByName: err=%v, got=%v", err, got)
	}

	_, err2 := s.GetVariableByName("notFound")
	if err2 == nil {
		t.Error("GetVariableByName with unknown name should return error")
	}
}

func TestVariableStore_GetAllVariables(t *testing.T) {
	s := NewVariableStore()
	_, _ = s.CreateVariable("a", TypeINT, nil)
	_, _ = s.CreateVariable("b", TypeBOOL, nil)
	_, _ = s.CreateVariable("c", TypeREAL, nil)

	all := s.GetAllVariables()
	if len(all) != 3 {
		t.Errorf("GetAllVariables: got %d, want 3", len(all))
	}
}

// =====================================================================
// UpdateValue
// =====================================================================

func TestVariableStore_UpdateValue(t *testing.T) {
	s := NewVariableStore()
	v, _ := s.CreateVariable("x", TypeINT, int16(0))

	// float64 → int16 への型変換
	err := s.UpdateValue(v.ID, float64(99))
	if err != nil {
		t.Fatalf("UpdateValue error: %v", err)
	}

	got, _ := s.GetVariable(v.ID)
	if got.Value != int16(99) {
		t.Errorf("UpdateValue: got %v, want int16(99)", got.Value)
	}
}

func TestVariableStore_UpdateValue_NotFound(t *testing.T) {
	s := NewVariableStore()
	err := s.UpdateValue("nonexistent-id", 0)
	if err == nil {
		t.Error("UpdateValue with unknown ID should return error")
	}
}

func TestVariableStore_UpdateValueByName(t *testing.T) {
	s := NewVariableStore()
	_, _ = s.CreateVariable("y", TypeDINT, int32(0))

	err := s.UpdateValueByName("y", float64(1000))
	if err != nil {
		t.Fatalf("UpdateValueByName error: %v", err)
	}

	got, _ := s.GetVariableByName("y")
	if got.Value != int32(1000) {
		t.Errorf("UpdateValueByName: got %v, want int32(1000)", got.Value)
	}
}

// =====================================================================
// DeleteVariable
// =====================================================================

func TestVariableStore_DeleteVariable(t *testing.T) {
	s := NewVariableStore()
	v, _ := s.CreateVariable("del", TypeINT, nil)

	err := s.DeleteVariable(v.ID)
	if err != nil {
		t.Fatalf("DeleteVariable error: %v", err)
	}

	_, err2 := s.GetVariable(v.ID)
	if err2 == nil {
		t.Error("variable should not be findable after deletion")
	}
	_, err3 := s.GetVariableByName("del")
	if err3 == nil {
		t.Error("variable should not be findable by name after deletion")
	}
}

func TestVariableStore_DeleteVariable_NotFound(t *testing.T) {
	s := NewVariableStore()
	err := s.DeleteVariable("does-not-exist")
	if err == nil {
		t.Error("DeleteVariable with unknown ID should return error")
	}
}

// =====================================================================
// GetMappings / SetMappings
// =====================================================================

func TestVariableStore_GetMappings_Initial(t *testing.T) {
	s := NewVariableStore()
	v, _ := s.CreateVariable("x", TypeINT, nil)

	mappings := s.GetMappings(v.ID)
	if mappings != nil {
		t.Errorf("initial mappings should be nil, got %v", mappings)
	}
}

func TestVariableStore_SetMappings(t *testing.T) {
	s := NewVariableStore()
	v, _ := s.CreateVariable("x", TypeINT, nil)

	mappings := []ProtocolMapping{
		{ProtocolType: "modbus", MemoryArea: "holdingRegisters", Address: 10, Endianness: "big"},
	}
	err := s.SetMappings(v.ID, mappings)
	if err != nil {
		t.Fatalf("SetMappings error: %v", err)
	}

	got := s.GetMappings(v.ID)
	if len(got) != 1 || got[0].Address != 10 {
		t.Errorf("GetMappings: got %v", got)
	}
}

func TestVariableStore_SetMappings_NotFound(t *testing.T) {
	s := NewVariableStore()
	err := s.SetMappings("nonexistent-id", nil)
	if err == nil {
		t.Error("SetMappings with unknown ID should return error")
	}
}

func TestVariableStore_SetMappings_NotifiesListeners(t *testing.T) {
	s := NewVariableStore()
	listener := &mockChangeListener{}
	s.AddListener(listener)

	v, _ := s.CreateVariable("x", TypeINT, nil)
	mappings := []ProtocolMapping{
		{ProtocolType: "modbus", MemoryArea: "holdingRegisters", Address: 5, Endianness: "big"},
	}
	_ = s.SetMappings(v.ID, mappings)

	if listener.calls == 0 {
		t.Error("SetMappings should notify listeners")
	}
}

// =====================================================================
// AddListener / RemoveListener
// =====================================================================

func TestVariableStore_Listener_UpdateValue(t *testing.T) {
	s := NewVariableStore()
	listener := &mockChangeListener{}
	s.AddListener(listener)

	v, _ := s.CreateVariable("x", TypeINT, nil)
	_ = s.UpdateValue(v.ID, float64(42))

	if listener.calls == 0 {
		t.Error("UpdateValue should notify listener")
	}
	if listener.lastVar == nil || listener.lastVar.Value != int16(42) {
		t.Errorf("listener received wrong value: %v", listener.lastVar)
	}
}

func TestVariableStore_RemoveListener(t *testing.T) {
	s := NewVariableStore()
	listener := &mockChangeListener{}
	s.AddListener(listener)
	s.RemoveListener(listener)

	v, _ := s.CreateVariable("x", TypeINT, nil)
	_ = s.UpdateValue(v.ID, float64(42))

	if listener.calls != 0 {
		t.Error("removed listener should not be notified")
	}
}

// =====================================================================
// FindVariableByMapping
// =====================================================================

func TestVariableStore_FindVariableByMapping_Found(t *testing.T) {
	s := NewVariableStore()
	v, _ := s.CreateVariable("x", TypeINT, nil) // 1ワード
	_ = s.SetMappings(v.ID, []ProtocolMapping{
		{ProtocolType: "modbus", MemoryArea: "holdingRegisters", Address: 10, Endianness: "big"},
	})

	found, mapping := s.FindVariableByMapping("modbus", "holdingRegisters", 10)
	if found == nil || found.Name != "x" {
		t.Errorf("FindVariableByMapping: found=%v, mapping=%v", found, mapping)
	}
}

func TestVariableStore_FindVariableByMapping_NotFound(t *testing.T) {
	s := NewVariableStore()
	v, _ := s.CreateVariable("x", TypeINT, nil)
	_ = s.SetMappings(v.ID, []ProtocolMapping{
		{ProtocolType: "modbus", MemoryArea: "holdingRegisters", Address: 10, Endianness: "big"},
	})

	// 異なるプロトコル
	found, _ := s.FindVariableByMapping("modbus-rtu", "holdingRegisters", 10)
	if found != nil {
		t.Error("should not find variable for different protocol")
	}

	// 範囲外アドレス（INT=1ワードなのでaddr=11はヒットしない）
	found2, _ := s.FindVariableByMapping("modbus", "holdingRegisters", 11)
	if found2 != nil {
		t.Error("should not find variable at out-of-range address")
	}
}

func TestVariableStore_FindVariableByMapping_AddressRange(t *testing.T) {
	s := NewVariableStore()
	// DINT は2ワード → アドレス10と11の両方でヒット
	v, _ := s.CreateVariable("val", TypeDINT, nil)
	_ = s.SetMappings(v.ID, []ProtocolMapping{
		{ProtocolType: "modbus", MemoryArea: "holdingRegisters", Address: 10, Endianness: "big"},
	})

	found10, _ := s.FindVariableByMapping("modbus", "holdingRegisters", 10)
	found11, _ := s.FindVariableByMapping("modbus", "holdingRegisters", 11)
	found12, _ := s.FindVariableByMapping("modbus", "holdingRegisters", 12)

	if found10 == nil || found10.Name != "val" {
		t.Error("address 10 should hit DINT variable")
	}
	if found11 == nil || found11.Name != "val" {
		t.Error("address 11 should hit DINT variable (2-word)")
	}
	if found12 != nil {
		t.Error("address 12 should NOT hit DINT variable")
	}
}

// =====================================================================
// StructType管理
// =====================================================================

func TestVariableStore_RegisterStructType(t *testing.T) {
	s := NewVariableStore()
	def, err := NewStructTypeDef("Point", []StructField{
		{Name: "x", DataType: TypeINT},
		{Name: "y", DataType: TypeINT},
	}, s)
	if err != nil {
		t.Fatalf("NewStructTypeDef error: %v", err)
	}

	err = s.RegisterStructType(def)
	if err != nil {
		t.Fatalf("RegisterStructType error: %v", err)
	}

	// 取得できる
	got, err := s.GetStructType("Point")
	if err != nil || got.Name != "Point" {
		t.Errorf("GetStructType: err=%v, got=%v", err, got)
	}
}

func TestVariableStore_RegisterStructType_Duplicate(t *testing.T) {
	s := NewVariableStore()
	def, _ := NewStructTypeDef("Foo", []StructField{{Name: "a", DataType: TypeINT}}, s)
	_ = s.RegisterStructType(def)

	// 同名を再登録→エラー
	def2, _ := NewStructTypeDef("Foo", []StructField{{Name: "b", DataType: TypeINT}}, s)
	// NewStructTypeDef は型名が既存と被っても作れるが（自身は未登録）、
	// 同じ名前の定義は存在確認なしに作成できる。RegisterStructType がチェックする。
	err := s.RegisterStructType(def2)
	if err == nil {
		t.Error("registering duplicate struct type should return error")
	}
}

func TestVariableStore_GetStructType_NotFound(t *testing.T) {
	s := NewVariableStore()
	_, err := s.GetStructType("NoSuchType")
	if err == nil {
		t.Error("GetStructType with unknown name should return error")
	}
}

func TestVariableStore_GetAllStructTypes(t *testing.T) {
	s := NewVariableStore()
	defA, _ := NewStructTypeDef("A", []StructField{{Name: "a", DataType: TypeINT}}, s)
	defB, _ := NewStructTypeDef("B", []StructField{{Name: "b", DataType: TypeINT}}, s)
	_ = s.RegisterStructType(defA)
	_ = s.RegisterStructType(defB)

	all := s.GetAllStructTypes()
	if len(all) != 2 {
		t.Errorf("GetAllStructTypes: got %d, want 2", len(all))
	}
}

func TestVariableStore_DeleteStructType(t *testing.T) {
	s := NewVariableStore()
	def, _ := NewStructTypeDef("TempType", []StructField{{Name: "v", DataType: TypeINT}}, s)
	_ = s.RegisterStructType(def)

	err := s.DeleteStructType("TempType")
	if err != nil {
		t.Fatalf("DeleteStructType error: %v", err)
	}

	_, err2 := s.GetStructType("TempType")
	if err2 == nil {
		t.Error("deleted struct type should not be retrievable")
	}
}

func TestVariableStore_DeleteStructType_InUse(t *testing.T) {
	s := NewVariableStore()
	def, _ := NewStructTypeDef("Pt", []StructField{{Name: "x", DataType: TypeINT}}, s)
	_ = s.RegisterStructType(def)
	_, _ = s.CreateVariable("myPt", DataType("Pt"), nil)

	// 使用中の型は削除できない
	err := s.DeleteStructType("Pt")
	if err == nil {
		t.Error("DeleteStructType of in-use type should return error")
	}
}

func TestVariableStore_DeleteStructType_NotFound(t *testing.T) {
	s := NewVariableStore()
	err := s.DeleteStructType("NoSuch")
	if err == nil {
		t.Error("DeleteStructType with unknown name should return error")
	}
}

// =====================================================================
// NewStructTypeDef - バリデーション
// =====================================================================

func TestNewStructTypeDef_Validation(t *testing.T) {
	s := NewVariableStore()

	// 名前なし
	_, err := NewStructTypeDef("", []StructField{{Name: "x", DataType: TypeINT}}, s)
	if err == nil {
		t.Error("empty name should return error")
	}

	// 予約語名
	_, err = NewStructTypeDef("INT", []StructField{{Name: "x", DataType: TypeINT}}, s)
	if err == nil {
		t.Error("reserved type name should return error")
	}

	// フィールドなし
	_, err = NewStructTypeDef("Empty", []StructField{}, s)
	if err == nil {
		t.Error("no fields should return error")
	}

	// フィールド名重複
	_, err = NewStructTypeDef("Dup", []StructField{
		{Name: "x", DataType: TypeINT},
		{Name: "x", DataType: TypeINT},
	}, s)
	if err == nil {
		t.Error("duplicate field names should return error")
	}

	// フィールド名なし
	_, err = NewStructTypeDef("EmptyField", []StructField{{Name: "", DataType: TypeINT}}, s)
	if err == nil {
		t.Error("empty field name should return error")
	}
}

func TestNewStructTypeDef_WordCount_Offsets(t *testing.T) {
	s := NewVariableStore()
	// MyStruct = {a: INT(1), b: DINT(2), c: BOOL(1)} = 4ワード
	def, err := NewStructTypeDef("MyStruct", []StructField{
		{Name: "a", DataType: TypeINT},
		{Name: "b", DataType: TypeDINT},
		{Name: "c", DataType: TypeBOOL},
	}, s)
	if err != nil {
		t.Fatalf("NewStructTypeDef error: %v", err)
	}

	if def.WordCount != 4 {
		t.Errorf("WordCount = %d, want 4", def.WordCount)
	}
	if def.Fields[0].Offset != 0 {
		t.Errorf("a.Offset = %d, want 0", def.Fields[0].Offset)
	}
	if def.Fields[1].Offset != 1 {
		t.Errorf("b.Offset = %d, want 1", def.Fields[1].Offset)
	}
	if def.Fields[2].Offset != 3 {
		t.Errorf("c.Offset = %d, want 3", def.Fields[2].Offset)
	}
}

func TestNewStructTypeDef_CyclicDependency(t *testing.T) {
	// A → B → A という循環参照を検出できることを確認
	// mockを使って A, B を事前定義
	mock := &mockTypeResolver{
		defs: map[string]*StructTypeDef{
			"A": {
				Name:      "A",
				Fields:    []StructField{{Name: "b", DataType: DataType("B"), Offset: 0}},
				WordCount: 1,
			},
			"B": {
				Name:      "B",
				Fields:    []StructField{{Name: "a", DataType: DataType("A"), Offset: 0}},
				WordCount: 1,
			},
		},
	}

	// C の field が A を参照 → A → B → A で循環
	_, err := NewStructTypeDef("C", []StructField{
		{Name: "a", DataType: DataType("A")},
	}, mock)
	if err == nil {
		t.Error("cyclic dependency should return error")
	}
}

// =====================================================================
// Snapshot / Restore ラウンドトリップ
// =====================================================================

func TestVariableStore_Snapshot_Restore_Scalar(t *testing.T) {
	s := NewVariableStore()
	v, _ := s.CreateVariable("x", TypeINT, float64(42))
	_ = s.SetMappings(v.ID, []ProtocolMapping{
		{ProtocolType: "modbus", MemoryArea: "holdingRegisters", Address: 5, Endianness: "big"},
	})

	snap := s.Snapshot()
	restored, err := snapshotRoundtrip(snap)
	if err != nil {
		t.Fatalf("JSON roundtrip error: %v", err)
	}

	s2 := NewVariableStore()
	if err := s2.Restore(restored); err != nil {
		t.Fatalf("Restore error: %v", err)
	}

	// 変数が復元されること
	got, err := s2.GetVariableByName("x")
	if err != nil {
		t.Fatalf("variable not restored: %v", err)
	}
	// JSON → float64 になるため ConvertValue で変換
	converted, _ := ConvertValue(got.Value, TypeINT)
	if converted != int16(42) {
		t.Errorf("restored value: got %v (%T), want int16(42)", converted, converted)
	}

	// マッピングが復元されること
	mappings := s2.GetMappings(got.ID)
	if len(mappings) != 1 || mappings[0].Address != 5 {
		t.Errorf("mappings not restored: %v", mappings)
	}
}

func TestVariableStore_Snapshot_Restore_StructType(t *testing.T) {
	s := NewVariableStore()
	def, _ := NewStructTypeDef("Pt", []StructField{
		{Name: "x", DataType: TypeINT},
		{Name: "y", DataType: TypeINT},
	}, s)
	_ = s.RegisterStructType(def)
	_, _ = s.CreateVariable("p", DataType("Pt"), nil)

	snap := s.Snapshot()
	restored, err := snapshotRoundtrip(snap)
	if err != nil {
		t.Fatalf("JSON roundtrip error: %v", err)
	}

	s2 := NewVariableStore()
	if err := s2.Restore(restored); err != nil {
		t.Fatalf("Restore error: %v", err)
	}

	// 構造体型が復元されること
	_, err = s2.GetStructType("Pt")
	if err != nil {
		t.Errorf("struct type Pt not restored: %v", err)
	}

	// 変数が復元されること
	_, err = s2.GetVariableByName("p")
	if err != nil {
		t.Errorf("variable p not restored: %v", err)
	}
}

// =====================================================================
// ClearAll
// =====================================================================

func TestVariableStore_ClearAll(t *testing.T) {
	s := NewVariableStore()
	_, _ = s.CreateVariable("a", TypeINT, nil)
	_, _ = s.CreateVariable("b", TypeBOOL, nil)
	def, _ := NewStructTypeDef("MyType", []StructField{{Name: "x", DataType: TypeINT}}, s)
	_ = s.RegisterStructType(def)

	s.ClearAll()

	if len(s.GetAllVariables()) != 0 {
		t.Error("ClearAll should remove all variables")
	}
	if len(s.GetAllStructTypes()) != 0 {
		t.Error("ClearAll should remove all struct types")
	}
}

// =====================================================================
// GetAllMappingsForProtocol
// =====================================================================

func TestVariableStore_GetAllMappingsForProtocol(t *testing.T) {
	s := NewVariableStore()
	v1, _ := s.CreateVariable("x", TypeINT, nil)
	v2, _ := s.CreateVariable("y", TypeINT, nil)

	_ = s.SetMappings(v1.ID, []ProtocolMapping{
		{ProtocolType: "modbus", MemoryArea: "holdingRegisters", Address: 10, Endianness: "big"},
	})
	_ = s.SetMappings(v2.ID, []ProtocolMapping{
		{ProtocolType: "modbus-rtu", MemoryArea: "holdingRegisters", Address: 20, Endianness: "big"},
		{ProtocolType: "modbus", MemoryArea: "holdingRegisters", Address: 30, Endianness: "big"},
	})

	modbusMap := s.GetAllMappingsForProtocol("modbus")
	if len(modbusMap) != 2 {
		t.Errorf("modbus mappings: got %d entries, want 2", len(modbusMap))
	}

	modbusRtuMap := s.GetAllMappingsForProtocol("modbus-rtu")
	if len(modbusRtuMap) != 1 {
		t.Errorf("modbus-rtu mappings: got %d entries, want 1", len(modbusRtuMap))
	}

	// 存在しないプロトコル
	emptyMap := s.GetAllMappingsForProtocol("unknown-protocol")
	if len(emptyMap) != 0 {
		t.Errorf("unknown-protocol mappings: got %d entries, want 0", len(emptyMap))
	}
}
