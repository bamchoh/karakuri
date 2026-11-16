package rtu

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// ファズテスト。外から届くバイト列で panic しない（panic するとアプリ全体が落ちる）ことと、応答の形が正しいことを確かめる。
// ふだんの go test では種（f.Add）だけを流す。探すときは go test -fuzz=FuzzRTUProcess -fuzztime=1m ./internal/protocols/modbus/rtu

// fuzzSeeds は、ファンクションコードごとの正しいリクエストと、境界・誤りのリクエスト（CRC を除いた中身）
var fuzzSeeds = []string{
	"01 01 00 00 00 08", "01 02 00 00 07 D0", "01 03 00 00 00 7D", "01 04 FF FF 00 01",
	"01 05 00 01 FF 00", "01 06 00 02 AB CD", "01 0F 00 04 00 03 01 05", "01 10 00 03 00 02 04 00 01 00 02",
	"01 03 00 00 00 00", "01 03 00 00 FF FF", "01 0F 00 00 FF FF 00", "01 10 00 00 00 7B F6",
	"01 2B 0E 01 00", "00 03 00 00 00 01", "01", "",
}

func seedBytes(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic(err)
	}
	return b
}

// RTU の 1 フレームの最大（Modbus over Serial Line V1.02 の 2.5.1.1）
const maxRTUFrame = 256

func FuzzRTUProcess(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(seedBytes(s), true)
	}
	f.Add(seedBytes("01 03 00 00 00 01 84 0A"), false) // CRC 付きのフレームをそのまま
	f.Fuzz(func(t *testing.T, data []byte, appendCRC bool) {
		frame := data
		if appendCRC {
			frame = AppendCRC(append([]byte(nil), data...))
		}
		_ = requestLength(frame)
		req, err := ParseRequest(frame)
		if err != nil {
			return
		}
		h := newMemHandler()
		h.disabled[7] = true
		res := NewProcessor(h).Process(req)
		if res == nil {
			if req.UnitID != 7 {
				t.Fatalf("UnitID %d のリクエストに応答しなかった: % X", req.UnitID, frame)
			}
			return
		}
		if !CheckCRC(res) {
			t.Fatalf("応答の CRC が正しくない: % X", res)
		}
		if len(res) > maxRTUFrame {
			t.Fatalf("応答が %d バイト（RTU の最大は %d）: リクエスト % X", len(res), maxRTUFrame, frame)
		}
		if res[0] != req.UnitID || res[1]&0x7F != req.FunctionCode&0x7F { // 0x80 以上のファンクションコード（仕様では使わない）は、例外応答と同じ値になる
			t.Fatalf("応答の UnitID・ファンクションコードがリクエストと違う: % X → % X", frame, res)
		}
		// 正常応答の読み取りなら、バイト数の欄が中身の長さと合う
		if res[1]&0x80 == 0 && req.FunctionCode >= FuncReadCoils && req.FunctionCode <= FuncReadInputRegisters {
			if int(res[2]) != len(res)-5 {
				t.Fatalf("バイト数の欄（%d）が中身の長さ（%d）と違う: % X", res[2], len(res)-5, res)
			}
		}
	})
}

func FuzzASCIIProcess(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(BuildASCIIFrame(seedBytes(s)))
	}
	f.Add([]byte(":010300000001FB\r\n"))
	f.Add([]byte(":0103\r\n"))
	f.Add([]byte(":ZZ\r\n"))
	f.Fuzz(func(t *testing.T, frame []byte) {
		req, err := ParseASCIIRequest(frame)
		if err != nil {
			return
		}
		res := NewProcessor(newMemHandler()).ProcessASCII(req)
		if res == nil {
			return
		}
		body, err := ParseASCIIFrame(res)
		if err != nil {
			t.Fatalf("応答が ASCII のフレームとして読めない: %q（%v）", res, err)
		}
		// ASCII のフレームは、RTU のフレーム（CRC を除く）を 16 進の文字にしたもの
		if len(body) > maxRTUFrame-2 {
			t.Fatalf("応答の中身が %d バイト: リクエスト %q", len(body), frame)
		}
		if !bytes.HasPrefix(body, []byte{req.UnitID}) {
			t.Fatalf("応答の UnitID がリクエストと違う: %q → %q", frame, res)
		}
	})
}
