package slmp

import (
	"bytes"
	"strings"
	"testing"
)

// マニュアルの形式 5 の例: 0401 で X40 から 5 点をビット単位で読む。経路は 05 07 03 0004 01 00、サムチェックは 05
func TestMCSerial_Format5ManualExample(t *testing.T) {
	example := unhexMC(t, "10 02 12 00 F8 05 07 03 04 00 01 00 01 04 01 00 40 00 00 9C 05 00 10 03 30 35")
	route := mcRoute{Station: 0x05, Network: 0x07, PC: 0x03, IO: 0x0004, DestStation: 0x01, Self: 0x00}
	req := unhexMC(t, "01 04 01 00 40 00 00 9C 05 00")

	if got := encodeF5(route, req, true); !bytes.Equal(got, example) {
		t.Errorf("作ったフレーム % X, want % X", got, example)
	}
	r, body, err := parseF5(example, true)
	if err != nil || r != route || !bytes.Equal(body, req) {
		t.Errorf("読んだ経路 %+v・中身 % X (%v)", r, body, err)
	}
	frame, skip, used, ok := nextMCFrame(append([]byte{0xFF}, example...), mcFormat5, true)
	if !ok || skip != 1 || used != len(example)+1 || !bytes.Equal(frame, example) {
		t.Errorf("切り出し: ok=%v skip=%d used=%d", ok, skip, used)
	}
}

// データに 10H が入ると、DLE を重ねて送り、データバイト数とサムチェックには数えない
func TestMCSerial_Format5DLEStuffing(t *testing.T) {
	route := hostRoute(0x10) // 局番も 10H
	body := []byte{0x01, 0x04, 0x00, 0x00, 0x10, 0x00, 0x00, 0xA8, 0x10, 0x00}
	f := encodeF5(route, body, true)
	if !bytes.Contains(f, []byte{0x10, 0x10, 0x00, 0xFF}) {
		t.Errorf("局番の 10H が重なっていない: % X", f)
	}
	r, got, err := parseF5(f, true)
	if err != nil || r != route || !bytes.Equal(got, body) {
		t.Errorf("読み戻し % X (%v)", got, err)
	}
	// 重ねた DLE の途中で切れていても、DLE ETX の前で止まる
	if _, _, _, ok := nextMCFrame(f[:len(f)-3], mcFormat5, true); ok {
		t.Error("揃っていないフレームを切り出した")
	}
	if frame, _, _, ok := nextMCFrame(f, mcFormat5, true); !ok || !bytes.Equal(frame, f) {
		t.Errorf("切り出し % X", frame)
	}
	// サムチェックなし
	nf := encodeF5(route, body, false)
	if _, got, err := parseF5(nf, false); err != nil || !bytes.Equal(got, body) {
		t.Errorf("サムチェックなし % X (%v)", got, err)
	}
	// サムチェックが違う
	bad := append([]byte(nil), f...)
	bad[len(bad)-1] = '0'
	if bad[len(bad)-1] == f[len(f)-1] {
		bad[len(bad)-1] = '1'
	}
	if _, _, err := parseF5(bad, true); err != errMCSum {
		t.Errorf("サムチェックの誤り %v", err)
	}
}

// 形式 4: マニュアルの経路の例（00 00 FF 03FF 00 00）と、要求・応答の形
func TestMCSerial_Format4(t *testing.T) {
	route := hostRoute(0)
	req := encodeF4(ctrlENQ, route, []byte("04010000D*0001000002"), true)
	body := "F80000FF03FF0000" + "04010000D*0001000002"
	if !strings.HasPrefix(string(req), "\x05"+body) || !strings.HasSuffix(string(req), "\r\n") {
		t.Errorf("要求 %q", req)
	}
	sum := mcSum([]byte(body))
	if want := []byte{"0123456789ABCDEF"[sum>>4], "0123456789ABCDEF"[sum&15]}; string(req[len(req)-4:len(req)-2]) != string(want) {
		t.Errorf("サムチェック %q, want %q", req[len(req)-4:len(req)-2], want)
	}
	f, err := parseF4(req, true)
	if err != nil || f.ctrl != ctrlENQ || f.route != route || string(f.text) != "04010000D*0001000002" {
		t.Errorf("要求を読めない: %+v (%v)", f, err)
	}

	// データありの応答: ETX までがサムチェックの範囲
	resp := encodeF4(ctrlSTX, route, []byte("12340002"), true)
	if !bytes.Contains(resp, []byte("12340002\x03")) {
		t.Errorf("応答 %q", resp)
	}
	if got := resp[len(resp)-4 : len(resp)-2]; string(got) != string(appendSum(nil, mcSum(resp[1:len(resp)-4]))) {
		t.Errorf("応答のサムチェック %q", got)
	}
	f, err = parseF4(resp, true)
	if err != nil || f.ctrl != ctrlSTX || string(f.text) != "12340002" {
		t.Errorf("応答を読めない: %+v (%v)", f, err)
	}
	// データなし・異常（サムチェックは付かない）
	if got := encodeF4(ctrlACK, route, nil, true); string(got) != "\x06F80000FF03FF0000\r\n" {
		t.Errorf("ACK %q", got)
	}
	nak := encodeF4(ctrlNAK, route, []byte("C059"), true)
	if string(nak) != "\x15F80000FF03FF0000C059\r\n" {
		t.Errorf("NAK %q", nak)
	}
	if f, err := parseF4(nak, true); err != nil || string(f.text) != "C059" {
		t.Errorf("NAK を読めない %+v (%v)", f, err)
	}
	// サムチェックなし
	if f, err := parseF4(encodeF4(ctrlENQ, route, []byte("01010000"), false), false); err != nil || string(f.text) != "01010000" {
		t.Errorf("サムチェックなし %+v (%v)", f, err)
	}
	// 誤り
	bad := append([]byte(nil), req...)
	bad[3] = '9' // 局番を変える（サムチェックが合わなくなる）
	if _, err := parseF4(bad, true); err != errMCSum {
		t.Errorf("サムチェックの誤り %v", err)
	}
	if _, err := parseF4([]byte("\x05F90000FF03FF0000\r\n"), false); err == nil {
		t.Error("3C フレームの ID を受け付けた")
	}

	// 切り出し: 前のごみを読み飛ばし、CR LF まで
	frame, skip, used, ok := nextMCFrame(append([]byte("xx"), req...), mcFormat4, true)
	if !ok || skip != 2 || used != len(req)+2 || !bytes.Equal(frame, req) {
		t.Errorf("切り出し ok=%v skip=%d used=%d", ok, skip, used)
	}
}
