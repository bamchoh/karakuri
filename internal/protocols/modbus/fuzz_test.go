package modbus

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"karakuri/internal/protocols/modbus/rtu"
)

// ファズテスト。外から届くバイト列で panic しない（panic するとアプリ全体が落ちる）ことと、出力の形が正しいことを確かめる。
// ふだんの go test では種（f.Add）だけを流す。探すときは go test -fuzz=FuzzTCPForwardRequests -fuzztime=1m ./internal/protocols/modbus

func seedBytes(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic(err)
	}
	return b
}

// Modbus TCP の中継は、届いたバイト列を MBAP ヘッダーで区切り、オフにした UnitID 宛て以外をそのまま送る。
// 送ったものは、区切りどおりのフレームが並んだものになる
func FuzzTCPForwardRequests(f *testing.F) {
	f.Add(seedBytes("00 01 00 00 00 06 01 03 00 00 00 01"))
	f.Add(seedBytes("00 01 00 00 00 06 05 03 00 00 00 01 00 02 00 00 00 06 01 03 00 00 00 01")) // オフにした UnitID 5 宛てと、1 宛て
	f.Add(seedBytes("00 01 00 00 00 00 01"))                                                    // 長さ 0
	f.Add(seedBytes("00 01 00 00 FF FF 01 03"))                                                 // 長さが大きく、途中で切れる
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		handler := NewDataStoreHandler(NewModbusDataStore(16, 16, 16, 16))
		handler.SetUnitIdEnabled(5, false)
		tr := &TCPTransport{handler: handler}
		var out bytes.Buffer
		tr.forwardRequests(bytes.NewReader(data), &out)

		// 送ったものを、同じ区切りで読み直す
		b := out.Bytes()
		for len(b) > 0 {
			if len(b) < mbapHeaderSize {
				t.Fatalf("フレームの途中で終わっている: % X", b)
			}
			n := mbapHeaderSize + max(int(binary.BigEndian.Uint16(b[4:6]))-1, 0)
			if len(b) < n {
				t.Fatalf("長さの欄より短い: % X", b)
			}
			if b[6] == 5 {
				t.Fatalf("オフにした UnitID 5 宛てを送った: % X", b[:n])
			}
			b = b[n:]
		}
		if out.Len() > len(data) {
			t.Fatalf("届いたより多く送った: %d > %d", out.Len(), len(data))
		}
	})
}

// RTU / ASCII のサーバーと同じ組み立て（rtu.Processor → RTUDataStoreAdapter → ModbusDataStore）で、
// でたらめなリクエストを処理しても panic せず、応答は RTU の 1 フレーム（256 バイト）に収まる
func FuzzRTUAdapter(f *testing.F) {
	for _, s := range []string{
		"01 01 00 00 00 08", "01 02 FF F0 00 10", "01 03 00 00 00 7D", "01 04 FF FF 00 01",
		"01 05 FF FF FF 00", "01 06 00 02 AB CD", "01 0F FF F8 00 08 01 FF", "01 10 FF FE 00 02 04 00 01 00 02",
	} {
		f.Add(seedBytes(s))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		req, err := rtu.ParseRequest(rtu.AppendCRC(append([]byte(nil), body...)))
		if err != nil {
			return
		}
		store := NewModbusDataStore(65536, 65536, 65536, 65536)
		res := rtu.NewProcessor(NewRTUDataStoreAdapter(NewDataStoreHandler(store))).Process(req)
		if res != nil && (len(res) > 256 || !rtu.CheckCRC(res)) {
			t.Fatalf("応答が正しくない（%d バイト）: % X → % X", len(res), body, res)
		}
	})
}

// クライアントが、機器から届いた応答を解釈する。リクエストも、利用者が 16 進で入れたものがそのまま来る
func FuzzClientDecodeResponse(f *testing.F) {
	pairs := [][2]string{
		{"03 00 00 00 02", "03 04 00 01 00 02"},
		{"01 00 00 00 0A", "01 02 FF 03"},
		{"02 00 00 00 08", "02 01 A5"},
		{"04 00 00 00 01", "04 02 12 34"},
		{"05 00 01 FF 00", "05 00 01 FF 00"},
		{"06 00 02 AB CD", "06 00 02 AB CD"},
		{"0F 00 00 00 03 01 05", "0F 00 00 00 03"},
		{"10 00 00 00 01 02 00 01", "10 00 00 00 01"},
		{"03 00 00 00 01", "83 02"},
		{"03 00 00 00 7D", "03 FA"},
		{"2B 0E 01 00", "2B 0E 01 01 00 00 01 00 03 41 42 43"},
		{"", ""},
	}
	for _, p := range pairs {
		f.Add(seedBytes(p[0]), seedBytes(p[1]))
	}
	f.Fuzz(func(t *testing.T, req, resp []byte) {
		_, _, _ = decodeResponse(req, resp)
	})
}

// RTU / ASCII のクライアントが、届いたフレームから UnitID と PDU を取り出す
func FuzzClientFramers(f *testing.F) {
	f.Add(rtu.AppendCRC(seedBytes("01 03 02 00 01")))
	f.Add(rtu.BuildASCIIFrame(seedBytes("01 03 02 00 01")))
	f.Add([]byte(":01\r\n"))
	f.Add([]byte("garbage:0103\r\n"))
	f.Fuzz(func(t *testing.T, frame []byte) {
		_, _, _ = rtuFramer{}.decode(frame)
		_, _, _ = asciiFramer{}.decode(frame)
		_ = rtuResponseLength(frame)
	})
}
