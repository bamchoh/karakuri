package slmp

import (
	"bytes"
	"strings"
	"testing"

	"karakuri/internal/domain/protocol"
)

func row(device, address, unit string, value ...string) map[string]interface{} {
	r := map[string]interface{}{"device": device, "address": address, "unit": unit}
	if len(value) > 0 {
		r["value"] = value[0]
	}
	return r
}

func rowsParams(rows ...map[string]interface{}) map[string]interface{} {
	items := make([]interface{}, len(rows))
	for i, r := range rows {
		items[i] = r
	}
	return map[string]interface{}{"items": items}
}

// マニュアル（SH-080008）の 0403 の通信例と同じ要求を、表の行から組み立てる。
// ダブルワードの行を途中に入れても、ワードのデバイスが先に並ぶ
func TestRandomRead_BuildsManualExample(t *testing.T) {
	f := &ClientFactory{}
	req, err := f.BuildRequest(commandRandomRead, rowsParams(
		row("D", "0", "word"),
		row("D", "1500", "dword"),
		row("TN", "0", "word"),
		row("Y", "160", "dword"),
		row("M", "100", "word"),
		row("M", "1111", "dword"),
		row("X", "20", "word"),
	))
	if err != nil {
		t.Fatal(err)
	}
	want := unhexMC(t, "03 04 00 00 04 03 00 00 00 A8 00 00 00 C2 64 00 00 90 20 00 00 9C DC 05 00 A8 60 01 00 9D 57 04 00 90")
	if !bytes.Equal(req, want) {
		t.Errorf("要求 % X, want % X", req, want)
	}
	if got := f.DescribeRequest(req); got != "0403 ランダム読み出し ワード 4 点・ダブルワード 3 点（D0, TN0, M100, …）" {
		t.Errorf("要約 %q", got)
	}

	// マニュアルの応答を読むと、デバイスごとの値になる
	data := unhexMC(t, "95 19 02 12 30 20 49 48 4E 4F 54 4C AF B9 DE C3 B7 BC DD BA")
	st, values, msg := decodeResponse(req, responseFrame(append([]byte{0, 0}, data...)))
	if st != protocol.CommandOK {
		t.Fatalf("%s %s", st, msg)
	}
	labels := []string{"D0", "TN0", "M100 から 16 点", "X20 から 16 点", "D1500（ダブルワード）", "Y160 から 32 点", "M1111 から 32 点"}
	if len(values) != len(labels) {
		t.Fatalf("値の数 %d", len(values))
	}
	for i, l := range labels {
		if values[i].Label != l {
			t.Errorf("%d: ラベル %q, want %q", i, values[i].Label, l)
		}
	}
	if *values[0].Word != 0x1995 || *values[1].Word != 0x1202 {
		t.Errorf("ワードの値 %+v", values[:2])
	}
	// D1500 = 4F4EH、D1501 = 4C54H → 4C544F4EH（1280593742）
	if values[4].Value != "1280593742 (0x4C544F4E)" || values[4].Word != nil {
		t.Errorf("ダブルワードの値 %+v", values[4])
	}
	// 符号付きで負になる値は、両方を出す
	if values[5].Value != "3286153647 / -1008813649 (0xC3DEB9AF)" {
		t.Errorf("負のダブルワード %q", values[5].Value)
	}

	// データの長さが合わなければエラー
	if st, _, _ := decodeResponse(req, responseFrame([]byte{0, 0, 1, 2})); st != protocol.CommandError {
		t.Errorf("短い応答で %s", st)
	}
}

// マニュアルの 1402（ビット単位）の通信例: M50 を OFF、Y2F を ON
func TestRandomWrite_BuildsManualBitExample(t *testing.T) {
	f := &ClientFactory{}
	req, err := f.BuildRequest(commandRandomWrite, rowsParams(row("M", "50", "bit", "0"), row("Y", "2F", "bit", "1")))
	if err != nil {
		t.Fatal(err)
	}
	if want := unhexMC(t, "02 14 01 00 02 32 00 00 90 00 2F 00 00 9D 01"); !bytes.Equal(req, want) {
		t.Errorf("要求 % X, want % X", req, want)
	}
	if got := f.DescribeRequest(req); got != "1402 ランダム書き込み ビット 2 点" {
		t.Errorf("要約 %q", got)
	}
}

func TestRandomWrite_WordsAndDWords(t *testing.T) {
	f := &ClientFactory{}
	req, err := f.BuildRequest(commandRandomWrite, rowsParams(
		row("D", "100", "dword", "70000"),
		row("D", "0", "word", "0x1234"),
		row("M", "16", "word", "-1"), // ビットデバイスを 16 点まとめて
		row("W", "1F", "dword", "-2"),
	))
	if err != nil {
		t.Fatal(err)
	}
	want := unhexMC(t, "02 14 00 00 02 02"+
		"00 00 00 A8 34 12"+ // D0 = 1234H
		"10 00 00 90 FF FF"+ // M16〜 = FFFFH
		"64 00 00 A8 70 11 01 00"+ // D100 = 70000
		"1F 00 00 B4 FE FF FF FF") // W1F = -2
	if !bytes.Equal(req, want) {
		t.Errorf("要求 % X, want % X", req, want)
	}
	if got := f.DescribeRequest(req); got != "1402 ランダム書き込み ワード 2 点・ダブルワード 2 点" {
		t.Errorf("要約 %q", got)
	}
}

func TestRandomCommands_Errors(t *testing.T) {
	f := &ClientFactory{}
	cases := []struct {
		name    string
		command string
		params  map[string]interface{}
		want    string
	}{
		{"行がない", commandRandomRead, map[string]interface{}{"items": []interface{}{}}, "1 つ以上"},
		{"デバイス番号が読めない", commandRandomRead, rowsParams(row("D", "x", "word")), "1 行目"},
		{"0403 にビット", commandRandomRead, rowsParams(row("M", "0", "bit")), "ワードかダブルワード"},
		{"範囲を超える", commandRandomRead, rowsParams(row("D", "16777215", "dword")), "範囲"},
		{"ビットとワードが混ざる", commandRandomWrite, rowsParams(row("M", "0", "bit", "1"), row("D", "0", "word", "1")), "同じコマンド"},
		{"ワードデバイスにビット", commandRandomWrite, rowsParams(row("D", "0", "bit", "1")), "ワードデバイス"},
		{"ビットの値", commandRandomWrite, rowsParams(row("M", "0", "bit", "2")), "0 か 1"},
		{"ダブルワードの範囲", commandRandomWrite, rowsParams(row("D", "0", "dword", "4294967296")), "範囲"},
		{"ワードの値", commandRandomWrite, rowsParams(row("D", "0", "word", "abc")), "D0 の値"},
	}
	for _, c := range cases {
		_, err := f.BuildRequest(c.command, c.params)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q を含む", c.name, err, c.want)
		}
	}
	// 点数の上限
	many := make([]map[string]interface{}, maxRandomPoints+1)
	for i := range many {
		many[i] = row("D", "0", "word")
	}
	if _, err := f.BuildRequest(commandRandomRead, rowsParams(many...)); err == nil {
		t.Error("上限を超えて組み立てられた")
	}
}

// サーバーとつないで、形式 4（ASCII）と形式 5 で、組み立てた 0403 / 1402 を送る
func TestRandomCommands_ThroughMCSerial(t *testing.T) {
	for _, v := range []struct {
		name   string
		format int
	}{{"形式4", mcFormat4}, {"形式5", mcFormat5}} {
		t.Run(v.name, func(t *testing.T) {
			p := startMCPair(t, v.format, true, 0, nil)

			r := p.send(t, commandRandomWrite, rowsParams(row("D", "10", "word", "4660"), row("D", "20", "dword", "70000")))
			expectMCStatus(t, r, protocol.CommandOK)
			if w, _ := p.store.ReadWords("D", 20, 2); w[0] != 0x1170 || w[1] != 0x0001 {
				t.Errorf("D20〜D21 = %v", w)
			}
			r = p.send(t, commandRandomWrite, rowsParams(row("M", "5", "bit", "1"), row("X", "1F", "bit", "1")))
			expectMCStatus(t, r, protocol.CommandOK)

			r = p.send(t, commandRandomRead, rowsParams(row("D", "20", "dword"), row("D", "10", "word"), row("M", "0", "word")))
			expectMCStatus(t, r, protocol.CommandOK)
			if len(r.Values) != 3 {
				t.Fatalf("値: %+v", r.Values)
			}
			if *r.Values[0].Word != 4660 || r.Values[0].Label != "D10" {
				t.Errorf("D10: %+v", r.Values[0])
			}
			if *r.Values[1].Word != 0x0020 { // M5 が ON
				t.Errorf("M0 から 16 点: %+v", r.Values[1])
			}
			if r.Values[2].Value != "70000 (0x00011170)" {
				t.Errorf("D20: %+v", r.Values[2])
			}
		})
	}
}
