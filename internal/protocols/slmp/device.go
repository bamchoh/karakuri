package slmp

import (
	"encoding/binary"
	"fmt"

	"karakuri/internal/domain/protocol"
)

// maxPoints は、デバイス 1 つの点数。
//
// 実機の点数（機種とパラメーターで違う）ではなく、SLMP の要求で指せる最大にする。
// デバイス番号は 3 バイトなので、0〜16,777,215（サブコマンド 0002 / 0003 の 4 バイトの番号も、この範囲で受け付ける）。
// メモリは、書かれたところだけを確保する
const maxPoints = 1 << 24

// deviceDef はデバイス 1 つの定義
type deviceDef struct {
	ID    string // デバイスの記号（"D"、"X"）。エリアの ID にも使う
	Name  string // 表示名
	Code  byte   // デバイスコード（バイナリコード）
	IsBit bool
	Hex   bool // デバイス番号を 16 進で書くデバイス（X・Y・B・W など）
}

// deviceDefs は対応するデバイス（MELSEC-Q / L のデバイスコード）
var deviceDefs = []deviceDef{
	{ID: "D", Name: "D（データレジスタ）", Code: 0xA8},
	{ID: "W", Name: "W（リンクレジスタ）", Code: 0xB4, Hex: true},
	{ID: "R", Name: "R（ファイルレジスタ）", Code: 0xAF},
	{ID: "ZR", Name: "ZR（ファイルレジスタ 連番）", Code: 0xB0, Hex: true},
	{ID: "M", Name: "M（内部リレー）", Code: 0x90, IsBit: true},
	{ID: "X", Name: "X（入力）", Code: 0x9C, IsBit: true, Hex: true},
	{ID: "Y", Name: "Y（出力）", Code: 0x9D, IsBit: true, Hex: true},
	{ID: "B", Name: "B（リンクリレー）", Code: 0xA0, IsBit: true, Hex: true},
	{ID: "L", Name: "L（ラッチリレー）", Code: 0x92, IsBit: true},
	{ID: "F", Name: "F（アナンシエータ）", Code: 0x93, IsBit: true},
	{ID: "V", Name: "V（エッジリレー）", Code: 0x94, IsBit: true},
	{ID: "S", Name: "S（ステップリレー）", Code: 0x98, IsBit: true},
	{ID: "TS", Name: "TS（タイマ 接点）", Code: 0xC1, IsBit: true},
	{ID: "TC", Name: "TC（タイマ コイル）", Code: 0xC0, IsBit: true},
	{ID: "TN", Name: "TN（タイマ 現在値）", Code: 0xC2},
	{ID: "SS", Name: "SS（積算タイマ 接点）", Code: 0xC7, IsBit: true},
	{ID: "SC", Name: "SC（積算タイマ コイル）", Code: 0xC6, IsBit: true},
	{ID: "SN", Name: "SN（積算タイマ 現在値）", Code: 0xC8},
	{ID: "CS", Name: "CS（カウンタ 接点）", Code: 0xC4, IsBit: true},
	{ID: "CC", Name: "CC（カウンタ コイル）", Code: 0xC3, IsBit: true},
	{ID: "CN", Name: "CN（カウンタ 現在値）", Code: 0xC5},
	{ID: "SM", Name: "SM（特殊リレー）", Code: 0x91, IsBit: true},
	{ID: "SD", Name: "SD（特殊レジスタ）", Code: 0xA9},
	{ID: "SB", Name: "SB（リンク特殊リレー）", Code: 0xA1, IsBit: true, Hex: true},
	{ID: "SW", Name: "SW（リンク特殊レジスタ）", Code: 0xB5, Hex: true},
	{ID: "DX", Name: "DX（ダイレクト入力）", Code: 0xA2, IsBit: true, Hex: true},
	{ID: "DY", Name: "DY（ダイレクト出力）", Code: 0xA3, IsBit: true, Hex: true},
	{ID: "Z", Name: "Z（インデックスレジスタ）", Code: 0xCC},
}

func deviceByID(id string) (deviceDef, bool) {
	for _, d := range deviceDefs {
		if d.ID == id {
			return d, true
		}
	}
	return deviceDef{}, false
}

func deviceByCode(code byte) (deviceDef, bool) {
	for _, d := range deviceDefs {
		if d.Code == code {
			return d, true
		}
	}
	return deviceDef{}, false
}

// memoryAreas は、ホストに見せるメモリエリアの一覧を返す（デバイス 1 つが、エリア 1 つ）
func memoryAreas() []protocol.MemoryArea {
	areas := make([]protocol.MemoryArea, len(deviceDefs))
	for i, d := range deviceDefs {
		areas[i] = protocol.MemoryArea{ID: d.ID, DisplayName: d.Name, IsBit: d.IsBit, Size: maxPoints, ShortName: d.ID, HexAddress: d.Hex}
	}
	return areas
}

// target は、デバイスと番号（要求の「デバイス番号 + デバイスコード」が指すもの）
type target struct {
	def    deviceDef
	number uint32
}

// label は、t から n 個先のデバイスを、三菱の書き方（"D100"、"X1F"）で返す
func (t target) label(n int) string {
	if t.def.Hex {
		return fmt.Sprintf("%s%X", t.def.ID, t.number+uint32(n))
	}
	return fmt.Sprintf("%s%d", t.def.ID, t.number+uint32(n))
}

// remaining は、t からデバイスの終わりまでの点数
func (t target) remaining() int {
	return maxPoints - int(t.number)
}

// deviceLength は、要求の中のデバイスの指定のバイト数（番号 3 + コード 1、長い形は 番号 4 + コード 2）
func deviceLength(long bool) int {
	if long {
		return 6
	}
	return 4
}

// parseTarget は、要求の中のデバイスの指定を読む。読めなければ、0 以外の終了コードを返す
func parseTarget(p []byte, long bool) (target, uint16) {
	var number uint32
	var code uint16
	if long {
		number = binary.LittleEndian.Uint32(p)
		code = binary.LittleEndian.Uint16(p[4:])
	} else {
		number = uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16
		code = uint16(p[3])
	}
	if code > 0xFF {
		return target{}, endDevice
	}
	def, ok := deviceByCode(byte(code))
	if !ok {
		return target{}, endDevice
	}
	if number >= maxPoints {
		return target{def: def}, endRange
	}
	return target{def: def, number: number}, endOK
}

// appendTo は、デバイスの指定（番号 3 バイト + コード 1 バイト）を足す
func (t target) appendTo(dst []byte) []byte {
	return append(dst, byte(t.number), byte(t.number>>8), byte(t.number>>16), t.def.Code)
}

// targetFromStore は、エリアの ID とアドレス（変数のマッピングで使うもの）からデバイスを決める
func targetFromStore(area string, address uint32) (target, error) {
	def, ok := deviceByID(area)
	if !ok {
		return target{}, fmt.Errorf("不明なデバイスです: %s", area)
	}
	if address >= maxPoints {
		return target{def: def}, fmt.Errorf("アドレスがデバイスの範囲を超えています")
	}
	return target{def: def, number: address}, nil
}
