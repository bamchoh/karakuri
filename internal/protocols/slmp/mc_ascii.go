package slmp

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

// MC プロトコルの ASCII コード（シリアルの 4C フレームの形式 1〜4）での、コマンドの書き方。
//
// 処理（handler）とクライアントの組み立て（BuildRequest）・解釈（decodeResponse）は、バイナリコードの形
// （コマンド 2 バイト + サブコマンド 2 バイト + データ。数値はリトルエンディアン）で動く。
// ASCII の形とは、ここで行き来する。ASCII の形は、数値を 16 進の大文字で上の桁から書く:
//
//   - コマンド 4 桁・サブコマンド 4 桁
//   - デバイス: デバイスコード 2 文字（1 文字の記号は '*' を足す: D → "D*"）+ デバイス番号 6 桁
//     （X・Y・B・W など 16 進で書くデバイスは 16 進、ほかは 10 進。上の桁の 0 は空白でもよい）。
//     バイナリは番号 3 バイト + コード 1 バイトで、並びが逆
//   - 点数 4 桁、ランダムの点数 2 桁、ワードのデータ 4 桁、ダブルワードのデータ 8 桁
//   - ビット単位のデータは 1 点 1 文字（'0' / '1'）。バイナリは 1 点 4 ビット
//   - ランダム書き込み（ビット単位）の ON / OFF は 2 桁（"00" / "01"）。バイナリは 1 バイト
//
// 対応するのは、サーバーが処理できるコマンド（0401・1401・0403・1402・0101・0619）の、MELSEC-Q / L の形
// （サブコマンド 0000 / 0001）だけ。MELSEC iQ-R の形（デバイスの指定が長い 0002 / 0003）は、ASCII では扱わない。
//
// 確かめた資料: 三菱電機「MELSEC Communication Protocol Reference Manual」（SH-080008）の通信例
// （0401 の M100〜M131 と T100〜T102、0403 の D0・T0…、1402 の M50・Y2F、0619 の "ABCDE"）

// deviceASCIICode は、デバイスの ASCII コード（2 文字）
func deviceASCIICode(def deviceDef) string {
	if len(def.ID) == 1 {
		return def.ID + "*"
	}
	return def.ID
}

func deviceByASCIICode(code string) (deviceDef, bool) {
	code = strings.ToUpper(code)
	for _, d := range deviceDefs {
		if deviceASCIICode(d) == code {
			return d, true
		}
	}
	return deviceDef{}, false
}

// appendDeviceASCII は、デバイスの指定（コード 2 文字 + 番号 6 桁）を足す
func appendDeviceASCII(dst []byte, t target) []byte {
	dst = append(dst, deviceASCIICode(t.def)...)
	if t.def.Hex {
		return fmt.Appendf(dst, "%06X", t.number)
	}
	return fmt.Appendf(dst, "%06d", t.number)
}

// asciiReader は、ASCII のデータを先頭から読む
type asciiReader struct {
	b   []byte
	err error
}

func (r *asciiReader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if len(r.b) < n {
		r.err = fmt.Errorf("データが短すぎます")
		return nil
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}

// hex は、n 桁の 16 進を読む
func (r *asciiReader) hex(n int) uint64 {
	s := r.take(n)
	if r.err != nil {
		return 0
	}
	v, err := strconv.ParseUint(string(s), 16, 64)
	if err != nil {
		r.err = fmt.Errorf("16 進ではない文字があります（%q）", s)
	}
	return v
}

// device は、デバイスの指定（コード 2 文字 + 番号 6 桁）を読む
func (r *asciiReader) device() target {
	code := r.take(2)
	num := r.take(6)
	if r.err != nil {
		return target{}
	}
	def, ok := deviceByASCIICode(string(code))
	if !ok {
		r.err = fmt.Errorf("不明なデバイスコードです（%q）", code)
		return target{}
	}
	text := strings.ReplaceAll(string(num), " ", "0") // 上の桁の 0 は空白でもよい
	base := 10
	if def.Hex {
		base = 16
	}
	v, err := strconv.ParseUint(text, base, 32)
	if err != nil || v >= maxPoints {
		r.err = fmt.Errorf("デバイス番号を読めません（%q）", num)
		return target{}
	}
	return target{def: def, number: uint32(v)}
}

func (r *asciiReader) done() error {
	if r.err == nil && len(r.b) != 0 {
		return fmt.Errorf("データが長すぎます（%d 文字余っています）", len(r.b))
	}
	return r.err
}

// binReader は、バイナリのデータを先頭から読む
type binReader struct {
	b   []byte
	err error
}

func (r *binReader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if len(r.b) < n {
		r.err = fmt.Errorf("データが短すぎます")
		return nil
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}

func (r *binReader) u8() int { b := r.take(1); return int(firstOr0(b)) }
func (r *binReader) u16() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}
func (r *binReader) u32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}
func (r *binReader) device() target {
	b := r.take(4)
	if b == nil {
		return target{}
	}
	t, end := parseTarget(b, false)
	if end != endOK {
		r.err = fmt.Errorf("デバイスの指定を読めません")
	}
	return t
}
func (r *binReader) done() error {
	if r.err == nil && len(r.b) != 0 {
		return fmt.Errorf("データが長すぎます（%d バイト余っています）", len(r.b))
	}
	return r.err
}

func firstOr0(b []byte) byte {
	if len(b) == 0 {
		return 0
	}
	return b[0]
}

// asciiSupported は、ASCII で扱えるコマンドとサブコマンドか
func asciiSupported(command, sub uint16) bool {
	switch command {
	case cmdBatchRead, cmdBatchWrite, cmdRandomWrite:
		return sub == 0 || sub == subBitUnits
	case cmdRandomRead, cmdTypeName, cmdSelfTest:
		return sub == 0
	}
	return false
}

// requestToASCII は、バイナリの形のリクエスト（コマンド + サブコマンド + データ）を ASCII にする
func requestToASCII(req []byte) ([]byte, error) {
	if len(req) < 4 {
		return nil, fmt.Errorf("リクエストが短すぎます")
	}
	command := binary.LittleEndian.Uint16(req)
	sub := binary.LittleEndian.Uint16(req[2:])
	if !asciiSupported(command, sub) {
		return nil, fmt.Errorf("コマンド %04X（サブコマンド %04X）は ASCII コードでは送れません", command, sub)
	}
	out := fmt.Appendf(nil, "%04X%04X", command, sub)
	r := &binReader{b: req[4:]}
	bitUnits := sub == subBitUnits
	switch command {
	case cmdBatchRead, cmdBatchWrite:
		t := r.device()
		count := int(r.u16())
		out = appendDeviceASCII(out, t)
		out = fmt.Appendf(out, "%04X", count)
		if command == cmdBatchWrite {
			if bitUnits {
				for _, on := range unpackNibbles(r.take((count+1)/2), count) {
					out = append(out, bitChar(on))
				}
			} else {
				for range count {
					out = fmt.Appendf(out, "%04X", r.u16())
				}
			}
		}
	case cmdRandomRead:
		words, dwords := r.u8(), r.u8()
		out = fmt.Appendf(out, "%02X%02X", words, dwords)
		for range words + dwords {
			out = appendDeviceASCII(out, r.device())
		}
	case cmdRandomWrite:
		if bitUnits {
			count := r.u8()
			out = fmt.Appendf(out, "%02X", count)
			for range count {
				out = appendDeviceASCII(out, r.device())
				out = fmt.Appendf(out, "%02X", r.u8())
			}
			break
		}
		words, dwords := r.u8(), r.u8()
		out = fmt.Appendf(out, "%02X%02X", words, dwords)
		for range words {
			out = appendDeviceASCII(out, r.device())
			out = fmt.Appendf(out, "%04X", r.u16())
		}
		for range dwords {
			out = appendDeviceASCII(out, r.device())
			out = fmt.Appendf(out, "%08X", r.u32())
		}
	case cmdSelfTest:
		count := int(r.u16())
		out = fmt.Appendf(out, "%04X", count)
		out = append(out, r.take(count)...)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

// requestFromASCII は、ASCII のリクエストを、バイナリの形（コマンド + サブコマンド + データ）にする
func requestFromASCII(a []byte) ([]byte, error) {
	r := &asciiReader{b: a}
	command := uint16(r.hex(4))
	sub := uint16(r.hex(4))
	if r.err != nil {
		return nil, r.err
	}
	if !asciiSupported(command, sub) {
		return nil, errASCIICommand{command, sub}
	}
	out := appendCommand(nil, command, sub)
	bitUnits := sub == subBitUnits
	switch command {
	case cmdBatchRead, cmdBatchWrite:
		t := r.device()
		count := int(r.hex(4))
		out = t.appendTo(out)
		out = binary.LittleEndian.AppendUint16(out, uint16(count))
		if command == cmdBatchWrite && r.err == nil {
			if bitUnits {
				bits := make([]bool, 0, count)
				for _, c := range r.take(count) {
					bits = append(bits, c == '1')
				}
				out = append(out, packNibbles(bits)...)
			} else {
				for range count {
					out = binary.LittleEndian.AppendUint16(out, uint16(r.hex(4)))
				}
			}
		}
	case cmdRandomRead:
		words, dwords := int(r.hex(2)), int(r.hex(2))
		out = append(out, byte(words), byte(dwords))
		for i := 0; i < words+dwords && r.err == nil; i++ {
			out = r.device().appendTo(out)
		}
	case cmdRandomWrite:
		if bitUnits {
			count := int(r.hex(2))
			out = append(out, byte(count))
			for i := 0; i < count && r.err == nil; i++ {
				out = r.device().appendTo(out)
				out = append(out, byte(r.hex(2)))
			}
			break
		}
		words, dwords := int(r.hex(2)), int(r.hex(2))
		out = append(out, byte(words), byte(dwords))
		for i := 0; i < words && r.err == nil; i++ {
			out = r.device().appendTo(out)
			out = binary.LittleEndian.AppendUint16(out, uint16(r.hex(4)))
		}
		for i := 0; i < dwords && r.err == nil; i++ {
			out = r.device().appendTo(out)
			out = binary.LittleEndian.AppendUint32(out, uint32(r.hex(8)))
		}
	case cmdSelfTest:
		count := int(r.hex(4))
		out = binary.LittleEndian.AppendUint16(out, uint16(count))
		out = append(out, r.take(count)...)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

// errASCIICommand は、ASCII で扱えないコマンド（サーバーは「コマンドの誤り」で応答する）
type errASCIICommand struct{ command, sub uint16 }

func (e errASCIICommand) Error() string {
	return fmt.Sprintf("コマンド %04X（サブコマンド %04X）は ASCII コードでは扱えません", e.command, e.sub)
}

func bitChar(on bool) byte {
	if on {
		return '1'
	}
	return '0'
}

// responseToASCII は、バイナリの形の応答データを ASCII にする。req はバイナリの形のリクエスト
func responseToASCII(req, data []byte) ([]byte, error) {
	command := binary.LittleEndian.Uint16(req)
	sub := binary.LittleEndian.Uint16(req[2:])
	r := &binReader{b: data}
	var out []byte
	switch command {
	case cmdBatchRead:
		_, count, _, _ := batchRequest(req)
		if sub == subBitUnits {
			for _, on := range unpackNibbles(r.take((count+1)/2), count) {
				out = append(out, bitChar(on))
			}
		} else {
			for range count {
				out = fmt.Appendf(out, "%04X", r.u16())
			}
		}
	case cmdRandomRead:
		words, dwords := int(req[4]), int(req[5])
		for range words {
			out = fmt.Appendf(out, "%04X", r.u16())
		}
		for range dwords {
			out = fmt.Appendf(out, "%08X", r.u32())
		}
	case cmdTypeName:
		out = append(out, r.take(16)...)
		out = fmt.Appendf(out, "%04X", r.u16())
	case cmdSelfTest:
		count := int(r.u16())
		out = fmt.Appendf(out, "%04X", count)
		out = append(out, r.take(count)...)
	default:
		// 書き込みの応答にデータはない
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

// responseFromASCII は、ASCII の応答データを、バイナリの形にする。req はバイナリの形のリクエスト
func responseFromASCII(req, a []byte) ([]byte, error) {
	command := binary.LittleEndian.Uint16(req)
	sub := binary.LittleEndian.Uint16(req[2:])
	r := &asciiReader{b: a}
	var out []byte
	switch command {
	case cmdBatchRead:
		_, count, _, _ := batchRequest(req)
		if sub == subBitUnits {
			bits := make([]bool, 0, count)
			for _, c := range r.take(count) {
				bits = append(bits, c == '1')
			}
			out = packNibbles(bits)
		} else {
			for range count {
				out = binary.LittleEndian.AppendUint16(out, uint16(r.hex(4)))
			}
		}
	case cmdRandomRead:
		words, dwords := int(req[4]), int(req[5])
		for range words {
			out = binary.LittleEndian.AppendUint16(out, uint16(r.hex(4)))
		}
		for range dwords {
			out = binary.LittleEndian.AppendUint32(out, uint32(r.hex(8)))
		}
	case cmdTypeName:
		out = append(out, r.take(16)...)
		out = binary.LittleEndian.AppendUint16(out, uint16(r.hex(4)))
	case cmdSelfTest:
		count := int(r.hex(4))
		out = binary.LittleEndian.AppendUint16(out, uint16(count))
		out = append(out, r.take(count)...)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}
