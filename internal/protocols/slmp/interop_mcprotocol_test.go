package slmp

import (
	"encoding/binary"
	"testing"

	"github.com/future-architect/go-mcprotocol/mcp"
)

// 他社製のクライアント（future-architect/go-mcprotocol。3E フレーム・バイナリ・TCP）から、Karakuri の SLMP サーバーにつなぐ。
// Karakuri のサーバーとクライアントは同じコードでフレームを作るので、同じ読み違えをしていても、自分同士の試験では見つからない
func TestInterop_MCProtocolClient(t *testing.T) {
	_, store, port := startServer(t)
	c, err := mcp.New3EClient("127.0.0.1", port, mcp.NewLocalStation())
	if err != nil {
		t.Fatal(err)
	}
	parse := func(name string) func(raw []byte, err error) *mcp.Response {
		return func(raw []byte, err error) *mcp.Response {
			t.Helper()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			r, err := mcp.NewParser().Do(raw)
			if err != nil {
				t.Fatalf("%s: 応答を読めない: %v（% X）", name, err, raw)
			}
			if r.SubHeader != "D000" {
				t.Fatalf("%s: サブヘッダー = %s（% X）", name, r.SubHeader, raw)
			}
			return r
		}
	}
	words := func(b []byte) []uint16 {
		out := make([]uint16, len(b)/2)
		for i := range out {
			out[i] = binary.LittleEndian.Uint16(b[i*2:])
		}
		return out
	}

	// 折り返しテスト（つながるかの確認に使われる）
	if err := c.HealthCheck(); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}

	// ワード単位の書き込みと読み出し
	data := []byte{0x34, 0x12, 0xEF, 0xBE, 0x00, 0x00}
	if r := parse("Write(D100)")(c.Write("D", 100, 3, data)); r.EndCode != "0000" {
		t.Fatalf("Write(D100): 終了コード %s", r.EndCode)
	}
	if w, _ := store.ReadWords("D", 100, 3); w[0] != 0x1234 || w[1] != 0xBEEF || w[2] != 0 {
		t.Errorf("D100〜（サーバーのメモリ） = %04X", w)
	}
	r := parse("Read(D100)")(c.Read("D", 100, 3))
	if r.EndCode != "0000" || len(r.Payload) != 6 {
		t.Fatalf("Read(D100): 終了コード %s、データ % X", r.EndCode, r.Payload)
	}
	if got := words(r.Payload); got[0] != 0x1234 || got[1] != 0xBEEF || got[2] != 0 {
		t.Errorf("Read(D100) = %04X", got)
	}

	// 16 進のデバイス（W は番号を 16 進で書く。W1F は 31 番）
	_ = store.WriteWord("W", 0x1F, 0xCAFE)
	if r := parse("Read(W1F)")(c.Read("W", 0x1F, 1)); r.EndCode != "0000" || words(r.Payload)[0] != 0xCAFE {
		t.Errorf("Read(W1F): 終了コード %s、データ % X", r.EndCode, r.Payload)
	}

	// ビット単位の読み出し（1 点を 4 ビットで返す）。M100〜M103 = ON OFF ON ON
	_ = store.WriteBits("M", 100, []bool{true, false, true, true})
	r = parse("BitRead(M100)")(c.BitRead("M", 100, 4))
	if r.EndCode != "0000" || len(r.Payload) != 2 || r.Payload[0] != 0x10 || r.Payload[1] != 0x11 {
		t.Errorf("BitRead(M100): 終了コード %s、データ % X, want 10 11", r.EndCode, r.Payload)
	}
	// ビットデバイスをワード単位で読む（M96 から 1 ワード。M100 がビット 4）
	if r := parse("Read(M96)")(c.Read("M", 96, 1)); r.EndCode != "0000" || words(r.Payload)[0] != 0x0D<<4 {
		t.Errorf("Read(M96): 終了コード %s、データ % X, want D0 00", r.EndCode, r.Payload)
	}

	// 最後の番号と、終わりを超える読み出し（異常終了の応答が返る）
	if r := parse("Write(D16777215)")(c.Write("D", 16777215, 1, []byte{0xCD, 0xAB})); r.EndCode != "0000" {
		t.Errorf("Write(D16777215): 終了コード %s", r.EndCode)
	}
	if r := parse("Read(D16777215)")(c.Read("D", 16777215, 1)); r.EndCode != "0000" || words(r.Payload)[0] != 0xABCD {
		t.Errorf("Read(D16777215): 終了コード %s、データ % X", r.EndCode, r.Payload)
	}
	if r := parse("Read(D16777215 から 2 点)")(c.Read("D", 16777215, 2)); r.EndCode != "56C0" { // 0xC056（リトルエンディアンで 56 C0）
		t.Errorf("終わりを超える読み出しの終了コード = %s, want 56C0（0xC056）", r.EndCode)
	}
}
