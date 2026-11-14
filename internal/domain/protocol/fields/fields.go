// Package fields は、プロトコルの実装が、設定やコマンドの項目の値（map[string]interface{}）を読むための関数を持つ。
//
// 画面から来た値は JSON を通るので数値は float64、Go のコードから来た値は int になる。どちらも受け付ける。
// プロトコルの実装同士は import し合わないので、共通に使う関数はここに置く。
package fields

import (
	"fmt"
	"strconv"
	"strings"
)

// IntPtr は、ConfigField の Min / Max に入れるポインタを作る
func IntPtr(i int) *int {
	return &i
}

// SetString は、settings[key] が文字列なら dst に入れる
func SetString(settings map[string]interface{}, key string, dst *string) {
	if v, ok := settings[key].(string); ok {
		*dst = v
	}
}

// SetInt は、settings[key] が数値なら dst に入れる
func SetInt(settings map[string]interface{}, key string, dst *int) {
	switch v := settings[key].(type) {
	case float64:
		*dst = int(v)
	case int:
		*dst = v
	}
}

// String は、文字列の項目を読む（文字列でなければ空）
func String(values map[string]interface{}, key string) string {
	s, _ := values[key].(string)
	return s
}

// Number は、数値（float64・int・int64）を int64 にする。整数でない float64 は受け付けない
func Number(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n != float64(int64(n)) {
			return 0, false
		}
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

// Int は数値の項目を読み、min〜max に入っているかを確かめる（文字列なら 10 進・0x・0b を受け付ける）。
// label は、エラーメッセージに入れる項目の名前
func Int(values map[string]interface{}, key, label string, min, max int) (int, error) {
	v, ok := values[key]
	if !ok || v == nil {
		return 0, fmt.Errorf("%sを入力してください", label)
	}
	var n int64
	if s, isStr := v.(string); isStr {
		parsed, err := ParseInteger(s)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", label, err)
		}
		n = parsed
	} else if num, isNum := Number(v); isNum {
		n = num
	} else {
		return 0, fmt.Errorf("%sが数値ではありません", label)
	}
	if n < int64(min) || n > int64(max) {
		return 0, fmt.Errorf("%sは %d〜%d で指定してください", label, min, max)
	}
	return int(n), nil
}

// ParseInteger は 10 進・16 進（0x）・2 進（0b）の整数を読む。負の数も受け付ける
func ParseInteger(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("値を入力してください")
	}
	neg := false
	body := s
	if strings.HasPrefix(body, "-") {
		neg = true
		body = body[1:]
	}
	base := 10
	lower := strings.ToLower(body)
	switch {
	case strings.HasPrefix(lower, "0x"):
		base, body = 16, body[2:]
	case strings.HasPrefix(lower, "0b"):
		base, body = 2, body[2:]
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(body, "_", ""), base, 64)
	if err != nil {
		return 0, fmt.Errorf("数値として読めません: %q", s)
	}
	if neg {
		n = -n
	}
	return n, nil
}

// ParseWord は 1 ワードの値を読む（-32768〜65535。負の数は 2 の補数にする）
func ParseWord(s string) (uint16, error) {
	n, err := ParseInteger(s)
	if err != nil {
		return 0, err
	}
	if n < -32768 || n > 65535 {
		return 0, fmt.Errorf("%q は 1 ワードに入りません（-32768〜65535）", strings.TrimSpace(s))
	}
	return uint16(n), nil
}

// SplitList は、カンマ・空白・改行で区切った並びを分ける
func SplitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '、'
	})
}

// ParseWordList は、ワードの値の並びを読む
func ParseWordList(s string) ([]uint16, error) {
	parts := SplitList(s)
	words := make([]uint16, 0, len(parts))
	for _, p := range parts {
		w, err := ParseWord(p)
		if err != nil {
			return nil, err
		}
		words = append(words, w)
	}
	return words, nil
}

// ParseBitList は、ビットの値（1 / 0、on / off、true / false）の並びを読む
func ParseBitList(s string) ([]bool, error) {
	parts := SplitList(s)
	bits := make([]bool, 0, len(parts))
	for _, p := range parts {
		switch strings.ToLower(p) {
		case "1", "on", "true":
			bits = append(bits, true)
		case "0", "off", "false":
			bits = append(bits, false)
		default:
			return nil, fmt.Errorf("%q は 1 か 0 ではありません", p)
		}
	}
	return bits, nil
}
