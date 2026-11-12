package variable

import (
	"fmt"
	"strconv"
	"strings"
)

// fieldPathSegment はフィールドパスの1要素
type fieldPathSegment struct {
	isIndex bool
	field   string
	index   int
}

// parseFieldPath はフィールドパス文字列を fieldPathSegment のスライスに分解する。
// インデックスは外部インデックス（表示ベース）。
// 例:
//
//	"speed"         → [{field:"speed"}]
//	"motor.speed"   → [{field:"motor"}, {field:"speed"}]
//	"items[2]"      → [{field:"items"}, {index:2}]
//	"items[2].name" → [{field:"items"}, {index:2}, {field:"name"}]
//	"[1]"           → [{index:1}]
func parseFieldPath(s string) []fieldPathSegment {
	var segs []fieldPathSegment
	for len(s) > 0 {
		switch s[0] {
		case '.':
			s = s[1:]
			end := strings.IndexAny(s, ".[")
			var name string
			if end < 0 {
				name, s = s, ""
			} else {
				name, s = s[:end], s[end:]
			}
			if name != "" {
				segs = append(segs, fieldPathSegment{field: name})
			}
		case '[':
			end := strings.Index(s, "]")
			if end < 0 {
				s = ""
				break
			}
			idx, _ := strconv.Atoi(s[1:end])
			segs = append(segs, fieldPathSegment{isIndex: true, index: idx})
			s = s[end+1:]
		default:
			// 先頭がフィールド名
			end := strings.IndexAny(s, ".[")
			var name string
			if end < 0 {
				name, s = s, ""
			} else {
				name, s = s[:end], s[end:]
			}
			if name != "" {
				segs = append(segs, fieldPathSegment{field: name})
			}
		}
	}
	return segs
}

// updateAtPath はルート値をパスに沿ってディープコピーしながら末端を newVal で置き換えた値を返す
func updateAtPath(root interface{}, path []fieldPathSegment, newVal interface{}) (interface{}, bool) {
	if len(path) == 0 {
		return newVal, true
	}
	seg := path[0]
	rest := path[1:]
	if seg.isIndex {
		arr, ok := root.([]interface{})
		if !ok {
			return nil, false
		}
		if seg.index < 0 || seg.index >= len(arr) {
			return nil, false
		}
		newArr := make([]interface{}, len(arr))
		copy(newArr, arr)
		updated, ok := updateAtPath(newArr[seg.index], rest, newVal)
		if !ok {
			return nil, false
		}
		newArr[seg.index] = updated
		return newArr, true
	}
	m, ok := root.(map[string]interface{})
	if !ok {
		return nil, false
	}
	newMap := make(map[string]interface{}, len(m))
	for k, v := range m {
		newMap[k] = v
	}
	updated, ok := updateAtPath(newMap[seg.field], rest, newVal)
	if !ok {
		return nil, false
	}
	newMap[seg.field] = updated
	return newMap, true
}

// resolveExternalPathToInternal は外部インデックス（表示ベース）のパスを
// 内部インデックス（0ベース）のパスに変換する。
// 例: "ARRAY[2..9] OF INT" の場合、index=3 → index=1 (3-2=1)
// ロック済みの状態で呼ぶこと（structTypes を参照するため）
func (s *VariableStore) resolveExternalPathToInternal(dataType DataType, path []fieldPathSegment) []fieldPathSegment {
	resolved := make([]fieldPathSegment, len(path))
	copy(resolved, path)
	current := dataType
	for i, seg := range path {
		if seg.isIndex {
			lower, isArray := ParseArrayLower(current)
			if isArray {
				resolved[i].index = seg.index - lower
			}
			// 配列要素型を追跡
			elemType, _, err := ParseArrayType(current)
			if err == nil {
				current = elemType
			}
		} else {
			// 構造体フィールドの型を追跡
			if current.IsStructType() {
				if st, ok := s.structTypes[string(current)]; ok {
					for _, f := range st.Fields {
						if f.Name == seg.field {
							current = f.DataType
							break
						}
					}
				}
			}
		}
	}
	return resolved
}

// getAtPath はパスに沿って値を辿り、末端の値を返す（内部インデックスを使用）
func getAtPath(root interface{}, path []fieldPathSegment) (interface{}, bool) {
	if len(path) == 0 {
		return root, true
	}
	seg := path[0]
	rest := path[1:]
	if seg.isIndex {
		arr, ok := root.([]interface{})
		if !ok {
			return nil, false
		}
		if seg.index < 0 || seg.index >= len(arr) {
			return nil, false
		}
		return getAtPath(arr[seg.index], rest)
	}
	m, ok := root.(map[string]interface{})
	if !ok {
		return nil, false
	}
	val, exists := m[seg.field]
	if !exists {
		return nil, false
	}
	return getAtPath(val, rest)
}

// ReadFieldValue は変数のフィールド/要素を外部パス（表示ベース）で読む。
// fieldPath は "motor.speed", "[2]", "[1].name" などの形式
func (s *VariableStore) ReadFieldValue(name, fieldPath string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, exists := s.byName[name]
	if !exists {
		return nil, fmt.Errorf("variable %s not found", name)
	}

	path := parseFieldPath(fieldPath)
	if len(path) == 0 {
		return nil, fmt.Errorf("field path is empty")
	}

	internalPath := s.resolveExternalPathToInternal(v.DataType, path)

	val, ok := getAtPath(v.Value, internalPath)
	if !ok {
		return nil, fmt.Errorf("failed to navigate path %q in variable %s", fieldPath, name)
	}
	return val, nil
}

// WriteFieldValueByName は名前で変数を検索してフィールド/要素を外部パス（表示ベース）で書く。
// fieldPath は "motor.speed", "[2]", "[1].name" などの形式
func (s *VariableStore) WriteFieldValueByName(name, fieldPath string, value interface{}) error {
	v, err := s.GetVariableByName(name)
	if err != nil {
		return err
	}
	return s.UpdateFieldValue(v.ID, fieldPath, value)
}

// UpdateFieldValue は変数の特定フィールド/要素のみをアトミックに更新する。
// fieldPath は外部インデックス（表示ベース）のパス文字列
// 例: "motor.speed", "items[1]"（ARRAY[1..10] の場合）, "items[2].name"
func (s *VariableStore) UpdateFieldValue(id, fieldPath string, value interface{}) error {
	s.mu.Lock()
	v, exists := s.variables[id]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("variable %s not found", id)
	}

	path := parseFieldPath(fieldPath)
	if len(path) == 0 {
		s.mu.Unlock()
		return fmt.Errorf("field path is empty")
	}

	// 外部インデックス（表示ベース）を内部インデックス（0ベース）に変換
	internalPath := s.resolveExternalPathToInternal(v.DataType, path)

	updated, ok := updateAtPath(v.Value, internalPath, value)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("failed to navigate path %q in variable %s", fieldPath, id)
	}

	converted, err := ConvertValueWithResolver(updated, v.DataType, s.unlocked())
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to convert updated value: %w", err)
	}

	v.Value = converted
	notify := s.prepareNotify(v, nil)
	s.mu.Unlock()
	notify()

	return nil
}

// ReadArrayElement は配列変数の要素を外部インデックス（表示ベース）で読む。
// 例: ARRAY[1..10] の場合、externalIndex=1 が最初の要素
func (s *VariableStore) ReadArrayElement(name string, externalIndex int) (interface{}, error) {
	s.mu.RLock()
	v, exists := s.byName[name]
	s.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("variable %s not found", name)
	}
	arr, ok := v.Value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("variable %s is not an array", name)
	}
	lower, _ := ParseArrayLower(v.DataType)
	internalIdx := externalIndex - lower
	if internalIdx < 0 || internalIdx >= len(arr) {
		return nil, fmt.Errorf("index %d out of range for array %s", externalIndex, name)
	}
	return arr[internalIdx], nil
}

// WriteArrayElement は配列変数の要素を外部インデックス（表示ベース）で書く。
// 例: ARRAY[1..10] の場合、externalIndex=1 が最初の要素
func (s *VariableStore) WriteArrayElement(name string, externalIndex int, value interface{}) error {
	v, err := s.GetVariableByName(name)
	if err != nil {
		return err
	}
	return s.UpdateFieldValue(v.ID, fmt.Sprintf("[%d]", externalIndex), value)
}
