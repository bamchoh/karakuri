// gofins は、応答を待つチャネルをロックなしで作り直す（incrementSid と listenLoop）ため、
// 競合検出（-race）では gofins の中の競合で落ちる。Karakuri の競合ではないので、-race のビルドでは流さない
// （CI の Windows のビルドとテストでは流れる）
//go:build !race

package fins

import (
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	gofins "github.com/l1va/gofins/fins"
)

// 他社製のクライアント（l1va/gofins。作者が実機の NJ501 で試したもの）から、Karakuri の FINS/UDP サーバーにつなぐ。
// Karakuri のサーバーとクライアントは同じコードでフレームを作るので、同じ読み違えをしていても、自分同士の試験では見つからない
func TestInterop_GofinsClient(t *testing.T) {
	_, port := startServer(t, transportUDP)
	// gofins は、自分の UDP のポートを決めて待ち受ける
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	localPort := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()

	c, err := gofins.NewClient(gofins.NewAddress("127.0.0.1", localPort, 0, 34, 0), gofins.NewAddress("127.0.0.1", port, 0, 10, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetTimeoutMs(2000)

	// ワード（DM・CIO・HR）
	if err := c.WriteWords(gofins.MemoryAreaDMWord, 1000, []uint16{0x1234, 0xBEEF, 0}); err != nil {
		t.Fatalf("WriteWords(DM): %v", err)
	}
	if got, err := c.ReadWords(gofins.MemoryAreaDMWord, 1000, 3); err != nil || !reflect.DeepEqual(got, []uint16{0x1234, 0xBEEF, 0}) {
		t.Errorf("ReadWords(DM) = %04X, %v", got, err)
	}
	if err := c.WriteWords(gofins.MemoryAreaHRWord, 10, []uint16{7}); err != nil {
		t.Fatalf("WriteWords(HR): %v", err)
	}
	if got, err := c.ReadWords(gofins.MemoryAreaHRWord, 10, 1); err != nil || got[0] != 7 {
		t.Errorf("ReadWords(HR) = %v, %v", got, err)
	}
	// 最後のアドレス
	if err := c.WriteWords(gofins.MemoryAreaDMWord, 65535, []uint16{0xABCD}); err != nil {
		t.Fatalf("WriteWords(D65535): %v", err)
	}
	if got, err := c.ReadWords(gofins.MemoryAreaDMWord, 65535, 1); err != nil || got[0] != 0xABCD {
		t.Errorf("ReadWords(D65535) = %v, %v", got, err)
	}
	// 終わりを超える読み出しは、終了コードの誤りになる（応答が返る）
	if _, err := c.ReadWords(gofins.MemoryAreaDMWord, 65535, 2); err == nil {
		t.Error("D65535 から 2 ワード読めた")
	}

	// 文字列（ワードを上位・下位の順に並べたもの）
	if err := c.WriteWords(gofins.MemoryAreaDMWord, 2000, []uint16{'K'<<8 | 'A', 'R'<<8 | 'A', 'K'<<8 | 'U', 'R'<<8 | 'I'}); err != nil {
		t.Fatalf("WriteWords(D2000): %v", err)
	}
	if got, err := c.ReadString(gofins.MemoryAreaDMWord, 2000, 4); err != nil || got != "KARAKURI" {
		t.Errorf("ReadString = %q, %v", got, err)
	}
	if got, err := c.ReadBytes(gofins.MemoryAreaDMWord, 2000, 1); err != nil || string(got) != "KA" {
		t.Errorf("ReadBytes = %q, %v", got, err)
	}
	// gofins の WriteString / WriteBytes は、個数（ワード）とデータ（バイト）が合わないフレームを送る
	// （gofins の不具合。gofins 自身も「実機で試していない」としている）。Karakuri は終了コード 1003 で断る（実機と同じ）。
	// 断ったので、値は変わらない
	if err := c.WriteString(gofins.MemoryAreaDMWord, 2000, "ABCDEFGH"); err == nil || !strings.Contains(err.Error(), "0x1003") {
		t.Errorf("個数とデータが合わない書き込み（WriteString）: %v, want 終了コード 0x1003", err)
	}
	if err := c.WriteBytes(gofins.MemoryAreaDMWord, 2000, []byte("ABCD")); err == nil || !strings.Contains(err.Error(), "0x1003") {
		t.Errorf("個数とデータが合わない書き込み（WriteBytes）: %v, want 終了コード 0x1003", err)
	}
	if got, err := c.ReadString(gofins.MemoryAreaDMWord, 2000, 4); err != nil || got != "KARAKURI" {
		t.Errorf("断った書き込みのあとの ReadString = %q, %v", got, err)
	}

	// ビット（WR と、DM をビットで見るエリア）。ワードの値とつじつまが合う（gofins は CIO を扱わない）
	if err := c.WriteBits(gofins.MemoryAreaWRBit, 100, 14, []bool{true, false, true}); err != nil {
		t.Fatalf("WriteBits(W100.14): %v", err)
	}
	if got, err := c.ReadBits(gofins.MemoryAreaWRBit, 100, 14, 3); err != nil || !reflect.DeepEqual(got, []bool{true, false, true}) {
		t.Errorf("ReadBits(W100.14) = %v, %v", got, err)
	}
	if got, err := c.ReadWords(gofins.MemoryAreaWRWord, 100, 2); err != nil || got[0] != 1<<14 || got[1] != 1 {
		t.Errorf("W100〜W101 のワード = %04X, %v（100.14 と 101.00 が ON のはず）", got, err)
	}
	if err := c.SetBit(gofins.MemoryAreaDMBit, 1000, 15); err != nil {
		t.Fatalf("SetBit(D1000.15): %v", err)
	}
	if err := c.ToggleBit(gofins.MemoryAreaDMBit, 1000, 4); err != nil {
		t.Fatalf("ToggleBit(D1000.04): %v", err)
	}
	if err := c.ResetBit(gofins.MemoryAreaDMBit, 1000, 2); err != nil {
		t.Fatalf("ResetBit(D1000.02): %v", err)
	}
	// 0x1234 → 15 番を ON、4 番を反転（ON → OFF）、2 番を OFF（ON → OFF）= 0x9220
	if got, err := c.ReadWords(gofins.MemoryAreaDMWord, 1000, 1); err != nil || got[0] != 0x9220 {
		t.Errorf("ビットを変えたあとの D1000 = %04X, %v, want 9220", got, err)
	}

	// 時計
	clock, err := c.ReadClock()
	if err != nil {
		t.Fatalf("ReadClock: %v", err)
	}
	if d := time.Since(*clock); d < -time.Minute || d > time.Minute {
		t.Errorf("時計 = %v（いまとの差 %v）", clock, d)
	}
}
