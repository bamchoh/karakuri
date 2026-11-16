package slmp

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

// ファズテスト。外から届くバイト列で panic しない（panic するとアプリ全体が落ちる）ことと、応答の形が正しいことを確かめる。
// ふだんの go test では種（f.Add）だけを流す。探すときは go test -fuzz=FuzzHandler -fuzztime=1m ./internal/protocols/slmp

func seedBytes(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic(err)
	}
	return b
}

// fuzzRequests は、コマンドから後ろ（コマンド・サブコマンド・データ）の種
var fuzzRequests = []string{
	"01 04 01 00 64 00 00 90 08 00", "01 04 00 00 64 00 00 A8 03 00", "01 04 00 00 1F 00 00 B4 01 00",
	"01 04 02 00 64 00 00 00 A8 00 01 00", "01 14 00 00 C8 00 00 A8 02 00 11 11 22 22", "01 14 01 00 C8 00 00 90 03 00 10 10",
	"03 04 00 00 02 01 64 00 00 A8 64 00 00 90 65 00 00 A8", "02 14 00 00 01 01 2C 01 00 A8 AA 00 2D 01 00 A8 78 56 34 12",
	"02 14 01 00 02 2C 01 00 90 01 2D 01 00 90 00", "01 01 00 00", "19 06 00 00 03 00 41 42 43",
	"01 04 00 00 FF FF FF A8 02 00", "01 04 00 00 64 00 00 A8 C1 03", "01 04 00 00 64 00", "01 10 00 00", "",
}

var fuzzRoute = [routeLength]byte{0x00, 0xFF, 0xFF, 0x03, 0x00}

// request は、監視タイマを付けて、3E か 4E のフレームにする
func request(kind byte, body []byte) []byte {
	return appendFrame(nil, kind, 0x1234, fuzzRoute, append([]byte{0x10, 0x00}, body...))
}

// サーバーがフレーム 1 つを処理する。応答は、要求と同じ種類のフレームとして読める
func FuzzHandler(f *testing.F) {
	for _, r := range fuzzRequests {
		f.Add(request(sub3ERequest, seedBytes(r)))
		f.Add(request(sub4ERequest, seedBytes(r)))
	}
	f.Add(seedBytes("D0 00 00 FF FF 03 00 02 00 00 00")) // 応答のフレーム
	f.Fuzz(func(t *testing.T, raw []byte) {
		res := newHandler(NewDataStore()).handle(raw)
		if res == nil {
			return
		}
		if _, err := parseFrame(res); err != nil {
			t.Fatalf("応答がフレームとして読めない: % X → % X（%v）", raw, res, err)
		}
	})
}

// 要求のフレームの形は正しく、中身（コマンドから後ろ）だけがでたらめなもの。
// ヘッダーで落ちずに、コマンドの処理まで届く
func FuzzHandlerBody(f *testing.F) {
	for _, r := range fuzzRequests {
		f.Add(seedBytes(r), false)
	}
	f.Fuzz(func(t *testing.T, body []byte, fourE bool) {
		kind := byte(sub3ERequest)
		if fourE {
			kind = sub4ERequest
		}
		res := newHandler(NewDataStore()).handle(request(kind, body))
		if res == nil {
			t.Fatalf("形の正しい要求に応答しなかった: % X", body)
		}
		resp, err := parseFrame(res)
		if err != nil {
			t.Fatalf("応答がフレームとして読めない: % X（%v）", res, err)
		}
		if resp.is4E() != fourE {
			t.Fatalf("応答のフレームの種類が要求と違う: % X", res)
		}
	})
}

// TCP で届くバイト列から、フレームを 1 つずつ読む
func FuzzReadFrame(f *testing.F) {
	f.Add(request(sub3ERequest, seedBytes(fuzzRequests[0])))
	f.Add(request(sub4ERequest, seedBytes(fuzzRequests[1])))
	f.Add(seedBytes("50 00 00 FF FF 03 00 FF FF")) // データ長が大きすぎる
	f.Add([]byte("500000FF03FF000018"))            // ASCII コードのフレーム
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		for range 8 {
			if _, err := readFrame(r); err != nil {
				return
			}
		}
	})
}

// MC プロトコル（シリアル）のサーバーが、届いたバイトを溜めてフレームに区切り、処理する。
// 形式 4 / 5 × サムチェックあり / なし。応答は、どれも同じ形式のフレームとして読める
func FuzzMCSerialReceive(f *testing.F) {
	for _, r := range fuzzRequests {
		req := seedBytes(r)
		if text, err := requestToASCII(req); err == nil {
			f.Add(encodeF4(ctrlENQ, hostRoute(0), text, true), uint8(0))
			f.Add(encodeF4(ctrlENQ, hostRoute(0), text, false), uint8(1))
		}
		f.Add(encodeF5(hostRoute(0), req, true), uint8(2))
		f.Add(encodeF5(hostRoute(0), req, false), uint8(3))
	}
	f.Add([]byte("\x05F800FF03FF00000401000000D*000064000A\r\n"), uint8(1))
	f.Add(seedBytes("10 02 12 00 F8 05 07 03 04 00 01 00 01 04 01 00 40 00 00 9C 05 00 10 03 30 35"), uint8(2)) // マニュアルの例
	f.Add(seedBytes("10 02 FF FF 10 10 10 03"), uint8(3))
	f.Fuzz(func(t *testing.T, data []byte, mode uint8) {
		cfg := DefaultMCSerialConfig()
		cfg.Format = mcFormat4
		if mode&2 != 0 {
			cfg.Format = mcFormat5
		}
		cfg.SumCheck = mode&1 == 0
		s := newMCSerialServer(cfg, NewDataStore(), nil)
		s.receive(*cfg, data, func(resp []byte) error {
			if cfg.Format == mcFormat5 {
				if _, _, err := parseF5(resp, cfg.SumCheck); err != nil {
					t.Fatalf("応答が形式 5 のフレームとして読めない: % X（%v）。届いたもの % X", resp, err, data)
				}
			} else if _, err := parseF4(resp, cfg.SumCheck); err != nil {
				t.Fatalf("応答が形式 4 のフレームとして読めない: %q（%v）。届いたもの %q", resp, err, data)
			}
			return nil
		})
	})
}

// MC プロトコルの ASCII コードのコマンドと、バイナリの形を行き来する。
// サーバーは要求を、クライアントは応答を ASCII から戻す。ASCII にできた要求は、戻してもう一度 ASCII にすると同じになる
func FuzzMCASCII(f *testing.F) {
	for _, r := range fuzzRequests {
		f.Add(seedBytes(r), []byte("0000"))
	}
	f.Add(seedBytes("01 04 00 00 64 00 00 A8 03 00"), []byte("12340002 1DEF"))
	f.Add([]byte{}, []byte("0401000"))
	f.Fuzz(func(t *testing.T, req, text []byte) {
		if a, err := requestToASCII(req); err == nil {
			back, err := requestFromASCII(a)
			if err != nil {
				t.Fatalf("ASCII にした要求を戻せない: % X → %q（%v）", req, a, err)
			}
			// バイナリの形は、ビットの値（0 以外は ON）や最後の半バイトの書き方が一通りではないので、ASCII で比べる
			again, err := requestToASCII(back)
			if err != nil || !bytes.Equal(again, a) {
				t.Fatalf("ASCII にして戻し、もう一度 ASCII にすると違う: % X → %q → % X → %q（%v）", req, a, back, again, err)
			}
		}
		_, _ = requestFromASCII(text)
		_, _ = responseFromASCII(req, text)
		_, _ = responseToASCII(req, text)
	})
}

// クライアントが、機器から届いた応答を解釈する。リクエストも、利用者が 16 進で入れたものがそのまま来る
func FuzzClientDecodeResponse(f *testing.F) {
	ok := func(data string) []byte {
		body := append([]byte{0, 0}, seedBytes(data)...)
		return appendFrame(nil, sub3EResponse, 0, fuzzRoute, body)
	}
	for _, p := range [][2]string{
		{"01 04 01 00 64 00 00 90 08 00", "00 01 00 11"},
		{"01 04 00 00 64 00 00 A8 03 00", "34 12 02 00 EF 1D"},
		{"03 04 00 00 02 01 64 00 00 A8 64 00 00 90 65 00 00 A8", "34 12 C8 00 02 00 EF 1D"},
		{"01 01 00 00", "51 30 33 55 44 45 43 50 55 20 20 20 20 20 20 20 68 02"},
		{"19 06 00 00 03 00 41 42 43", "03 00 41 42 43"},
		{"01 04 00 00 64 00 00 A8 03 00", "34 12"},
		{"", ""},
	} {
		f.Add(seedBytes(p[0]), ok(p[1]))
	}
	f.Add(seedBytes("01 04 00 00 64 00 00 A8 03 00"), seedBytes("D0 00 00 FF FF 03 00 0B 00 51 C0 00 FF FF 03 00 01 04 00 00"))
	f.Fuzz(func(t *testing.T, req, raw []byte) {
		resp, err := parseFrame(raw)
		if err != nil {
			return
		}
		_, _, _ = decodeResponse(req, resp)
	})
}

// MC プロトコル（シリアル）のクライアントが、届いた応答のフレームを読み、SLMP と同じ解釈に回す
func FuzzMCSerialClientDecode(f *testing.F) {
	req := seedBytes("01 04 00 00 64 00 00 A8 03 00")
	f.Add(req, encodeF4(ctrlSTX, hostRoute(0), []byte("123400021DEF\x03"), true), uint8(0))
	f.Add(req, encodeF4(ctrlACK, hostRoute(0), nil, false), uint8(1))
	f.Add(req, encodeF4(ctrlNAK, hostRoute(0), []byte("C051"), false), uint8(1))
	f.Add(req, encodeF5(hostRoute(0), seedBytes("FF FF 00 00 34 12 02 00 EF 1D"), true), uint8(2))
	f.Add([]byte{}, encodeF5(hostRoute(0), seedBytes("FF FF 00 00"), false), uint8(3))
	f.Fuzz(func(t *testing.T, request, resp []byte, mode uint8) {
		c := &MCSerialClient{format: mcFormat4, sum: mode&1 == 0, route: hostRoute(0)}
		if mode&2 != 0 {
			c.format = mcFormat5
		}
		end, data, err := c.decode(request, resp)
		if err != nil {
			return
		}
		body := binary.LittleEndian.AppendUint16(nil, end)
		_, _, _ = decodeResponse(request, responseFrame(append(body, data...)))
	})
}
