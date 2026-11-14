package fins

import (
	"encoding/binary"
	"fmt"
	"strings"

	"karakuri/internal/domain/protocol"
)

// --- メモリエリア ---

// エリアの ID（DataStore とクライアントのマッピングで共通）。
//
//   - ビットのエリア（"CIO_BIT"）は、同じ名前のワードのエリアと同じメモリを、ビットごとに見たもの
//     （アドレス = ワード × 16 + ビット）。ホストには MemoryArea.BitsOf で知らせる
//   - フラグのエリア（"T_FLAG"）は、タイマ・カウンタのアップフラグ。現在値（"T"）とは別のメモリで、
//     アドレスはタイマ・カウンタの番号
//   - EM のバンクは "EM0"〜"EM18"（バンクの番号は 16 進）
const (
	AreaCIO = "CIO"
	AreaWR  = "WR"
	AreaHR  = "HR"
	AreaAR  = "AR"
	AreaDM  = "DM"
	AreaT   = "T"
	AreaC   = "C"
	AreaEM0 = "EM0"

	bitSuffix  = "_BIT"
	flagSuffix = "_FLAG"

	// emBanks は EM のバンクの数（CJ2 の最大。バンク 0〜18（16 進））
	emBanks = 0x19
	// counterBase は、FINS のアドレスでの、カウンタの先頭（タイマと同じエリア種別を使い、アドレスで分ける）
	counterBase = 0x8000
)

// areaDef はメモリエリア 1 つの定義
type areaDef struct {
	ID       string
	Name     string // 表示名
	Prefix   string // アドレスの前に付ける文字（"D100" の "D"）
	WordCode byte   // FINS のエリア種別（ワード）
	BitCode  byte   // FINS のエリア種別（ビット）
	Words    int    // ワード数
	Base     int    // FINS のアドレスでの先頭（カウンタは 0x8000。ほかは 0）
	BitArea  bool   // ワードのビットを、ビットのエリアとしても見せるか
	// Flags が true なら、BitCode はワードの中のビットではなく、番号ごとに 1 つあるフラグ（タイマ・カウンタのアップフラグ）
	Flags    bool
	FlagName string // フラグのエリアの表示名
}

// areaDefs は対応するエリア（CS / CJ シリーズの種別コードと大きさ）
var areaDefs = buildAreaDefs()

func buildAreaDefs() []areaDef {
	defs := []areaDef{
		{ID: AreaCIO, Name: "CIO（チャネル I/O）", Prefix: "CIO ", WordCode: 0xB0, BitCode: 0x30, Words: 6144, BitArea: true},
		{ID: AreaWR, Name: "WR（内部補助リレー）", Prefix: "W", WordCode: 0xB1, BitCode: 0x31, Words: 512, BitArea: true},
		{ID: AreaHR, Name: "HR（保持リレー）", Prefix: "H", WordCode: 0xB2, BitCode: 0x32, Words: 1536, BitArea: true},
		{ID: AreaAR, Name: "AR（特殊補助リレー）", Prefix: "A", WordCode: 0xB3, BitCode: 0x33, Words: 960, BitArea: true},
		{ID: AreaDM, Name: "DM（データメモリ）", Prefix: "D", WordCode: 0x82, BitCode: 0x02, Words: 32768, BitArea: true},
		{ID: AreaT, Name: "T（タイマ 現在値）", FlagName: "T（タイマ アップフラグ）", Prefix: "T", WordCode: 0x89, BitCode: 0x09, Words: 4096, Flags: true},
		{ID: AreaC, Name: "C（カウンタ 現在値）", FlagName: "C（カウンタ アップフラグ）", Prefix: "C", WordCode: 0x89, BitCode: 0x09, Words: 4096, Base: counterBase, Flags: true},
	}
	for bank := 0; bank < emBanks; bank++ {
		// バンク 0〜F は 0xA0〜0xAF / 0x20〜0x2F、バンク 10〜18 は 0x60〜0x68 / 0xE0〜0xE8
		wordCode, bitCode := byte(0xA0+bank), byte(0x20+bank)
		if bank > 0x0F {
			wordCode, bitCode = byte(0x60+bank-0x10), byte(0xE0+bank-0x10)
		}
		defs = append(defs, areaDef{
			ID:   fmt.Sprintf("EM%X", bank),
			Name: fmt.Sprintf("EM バンク %X（拡張データメモリ）", bank), Prefix: fmt.Sprintf("E%X_", bank),
			WordCode: wordCode, BitCode: bitCode, Words: 32768, BitArea: true,
		})
	}
	return defs
}

func areaByID(id string) (areaDef, bool) {
	for _, a := range areaDefs {
		if a.ID == id {
			return a, true
		}
	}
	return areaDef{}, false
}

// memoryAreas は、ホストに見せるメモリエリアの一覧を返す（ワードのエリア、ビットのエリア、フラグのエリアの順）
func memoryAreas() []protocol.MemoryArea {
	areas := make([]protocol.MemoryArea, 0, len(areaDefs)+8)
	for _, a := range areaDefs {
		areas = append(areas, protocol.MemoryArea{ID: a.ID, DisplayName: a.Name, Size: uint32(a.Words), ShortName: a.ID})
	}
	for _, a := range areaDefs {
		if a.BitArea {
			areas = append(areas, protocol.MemoryArea{
				ID: a.ID + bitSuffix, DisplayName: a.Name + " ビット", IsBit: true, Size: uint32(a.Words * 16), ShortName: a.ID, BitsOf: a.ID,
			})
		}
	}
	for _, a := range areaDefs {
		if a.Flags {
			areas = append(areas, protocol.MemoryArea{
				ID: a.ID + flagSuffix, DisplayName: a.FlagName, IsBit: true, Size: uint32(a.Words), ShortName: a.ID + "F",
			})
		}
	}
	return areas
}

// --- エリアの中の場所 ---

// target は、エリアの中の場所（リクエストのエリア種別とアドレスが指すもの）
type target struct {
	def   areaDef
	isBit bool // ビットの種別（ワードの中のビットか、フラグ）
	word  int  // エリアの中の番号（0 始まり。ワードの番号、またはタイマ・カウンタの番号）
	bit   int  // ワードの中のビット（フラグのときは 0）
}

func (t target) isFlag() bool {
	return t.isBit && t.def.Flags
}

// code は FINS のエリア種別を返す
func (t target) code() byte {
	if t.isBit {
		return t.def.BitCode
	}
	return t.def.WordCode
}

// appendTo は、エリア種別(1) + アドレス（ワード 2 バイト + ビット 1 バイト）を足す
func (t target) appendTo(dst []byte) []byte {
	dst = append(dst, t.code())
	dst = binary.BigEndian.AppendUint16(dst, uint16(t.def.Base+t.word))
	return append(dst, byte(t.bit))
}

// storeArea は、DataStore に渡すエリアの ID を返す。ビットのエリアを持つエリアのビットは、ビットのエリアの ID にする
// （ビットのエリアにマッピングした変数に、書き込みが反映されるようにするため）
func (t target) storeArea() string {
	switch {
	case t.isFlag():
		return t.def.ID + flagSuffix
	case t.isBit && t.def.BitArea:
		return t.def.ID + bitSuffix
	}
	return t.def.ID
}

// bitAddress は、ビットを DataStore で読み書きするときのアドレスを返す（フラグは番号、ほかは ワード × 16 + ビット）
func (t target) bitAddress() uint32 {
	if t.isFlag() {
		return uint32(t.word)
	}
	return uint32(t.word)*16 + uint32(t.bit)
}

// remaining は、t からエリアの終わりまでの個数（ワード・ビット・フラグの数）
func (t target) remaining() int {
	if t.isBit && !t.def.Flags {
		return t.def.Words*16 - int(t.bitAddress())
	}
	return t.def.Words - t.word
}

// label は、t から n 個先のアドレスを、オムロンの書き方（"D100"、"CIO 100.05"、"T5"）で返す
func (t target) label(n int) string {
	if t.isBit && !t.def.Flags {
		addr := int(t.bitAddress()) + n
		return fmt.Sprintf("%s%d.%02d", t.def.Prefix, addr/16, addr%16)
	}
	return fmt.Sprintf("%s%d", t.def.Prefix, t.word+n)
}

// targetFromWire は、FINS のエリア種別とアドレスから場所を決める。読めなければ、0 以外の終了コードを返す。
//
// CS / CJ の種別に加えて、古い機種（CV モード）の CIO の種別（0x80 / 0x00）と、EM のカレントバンク（0x98。バンク 0 として扱う）も受け付ける。
// タイマとカウンタは同じ種別で、アドレスが 0x8000 以上ならカウンタ
func targetFromWire(code byte, address, bit int) (target, uint16) {
	switch code {
	case 0x80:
		code = 0xB0
	case 0x00:
		code = 0x30
	case 0x98:
		code = 0xA0
	}
	var t target
	found := false
	for _, a := range areaDefs {
		isBit := a.BitCode == code
		if (a.WordCode != code && !isBit) || address < a.Base {
			continue
		}
		if !found || a.Base > t.def.Base {
			t = target{def: a, isBit: isBit, word: address - a.Base, bit: bit}
			found = true
		}
	}
	if !found {
		return target{}, endNoAreaType
	}
	switch {
	case t.word >= t.def.Words:
		return t, endAddressRange
	case t.isBit && !t.def.Flags && bit > 15:
		return t, endAddressRange
	case (!t.isBit || t.def.Flags) && bit != 0:
		return t, endAddressRange
	}
	return t, endNormal
}

// targetFromStore は、DataStore のエリアの ID とアドレス（変数のマッピングで使うもの）から場所を決める
func targetFromStore(area string, address uint32) (target, error) {
	var t target
	switch {
	case strings.HasSuffix(area, flagSuffix):
		def, ok := areaByID(strings.TrimSuffix(area, flagSuffix))
		if !ok || !def.Flags {
			return target{}, fmt.Errorf("不明なエリアです: %s", area)
		}
		t = target{def: def, isBit: true, word: int(address)}
	case strings.HasSuffix(area, bitSuffix):
		def, ok := areaByID(strings.TrimSuffix(area, bitSuffix))
		if !ok || !def.BitArea {
			return target{}, fmt.Errorf("不明なエリアです: %s", area)
		}
		t = target{def: def, isBit: true, word: int(address / 16), bit: int(address % 16)}
	default:
		def, ok := areaByID(area)
		if !ok {
			return target{}, fmt.Errorf("不明なエリアです: %s", area)
		}
		t = target{def: def, word: int(address)}
	}
	if t.word >= t.def.Words {
		return t, fmt.Errorf("アドレスがエリアの範囲を超えています")
	}
	return t, nil
}
