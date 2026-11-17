package application

import (
	"fmt"
	"strconv"
	"strings"

	"karakuri/internal/domain/variable"
)

// モニタリングの値のうち、中身がいくつもあるもの（構造体・配列の変数、値をいくつも返す接続先の項目）は、
// 中身を 1 つずつの行にして返す（画面は、項目の行の下に、字下げして並べる）

// 1 つの項目から作る行の上限（大きい配列で、画面が重くならないように）
const monitoringMaxRows = 200

// monitoringRows は、行を集める入れもの（上限を超えた分は、数だけ数える）
type monitoringRows struct {
	rows    []MonitoringValueRowDTO
	skipped int
}

func (r *monitoringRows) add(row MonitoringValueRowDTO) {
	if len(r.rows) >= monitoringMaxRows {
		r.skipped++
		return
	}
	r.rows = append(r.rows, row)
}

// result は、集めた行（上限を超えていれば、最後に「あと N 行」の行を付ける）
func (r *monitoringRows) result() []MonitoringValueRowDTO {
	if r.skipped > 0 {
		r.rows = append(r.rows, MonitoringValueRowDTO{Label: fmt.Sprintf("…あと %d 行", r.skipped), Depth: 1, More: true})
	}
	return r.rows
}

// isComposite は、値が構造体か配列か
func isComposite(value interface{}) bool {
	switch value.(type) {
	case map[string]interface{}, []interface{}:
		return true
	}
	return false
}

// variableRows は、構造体・配列の値 value（型は dt、変数の先頭からのパスは path）の中身を行にする。
// 構造体のフィールドは定義の順、配列の添字は宣言どおり（ARRAY[1..3] なら 1 から）
func (s *PLCService) variableRows(dt variable.DataType, value interface{}, path string) []MonitoringValueRowDTO {
	rows := &monitoringRows{}
	s.appendVariableRows(rows, dt, value, path, 1)
	return rows.result()
}

func (s *PLCService) appendVariableRows(rows *monitoringRows, dt variable.DataType, value interface{}, path string, depth int) {
	addChild := func(label, childPath string, childType variable.DataType, child interface{}) {
		row := MonitoringValueRowDTO{Path: childPath, Label: label, Depth: depth}
		if isComposite(child) {
			row.Composite = true
			rows.add(row)
			s.appendVariableRows(rows, childType, child, childPath, depth+1)
			return
		}
		row.Value = child
		row.Text = monitoringText(child)
		rows.add(row)
	}

	switch v := value.(type) {
	case []interface{}:
		elemType, _, err := variable.ParseArrayType(dt)
		if err != nil {
			elemType = ""
		}
		lower, _ := variable.ParseArrayLower(dt)
		for i, elem := range v {
			label := "[" + strconv.Itoa(lower+i) + "]"
			addChild(label, path+label, elemType, elem)
		}
	case map[string]interface{}:
		// フィールドは、型の定義の順に並べる（定義が引けなければ、値にあるものを名前の順で）
		var names []string
		types := map[string]variable.DataType{}
		if def, err := s.variableStore.TypeResolver().ResolveStructDef(string(dt)); err == nil && def != nil {
			for _, f := range def.Fields {
				if _, ok := v[f.Name]; ok {
					names = append(names, f.Name)
					types[f.Name] = f.DataType
				}
			}
		} else {
			names = sortedKeys(v)
		}
		for _, name := range names {
			childPath := name
			if path != "" {
				childPath = path + "." + name
			}
			addChild(name, childPath, types[name], v[name])
		}
	}
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// 数が少ないので、挿入ソートで足りる
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// typeAtPath は、型 dt の変数の、パス path（"motor.speed"、"[1].x"、"[1][2]"）のところの型を返す。
// たどれなければ空（そのときは、配列の添字を 0 から、フィールドを名前の順で出す）
func (s *PLCService) typeAtPath(dt variable.DataType, path string) variable.DataType {
	rest := path
	for rest != "" {
		switch {
		case strings.HasPrefix(rest, "["):
			end := strings.Index(rest, "]")
			if end < 0 {
				return ""
			}
			elemType, _, err := variable.ParseArrayType(dt)
			if err != nil {
				return ""
			}
			dt, rest = elemType, rest[end+1:]
		default:
			rest = strings.TrimPrefix(rest, ".")
			end := strings.IndexAny(rest, ".[")
			if end < 0 {
				end = len(rest)
			}
			name := rest[:end]
			def, err := s.variableStore.TypeResolver().ResolveStructDef(string(dt))
			if err != nil || def == nil {
				return ""
			}
			found := false
			for _, f := range def.Fields {
				if f.Name == name {
					dt, found = f.DataType, true
					break
				}
			}
			if !found {
				return ""
			}
			rest = rest[end:]
		}
	}
	return dt
}

// resultValueRows は、接続先から読んだ値がいくつもあるとき、1 つずつの行にする
func resultValueRows(values []ClientResultValueDTO) []MonitoringValueRowDTO {
	if len(values) <= 1 {
		return nil
	}
	rows := &monitoringRows{}
	for _, rv := range values {
		rows.add(MonitoringValueRowDTO{Label: rv.Label, Depth: 1, Text: rv.Value})
	}
	return rows.result()
}
