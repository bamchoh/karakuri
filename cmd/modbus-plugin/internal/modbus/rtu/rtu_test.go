package rtu

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// unhex は "01 03 00 00" のような 16 進表記をバイト列にする
func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("unhex(%q): %v", s, err)
	}
	return b
}

// ===== CRC / LRC =====

// Modbus の仕様書・資料に載っている既知のフレーム（末尾 2 バイトが CRC、下位バイトが先）
var knownRTUFrames = []string{
	"01 03 00 00 00 0A C5 CD",
	"11 03 00 6B 00 03 76 87",
	"01 06 00 01 00 03 98 0B",
}

func TestCRC16_KnownFrames(t *testing.T) {
	for _, f := range knownRTUFrames {
		frame := unhex(t, f)
		body := frame[:len(frame)-2]
		want := uint16(frame[len(frame)-2]) | uint16(frame[len(frame)-1])<<8
		if got := CRC16(body); got != want {
			t.Errorf("CRC16(% X) = %04X, want %04X", body, got, want)
		}
		if got := AppendCRC(append([]byte{}, body...)); !bytes.Equal(got, frame) {
			t.Errorf("AppendCRC(% X) = % X, want % X", body, got, frame)
		}
		if !CheckCRC(frame) {
			t.Errorf("CheckCRC(% X) = false", frame)
		}
	}
}

func TestCheckCRC_RejectsBrokenFrames(t *testing.T) {
	frame := unhex(t, knownRTUFrames[0])
	for i := range frame {
		broken := append([]byte{}, frame...)
		broken[i] ^= 0x01
		if CheckCRC(broken) {
			t.Errorf("byte %d を 1 ビット変えたフレームが通った: % X", i, broken)
		}
	}
	for _, short := range [][]byte{nil, {0x01}, {0x01, 0x03}} {
		if CheckCRC(short) {
			t.Errorf("CheckCRC(% X) = true, 短すぎるフレームは false のはず", short)
		}
	}
}

func TestLRC_KnownValues(t *testing.T) {
	cases := []struct {
		data string
		want byte
	}{
		{"11 03 00 6B 00 03", 0x7E}, // 仕様書の例 ":1103006B00037E"
		{"01 03 00 00 00 01", 0xFB},
		{"00", 0x00},
		{"FF", 0x01},
	}
	for _, c := range cases {
		data := unhex(t, c.data)
		if got := LRC(data); got != c.want {
			t.Errorf("LRC(% X) = %02X, want %02X", data, got, c.want)
		}
		if !CheckLRC(data, c.want) || CheckLRC(data, c.want+1) {
			t.Errorf("CheckLRC(% X) が正しい値だけを通していない", data)
		}
	}
}

// ===== RTU: リクエストの解析 =====

func rtuFrame(t *testing.T, body string) []byte {
	return AppendCRC(unhex(t, body))
}

func TestParseRequest(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Request
	}{
		{"FC01 コイル読み取り", "11 01 00 13 00 25", Request{UnitID: 0x11, FunctionCode: 0x01, Address: 0x13, Quantity: 0x25}},
		{"FC02 入力読み取り", "11 02 00 C4 00 16", Request{UnitID: 0x11, FunctionCode: 0x02, Address: 0xC4, Quantity: 0x16}},
		{"FC03 保持レジスタ読み取り", "11 03 00 6B 00 03", Request{UnitID: 0x11, FunctionCode: 0x03, Address: 0x6B, Quantity: 3}},
		{"FC04 入力レジスタ読み取り", "11 04 00 08 00 01", Request{UnitID: 0x11, FunctionCode: 0x04, Address: 8, Quantity: 1}},
		{"FC05 単一コイル書き込み", "11 05 00 AC FF 00", Request{UnitID: 0x11, FunctionCode: 0x05, Address: 0xAC, Quantity: 1, Data: []byte{0xFF, 0x00}}},
		{"FC06 単一レジスタ書き込み", "11 06 00 01 00 03", Request{UnitID: 0x11, FunctionCode: 0x06, Address: 1, Quantity: 1, Data: []byte{0x00, 0x03}}},
		{"FC0F 複数コイル書き込み", "11 0F 00 13 00 0A 02 CD 01", Request{UnitID: 0x11, FunctionCode: 0x0F, Address: 0x13, Quantity: 10, Data: []byte{0xCD, 0x01}}},
		{"FC10 複数レジスタ書き込み", "11 10 00 01 00 02 04 00 0A 01 02", Request{UnitID: 0x11, FunctionCode: 0x10, Address: 1, Quantity: 2, Data: []byte{0x00, 0x0A, 0x01, 0x02}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// RTU
			got, err := ParseRequest(rtuFrame(t, c.body))
			if err != nil {
				t.Fatalf("ParseRequest: %v", err)
			}
			if !reflect.DeepEqual(*got, c.want) {
				t.Errorf("RTU: got %+v, want %+v", *got, c.want)
			}
			// ASCII（同じ内容を ASCII フレームにしたもの）
			got, err = ParseASCIIRequest(BuildASCIIFrame(unhex(t, c.body)))
			if err != nil {
				t.Fatalf("ParseASCIIRequest: %v", err)
			}
			if !reflect.DeepEqual(*got, c.want) {
				t.Errorf("ASCII: got %+v, want %+v", *got, c.want)
			}
		})
	}
}

func TestParseRequest_Errors(t *testing.T) {
	// CRC 不一致
	broken := rtuFrame(t, "11 03 00 6B 00 03")
	broken[2] ^= 0xFF
	if _, err := ParseRequest(broken); !errors.Is(err, ErrInvalidCRC) {
		t.Errorf("CRC 不一致: err = %v, want ErrInvalidCRC", err)
	}
	// 短すぎるフレーム
	if _, err := ParseRequest(unhex(t, "11 03 00")); !errors.Is(err, ErrFrameTooShort) {
		t.Errorf("短いフレーム: err = %v, want ErrFrameTooShort", err)
	}
	// バイト数の宣言よりデータが少ない（FC10 で 4 バイトと宣言して 2 バイトしかない）
	if _, err := ParseRequest(rtuFrame(t, "11 10 00 01 00 02 04 00 0A")); !errors.Is(err, ErrFrameTooShort) {
		t.Errorf("データ不足: err = %v, want ErrFrameTooShort", err)
	}
	// 対応していない機能コード
	if _, err := ParseRequest(rtuFrame(t, "11 2B 00 00 00 00")); err == nil {
		t.Error("対応していない機能コードがエラーにならなかった")
	}
}

// ===== RTU: レスポンスの組み立て =====

func TestBuildResponses(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		body string
	}{
		{"ビット読み取り（10 点 → 2 バイト、下位ビットが先頭）",
			BuildReadBitsResponse(0x11, 0x01, []bool{true, false, true, true, false, false, true, true, true, false}),
			"11 01 02 CD 01"},
		{"レジスタ読み取り", BuildReadRegistersResponse(0x11, 0x03, []uint16{0xAE41, 0x5652}), "11 03 04 AE 41 56 52"},
		{"単一書き込み", BuildWriteSingleResponse(0x11, 0x06, 1, 3), "11 06 00 01 00 03"},
		{"複数書き込み", BuildWriteMultipleResponse(0x11, 0x10, 1, 2), "11 10 00 01 00 02"},
		{"例外（機能コードの最上位ビットが立つ）", BuildExceptionResponse(0x11, 0x03, ExceptionIllegalDataAddress), "11 83 02"},
	}
	for _, c := range cases {
		if want := rtuFrame(t, c.body); !bytes.Equal(c.got, want) {
			t.Errorf("%s: got % X, want % X", c.name, c.got, want)
		}
	}
}

// ===== ASCII フレーム =====

func TestASCIIFrame_KnownFrame(t *testing.T) {
	// 仕様書の例
	const frame = ":1103006B00037E\r\n"
	body := unhex(t, "11 03 00 6B 00 03")

	if got := BuildASCIIFrame(append([]byte{}, body...)); string(got) != frame {
		t.Errorf("BuildASCIIFrame = %q, want %q", got, frame)
	}
	got, err := ParseASCIIFrame([]byte(frame))
	if err != nil {
		t.Fatalf("ParseASCIIFrame: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("ParseASCIIFrame = % X, want % X", got, body)
	}
	// 小文字の 16 進も読める
	if got, err := ParseASCIIFrame([]byte(":1103006b00037e\r\n")); err != nil || !bytes.Equal(got, body) {
		t.Errorf("小文字: got % X, err %v", got, err)
	}
}

func TestParseASCIIFrame_Errors(t *testing.T) {
	cases := map[string]string{
		"短すぎる":          ":11\r\n",
		"開始文字が違う":       ";1103006B00037E\r\n",
		"終端が CR LF でない": ":1103006B00037E\n\n",
		"16 進の桁数が奇数":    ":1103006B00037\r\n",
		"16 進でない文字":     ":1103006B0003ZZ\r\n",
		"LRC 不一致":       ":1103006B00037F\r\n",
	}
	for name, frame := range cases {
		if _, err := ParseASCIIFrame([]byte(frame)); err == nil {
			t.Errorf("%s: %q がエラーにならなかった", name, frame)
		}
	}
}

func TestBuildASCIIResponses(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		body string
	}{
		{"ビット読み取り", BuildASCIIReadBitsResponse(0x11, 0x01, []bool{true, false, true, true}), "11 01 01 0D"},
		{"レジスタ読み取り", BuildASCIIReadRegistersResponse(0x11, 0x03, []uint16{0xAE41}), "11 03 02 AE 41"},
		{"単一書き込み", BuildASCIIWriteSingleResponse(0x11, 0x06, 1, 3), "11 06 00 01 00 03"},
		{"複数書き込み", BuildASCIIWriteMultipleResponse(0x11, 0x10, 1, 2), "11 10 00 01 00 02"},
		{"例外", BuildASCIIExceptionResponse(0x11, 0x03, ExceptionIllegalDataAddress), "11 83 02"},
	}
	for _, c := range cases {
		// ASCII フレームを解析すると、同じ内容のバイト列に戻る
		body, err := ParseASCIIFrame(c.got)
		if err != nil {
			t.Errorf("%s: 組み立てたフレーム %q を解析できない: %v", c.name, c.got, err)
			continue
		}
		if want := unhex(t, c.body); !bytes.Equal(body, want) {
			t.Errorf("%s: got % X, want % X", c.name, body, want)
		}
	}
}

// ===== リクエストの処理（RTU の Processor と ASCII サーバーで同じ結果になること） =====

// memHandler はメモリ上の値で応答する RequestHandler
type memHandler struct {
	coils     map[uint16]bool
	registers map[uint16]uint16
	disabled  map[byte]bool
	err       error // nil 以外なら全ての処理がこのエラーを返す
}

func newMemHandler() *memHandler {
	return &memHandler{coils: map[uint16]bool{}, registers: map[uint16]uint16{}, disabled: map[byte]bool{}}
}

func (h *memHandler) readBits(address, quantity uint16) ([]bool, error) {
	if h.err != nil {
		return nil, h.err
	}
	vals := make([]bool, quantity)
	for i := range vals {
		vals[i] = h.coils[address+uint16(i)]
	}
	return vals, nil
}

func (h *memHandler) readWords(address, quantity uint16) ([]uint16, error) {
	if h.err != nil {
		return nil, h.err
	}
	vals := make([]uint16, quantity)
	for i := range vals {
		vals[i] = h.registers[address+uint16(i)]
	}
	return vals, nil
}

func (h *memHandler) HandleReadCoils(_ byte, a, q uint16) ([]bool, error) { return h.readBits(a, q) }
func (h *memHandler) HandleReadDiscreteInputs(_ byte, a, q uint16) ([]bool, error) {
	return h.readBits(a, q)
}
func (h *memHandler) HandleReadHoldingRegisters(_ byte, a, q uint16) ([]uint16, error) {
	return h.readWords(a, q)
}
func (h *memHandler) HandleReadInputRegisters(_ byte, a, q uint16) ([]uint16, error) {
	return h.readWords(a, q)
}
func (h *memHandler) HandleWriteSingleCoil(_ byte, a uint16, v bool) error {
	return h.HandleWriteMultipleCoils(0, a, []bool{v})
}
func (h *memHandler) HandleWriteSingleRegister(_ byte, a, v uint16) error {
	return h.HandleWriteMultipleRegisters(0, a, []uint16{v})
}
func (h *memHandler) HandleWriteMultipleCoils(_ byte, a uint16, vals []bool) error {
	if h.err != nil {
		return h.err
	}
	for i, v := range vals {
		h.coils[a+uint16(i)] = v
	}
	return nil
}
func (h *memHandler) HandleWriteMultipleRegisters(_ byte, a uint16, vals []uint16) error {
	if h.err != nil {
		return h.err
	}
	for i, v := range vals {
		h.registers[a+uint16(i)] = v
	}
	return nil
}
func (h *memHandler) IsUnitIDEnabled(unitID byte) bool { return !h.disabled[unitID] }

// process は同じリクエストを RTU の Processor と ASCII サーバーの両方で処理し、
// レスポンスの中身（CRC / LRC を除いたバイト列）が一致することを確かめて返す
func process(t *testing.T, h *memHandler, body string) []byte {
	t.Helper()
	req, err := ParseRequest(rtuFrame(t, body))
	if err != nil {
		t.Fatalf("ParseRequest(%s): %v", body, err)
	}
	rtuResp := NewProcessor(h).Process(req)
	if !CheckCRC(rtuResp) {
		t.Fatalf("RTU のレスポンスの CRC が正しくない: % X", rtuResp)
	}
	rtuBody := rtuResp[:len(rtuResp)-2]

	asciiReq, err := ParseASCIIRequest(BuildASCIIFrame(unhex(t, body)))
	if err != nil {
		t.Fatalf("ParseASCIIRequest(%s): %v", body, err)
	}
	asciiBody, err := ParseASCIIFrame((&ASCIIServer{handler: h}).processRequest(asciiReq))
	if err != nil {
		t.Fatalf("ASCII のレスポンスを解析できない: %v", err)
	}
	if !bytes.Equal(rtuBody, asciiBody) {
		t.Errorf("%s: RTU と ASCII でレスポンスが違う: RTU % X / ASCII % X", body, rtuBody, asciiBody)
	}
	return rtuBody
}

func TestProcess_ReadAndWrite(t *testing.T) {
	h := newMemHandler()
	expect := func(body, want string) {
		t.Helper()
		if got := process(t, h, body); !bytes.Equal(got, unhex(t, want)) {
			t.Errorf("%s → % X, want %s", body, got, want)
		}
	}

	// 書き込みは、リクエストの先頭部分をそのまま返す
	expect("01 06 00 10 12 34", "01 06 00 10 12 34")
	expect("01 10 00 11 00 02 04 00 0A 01 02", "01 10 00 11 00 02")
	expect("01 05 00 03 FF 00", "01 05 00 03 FF 00")
	expect("01 0F 00 08 00 0A 02 CD 01", "01 0F 00 08 00 0A")

	// 書いた値が読める
	expect("01 03 00 10 00 03", "01 03 06 12 34 00 0A 01 02")
	expect("01 04 00 10 00 01", "01 04 02 12 34")
	expect("01 01 00 03 00 01", "01 01 01 01")
	expect("01 01 00 08 00 0A", "01 01 02 CD 01")
	expect("01 02 00 08 00 08", "01 02 01 CD")

	// コイルを OFF に戻す
	expect("01 05 00 03 00 00", "01 05 00 03 00 00")
	expect("01 01 00 03 00 01", "01 01 01 00")
}

func TestProcess_Exceptions(t *testing.T) {
	h := newMemHandler()

	// 単一コイル書き込みの値は FF00 / 0000 だけ
	if got := process(t, h, "01 05 00 03 12 34"); !bytes.Equal(got, unhex(t, "01 85 03")) {
		t.Errorf("不正なコイル値: got % X, want 01 85 03", got)
	}

	// ハンドラーのエラーは例外コードになる
	cases := []struct {
		err  error
		code string
	}{
		{ErrIllegalFunction, "01"},
		{ErrIllegalDataAddress, "02"},
		{ErrIllegalDataValue, "03"},
		{NewModbusException(ExceptionSlaveDeviceBusy), "06"},
		{errors.New("その他のエラー"), "04"},
	}
	for _, c := range cases {
		h.err = c.err
		for _, req := range []struct{ body, fc string }{
			{"01 01 00 00 00 01", "81"},
			{"01 02 00 00 00 01", "82"},
			{"01 03 00 00 00 01", "83"},
			{"01 04 00 00 00 01", "84"},
			{"01 05 00 00 FF 00", "85"},
			{"01 06 00 00 00 01", "86"},
			{"01 0F 00 00 00 01 01 01", "8F"},
			{"01 10 00 00 00 01 02 00 01", "90"},
		} {
			want := unhex(t, "01 "+req.fc+" "+c.code)
			if got := process(t, h, req.body); !bytes.Equal(got, want) {
				t.Errorf("err=%v, %s: got % X, want % X", c.err, req.body, got, want)
			}
		}
	}
}

func TestProcessor_DisabledUnitIDGetsNoResponse(t *testing.T) {
	h := newMemHandler()
	h.disabled[0x02] = true
	req, err := ParseRequest(rtuFrame(t, "02 03 00 00 00 01"))
	if err != nil {
		t.Fatal(err)
	}
	if resp := NewProcessor(h).Process(req); resp != nil {
		t.Errorf("無効な UnitID に応答した: % X", resp)
	}
}

func TestModbusException_Error(t *testing.T) {
	if got := NewModbusException(ExceptionIllegalDataAddress).Error(); got != "illegal data address" {
		t.Errorf("Error() = %q", got)
	}
	if got := NewModbusException(0x7F).Error(); got != "unknown exception" {
		t.Errorf("未知のコード: Error() = %q", got)
	}
}
