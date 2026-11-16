package slmp

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func unhexMC(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

// マニュアル（SH-080008）の通信例: 同じ要求の ASCII とバイナリが、行き来できる
func TestMCASCII_ManualRequestExamples(t *testing.T) {
	cases := []struct {
		name   string
		ascii  string
		binary string
	}{
		// 0401 ビットデバイスをワード単位で: M100〜M131（2 ワード）
		{"0401 M100 2 ワード", "04010000M*0001000002", "01 04 00 00 64 00 00 90 02 00"},
		// 0401 ワード単位: T100〜T102
		{"0401 TN100 3 点", "04010000TN0001000003", "01 04 00 00 64 00 00 C2 03 00"},
		// 0403: ワード D0・T0・M100・X20、ダブルワード D1500・Y160・M1111
		{"0403", "040300000403D*000000TN000000M*000100X*000020D*001500Y*000160M*001111",
			"03 04 00 00 04 03 00 00 00 A8 00 00 00 C2 64 00 00 90 20 00 00 9C DC 05 00 A8 60 01 00 9D 57 04 00 90"},
		// 1402 ビット単位: M50 を OFF、Y2F を ON
		{"1402 ビット", "1402000102M*00005000Y*00002F01", "02 14 01 00 02 32 00 00 90 00 2F 00 00 9D 01"},
		// 0619: "ABCDE"
		{"0619", "061900000005ABCDE", "19 06 00 00 05 00 41 42 43 44 45"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bin := unhexMC(t, c.binary)
			got, err := requestFromASCII([]byte(c.ascii))
			if err != nil || !bytes.Equal(got, bin) {
				t.Errorf("ASCII → バイナリ: % X (%v), want % X", got, err, bin)
			}
			back, err := requestToASCII(bin)
			if err != nil || string(back) != c.ascii {
				t.Errorf("バイナリ → ASCII: %q (%v), want %q", back, err, c.ascii)
			}
		})
	}
}

func TestMCASCII_ManualResponseExamples(t *testing.T) {
	cases := []struct {
		name   string
		req    string // バイナリの要求
		ascii  string
		binary string
	}{
		// M100〜M131 をワードで: 1234H と 0002H
		{"0401 ワード", "01 04 00 00 64 00 00 90 02 00", "12340002", "34 12 02 00"},
		// T100〜T102: 1234H・0002H・1DEFH
		{"0401 TN", "01 04 00 00 64 00 00 C2 03 00", "123400021DEF", "34 12 02 00 EF 1D"},
		// 0403: ワード 4 点（D0=1995H・T0=1202H・M100〜=2030H・X20〜=4849H）、ダブルワード 3 点
		{"0403", "03 04 00 00 04 03 00 00 00 A8 00 00 00 C2 64 00 00 90 20 00 00 9C DC 05 00 A8 60 01 00 9D 57 04 00 90",
			"19951202203048494C544F4EC3DEB9AFBADDBCB7",
			"95 19 02 12 30 20 49 48 4E 4F 54 4C AF B9 DE C3 B7 BC DD BA"},
		// 0619: "ABCDE" が返る
		{"0619", "19 06 00 00 05 00 41 42 43 44 45", "0005ABCDE", "05 00 41 42 43 44 45"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := unhexMC(t, c.req)
			bin := unhexMC(t, c.binary)
			got, err := responseFromASCII(req, []byte(c.ascii))
			if err != nil || !bytes.Equal(got, bin) {
				t.Errorf("ASCII → バイナリ: % X (%v), want % X", got, err, bin)
			}
			back, err := responseToASCII(req, bin)
			if err != nil || string(back) != c.ascii {
				t.Errorf("バイナリ → ASCII: %q (%v), want %q", back, err, c.ascii)
			}
		})
	}
}

// ビット単位の書き込みと読み出し（1 点 1 文字）、ワード単位の書き込み、ダブルワードのランダム書き込み
func TestMCASCII_RoundTrips(t *testing.T) {
	for _, bin := range [][]byte{
		writeReq(t, "M", 100, nil, []bool{true, false, true}),
		writeReq(t, "D", 350, []uint16{0x56AB, 0x170F}, nil),
		writeReq(t, "X", 0x1F, nil, []bool{true}),
		unhexMC(t, "02 14 00 00 01 01 00 00 00 A8 34 12 DC 05 00 A8 4E 4F 54 4C"), // 1402 ワード 1 点 + ダブルワード 1 点
		unhexMC(t, "01 01 00 00"),                                                 // 0101
	} {
		a, err := requestToASCII(bin)
		if err != nil {
			t.Fatalf("% X: %v", bin, err)
		}
		back, err := requestFromASCII(a)
		if err != nil || !bytes.Equal(back, bin) {
			t.Errorf("%q → % X (%v), want % X", a, back, err, bin)
		}
	}
	if a, _ := requestToASCII(writeReq(t, "M", 100, nil, []bool{true, false, true})); string(a) != "14010001M*0001000003101" {
		t.Errorf("ビット単位の書き込み %q", a)
	}
	if a, _ := requestToASCII(writeReq(t, "X", 0x1F, nil, []bool{true})); string(a) != "14010001X*00001F00011" {
		t.Errorf("16 進のデバイス %q", a)
	}

	// 0101 の応答: 形名 16 文字 + 形名コード 4 桁
	model := append([]byte("Q03UDECPU       "), 0x68, 0x02)
	a, err := responseToASCII(unhexMC(t, "01 01 00 00"), model)
	if err != nil || string(a) != "Q03UDECPU       0268" {
		t.Errorf("0101 の応答 %q (%v)", a, err)
	}
	// ビット単位の読み出しの応答: 1 点 1 文字
	req, _ := readRequest(target{def: mustDevice(t, "M"), number: 0}, 3)
	a, err = responseToASCII(req, packNibbles([]bool{true, false, true}))
	if err != nil || string(a) != "101" {
		t.Errorf("ビット単位の応答 %q (%v)", a, err)
	}
}

func TestMCASCII_Errors(t *testing.T) {
	for _, s := range []string{
		"0401",                   // 短い
		"04010000Q*0001000001",   // 不明なデバイス
		"04010000D*00A1000001",   // 10 進のデバイスに 16 進
		"04010000D*00010000G1",   // 点数が 16 進ではない
		"04010000D*0001000001XX", // 余り
		"0401000 D*0001000001",   // サブコマンドが 16 進ではない
	} {
		if _, err := requestFromASCII([]byte(s)); err == nil {
			t.Errorf("%q を読めてしまった", s)
		}
	}
	if _, err := requestFromASCII([]byte("04010002")); err == nil {
		t.Error("iQ-R の形（0002）を受け付けた")
	} else if _, ok := err.(errASCIICommand); !ok {
		t.Errorf("扱えないコマンドの誤りの種類 %T", err)
	}
	if _, err := requestToASCII(unhexMC(t, "01 08 00 00")); err == nil {
		t.Error("扱えないコマンドを ASCII にできてしまった")
	}

	// ASCII の番号は 6 桁。10 進のデバイスは 999999 まで（はみ出すと、受け取った側が番号と点数を取り違える）
	if got, err := requestToASCII(unhexMC(t, "01 04 00 00 3F 42 0F A8 01 00")); err != nil || string(got) != "04010000D*9999990001" { // D999999
		t.Errorf("D999999: %q (%v)", got, err)
	}
	for name, req := range map[string]string{
		"一括読み出し D1000000":       "01 04 00 00 40 42 0F A8 01 00",
		"一括読み出し D16777215":      "01 04 00 00 FF FF FF A8 01 00",
		"ランダム読み出し M1000000":     "03 04 00 00 01 00 40 42 0F 90",
		"ランダム書き込み D1000000":     "02 14 00 00 01 00 40 42 0F A8 01 00",
		"ビットのランダム書き込み M1000000": "02 14 01 00 01 40 42 0F 90 01",
	} {
		if got, err := requestToASCII(unhexMC(t, req)); err == nil {
			t.Errorf("%s を ASCII にできてしまった: %q", name, got)
		}
	}
	// ビット単位の一括書き込みで、データが点数より短い（利用者が 16 進で入れたもの）。以前は panic した
	if got, err := requestToASCII(unhexMC(t, "01 14 01 00 64 00 00 90 08 00")); err == nil {
		t.Errorf("データのないビットの書き込みを ASCII にできてしまった: %q", got)
	}
	// 16 進のデバイスは 6 桁で全部の番号を書ける
	if got, err := requestToASCII(unhexMC(t, "01 04 00 00 FF FF FF B4 01 00")); err != nil || string(got) != "04010000W*FFFFFF0001" { // WFFFFFF
		t.Errorf("WFFFFFF: %q (%v)", got, err)
	}
	// 上の桁の 0 は空白でもよい
	if got, err := requestFromASCII([]byte("04010000D*   1000001")); err != nil || !bytes.Equal(got, unhexMC(t, "01 04 00 00 64 00 00 A8 01 00")) {
		t.Errorf("空白の番号 % X (%v)", got, err)
	}
}

func mustDevice(t *testing.T, id string) deviceDef {
	t.Helper()
	d, ok := deviceByID(id)
	if !ok {
		t.Fatalf("デバイス %s がない", id)
	}
	return d
}

func writeReq(t *testing.T, id string, number uint32, words []uint16, bits []bool) []byte {
	t.Helper()
	req, err := writeRequest(target{def: mustDevice(t, id), number: number}, words, bits)
	if err != nil {
		t.Fatal(err)
	}
	return req
}
