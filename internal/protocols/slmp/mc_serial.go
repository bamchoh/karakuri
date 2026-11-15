package slmp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
)

// MC プロトコルの 4C フレーム（シリアルコミュニケーションユニット C24 などの、シリアルで使う形）。
// 形式 4（ASCII コード、CR LF で終わる）と形式 5（バイナリコード）に対応する。
//
// 形式 4（数値は 16 進の大文字で、上の桁から）:
//
//	要求:               ENQ "F8" アクセス経路(14 文字) 要求データ [サムチェック 2 文字] CR LF
//	応答（データあり）: STX "F8" アクセス経路 応答データ ETX [サムチェック] CR LF
//	応答（データなし）: ACK "F8" アクセス経路 CR LF
//	応答（異常）:       NAK "F8" アクセス経路 エラーコード(4 文字) CR LF
//
// 形式 5（数値はリトルエンディアン）:
//
//	DLE STX データバイト数(2) F8 アクセス経路(7) 要求データ DLE ETX [サムチェック 2 文字]
//	応答は、アクセス経路のあとに FFFF（応答 ID）と 0000（正常完了）か エラーコード(2)、正常ならそのあとに応答データ
//	データバイト数は、F8 から DLE ETX の前までのバイト数。DLE（10H）と同じバイトは 10H 10H と 2 つ送る（数えるのは 1 つ）
//
// アクセス経路: 局番・ネットワーク番号・PC 番号・要求先ユニット I/O 番号(2)・要求先ユニット局番・自局番。
// 自局（つないだ C24 の CPU）へは 局番 00〜1F・00・FF・03FF・00・00。
//
// サムチェックは、範囲のバイトを足した下位 1 バイトを、16 進 2 文字（上の桁から）で書いたもの。使うかどうかは設定で決まる。
// 範囲は、形式 4 は ENQ / STX の次から サムチェックの直前まで（データありの応答では ETX を含む）、形式 5 は データバイト数から
// DLE ETX の前まで（重ねて送った DLE は数えない）。
//
// 確かめた資料: 三菱電機「MELSEC Communication Protocol Reference Manual」（SH-080008）の、形式 4・形式 5 のメッセージの形、
// アクセス経路の例（00 00 FF 03FF 00 00）、形式 5 のサムチェックの例（10 02 12 00 F8 05 07 … 10 03 30 35。サムチェックは 05）。
// 形式 4 の応答のサムチェックの範囲に ETX を含むことは、マニュアルの本文からは読み取れておらず、記憶による。

const (
	mcFrameID4C = 0xF8

	ctrlSTX = 0x02
	ctrlETX = 0x03
	ctrlENQ = 0x05
	ctrlACK = 0x06
	ctrlDLE = 0x10
	ctrlNAK = 0x15
	ctrlCR  = 0x0D
	ctrlLF  = 0x0A

	mcFormat4 = 4
	mcFormat5 = 5

	// mcRouteLength は、アクセス経路のバイト数（ASCII ではその倍の文字数）
	mcRouteLength = 7
	// maxMCFrame は、受け付ける 1 つのフレームの最大の長さ（ASCII で 960 ワードの書き込みは 4000 文字ほど）
	maxMCFrame = 8192
	// maxMCStation は局番の最大（00〜1F）
	maxMCStation = 0x1F
)

// mcRoute は、4C フレームのアクセス経路
type mcRoute struct {
	Station     byte
	Network     byte
	PC          byte
	IO          uint16
	DestStation byte
	Self        byte
}

// hostRoute は、つないだ C24 の CPU（自局）へのアクセス経路
func hostRoute(station byte) mcRoute {
	return mcRoute{Station: station, Network: 0x00, PC: 0xFF, IO: 0x03FF, DestStation: 0x00, Self: 0x00}
}

func (r mcRoute) appendASCII(dst []byte) []byte {
	return fmt.Appendf(dst, "%02X%02X%02X%04X%02X%02X", r.Station, r.Network, r.PC, r.IO, r.DestStation, r.Self)
}

func (r mcRoute) appendBinary(dst []byte) []byte {
	dst = append(dst, r.Station, r.Network, r.PC)
	dst = binary.LittleEndian.AppendUint16(dst, r.IO)
	return append(dst, r.DestStation, r.Self)
}

func parseRouteASCII(b []byte) (mcRoute, error) {
	if len(b) < 2*mcRouteLength {
		return mcRoute{}, errMCShort
	}
	v := make([]uint64, 6)
	for i, w := range []int{2, 2, 2, 4, 2, 2} {
		n, err := strconv.ParseUint(string(b[:w]), 16, 16)
		if err != nil {
			return mcRoute{}, fmt.Errorf("アクセス経路を読めません（%q）", b[:w])
		}
		v[i] = n
		b = b[w:]
	}
	return mcRoute{Station: byte(v[0]), Network: byte(v[1]), PC: byte(v[2]), IO: uint16(v[3]), DestStation: byte(v[4]), Self: byte(v[5])}, nil
}

func parseRouteBinary(b []byte) mcRoute {
	return mcRoute{Station: b[0], Network: b[1], PC: b[2], IO: binary.LittleEndian.Uint16(b[3:]), DestStation: b[5], Self: b[6]}
}

var (
	errMCShort  = errors.New("フレームが短すぎます")
	errMCSum    = errors.New("サムチェックが合いません")
	errMCFormat = errors.New("4C フレームの形ではありません")
)

func mcSum(b []byte) byte {
	var s byte
	for _, c := range b {
		s += c
	}
	return s
}

func appendSum(dst []byte, sum byte) []byte {
	return fmt.Appendf(dst, "%02X", sum)
}

func parseSum(b []byte) (byte, error) {
	v, err := strconv.ParseUint(string(b), 16, 8)
	if err != nil {
		return 0, errMCFormat
	}
	return byte(v), nil
}

// --- 形式 4（ASCII） ---

// encodeF4 は、形式 4 のフレームを作る。ctrl は ENQ / STX / ACK / NAK、text は経路のあと（要求データ・応答データ・エラーコード）
func encodeF4(ctrl byte, route mcRoute, text []byte, sum bool) []byte {
	out := []byte{ctrl}
	out = fmt.Appendf(out, "%02X", mcFrameID4C)
	out = route.appendASCII(out)
	out = append(out, text...)
	switch ctrl {
	case ctrlSTX:
		out = append(out, ctrlETX)
		if sum {
			out = appendSum(out, mcSum(out[1:]))
		}
	case ctrlENQ:
		if sum {
			out = appendSum(out, mcSum(out[1:]))
		}
	}
	return append(out, ctrlCR, ctrlLF)
}

// mcFrameF4 は、読み取った形式 4 のフレーム
type mcFrameF4 struct {
	ctrl  byte
	route mcRoute
	text  []byte // 経路のあと（ETX・サムチェック・CR LF の前まで）
}

// parseF4 は、制御コードから CR LF までの形式 4 のフレームを読む（サムチェックがあれば確かめる）
func parseF4(b []byte, sum bool) (mcFrameF4, error) {
	if len(b) < 1+2+2*mcRouteLength+2 || !bytes.HasSuffix(b, []byte{ctrlCR, ctrlLF}) {
		return mcFrameF4{}, errMCShort
	}
	f := mcFrameF4{ctrl: b[0]}
	body := b[1 : len(b)-2]
	if string(body[:2]) != "F8" {
		return f, fmt.Errorf("フレーム ID が F8（4C フレーム）ではありません（%q）", body[:2])
	}
	hasSum := sum && (f.ctrl == ctrlENQ || f.ctrl == ctrlSTX)
	if hasSum {
		if len(body) < 2+2*mcRouteLength+2 {
			return f, errMCShort
		}
		want, err := parseSum(body[len(body)-2:])
		if err != nil {
			return f, err
		}
		body = body[:len(body)-2]
		if mcSum(body) != want {
			return f, errMCSum
		}
	}
	if f.ctrl == ctrlSTX {
		if len(body) == 0 || body[len(body)-1] != ctrlETX {
			return f, fmt.Errorf("応答に ETX がありません")
		}
		body = body[:len(body)-1]
	}
	route, err := parseRouteASCII(body[2:])
	if err != nil {
		return f, err
	}
	f.route = route
	f.text = body[2+2*mcRouteLength:]
	return f, nil
}

// --- 形式 5（バイナリ） ---

// encodeF5 は、形式 5 のフレームを作る。body はアクセス経路のあと
func encodeF5(route mcRoute, body []byte, sum bool) []byte {
	payload := make([]byte, 0, 2+1+mcRouteLength+len(body))
	payload = binary.LittleEndian.AppendUint16(payload, uint16(1+mcRouteLength+len(body)))
	payload = append(payload, mcFrameID4C)
	payload = route.appendBinary(payload)
	payload = append(payload, body...)

	out := make([]byte, 0, len(payload)+8)
	out = append(out, ctrlDLE, ctrlSTX)
	for _, c := range payload {
		if c == ctrlDLE {
			out = append(out, ctrlDLE) // DLE と同じバイトは、2 つ送る
		}
		out = append(out, c)
	}
	out = append(out, ctrlDLE, ctrlETX)
	if sum {
		out = appendSum(out, mcSum(payload))
	}
	return out
}

// parseF5 は、DLE STX から（サムチェックまでの）形式 5 のフレームを読み、アクセス経路とそのあとを返す
func parseF5(b []byte, sum bool) (mcRoute, []byte, error) {
	if len(b) < 4 || b[0] != ctrlDLE || b[1] != ctrlSTX {
		return mcRoute{}, nil, errMCFormat
	}
	tail := 2
	if !sum {
		tail = 0
	}
	if len(b) < 2+4+tail || b[len(b)-tail-2] != ctrlDLE || b[len(b)-tail-1] != ctrlETX {
		return mcRoute{}, nil, errMCFormat
	}
	escaped := b[2 : len(b)-tail-2]
	payload := make([]byte, 0, len(escaped))
	for i := 0; i < len(escaped); i++ {
		c := escaped[i]
		if c == ctrlDLE {
			if i+1 >= len(escaped) || escaped[i+1] != ctrlDLE {
				return mcRoute{}, nil, fmt.Errorf("データの中の DLE が重なっていません")
			}
			i++
		}
		payload = append(payload, c)
	}
	if sum {
		want, err := parseSum(b[len(b)-2:])
		if err != nil {
			return mcRoute{}, nil, err
		}
		if mcSum(payload) != want {
			return mcRoute{}, nil, errMCSum
		}
	}
	if len(payload) < 2+1+mcRouteLength {
		return mcRoute{}, nil, errMCShort
	}
	if n := int(binary.LittleEndian.Uint16(payload)); n != len(payload)-2 {
		return mcRoute{}, nil, fmt.Errorf("データバイト数（%d）が、フレームの長さ（%d バイト）と合いません", n, len(payload)-2)
	}
	if payload[2] != mcFrameID4C {
		return mcRoute{}, nil, fmt.Errorf("フレーム ID が F8（4C フレーム）ではありません（%02X）", payload[2])
	}
	return parseRouteBinary(payload[3:]), payload[3+mcRouteLength:], nil
}

// --- 受け取ったバイトからフレームを切り出す ---

// nextMCFrame は、buf の先頭から 1 つのフレームを切り出す。まだ揃っていなければ ok = false。
// skip は、フレームの前にあった読み捨てるバイト数（フレームが揃っていなくても、読み捨ててよい分）
func nextMCFrame(buf []byte, format int, sum bool) (frame []byte, skip, used int, ok bool) {
	if format == mcFormat5 {
		start := bytes.Index(buf, []byte{ctrlDLE, ctrlSTX})
		if start < 0 {
			// 最後の 1 バイトが DLE なら、次に STX が来るかもしれないので残す
			if n := len(buf); n > 0 && buf[n-1] == ctrlDLE {
				return nil, n - 1, 0, false
			}
			return nil, len(buf), 0, false
		}
		for i := start + 2; i+1 < len(buf); i++ {
			if buf[i] != ctrlDLE {
				continue
			}
			if buf[i+1] == ctrlDLE {
				i++ // 重ねた DLE（データの 10H）
				continue
			}
			if buf[i+1] != ctrlETX {
				continue // 壊れたフレーム。parseF5 で誤りになる
			}
			end := i + 2
			if sum {
				end += 2
			}
			if end > len(buf) {
				return nil, start, 0, false
			}
			return buf[start:end], start, end, true
		}
		return nil, start, 0, false
	}
	start := bytes.IndexAny(buf, string([]byte{ctrlENQ, ctrlSTX, ctrlACK, ctrlNAK}))
	if start < 0 {
		return nil, len(buf), 0, false
	}
	i := bytes.Index(buf[start:], []byte{ctrlCR, ctrlLF})
	if i < 0 {
		return nil, start, 0, false
	}
	end := start + i + 2
	return buf[start:end], start, end, true
}
