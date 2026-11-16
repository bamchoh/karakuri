package fins

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// ファズテスト。外から届くバイト列で panic しない（panic するとアプリ全体が落ちる）ことと、応答の形が正しいことを確かめる。
// ふだんの go test では種（f.Add）だけを流す。探すときは go test -fuzz=FuzzHandler -fuzztime=1m ./internal/protocols/fins

func seedBytes(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic(err)
	}
	return b
}

// fuzzCommands は、コマンドコードから後ろ（パラメーター）の種
var fuzzCommands = []string{
	"01 01 82 00 64 00 00 02", "01 01 30 00 64 04 00 03", "01 01 B1 FF FF 00 00 02", "01 01 89 80 00 00 00 01",
	"01 02 82 00 C8 00 00 02 00 01 00 02", "01 02 31 00 0A 0F 00 02 01 01",
	"01 03 82 01 00 00 00 03 55 AA", "01 04 82 00 64 00 30 00 64 05 82 00 65 00",
	"05 01 00", "06 01", "07 01", "23 01", "01 01 82 00 00", "01", "",
}

const fuzzHead = "80 00 02 00 01 00 00 05 00 7A" // 宛先 0.1.0、送信元 0.5.0、SID 0x7A

// サーバーがフレーム 1 つ（FINS/UDP の 1 データグラム、FINS/TCP の本体）を処理する
func FuzzHandler(f *testing.F) {
	for _, c := range fuzzCommands {
		f.Add(seedBytes(fuzzHead + c))
	}
	f.Add(seedBytes("C0 00 02 00 01 00 00 05 00 7A 01 01 00 00")) // 応答のフレーム
	f.Fuzz(func(t *testing.T, frame []byte) {
		res := newHandler(NewDataStore()).handle(frame)
		if res == nil {
			return
		}
		if len(res) < headerLength+4 {
			t.Fatalf("応答が短い（%d バイト）: % X → % X", len(res), frame, res)
		}
		if res[0]&icfResponse == 0 {
			t.Fatalf("応答の ICF が応答になっていない: % X", res)
		}
		if !bytes.Equal(res[headerLength:headerLength+2], frame[headerLength:headerLength+2]) {
			t.Fatalf("応答のコマンドコードがリクエストと違う: % X → % X", frame, res)
		}
	})
}

// FINS/TCP のメッセージ（16 バイトのヘッダー + 本体）を読む
func FuzzReadTCPMessage(f *testing.F) {
	f.Add(seedBytes("46494E53 0000000C 00000000 00000000 00000000"))                                                      // ノードアドレスの要求
	f.Add(seedBytes("46494E53 0000001A 00000002 00000000" + strings.ReplaceAll(fuzzHead, " ", "") + "01018200640000 02")) // FINS フレーム
	f.Add(seedBytes("46494E53 FFFFFFFF 00000002 00000000"))                                                               // 長さが大きすぎる
	f.Add(seedBytes("00000000"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = readTCPMessage(bytes.NewReader(data))
	})
}

// Host Link のサーバーが、届いた文字を溜めてフレームに区切り、処理する。
// 応答は、どれも Host Link のフレームとして読める
func FuzzHostLinkReceive(f *testing.F) {
	for _, c := range fuzzCommands {
		f.Add(encodeHostLinkCommand(0, 0, seedBytes(fuzzHead+c)))
	}
	f.Add([]byte("@00FA00000000001010182006400000210*\r"))
	f.Add([]byte("@00RD00000001*\r")) // FA 以外のヘッダーコード
	f.Add([]byte("garbage@00FA0*\r@01FA*\r"))
	f.Add([]byte("@00FA0000000000101\r")) // '*' のない途中のフレーム
	f.Fuzz(func(t *testing.T, data []byte) {
		s := newHostLinkServer(DefaultHostLinkConfig(), NewDataStore(), nil)
		s.receive(0, data, func(resp []byte) error {
			if _, err := parseHostLinkFrame(resp); err != nil {
				t.Fatalf("応答が Host Link のフレームとして読めない: %q（%v）。届いたもの %q", resp, err, data)
			}
			return nil
		})
	})
}

// Host Link のフレームを読み、FINS のフレームに戻す（サーバーもクライアントも使う）
func FuzzHostLinkParse(f *testing.F) {
	f.Add(encodeHostLinkCommand(0, 0, seedBytes(fuzzHead+"01 01 82 00 64 00 00 02")))
	f.Add(encodeHostLinkResponse(0, seedBytes("C0 00 02 00 05 00 00 01 00 7A 01 01 00 00 12 34")))
	f.Add(encodeHostLinkError(0, hostLinkHeaderFA, hostLinkEndFormat))
	f.Add([]byte("@00IC4A*\r"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if fr, err := parseHostLinkFrame(data); err == nil {
			_ = fr
		}
		_, _ = finsFromHostLink(data)
		_, _ = decodeHex(data)
	})
}

// クライアントが、機器から届いた応答を解釈する。リクエストも、利用者が 16 進で入れたものがそのまま来る
func FuzzClientDecodeResponse(f *testing.F) {
	const replyHead = "C0 00 02 00 05 00 00 01 00 7A"
	pairs := [][2]string{
		{"01 01 82 00 64 00 00 02", replyHead + "01 01 00 00 12 34 AB CD"},
		{"01 01 30 00 64 04 00 03", replyHead + "01 01 00 00 00 01 00"},
		{"01 04 82 00 64 00 30 00 64 05", replyHead + "01 04 00 00 82 12 34 30 01"},
		{"05 01 00", replyHead + "05 01 00 00 43 4A 32 4D"},
		{"06 01", replyHead + "06 01 00 00 01 00"},
		{"07 01", replyHead + "07 01 00 00 26 11 13 09 08 07 05"},
		{"01 01 82 00 64 00 00 02", replyHead + "01 01 11 03"},
		{"01 01 82 00 64 00 00 02", replyHead + "01 01 00 00 12"},
		{"", replyHead + "00 00 00 00"},
	}
	for _, p := range pairs {
		f.Add(seedBytes(p[0]), seedBytes(p[1]))
	}
	f.Fuzz(func(t *testing.T, req, frame []byte) {
		_, _, _ = decodeResponse(req, frame)
	})
}
