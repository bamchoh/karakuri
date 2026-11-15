package slmp

import (
	"encoding/binary"
	"fmt"
	"strings"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/fields"
)

// ランダム読み出し（0403）とランダム書き込み（1402）の組み立てと解釈。
// デバイスは、画面の「行を足せる表」（ConfigField の Type "rows"）で 1 行に 1 つずつ入れる。
//
//   - 単位は、ワード（1 ワード）・ダブルワード（2 ワード。下位のワードが先のデバイス番号）・ビット（1402 だけ）
//   - 要求では、ワードのデバイスを先に、ダブルワードのデバイスをあとに並べる（SLMP の決まり）。
//     入れた順と違っても、送るときはこの順にし、結果もこの順で出す
//   - 1402 は、すべての行がビットならビット単位（サブコマンド 0001）、そうでなければワード単位（0000）。
//     ビットとワード・ダブルワードは、同じコマンドで書けない
//   - サブコマンドは MELSEC-Q / L の形（デバイス番号 3 バイト）。MC プロトコル（シリアル）の形式 4 でも ASCII に直して送れる

const (
	commandRandomRead  = "randomRead"
	commandRandomWrite = "randomWrite"

	unitWordValue  = "word"
	unitDWordValue = "dword"
	unitBitValue   = "bit"
)

func randomDeviceColumns(units []protocol.FieldOption, withValue bool) []protocol.ConfigField {
	cols := []protocol.ConfigField{
		deviceField(),
		{Name: "address", Label: "デバイス番号", Type: "text", Required: true, Default: "0"},
		{Name: "unit", Label: "単位", Type: "select", Required: true, Default: unitWordValue, Options: units},
	}
	if withValue {
		cols = append(cols, protocol.ConfigField{Name: "value", Label: "値", Type: "text", Required: true, Default: "0"})
	}
	return cols
}

var (
	readUnits = []protocol.FieldOption{
		{Value: unitWordValue, Label: "ワード"},
		{Value: unitDWordValue, Label: "ダブルワード"},
	}
	writeUnits = []protocol.FieldOption{
		{Value: unitWordValue, Label: "ワード"},
		{Value: unitDWordValue, Label: "ダブルワード"},
		{Value: unitBitValue, Label: "ビット"},
	}
)

func randomCommands() []protocol.CommandDefinition {
	return []protocol.CommandDefinition{
		{ID: commandRandomRead, DisplayName: commandTitle(cmdRandomRead), Pollable: true, WordValues: true, Fields: []protocol.ConfigField{
			{Name: "items", Label: "読み出すデバイス", Description: fmt.Sprintf("1 行に 1 つのデバイス（合わせて %d 点まで）。ビットデバイス（M・X など）をワードで読むと、その番号から 16 点。ダブルワードは 2 ワード（下位のワードが先の番号）。送るときは、ワードのデバイスを先に並べます。", maxRandomPoints),
				Type: "rows", Required: true, Max: fields.IntPtr(maxRandomPoints), Columns: randomDeviceColumns(readUnits, false),
				Default: []interface{}{map[string]interface{}{"device": "D", "address": "0", "unit": unitWordValue}}},
		}},
		{ID: commandRandomWrite, DisplayName: commandTitle(cmdRandomWrite), Fields: []protocol.ConfigField{
			{Name: "items", Label: "書き込むデバイス", Description: fmt.Sprintf("1 行に 1 つのデバイスと値。ワード・ダブルワードは合わせて %d 点まで、ビット（0 / 1）は %d 点まで。ビットと、ワード・ダブルワードは、同じコマンドで書けません。値は 10 進（負の数も可）、0x で始めると 16 進。", maxRandomWrite, maxRandomBits),
				Type: "rows", Required: true, Max: fields.IntPtr(maxRandomWrite), Columns: randomDeviceColumns(writeUnits, true),
				Default: []interface{}{map[string]interface{}{"device": "D", "address": "0", "unit": unitWordValue, "value": "0"}}},
		}},
	}
}

// randomRow は、表の 1 行を読んだもの
type randomRow struct {
	t     target
	unit  string
	value string
}

func readRandomRows(params map[string]interface{}) ([]randomRow, error) {
	rows := fields.Rows(params, "items")
	if len(rows) == 0 {
		return nil, fmt.Errorf("デバイスを 1 つ以上入れてください")
	}
	out := make([]randomRow, 0, len(rows))
	for i, r := range rows {
		t, err := buildTarget(r)
		if err != nil {
			return nil, fmt.Errorf("%d 行目: %w", i+1, err)
		}
		unit := fields.String(r, "unit")
		if unit == "" {
			unit = unitWordValue
		}
		switch unit {
		case unitWordValue, unitDWordValue, unitBitValue:
		default:
			return nil, fmt.Errorf("%d 行目: 単位を選んでください", i+1)
		}
		value := fields.String(r, "value")
		if n, isNumber := fields.Number(r["value"]); isNumber && value == "" {
			value = fmt.Sprint(n)
		}
		out = append(out, randomRow{t: t, unit: unit, value: strings.TrimSpace(value)})
	}
	return out, nil
}

// splitByUnit は、行をワードとダブルワードに分ける（入れた順のまま）
func splitByUnit(rows []randomRow) (words, dwords []randomRow) {
	for _, r := range rows {
		if r.unit == unitDWordValue {
			dwords = append(dwords, r)
		} else {
			words = append(words, r)
		}
	}
	return words, dwords
}

func checkSpan(i int, r randomRow) error {
	n := 1
	if r.unit == unitDWordValue {
		n = 2
	}
	if wordSpan(r.t, n) > r.t.remaining() {
		return fmt.Errorf("%d 行目: %s がデバイスの範囲を超えています", i+1, r.t.label(0))
	}
	return nil
}

func buildRandomRead(params map[string]interface{}) ([]byte, error) {
	rows, err := readRandomRows(params)
	if err != nil {
		return nil, err
	}
	if len(rows) > maxRandomPoints {
		return nil, fmt.Errorf("デバイスは %d 点までにしてください（%d 点あります）", maxRandomPoints, len(rows))
	}
	for i, r := range rows {
		if r.unit == unitBitValue {
			return nil, fmt.Errorf("%d 行目: ランダム読み出しの単位は、ワードかダブルワードにしてください", i+1)
		}
		if err := checkSpan(i, r); err != nil {
			return nil, err
		}
	}
	words, dwords := splitByUnit(rows)
	req := appendCommand(nil, cmdRandomRead, 0)
	req = append(req, byte(len(words)), byte(len(dwords)))
	for _, r := range append(words, dwords...) {
		req = r.t.appendTo(req)
	}
	return req, nil
}

// parseDWord は、ダブルワードの値（-2147483648〜4294967295。0x で始めると 16 進）を読む
func parseDWord(s string) (uint32, error) {
	n, err := fields.ParseInteger(s)
	if err != nil {
		return 0, err
	}
	if n < -2147483648 || n > 4294967295 {
		return 0, fmt.Errorf("%s はダブルワードの範囲（-2147483648〜4294967295）の外です", s)
	}
	return uint32(n), nil
}

func parseBitValue(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "1", "on", "true":
		return true, nil
	case "0", "off", "false":
		return false, nil
	}
	return false, fmt.Errorf("ビットの値は 0 か 1 にしてください（%q）", s)
}

func buildRandomWrite(params map[string]interface{}) ([]byte, error) {
	rows, err := readRandomRows(params)
	if err != nil {
		return nil, err
	}
	bits := 0
	for _, r := range rows {
		if r.unit == unitBitValue {
			bits++
		}
	}
	if bits > 0 && bits != len(rows) {
		return nil, fmt.Errorf("ビットと、ワード・ダブルワードは、同じコマンドで書けません（コマンドを分けてください）")
	}

	if bits > 0 {
		if len(rows) > maxRandomBits {
			return nil, fmt.Errorf("ビットは %d 点までにしてください（%d 点あります）", maxRandomBits, len(rows))
		}
		req := appendCommand(nil, cmdRandomWrite, subBitUnits)
		req = append(req, byte(len(rows)))
		for i, r := range rows {
			if !r.t.def.IsBit {
				return nil, fmt.Errorf("%d 行目: %s はワードデバイスなので、ビットでは書けません", i+1, r.t.def.ID)
			}
			on, err := parseBitValue(r.value)
			if err != nil {
				return nil, fmt.Errorf("%d 行目: %w", i+1, err)
			}
			req = r.t.appendTo(req)
			if on {
				req = append(req, 0x01)
			} else {
				req = append(req, 0x00)
			}
		}
		return req, nil
	}

	if len(rows) > maxRandomWrite {
		return nil, fmt.Errorf("ワード・ダブルワードは %d 点までにしてください（%d 点あります）", maxRandomWrite, len(rows))
	}
	words, dwords := splitByUnit(rows)
	req := appendCommand(nil, cmdRandomWrite, 0)
	req = append(req, byte(len(words)), byte(len(dwords)))
	for i, r := range append(words, dwords...) {
		if err := checkSpan(i, r); err != nil {
			return nil, err
		}
		req = r.t.appendTo(req)
		if r.unit == unitDWordValue {
			v, err := parseDWord(r.value)
			if err != nil {
				return nil, fmt.Errorf("%s の値: %w", r.t.label(0), err)
			}
			req = binary.LittleEndian.AppendUint32(req, v)
			continue
		}
		v, err := fields.ParseWord(r.value)
		if err != nil {
			return nil, fmt.Errorf("%s の値: %w", r.t.label(0), err)
		}
		req = binary.LittleEndian.AppendUint16(req, v)
	}
	return req, nil
}

// randomReadTargets は、0403 のリクエスト（サブコマンド 0000）からデバイスを読む
func randomReadTargets(req []byte) (words, dwords []target, ok bool) {
	if len(req) < 6 || binary.LittleEndian.Uint16(req[2:]) != 0 {
		return nil, nil, false
	}
	nw, nd := int(req[4]), int(req[5])
	p := req[6:]
	if len(p) != (nw+nd)*4 {
		return nil, nil, false
	}
	for i := 0; i < nw+nd; i++ {
		t, end := parseTarget(p[i*4:], false)
		if end != endOK {
			return nil, nil, false
		}
		if i < nw {
			words = append(words, t)
		} else {
			dwords = append(dwords, t)
		}
	}
	return words, dwords, true
}

func describeRandomRead(req []byte) string {
	words, dwords, ok := randomReadTargets(req)
	if !ok {
		return commandTitle(cmdRandomRead)
	}
	return fmt.Sprintf("%s %s", commandTitle(cmdRandomRead), countsText(len(words), len(dwords), deviceList(words, dwords)))
}

func describeRandomWrite(req []byte) string {
	head := commandTitle(cmdRandomWrite)
	if len(req) < 5 {
		return head
	}
	switch binary.LittleEndian.Uint16(req[2:]) {
	case subBitUnits:
		return fmt.Sprintf("%s ビット %d 点", head, req[4])
	case 0:
		if len(req) >= 6 {
			return fmt.Sprintf("%s %s", head, countsText(int(req[4]), int(req[5]), ""))
		}
	}
	return head
}

func countsText(words, dwords int, list string) string {
	var parts []string
	if words > 0 {
		parts = append(parts, fmt.Sprintf("ワード %d 点", words))
	}
	if dwords > 0 {
		parts = append(parts, fmt.Sprintf("ダブルワード %d 点", dwords))
	}
	s := strings.Join(parts, "・")
	if list != "" {
		s += "（" + list + "）"
	}
	return s
}

// deviceList は、デバイスを並べた短い文字（多ければ省略する）
func deviceList(words, dwords []target) string {
	all := append(append([]target(nil), words...), dwords...)
	labels := make([]string, 0, 4)
	for i, t := range all {
		if i == 3 {
			labels = append(labels, "…")
			break
		}
		labels = append(labels, t.label(0))
	}
	return strings.Join(labels, ", ")
}

// decodeRandomRead は、0403 の応答のデータを、デバイスごとの値にする（ワードのデバイス、ダブルワードのデバイスの順）
func decodeRandomRead(req, data []byte) ([]protocol.ResultValue, string, bool) {
	words, dwords, ok := randomReadTargets(req)
	if !ok {
		return nil, "", false
	}
	if len(data) != len(words)*2+len(dwords)*4 {
		return nil, fmt.Sprintf("応答のデータが ワード %d 点・ダブルワード %d 点分ではありません（%d バイト）", len(words), len(dwords), len(data)), true
	}
	values := make([]protocol.ResultValue, 0, len(words)+len(dwords))
	for i, t := range words {
		w := binary.LittleEndian.Uint16(data[i*2:])
		label := t.label(0)
		if t.def.IsBit {
			label = fmt.Sprintf("%s から 16 点", t.label(0))
		}
		values = append(values, protocol.ResultValue{Label: label, Value: wordText(w), Word: &w})
	}
	p := data[len(words)*2:]
	for i, t := range dwords {
		v := binary.LittleEndian.Uint32(p[i*4:])
		label := t.label(0) + "（ダブルワード）"
		if t.def.IsBit {
			label = fmt.Sprintf("%s から 32 点", t.label(0))
		}
		values = append(values, protocol.ResultValue{Label: label, Value: dwordText(v)})
	}
	return values, "", true
}

// dwordText は、ダブルワードの値の表示（符号なし・符号付きが違えば両方・16 進）
func dwordText(v uint32) string {
	if int32(v) < 0 {
		return fmt.Sprintf("%d / %d (0x%08X)", v, int32(v), v)
	}
	return fmt.Sprintf("%d (0x%08X)", v, v)
}
