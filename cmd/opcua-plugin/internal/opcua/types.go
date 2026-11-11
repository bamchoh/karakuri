package opcua

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/bamchoh/uastack/ua"
	"github.com/bamchoh/uastack/ua/id"
)

// ===== PLC データ型文字列の解析 =====

// parseArrayType は配列型文字列を解析して要素型・下限・要素数を返す。
//
//	"ARRAY[2..9] OF INT"       → ("INT", 2, 8, true)
//	"ARRAY[0..2, 0..4] OF INT" → ("ARRAY[0..4] OF INT", 0, 3, true)
//	"ARRAY[INT;10]"（旧形式）   → ("INT", 0, 10, true)
func parseArrayType(dataType string) (elemType string, lower, size int, isArray bool) {
	if !strings.HasPrefix(dataType, "ARRAY[") {
		return "", 0, 0, false
	}
	if ofIdx := strings.Index(dataType, "] OF "); ofIdx >= 0 {
		dims := strings.Split(dataType[len("ARRAY["):ofIdx], ",")
		elem := dataType[ofIdx+len("] OF "):]
		bounds := strings.SplitN(strings.TrimSpace(dims[0]), "..", 2)
		if len(bounds) != 2 {
			return "", 0, 0, false
		}
		lo, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
		up, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
		if err1 != nil || err2 != nil || up < lo {
			return "", 0, 0, false
		}
		if len(dims) > 1 {
			rest := make([]string, len(dims)-1)
			for i, d := range dims[1:] {
				rest[i] = strings.TrimSpace(d)
			}
			elem = fmt.Sprintf("ARRAY[%s] OF %s", strings.Join(rest, ", "), elem)
		}
		return elem, lo, up - lo + 1, true
	}
	if !strings.HasSuffix(dataType, "]") {
		return "", 0, 0, false
	}
	inner := dataType[len("ARRAY[") : len(dataType)-1]
	idx := strings.LastIndex(inner, ";")
	if idx < 0 {
		return "", 0, 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(inner[idx+1:]))
	if err != nil || n <= 0 {
		return "", 0, 0, false
	}
	return strings.TrimSpace(inner[:idx]), 0, n, true
}

// scalarBuiltin は PLC スカラー型に対応する OPC UA 組み込み型を返す。スカラー型でなければ false。
func scalarBuiltin(dataType string) (ua.TypeID, bool) {
	switch dataType {
	case "BOOL":
		return ua.TypeBoolean, true
	case "SINT":
		return ua.TypeSByte, true
	case "INT":
		return ua.TypeInt16, true
	case "DINT":
		return ua.TypeInt32, true
	case "LINT":
		return ua.TypeInt64, true
	case "USINT":
		return ua.TypeByte, true
	case "UINT":
		return ua.TypeUInt16, true
	case "UDINT":
		return ua.TypeUInt32, true
	case "ULINT":
		return ua.TypeUInt64, true
	case "REAL":
		return ua.TypeFloat, true
	case "LREAL":
		return ua.TypeDouble, true
	case "STRING", "TIME", "DATE", "TIME_OF_DAY", "DATE_AND_TIME":
		return ua.TypeString, true
	}
	if strings.HasPrefix(dataType, "STRING[") && strings.HasSuffix(dataType, "]") {
		return ua.TypeString, true
	}
	return ua.TypeNull, false
}

// isStructType はスカラーでも配列でもない型名（構造体型）かどうかを返す
func isStructType(dataType string) bool {
	if dataType == "" {
		return false
	}
	if _, ok := scalarBuiltin(dataType); ok {
		return false
	}
	_, _, _, isArr := parseArrayType(dataType)
	return !isArr
}

// nodeShape は PLC 型を OPC UA 変数ノードとして公開するときの形
type nodeShape struct {
	builtin ua.TypeID // 値の組み込み型（構造体・ネスト配列は String = JSON）
	array   bool      // 1 次元配列として公開するか
	size    int       // 配列の要素数
}

// shapeOf は PLC 型の公開形を返す。
// スカラー → 対応する組み込み型、スカラーの配列 → その型の配列、
// 構造体 → JSON 文字列、構造体やネスト配列の配列 → JSON 文字列の配列。
func shapeOf(dataType string) nodeShape {
	if t, ok := scalarBuiltin(dataType); ok {
		return nodeShape{builtin: t}
	}
	if elem, _, size, ok := parseArrayType(dataType); ok {
		if t, ok := scalarBuiltin(elem); ok {
			return nodeShape{builtin: t, array: true, size: size}
		}
		return nodeShape{builtin: ua.TypeString, array: true, size: size}
	}
	return nodeShape{builtin: ua.TypeString}
}

// dataTypeNodeID は組み込み型の DataType NodeId を返す
func dataTypeNodeID(t ua.TypeID) ua.NodeId {
	switch t {
	case ua.TypeBoolean:
		return ua.NewNumericNodeId(0, id.Boolean)
	case ua.TypeSByte:
		return ua.NewNumericNodeId(0, id.SByte)
	case ua.TypeByte:
		return ua.NewNumericNodeId(0, id.Byte)
	case ua.TypeInt16:
		return ua.NewNumericNodeId(0, id.Int16)
	case ua.TypeUInt16:
		return ua.NewNumericNodeId(0, id.UInt16)
	case ua.TypeInt32:
		return ua.NewNumericNodeId(0, id.Int32)
	case ua.TypeUInt32:
		return ua.NewNumericNodeId(0, id.UInt32)
	case ua.TypeInt64:
		return ua.NewNumericNodeId(0, id.Int64)
	case ua.TypeUInt64:
		return ua.NewNumericNodeId(0, id.UInt64)
	case ua.TypeFloat:
		return ua.NewNumericNodeId(0, id.Float)
	case ua.TypeDouble:
		return ua.NewNumericNodeId(0, id.Double)
	default:
		return ua.NewNumericNodeId(0, id.String)
	}
}

// ===== PLC 値 → OPC UA Variant =====

// toVariant は PLC の値（msgpack デコード後の値）を型に合った Variant に変換する。
// 値が欠けている場合はゼロ値を使う（Null の Variant は作らない）。
func toVariant(val interface{}, sh nodeShape) ua.Variant {
	if !sh.array {
		return ua.NewVariant(toScalar(val, sh.builtin))
	}
	elems, _ := val.([]interface{})
	get := func(i int) interface{} {
		if i < len(elems) {
			return elems[i]
		}
		return nil
	}
	n := sh.size
	if len(elems) > n {
		n = len(elems)
	}
	switch sh.builtin {
	case ua.TypeBoolean:
		return ua.NewVariant(fillSlice(n, get, toBool))
	case ua.TypeSByte:
		return ua.NewVariant(fillSlice(n, get, func(v interface{}) int8 { return int8(toInt64(v)) }))
	case ua.TypeByte:
		return ua.NewByteArrayVariant(fillSlice(n, get, func(v interface{}) uint8 { return uint8(toUint64(v)) }))
	case ua.TypeInt16:
		return ua.NewVariant(fillSlice(n, get, func(v interface{}) int16 { return int16(toInt64(v)) }))
	case ua.TypeUInt16:
		return ua.NewVariant(fillSlice(n, get, func(v interface{}) uint16 { return uint16(toUint64(v)) }))
	case ua.TypeInt32:
		return ua.NewVariant(fillSlice(n, get, func(v interface{}) int32 { return int32(toInt64(v)) }))
	case ua.TypeUInt32:
		return ua.NewVariant(fillSlice(n, get, func(v interface{}) uint32 { return uint32(toUint64(v)) }))
	case ua.TypeInt64:
		return ua.NewVariant(fillSlice(n, get, toInt64))
	case ua.TypeUInt64:
		return ua.NewVariant(fillSlice(n, get, toUint64))
	case ua.TypeFloat:
		return ua.NewVariant(fillSlice(n, get, func(v interface{}) float32 { return float32(toFloat64(v)) }))
	case ua.TypeDouble:
		return ua.NewVariant(fillSlice(n, get, toFloat64))
	default:
		return ua.NewVariant(fillSlice(n, get, toText))
	}
}

func fillSlice[T any](n int, get func(int) interface{}, conv func(interface{}) T) []T {
	out := make([]T, n)
	for i := range out {
		out[i] = conv(get(i))
	}
	return out
}

func toScalar(val interface{}, t ua.TypeID) interface{} {
	switch t {
	case ua.TypeBoolean:
		return toBool(val)
	case ua.TypeSByte:
		return int8(toInt64(val))
	case ua.TypeByte:
		return uint8(toUint64(val))
	case ua.TypeInt16:
		return int16(toInt64(val))
	case ua.TypeUInt16:
		return uint16(toUint64(val))
	case ua.TypeInt32:
		return int32(toInt64(val))
	case ua.TypeUInt32:
		return uint32(toUint64(val))
	case ua.TypeInt64:
		return toInt64(val)
	case ua.TypeUInt64:
		return toUint64(val)
	case ua.TypeFloat:
		return float32(toFloat64(val))
	case ua.TypeDouble:
		return toFloat64(val)
	default:
		return toText(val)
	}
}

// toText は文字列はそのまま、構造体・配列などは JSON 文字列にする
func toText(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func toBool(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		b, _ := strconv.ParseBool(x)
		return b
	}
	return toFloat64(v) != 0
}

func toInt64(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case uint64:
		return int64(x)
	case int:
		return int64(x)
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case uint:
		return int64(x)
	case uint8:
		return int64(x)
	case uint16:
		return int64(x)
	case uint32:
		return int64(x)
	case float32:
		return int64(x)
	case float64:
		return int64(x)
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

func toUint64(v interface{}) uint64 {
	switch x := v.(type) {
	case uint64:
		return x
	case int64:
		return uint64(x)
	case float32:
		return uint64(x)
	case float64:
		return uint64(x)
	}
	return uint64(toInt64(v))
}

func toFloat64(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case uint64:
		return float64(x)
	case bool:
		if x {
			return 1
		}
		return 0
	}
	return float64(toInt64(v))
}

// ===== OPC UA Variant → PLC 値 =====

// fromVariant はクライアントが書き込んだ Variant をホストの VariableStore に渡せる値に変換する。
// dataType はノードの PLC 型。構造体（JSON 文字列）はマップに、配列は []interface{} にする。
func fromVariant(v ua.Variant, dataType string) (interface{}, error) {
	val := v.Value()
	if !v.IsArray() {
		if s, ok := val.(string); ok && isStructType(dataType) {
			return parseJSON(s)
		}
		return val, nil
	}

	elemType, _, _, _ := parseArrayType(dataType)
	_, scalarElem := scalarBuiltin(elemType)
	var out []interface{}
	switch x := val.(type) {
	case []bool:
		out = toAnySlice(x)
	case []int8:
		out = toAnySlice(x)
	case []uint8:
		out = toAnySlice(x)
	case []int16:
		out = toAnySlice(x)
	case []uint16:
		out = toAnySlice(x)
	case []int32:
		out = toAnySlice(x)
	case []uint32:
		out = toAnySlice(x)
	case []int64:
		out = toAnySlice(x)
	case []uint64:
		out = toAnySlice(x)
	case []float32:
		out = toAnySlice(x)
	case []float64:
		out = toAnySlice(x)
	case []string:
		out = make([]interface{}, len(x))
		for i, s := range x {
			if scalarElem {
				out[i] = s
				continue
			}
			// 構造体やネスト配列の要素は JSON 文字列で受け取る
			parsed, err := parseJSON(s)
			if err != nil {
				return nil, fmt.Errorf("要素 %d: %w", i, err)
			}
			out[i] = parsed
		}
	default:
		return nil, fmt.Errorf("未対応の配列型: %T", val)
	}
	return out, nil
}

func toAnySlice[T any](s []T) []interface{} {
	out := make([]interface{}, len(s))
	for i, e := range s {
		out[i] = e
	}
	return out
}

func parseJSON(s string) (interface{}, error) {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("JSON として解釈できません: %w", err)
	}
	return v, nil
}
