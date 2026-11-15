package fins

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func unhexFins(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

// 実機の通信の例（MrPLC のフォーラム）。ヘッダー 10 バイトの形（ICF 80 / C0）
const (
	exampleHostLinkCommand  = "@00FA5800007013C00000000010101320007000004" + "0F*\r"
	exampleHostLinkResponse = "@00FA00C00002000000013C00010101000001000100" + "46*\r"
)

func TestHostLink_ExampleFrames(t *testing.T) {
	// コマンド: FCS が合い、FINS のフレーム（ヘッダー 10 バイト）が取り出せる
	f, err := parseHostLinkFrame([]byte(exampleHostLinkCommand))
	if err != nil {
		t.Fatalf("コマンドを読めない: %v", err)
	}
	if f.unit != 0 || f.code != "FA" || f.text[0] != '5' {
		t.Errorf("号機 %d・ヘッダー %s・待ち時間 %c", f.unit, f.code, f.text[0])
	}
	raw, err := decodeHex(f.text[1:])
	if err != nil {
		t.Fatal(err)
	}
	fins, err := finsFromHostLink(raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := unhexFins(t, "80 00 07 01 3C 00 00 00 00 01 01 01 32 00 07 00 00 04"); !bytes.Equal(fins, want) {
		t.Errorf("FINS % X, want % X", fins, want)
	}
	// 同じ FINS のフレームから、同じコマンドが作れる
	if got := encodeHostLinkCommand(0, 5, fins); string(got) != exampleHostLinkCommand {
		t.Errorf("作ったコマンド %q, want %q", got, exampleHostLinkCommand)
	}

	// 応答: 終了コード 00 のあとが FINS の応答
	f, err = parseHostLinkFrame([]byte(exampleHostLinkResponse))
	if err != nil {
		t.Fatalf("応答を読めない: %v", err)
	}
	if string(f.text[:2]) != hostLinkEndNormal {
		t.Errorf("終了コード %s", f.text[:2])
	}
	raw, _ = decodeHex(f.text[2:])
	fins, err = finsFromHostLink(raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := unhexFins(t, "C0 00 02 00 00 00 01 3C 00 01 01 01 00 00 01 00 01 00"); !bytes.Equal(fins, want) {
		t.Errorf("FINS % X, want % X", fins, want)
	}
	if got := encodeHostLinkResponse(0, fins); string(got) != exampleHostLinkResponse {
		t.Errorf("作った応答 %q, want %q", got, exampleHostLinkResponse)
	}
}

// 短い形（ICF の最上位ビットが 0）: ヘッダーは ICF DA2 SA2 SID の 4 バイト
func TestHostLink_ShortForm(t *testing.T) {
	full := append(header{ICF: 0x00, GCT: 0x02, DA2: 0x00, SA2: 0x00, SID: 0x12}.append(nil), 0x01, 0x01, 0x82, 0x00, 0x64, 0x00, 0x00, 0x01)
	cmd := encodeHostLinkCommand(3, 0, full)
	if want := "@03FA0" + "00000012" + "0101820064000001"; !strings.HasPrefix(string(cmd), want) {
		t.Errorf("コマンド %q, want %q で始まる", cmd, want)
	}
	f, err := parseHostLinkFrame(cmd)
	if err != nil || f.unit != 3 {
		t.Fatalf("読めない: %v（号機 %d）", err, f.unit)
	}
	raw, _ := decodeHex(f.text[1:])
	back, err := finsFromHostLink(raw)
	if err != nil || !bytes.Equal(back, full) {
		t.Errorf("戻した FINS % X, want % X (%v)", back, full, err)
	}

	// 応答は ICF 40 の短い形
	resp := append(header{ICF: 0x40, GCT: 0x02, SID: 0x12}.append(nil), 0x01, 0x01, 0x00, 0x00, 0x12, 0x34)
	if got := encodeHostLinkResponse(3, resp); !strings.HasPrefix(string(got), "@03FA00"+"40000012"+"010100001234") {
		t.Errorf("応答 %q", got)
	}
}

func TestHostLink_ParseErrors(t *testing.T) {
	good := []byte(exampleHostLinkCommand)
	broken := append([]byte(nil), good...)
	broken[len(broken)-4] = '1' // FCS を変える
	if _, err := parseHostLinkFrame(broken); err != errHostLinkFCS {
		t.Errorf("FCS が違うのに %v", err)
	}
	for _, s := range []string{"", "@00FA", "00FA0F*\r", "@0XFA00*\r", "@00FA5800\r", "@00FAZZ*\r"} {
		if _, err := parseHostLinkFrame([]byte(s)); err == nil {
			t.Errorf("%q を読めてしまった", s)
		}
	}
	if _, err := decodeHex([]byte("0G")); err == nil {
		t.Error("16 進でない文字を読めてしまった")
	}
	if _, err := finsFromHostLink([]byte{0x80, 0x00}); err == nil {
		t.Error("短すぎるヘッダーを読めてしまった")
	}
}

// 未定義のコマンドへの応答 "@00IC4A*"（よく知られた形。FCS が合う）
func TestHostLink_ICResponse(t *testing.T) {
	if got := encodeHostLinkError(0, hostLinkHeaderIC, ""); string(got) != "@00IC4A*\r" {
		t.Errorf("IC の応答 %q", got)
	}
}
