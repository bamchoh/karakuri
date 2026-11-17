package variable

import "fmt"

// UpdateStructType は、登録済みの構造体型 name のフィールドを fields に置き換える。
// 型を使っている変数があっても変えられる（消して登録し直すのと違い、変数は残る）。
//
//   - この型を中に持つ構造体型のオフセットとワード数を計算し直す
//   - この型を使う変数（配列の要素・ほかの構造体のフィールドとして使うものも）の値を、新しい形に直す:
//     名前の同じフィールドの値は残し（型が変わって収まらなければ既定値）、増えたフィールドは既定値、なくなったフィールドは捨てる
//   - 値を直した変数は、リスナーに知らせる（マッピング先のレジスタに書き直すため）
//
// フィールドが正しくない・型が循環するときは、何も変えずにエラーを返す
func (s *VariableStore) UpdateStructType(name string, fields []StructField) (*StructTypeDef, error) {
	s.mu.Lock()
	if _, ok := s.structTypes[name]; !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("struct type %s not found", name)
	}
	// いまの定義のまま検査する（新しいフィールドが、この型を中に持つ型を指していれば、循環として見つかる）
	def, err := NewStructTypeDef(name, stripOffsets(fields), s.unlocked())
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.layoutVersion++
	s.structTypes[name] = def

	// この型を中に持つ型のオフセットとワード数を計算し直す。
	// 入れ子の深さの分だけくり返せば、外側の型まで行き渡る（型の数を超えることはない）
	for range s.structTypes {
		for typeName, st := range s.structTypes {
			if typeName == name {
				continue
			}
			if rebuilt, err := NewStructTypeDef(typeName, stripOffsets(st.Fields), s.unlocked()); err == nil {
				s.structTypes[typeName] = rebuilt
			}
		}
	}
	def = s.structTypes[name]

	// この型を使う変数の値を、新しい形に直す
	var notifies []func()
	for _, v := range s.variables {
		if !s.dependsOnStruct(v.DataType, name, nil) {
			continue
		}
		v.Value = s.migrateValue(v.Value, v.DataType)
		notifies = append(notifies, s.prepareNotify(v, nil))
	}
	result := *def
	s.mu.Unlock()

	for _, notify := range notifies {
		notify()
	}
	return &result, nil
}

// stripOffsets は、フィールドの名前と型だけを写す（オフセットは NewStructTypeDef が計算する）
func stripOffsets(fields []StructField) []StructField {
	out := make([]StructField, len(fields))
	for i, f := range fields {
		out[i] = StructField{Name: f.Name, DataType: f.DataType}
	}
	return out
}

// dependsOnStruct は、型 dt が、構造体型 name を（配列の要素・フィールドとして、入れ子も含めて）使っているか（ロック済みで呼ぶ）
func (s *VariableStore) dependsOnStruct(dt DataType, name string, visited map[string]bool) bool {
	if dt.IsArrayType() {
		elemType, _, err := ParseArrayType(dt)
		if err != nil {
			return false
		}
		return s.dependsOnStruct(elemType, name, visited)
	}
	if !dt.IsStructType() {
		return false
	}
	if string(dt) == name {
		return true
	}
	if visited == nil {
		visited = map[string]bool{}
	}
	if visited[string(dt)] {
		return false
	}
	visited[string(dt)] = true
	st, ok := s.structTypes[string(dt)]
	if !ok {
		return false
	}
	for _, f := range st.Fields {
		if s.dependsOnStruct(f.DataType, name, visited) {
			return true
		}
	}
	return false
}

// migrateValue は、値 old を型 dt の形に直す（ロック済みで呼ぶ）。
// 形の合うところは値を残し、合わないところ・足りないところは既定値にする。もとの値は書き換えない
func (s *VariableStore) migrateValue(old interface{}, dt DataType) interface{} {
	fallback := func() interface{} {
		v, err := s.generateDefaultValue(dt)
		if err != nil {
			return old
		}
		return v
	}
	switch {
	case dt.IsArrayType():
		elemType, size, err := ParseArrayType(dt)
		arr, ok := old.([]interface{})
		if err != nil || !ok {
			return fallback()
		}
		out := make([]interface{}, size)
		for i := range out {
			if i < len(arr) {
				out[i] = s.migrateValue(arr[i], elemType)
			} else if v, err := s.generateDefaultValue(elemType); err == nil {
				out[i] = v
			}
		}
		return out
	case dt.IsStructType():
		st, found := s.structTypes[string(dt)]
		m, ok := old.(map[string]interface{})
		if !found || !ok {
			return fallback()
		}
		out := make(map[string]interface{}, len(st.Fields))
		for _, f := range st.Fields {
			if fv, exists := m[f.Name]; exists {
				out[f.Name] = s.migrateValue(fv, f.DataType)
			} else if v, err := s.generateDefaultValue(f.DataType); err == nil {
				out[f.Name] = v
			}
		}
		return out
	default:
		converted, err := ConvertValue(old, dt)
		if err != nil {
			return fallback()
		}
		return converted
	}
}
