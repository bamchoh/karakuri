package slmp

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("16 進として読めません: %q", s)
	}
	return b
}

func hexOf(b []byte) string {
	return strings.ToUpper(hex.EncodeToString(b))
}

// request3E は、3E フレームの要求を作る（要求先は自局の CPU、監視タイマは 0010）。body は コマンド + サブコマンド + データ
func request3E(t *testing.T, body string) []byte {
	t.Helper()
	b := append([]byte{0x10, 0x00}, unhex(t, body)...)
	return appendFrame(nil, sub3ERequest, 0, [routeLength]byte{0x00, 0xFF, 0xFF, 0x03, 0x00}, b)
}

// 要求のフレームをそのまま処理して、応答のバイト列を確かめる
func TestHandler_Frames(t *testing.T) {
	store := NewDataStore()
	h := newHandler(store)
	// M100〜M107 = 0 0 0 1 0 0 1 1（マニュアルの例と同じ並び）
	_ = store.WriteBits("M", 100, []bool{false, false, false, true, false, false, true, true})
	_ = store.WriteWords("D", 100, []uint16{0x1234, 0x0002, 0x1DEF})
	_ = store.WriteWord("W", 0x1F, 0xBEEF)

	const head = "D0 00 00 FF FF 03 00" // 応答の、データ長の前まで
	// 異常終了の応答のうしろに付く、要求の内容（要求先の指定 5 バイト。このあとにコマンドとサブコマンド）
	const route = "00 FF FF 03 00"
	cases := []struct {
		name, request, want string
	}{
		{"ビット単位の一括読み出し（M100 から 8 点）", "01 04 01 00 64 00 00 90 08 00", head + "06 00 00 00 00 01 00 11"},
		{"ビット単位（点数が奇数なら、最後の下位は 0）", "01 04 01 00 64 00 00 90 07 00", head + "06 00 00 00 00 01 00 10"},
		{"ワード単位の一括読み出し（D100 から 3 点）", "01 04 00 00 64 00 00 A8 03 00", head + "08 00 00 00 34 12 02 00 EF 1D"},
		{"ビットデバイスをワード単位で読む（M96 から 1 ワード。M100 がビット 4）", "01 04 00 00 60 00 00 90 01 00", head + "04 00 00 00 80 0C"},
		{"16 進のデバイス（W1F）", "01 04 00 00 1F 00 00 B4 01 00", head + "04 00 00 00 EF BE"},
		{"長い形のデバイスの指定（サブコマンド 0002。番号 4 バイト・コード 2 バイト）", "01 04 02 00 64 00 00 00 A8 00 01 00", head + "04 00 00 00 34 12"},
		{"ワード単位の一括書き込み（D200 から 2 点）", "01 14 00 00 C8 00 00 A8 02 00 11 11 22 22", head + "02 00 00 00"},
		{"ビット単位の一括書き込み（M200 から 3 点: ON OFF ON）", "01 14 01 00 C8 00 00 90 03 00 10 10", head + "02 00 00 00"},
		{"ビットデバイスにワード単位で書く（Y10 から 16 点）", "01 14 00 00 10 00 00 9D 01 00 05 80", head + "02 00 00 00"},
		{"ランダム読み出し（ワード D100・M100、ダブルワード D101）", "03 04 00 00 02 01 64 00 00 A8 64 00 00 90 65 00 00 A8", head + "0A 00 00 00 34 12 C8 00 02 00 EF 1D"},
		{"ランダム書き込み（ワード D300、ダブルワード D301）", "02 14 00 00 01 01 2C 01 00 A8 AA 00 2D 01 00 A8 78 56 34 12", head + "02 00 00 00"},
		{"ランダム書き込み（ビット単位。M300 を ON、M301 を OFF）", "02 14 01 00 02 2C 01 00 90 01 2D 01 00 90 00", head + "02 00 00 00"},
		{"形名の読み出し", "01 01 00 00", head + "14 00 00 00 51 30 33 55 44 45 43 50 55 20 20 20 20 20 20 20 68 02"},
		{"折り返しテスト", "19 06 00 00 03 00 41 42 43", head + "07 00 00 00 03 00 41 42 43"},
		{"最後のデバイス（D16777215）を書く", "01 14 00 00 FF FF FF A8 01 00 CD AB", head + "02 00 00 00"},
		{"最後のビット（M16777215）を書く", "01 14 01 00 FF FF FF 90 01 00 10", head + "02 00 00 00"},

		{"知らないコマンド", "01 10 00 00", head + "0B 00 59 C0 " + route + " 01 10 00 00"},
		{"知らないサブコマンド", "01 04 80 00 64 00 00 A8 01 00", head + "0B 00 59 C0 " + route + " 01 04 80 00"},
		{"知らないデバイスコード", "01 04 00 00 64 00 00 77 01 00", head + "0B 00 5B C0 " + route + " 01 04 00 00"},
		{"ワードデバイスをビット単位で読む", "01 04 01 00 64 00 00 A8 01 00", head + "0B 00 5C C0 " + route + " 01 04 01 00"},
		{"点数が 0", "01 04 00 00 64 00 00 A8 00 00", head + "0B 00 51 C0 " + route + " 01 04 00 00"},
		{"点数が多すぎる（ワード 961 点）", "01 04 00 00 64 00 00 A8 C1 03", head + "0B 00 51 C0 " + route + " 01 04 00 00"},
		{"デバイスの終わりを超える（D16777215 から 2 点）", "01 04 00 00 FF FF FF A8 02 00", head + "0B 00 56 C0 " + route + " 01 04 00 00"},
		{"長い形の番号が、指せる範囲を超える", "01 04 02 00 00 00 00 01 A8 00 01 00", head + "0B 00 56 C0 " + route + " 01 04 02 00"},
		{"書き込みのデータが足りない", "01 14 00 00 C8 00 00 A8 02 00 11 11", head + "0B 00 61 C0 " + route + " 01 14 00 00"},
		{"読み出しの要求が長い", "01 04 00 00 64 00 00 A8 01 00 00", head + "0B 00 61 C0 " + route + " 01 04 00 00"},
		{"デバイスの指定が途中で切れている", "01 04 00 00 64 00", head + "0B 00 61 C0 " + route + " 01 04 00 00"},
	}
	for _, c := range cases {
		got := h.handle(request3E(t, c.request))
		want := unhex(t, c.want)
		if !bytes.Equal(got, want) {
			t.Errorf("%s:\n got  %s\n want %s", c.name, hexOf(got), hexOf(want))
		}
	}

	// 書いた値
	if w, _ := store.ReadWords("D", 200, 2); w[0] != 0x1111 || w[1] != 0x2222 {
		t.Errorf("D200 = %04X", w)
	}
	if b, _ := store.ReadBits("M", 200, 3); !b[0] || b[1] || !b[2] {
		t.Errorf("M200 = %v", b)
	}
	// Y10 から 16 点に 0x8005: Y10・Y12・Y1F が ON
	if b, _ := store.ReadBits("Y", 0x10, 16); !b[0] || b[1] || !b[2] || !b[15] {
		t.Errorf("Y10 = %v", b)
	}
	if w, _ := store.ReadWords("D", 300, 3); w[0] != 0x00AA || w[1] != 0x5678 || w[2] != 0x1234 {
		t.Errorf("D300 = %04X", w)
	}
	if b, _ := store.ReadBits("M", 300, 2); !b[0] || b[1] {
		t.Errorf("M300 = %v", b)
	}
	if w, _ := store.ReadWord("D", maxPoints-1); w != 0xABCD {
		t.Errorf("D16777215 = %04X", w)
	}
	if b, _ := store.ReadBit("M", maxPoints-1); !b {
		t.Error("M16777215 が ON になっていない")
	}

	// 4E フレーム: シリアル番号を応答に入れて返す
	req4E := appendFrame(nil, sub4ERequest, 0x1234, [routeLength]byte{0x00, 0xFF, 0xFF, 0x03, 0x00}, unhex(t, "10 00 01 04 00 00 64 00 00 A8 01 00"))
	if got, want := h.handle(req4E), unhex(t, "D4 00 34 12 00 00 00 FF FF 03 00 04 00 00 00 34 12"); !bytes.Equal(got, want) {
		t.Errorf("4E フレーム:\n got  %s\n want %s", hexOf(got), hexOf(want))
	}

	// 応答しないもの: フレームとして読めない・データ長が合わない・応答のフレーム・ASCII コードのフレーム
	for name, raw := range map[string][]byte{
		"短すぎる":           {0x50},
		"知らないサブヘッダー":     unhex(t, "99 00 00 FF FF 03 00 02 00 10 00"),
		"データ長が合わない":      unhex(t, "50 00 00 FF FF 03 00 0C 00 10 00 01 04"),
		"応答のフレーム":        unhex(t, "D0 00 00 FF FF 03 00 02 00 00 00"),
		"ASCII コードのフレーム": []byte("500000FF03FF000018001004010000D*0001000001"),
	} {
		if got := h.handle(raw); got != nil {
			t.Errorf("%s: 応答した: %s", name, hexOf(got))
		}
	}
	// コマンドまで届いていない要求には、データ長の誤りを返す
	if got, want := h.handle(unhex(t, "50 00 00 FF FF 03 00 02 00 10 00")), unhex(t, head+"0B 00 61 C0 "+route+" 00 00 00 00"); !bytes.Equal(got, want) {
		t.Errorf("コマンドのない要求:\n got  %s\n want %s", hexOf(got), hexOf(want))
	}
}

// メモリは、値が書かれたところだけを確保する（デバイスは 1 つ 16,777,216 点ある）
func TestDataStore_AllocatesOnWrite(t *testing.T) {
	s := NewDataStore()
	if n := s.allocatedWords(); n != 0 {
		t.Fatalf("作った直後に %d ワード確保している", n)
	}
	for _, d := range deviceDefs {
		// 最後の点まで読める。その先は読めない
		if d.IsBit {
			if _, err := s.ReadBits(d.ID, maxPoints-8, 8); err != nil {
				t.Errorf("%s: 最後の 8 点を読めない: %v", d.ID, err)
			}
			if _, err := s.ReadBits(d.ID, maxPoints-8, 9); err == nil {
				t.Errorf("%s: 範囲外を読めた", d.ID)
			}
		} else {
			if _, err := s.ReadWords(d.ID, maxPoints-8, 8); err != nil {
				t.Errorf("%s: 最後の 8 点を読めない: %v", d.ID, err)
			}
			if _, err := s.ReadWords(d.ID, maxPoints-8, 9); err == nil {
				t.Errorf("%s: 範囲外を読めた", d.ID)
			}
		}
	}
	if n := s.allocatedWords(); n != 0 {
		t.Fatalf("読み出しで %d ワード確保した", n)
	}
	_ = s.WriteWord("D", 100, 1)
	_ = s.WriteBit("M", maxPoints-1, true)
	if n := s.allocatedWords(); n != 2*1024 {
		t.Errorf("2 か所に書いたあと = %d ワード, want 2048", n)
	}
	if err := s.WriteBit("XX", 0, true); err == nil {
		t.Error("ないデバイスに書けた")
	}
	// エリアの一覧は、デバイスと同じ数で、どれも最大の大きさ
	areas := s.GetAreas()
	if len(areas) != len(deviceDefs) {
		t.Fatalf("エリア数 = %d, want %d", len(areas), len(deviceDefs))
	}
	for _, a := range areas {
		if a.Size != 1<<24 {
			t.Errorf("%s の大きさ = %d, want 16777216", a.ID, a.Size)
		}
	}
}

// デバイスコードと ID は重ならない
func TestDeviceDefs_Unique(t *testing.T) {
	codes, ids := map[byte]string{}, map[string]bool{}
	for _, d := range deviceDefs {
		if other, dup := codes[d.Code]; dup {
			t.Errorf("デバイスコード %02X が %s と %s で重なっている", d.Code, other, d.ID)
		}
		if ids[d.ID] {
			t.Errorf("ID %s が重なっている", d.ID)
		}
		codes[d.Code], ids[d.ID] = d.ID, true
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startServer(t *testing.T) (*Server, *DataStore, int) {
	t.Helper()
	store := NewDataStore()
	port := freePort(t)
	srv := NewServer(&Config{Host: "127.0.0.1", Port: port, Transport: transportBoth}, store)
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv, store, port
}

func newClient(t *testing.T, port int, transport, frameKind string) *Client {
	t.Helper()
	f := &ClientFactory{}
	c, err := f.NewClient(map[string]interface{}{"host": "127.0.0.1", "port": port, "transport": transport, "frame": frameKind, "timeoutMs": 300})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c.(*Client)
}

// クライアントからサーバーへ、TCP / UDP と 3E / 4E の組み合わせで読み書きする
func TestClientAndServer(t *testing.T) {
	srv, store, port := startServer(t)
	f := &ClientFactory{}
	send := func(c *Client, command string, params map[string]interface{}) protocol.CommandResult {
		t.Helper()
		req, err := f.BuildRequest(command, params)
		if err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		return c.Send(context.Background(), req)
	}

	for _, transport := range []string{transportTCP, transportUDP} {
		for _, kind := range []string{frame3E, frame4E} {
			t.Run(transport+"/"+kind, func(t *testing.T) {
				c := newClient(t, port, transport, kind)
				_ = store.WriteWords("D", 100, []uint16{0, 0})

				// ワードデバイス
				if r := send(c, commandWrite, map[string]interface{}{"device": "D", "address": "100", "values": "1234, 0xABCD"}); r.Status != protocol.CommandOK {
					t.Fatalf("書き込み: %+v", r)
				}
				r := send(c, commandRead, map[string]interface{}{"device": "D", "address": "100", "count": float64(2)})
				if r.Status != protocol.CommandOK || len(r.Values) != 2 || *r.Values[0].Word != 1234 || *r.Values[1].Word != 0xABCD {
					t.Fatalf("読み出し: %+v", r)
				}
				if r.Values[0].Label != "D100" || r.Values[1].Label != "D101" || r.Values[1].Value != "43981 (0xABCD)" {
					t.Errorf("値の表示: %+v", r.Values)
				}
				if !strings.Contains(r.Summary, "0401 一括読み出し D100 から 2 点（ワード単位）") {
					t.Errorf("要約 = %q", r.Summary)
				}
				// 送ったバイト列の先頭: サブヘッダー（4E はシリアル番号つき）・要求先の指定・データ長・監視タイマ
				wantSent := "500000FFFF03000C001000" + "01040000640000A80200"
				if kind == frame4E {
					wantSent = "5400" + hexOf(r.Sent[2:4]) + "0000" + "00FFFF03000C001000" + "01040000640000A80200"
				}
				if got := hexOf(r.Sent); got != wantSent {
					t.Errorf("送ったバイト列 = %s, want %s", got, wantSent)
				}

				// ビットデバイス。デバイス番号は、16 進のデバイスなら 16 進で入れる
				if r := send(c, commandWrite, map[string]interface{}{"device": "X", "address": "1F", "values": "1 0 1"}); r.Status != protocol.CommandOK {
					t.Fatalf("ビットの書き込み: %+v", r)
				}
				if b, _ := store.ReadBits("X", 0x1F, 3); !b[0] || b[1] || !b[2] {
					t.Errorf("X1F から = %v", b)
				}
				r = send(c, commandRead, map[string]interface{}{"device": "X", "address": "1f", "count": float64(3)})
				if r.Status != protocol.CommandOK || len(r.Values) != 3 || !*r.Values[0].Bit || *r.Values[1].Bit || !*r.Values[2].Bit {
					t.Fatalf("ビットの読み出し: %+v", r)
				}
				if r.Values[0].Label != "X1F" || r.Values[1].Label != "X20" || r.Values[0].Value != "ON" {
					t.Errorf("ビットの表示: %+v", r.Values)
				}

				// 最大の点数（ワード 960 点・ビット 7168 点）
				if r := send(c, commandRead, map[string]interface{}{"device": "D", "address": "0", "count": float64(maxWordPoints)}); r.Status != protocol.CommandOK || len(r.Values) != maxWordPoints {
					t.Errorf("ワード 960 点: %s %s（%d 個）", r.Status, r.Error, len(r.Values))
				}
				if r := send(c, commandRead, map[string]interface{}{"device": "M", "address": "0", "count": float64(maxBitPoints)}); r.Status != protocol.CommandOK || len(r.Values) != maxBitPoints {
					t.Errorf("ビット 7168 点: %s %s（%d 個）", r.Status, r.Error, len(r.Values))
				}

				// 形名・折り返しテスト
				if r := send(c, commandTypeName, nil); r.Status != protocol.CommandOK || r.Values[0].Value != cpuModel || r.Values[1].Value != "0x0268" {
					t.Errorf("形名: %+v", r)
				}
				if r := send(c, commandSelfTest, map[string]interface{}{"data": "Hello123"}); r.Status != protocol.CommandOK || r.Values[0].Value != "送ったデータと同じ" || r.Values[1].Value != "Hello123" {
					t.Errorf("折り返しテスト: %+v", r)
				}

				// 異常終了（接続は閉じない）。16 進で入れた、対応していないコマンド
				r = c.Send(context.Background(), unhex(t, "01 10 00 00"))
				if r.Status != protocol.CommandException || !strings.Contains(r.Error, "0xC059") || !c.Connected() {
					t.Fatalf("知らないコマンド: %+v", r)
				}
				// 16 進で入れた、ビットデバイスのワード単位の読み出し
				_ = store.WriteBits("M", 0, []bool{true, false, false, false, false, false, false, false, false, false, false, false, false, false, false, true})
				r = c.Send(context.Background(), unhex(t, "01 04 00 00 00 00 00 90 01 00"))
				if r.Status != protocol.CommandOK || len(r.Values) != 1 || *r.Values[0].Word != 0x8001 || r.Values[0].Label != "M0 から 16 点" {
					t.Errorf("ビットデバイスのワード単位: %+v", r)
				}
			})
		}
	}

	// 止めると、つながらない。もう一度起動すると、メモリは残っていて、また読める
	c := newClient(t, port, transportTCP, frame3E)
	if r := send(c, commandRead, map[string]interface{}{"device": "D", "address": "100", "count": float64(1)}); r.Status != protocol.CommandOK {
		t.Fatalf("止める前: %+v", r)
	}
	if err := srv.Stop(); err != nil {
		t.Fatal(err)
	}
	if r := send(c, commandRead, map[string]interface{}{"device": "D", "address": "100", "count": float64(1)}); r.Status == protocol.CommandOK || c.Connected() {
		t.Fatalf("止めたあとに読めた: %+v", r)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := send(c, commandRead, map[string]interface{}{"device": "D", "address": "100", "count": float64(1)}); r.Status != protocol.CommandOK || *r.Values[0].Word != 1234 {
		t.Fatalf("起動し直したあと: %+v", r)
	}
}

// コマンドの組み立ての誤りは、送る前に分かる
func TestBuildRequest_Errors(t *testing.T) {
	f := &ClientFactory{}
	for name, c := range map[string]struct {
		command string
		params  map[string]interface{}
	}{
		"デバイスがない":         {commandRead, map[string]interface{}{"device": "QQ", "address": "0", "count": float64(1)}},
		"番号が空":            {commandRead, map[string]interface{}{"device": "D", "address": "", "count": float64(1)}},
		"10 進のデバイスに 16 進": {commandRead, map[string]interface{}{"device": "D", "address": "1F", "count": float64(1)}},
		"番号が大きすぎる":        {commandRead, map[string]interface{}{"device": "D", "address": "16777216", "count": float64(1)}},
		"終わりを超える":         {commandRead, map[string]interface{}{"device": "D", "address": "16777215", "count": float64(2)}},
		"ワードの点数が多い":       {commandRead, map[string]interface{}{"device": "D", "address": "0", "count": float64(961)}},
		"ビットの値でない":        {commandWrite, map[string]interface{}{"device": "M", "address": "0", "values": "2"}},
		"値がない":            {commandWrite, map[string]interface{}{"device": "D", "address": "0", "values": ""}},
		"折り返しのデータが英数字でない": {commandSelfTest, map[string]interface{}{"data": "あ"}},
		"知らないコマンド":        {"nope", nil},
	} {
		if req, err := f.BuildRequest(c.command, c.params); err == nil {
			t.Errorf("%s: 作れてしまった: %s", name, hexOf(req))
		}
	}
	// 16 進のデバイスは、0x を付けても付けなくても同じ。最後の番号まで指定できる
	a, err1 := f.BuildRequest(commandRead, map[string]interface{}{"device": "W", "address": "FFFFFF", "count": float64(1)})
	b, err2 := f.BuildRequest(commandRead, map[string]interface{}{"device": "W", "address": "0xffffff", "count": float64(1)})
	if err1 != nil || err2 != nil || !bytes.Equal(a, b) || hexOf(a) != "01040000FFFFFFB40100" {
		t.Errorf("W FFFFFF: %s / %s（%v, %v）", hexOf(a), hexOf(b), err1, err2)
	}
	// 接続先の設定の検査
	for name, settings := range map[string]map[string]interface{}{
		"ホストが空":    {"host": " "},
		"ポートが範囲外":  {"port": float64(70000)},
		"知らないフレーム": {"frame": "1e"},
		"局番が範囲外":   {"station": float64(256)},
	} {
		if _, err := f.NormalizeSettings(settings); err == nil {
			t.Errorf("%s: 設定を受け付けた", name)
		}
	}
	if got, err := f.NormalizeSettings(nil); err != nil || got["port"] != defaultPort || got["station"] != defaultStation || got["moduleIO"] != defaultModuleIO {
		t.Errorf("既定の設定 = %v（%v）", got, err)
	}
}

// 変数のマッピングに使うリクエストは、どのデバイスでも作れて、サーバーが応答する
func TestMapping_AllDevices(t *testing.T) {
	_, store, port := startServer(t)
	c := newClient(t, port, transportUDP, frame4E)
	f := &ClientFactory{}
	for _, area := range f.MappingAreas() {
		last := area.Size - 1
		var words []uint16
		var bits []bool
		if area.IsBit {
			bits = []bool{true}
		} else {
			words = []uint16{0xBEEF}
		}
		req, err := f.BuildWriteRequest(area.ID, last, words, bits)
		if err != nil {
			t.Errorf("%s: 書き込みのリクエスト: %v", area.ID, err)
			continue
		}
		if r := c.Send(context.Background(), req); r.Status != protocol.CommandOK {
			t.Errorf("%s: 書き込み: %s %s", area.ID, r.Status, r.Error)
		}
		req, err = f.BuildReadRequest(area.ID, last, 1)
		if err != nil {
			t.Errorf("%s: 読み出しのリクエスト: %v", area.ID, err)
			continue
		}
		r := c.Send(context.Background(), req)
		if r.Status != protocol.CommandOK || len(r.Values) != 1 {
			t.Errorf("%s: 読み出し: %+v", area.ID, r)
			continue
		}
		if area.IsBit {
			if r.Values[0].Bit == nil || !*r.Values[0].Bit {
				t.Errorf("%s: 読んだ値 = %+v, want ON", area.ID, r.Values[0])
			}
			if b, _ := store.ReadBit(area.ID, last); !b {
				t.Errorf("%s: サーバーの値が ON になっていない", area.ID)
			}
		} else if r.Values[0].Word == nil || *r.Values[0].Word != 0xBEEF {
			t.Errorf("%s: 読んだ値 = %+v, want BEEF", area.ID, r.Values[0])
		}
		if f.MaxReadCount(area.ID) == 0 || f.MaxWriteCount(area.ID) == 0 {
			t.Errorf("%s: 1 回で読み書きできる点数が 0", area.ID)
		}
		// 終わりを超えるリクエストは作れない
		if _, err := f.BuildReadRequest(area.ID, last, 2); err == nil {
			t.Errorf("%s: 終わりを超える読み出しを作れた", area.ID)
		}
	}
	if f.MaxReadCount("XX") != 0 {
		t.Error("ないデバイスの点数が 0 でない")
	}
	if _, err := f.BuildReadRequest("XX", 0, 1); err == nil {
		t.Error("ないデバイスのリクエストを作れた")
	}
}

// 応答しない相手・壊れた応答・遅れて届いた応答
func TestClient_FakeDevice(t *testing.T) {
	// 受け取った要求ごとに、respond が返すバイト列を順に送る（UDP）
	fake := func(respond func(request []byte) [][]byte) int {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pc.Close() })
		go func() {
			buf := make([]byte, 65535)
			for {
				n, from, err := pc.ReadFrom(buf)
				if err != nil {
					return
				}
				for _, resp := range respond(append([]byte(nil), buf[:n]...)) {
					_, _ = pc.WriteTo(resp, from)
				}
			}
		}()
		return pc.LocalAddr().(*net.UDPAddr).Port
	}
	reply := func(request []byte, serialOffset uint16, data ...byte) []byte {
		f, err := parseFrame(request)
		if err != nil {
			panic(fmt.Sprint("フェイクの相手が要求を読めない: ", err))
		}
		f.Serial += serialOffset
		return f.reply(endOK, 0, 0, data)
	}
	read := unhex(t, "01 04 00 00 64 00 00 A8 01 00")

	t.Run("4E: シリアル番号が違う応答は読み捨てる", func(t *testing.T) {
		port := fake(func(req []byte) [][]byte {
			return [][]byte{reply(req, 0xFFFF, 0xAD, 0xDE), reply(req, 0, 0x34, 0x12)}
		})
		c := newClient(t, port, transportUDP, frame4E)
		r := c.Send(context.Background(), read)
		if r.Status != protocol.CommandOK || len(r.Values) != 1 || *r.Values[0].Word != 0x1234 {
			t.Fatalf("遅れた応答を、今の応答として読んだ: %+v", r)
		}
	})

	t.Run("応答がなければタイムアウトにして、接続を閉じる。次はつなぎ直す", func(t *testing.T) {
		var silent atomic.Bool
		silent.Store(true)
		port := fake(func(req []byte) [][]byte {
			if silent.Load() {
				return nil
			}
			return [][]byte{reply(req, 0, 0x34, 0x12)}
		})
		c := newClient(t, port, transportUDP, frame3E)
		start := time.Now()
		r := c.Send(context.Background(), read)
		if r.Status != protocol.CommandTimeout || c.Connected() {
			t.Fatalf("応答がないとき: %+v（接続中 = %v）", r, c.Connected())
		}
		if elapsed := time.Since(start); elapsed < 250*time.Millisecond || elapsed > 2*time.Second {
			t.Errorf("タイムアウトまで %v", elapsed)
		}
		silent.Store(false)
		if r := c.Send(context.Background(), read); r.Status != protocol.CommandOK || !c.Connected() {
			t.Fatalf("つなぎ直したあと: %+v", r)
		}
	})

	t.Run("壊れた応答は、エラーにして接続を閉じる", func(t *testing.T) {
		for name, resp := range map[string]func(req []byte) []byte{
			"SLMP でないバイト列":  func([]byte) []byte { return []byte{0x01, 0x02, 0x03} },
			"データ長が合わない":     func(req []byte) []byte { return append(reply(req, 0, 0x34, 0x12), 0xFF) },
			"データが足りない":      func(req []byte) []byte { return reply(req, 0, 0x34) },
			"要求のフレームが返ってきた": func(req []byte) []byte { return req },
			"フレームの種類が違う": func(req []byte) []byte {
				f, _ := parseFrame(req)
				f.Kind = sub4ERequest
				return f.reply(endOK, 0, 0, []byte{0x34, 0x12})
			},
		} {
			port := fake(func(req []byte) [][]byte { return [][]byte{resp(req)} })
			c := newClient(t, port, transportUDP, frame3E)
			if r := c.Send(context.Background(), read); r.Status != protocol.CommandError || c.Connected() {
				t.Errorf("%s: %+v（接続中 = %v）", name, r, c.Connected())
			}
		}
	})

	t.Run("取り消すと、すぐに戻る", func(t *testing.T) {
		port := fake(func([]byte) [][]byte { return nil })
		c := newClient(t, port, transportUDP, frame3E)
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(30*time.Millisecond, cancel)
		start := time.Now()
		if r := c.Send(ctx, read); r.Status != protocol.CommandError || time.Since(start) > 250*time.Millisecond {
			t.Errorf("取り消し: %+v（%v）", r, time.Since(start))
		}
	})

	t.Run("リクエストが短すぎれば、送らない", func(t *testing.T) {
		c := newClient(t, 1, transportUDP, frame3E)
		if r := c.Send(context.Background(), []byte{0x01, 0x04}); r.Status != protocol.CommandError || len(r.Sent) != 0 {
			t.Errorf("短いリクエスト: %+v", r)
		}
	})
}

// TCP: フレームとして読めないものが来たら、サーバーは接続を閉じる。続けて送った要求は、順に処理する
func TestServer_TCPStream(t *testing.T) {
	_, store, port := startServer(t)
	_ = store.WriteWords("D", 0, []uint16{0x0001, 0x0002})
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	// 2 つの要求を、1 回の書き込みで送る
	two := append(request3E(t, "01 04 00 00 00 00 00 A8 01 00"), request3E(t, "01 04 00 00 01 00 00 A8 01 00")...)
	if _, err := conn.Write(two); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"D0 00 00 FF FF 03 00 04 00 00 00 01 00", "D0 00 00 FF FF 03 00 04 00 00 00 02 00"} {
		raw, err := readFrame(conn)
		if err != nil || !bytes.Equal(raw, unhex(t, want)) {
			t.Fatalf("%d 番目の応答 = %s（%v）", i+1, hexOf(raw), err)
		}
	}
	// SLMP でないバイト列を送ると、閉じられる
	if _, err := conn.Write([]byte("GET / HTTP/1.0\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if n, err := conn.Read(make([]byte, 16)); err == nil {
		t.Errorf("SLMP でないバイト列に %d バイト返ってきた", n)
	}
}
