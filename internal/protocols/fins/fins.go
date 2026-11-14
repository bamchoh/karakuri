// Package fins は、オムロンの FINS（Factory Interface Network Service）のサーバーとクライアント。
//
// 対応するのは Ethernet の 2 つの運び方:
//   - FINS/UDP: FINS フレームをそのまま UDP のデータグラムに入れる
//   - FINS/TCP: FINS フレームの前に 16 バイトのヘッダー（"FINS"・長さ・コマンド・エラーコード）を付ける。
//     つないだ直後に、ノードアドレスを決めるやりとりを 1 回行う
//
// FINS フレームは、10 バイトのヘッダー（ICF RSV GCT DNA DA1 DA2 SNA SA1 SA2 SID）と、
// コマンドコード 2 バイト（MRC SRC）、パラメーターでできている。
// 応答は、ヘッダー（宛先と送信元を入れ替えたもの）・コマンドコード・終了コード 2 バイト・データ。
package fins

import (
	"encoding/binary"
	"fmt"

	"karakuri/internal/domain/protocol"
)

// ProtocolFINS は FINS のプロトコルタイプ（サーバーとクライアントで共通）
const ProtocolFINS protocol.ProtocolType = "fins"

const (
	headerLength = 10
	// maxFrameLength は FINS フレームの最大長（Ethernet の FINS の上限）
	maxFrameLength = 2012

	icfResponse   = 0x40 // ICF のビット 6: 応答
	icfNoResponse = 0x01 // ICF のビット 0: 応答不要

	defaultPort = 9600

	// 1 回のリクエストで読める・書ける個数（フレームの最大長から決まる、Ethernet の FINS の値）
	maxReadCount  = 999
	maxWriteCount = 996
)

// コマンドコード（MRC << 8 | SRC）
const (
	cmdMemoryRead     uint16 = 0x0101
	cmdMemoryWrite    uint16 = 0x0102
	cmdMemoryFill     uint16 = 0x0103
	cmdMultipleRead   uint16 = 0x0104
	cmdCPUUnitData    uint16 = 0x0501
	cmdCPUUnitStatus  uint16 = 0x0601
	cmdClockRead      uint16 = 0x0701
	endNormal         uint16 = 0x0000
	endUndefinedCmd   uint16 = 0x0401
	endTooLong        uint16 = 0x1001
	endTooShort       uint16 = 0x1002
	endCountMismatch  uint16 = 0x1003
	endNoAreaType     uint16 = 0x1101
	endAddressRange   uint16 = 0x1103
	endAddressExceeds uint16 = 0x1104
	endResponseLong   uint16 = 0x110B
	endParameter      uint16 = 0x110C
)

var commandNames = map[uint16]string{
	cmdMemoryRead:    "メモリエリアの読み出し",
	cmdMemoryWrite:   "メモリエリアの書き込み",
	cmdMemoryFill:    "メモリエリアの一括書き込み",
	cmdMultipleRead:  "メモリエリアの複合読み出し",
	cmdCPUUnitData:   "CPU ユニット情報の読み出し",
	cmdCPUUnitStatus: "CPU ユニットステータスの読み出し",
	cmdClockRead:     "時計の読み出し",
}

var endCodeNames = map[uint16]string{
	0x0101: "自ノードがネットワークに参加していない",
	0x0102: "トークンのタイムアウト",
	0x0201: "相手ノードがネットワークに参加していない",
	0x0202: "該当するユニットがない",
	0x0205: "応答のタイムアウト",
	0x0401: "未定義のコマンド",
	0x0402: "対応していない機種・バージョン",
	0x0501: "相手アドレスの設定の誤り",
	0x1001: "コマンドが長すぎる",
	0x1002: "コマンドが短すぎる",
	0x1003: "要素数とデータ数が合わない",
	0x1004: "コマンドの形式の誤り",
	0x1101: "エリアの種別がない",
	0x1103: "先頭アドレスが範囲外",
	0x1104: "アドレスの範囲を超えている",
	0x110B: "応答が長すぎる",
	0x110C: "パラメーターの誤り",
	0x2002: "プロテクト中",
	0x2101: "読み出し専用のエリア",
	0x2102: "プロテクト中で書き込めない",
	0x2108: "運転モードが違うので書き込めない",
	0x2201: "運転モードが違う（実行中）",
	0x2202: "運転モードが違う（停止中）",
}

// header は FINS フレームのヘッダー
type header struct {
	ICF, RSV, GCT byte
	DNA, DA1, DA2 byte
	SNA, SA1, SA2 byte
	SID           byte
}

func parseHeader(b []byte) header {
	return header{ICF: b[0], RSV: b[1], GCT: b[2], DNA: b[3], DA1: b[4], DA2: b[5], SNA: b[6], SA1: b[7], SA2: b[8], SID: b[9]}
}

func (h header) append(dst []byte) []byte {
	return append(dst, h.ICF, h.RSV, h.GCT, h.DNA, h.DA1, h.DA2, h.SNA, h.SA1, h.SA2, h.SID)
}

// reply は、このヘッダーのリクエストへの応答のヘッダーを返す（宛先と送信元を入れ替える）
func (h header) reply() header {
	return header{
		ICF: 0xC0, RSV: 0x00, GCT: 0x02,
		DNA: h.SNA, DA1: h.SA1, DA2: h.SA2,
		SNA: h.DNA, SA1: h.DA1, SA2: h.DA2,
		SID: h.SID,
	}
}

// --- メモリエリア ---

// エリアの ID（DataStore とクライアントのマッピングで共通）。
// ビットのエリアは、同じ名前のワードのエリアと同じメモリを、ビットごとに見たもの（アドレス = ワード × 16 + ビット）。
// ホストには MemoryArea.BitsOf で知らせる
const (
	AreaCIO = "CIO"
	AreaWR  = "WR"
	AreaHR  = "HR"
	AreaAR  = "AR"
	AreaDM  = "DM"
	AreaEM0 = "EM0"

	bitSuffix = "_BIT"
)

// areaDef はメモリエリア 1 つの定義
type areaDef struct {
	ID       string
	Name     string // 表示名
	Prefix   string // アドレスの前に付ける文字（"D100" の "D"）
	WordCode byte   // FINS のエリア種別（ワード）
	BitCode  byte   // FINS のエリア種別（ビット）
	Words    int    // ワード数
	BitArea  bool   // ビットのエリアとしても見せるか
}

// areaDefs は対応するエリア（CS / CJ シリーズの種別コードと大きさ）
var areaDefs = []areaDef{
	{ID: AreaCIO, Name: "CIO（チャネル I/O）", Prefix: "CIO ", WordCode: 0xB0, BitCode: 0x30, Words: 6144, BitArea: true},
	{ID: AreaWR, Name: "WR（内部補助リレー）", Prefix: "W", WordCode: 0xB1, BitCode: 0x31, Words: 512, BitArea: true},
	{ID: AreaHR, Name: "HR（保持リレー）", Prefix: "H", WordCode: 0xB2, BitCode: 0x32, Words: 1536, BitArea: true},
	{ID: AreaAR, Name: "AR（特殊補助リレー）", Prefix: "A", WordCode: 0xB3, BitCode: 0x33, Words: 960, BitArea: true},
	{ID: AreaDM, Name: "DM（データメモリ）", Prefix: "D", WordCode: 0x82, BitCode: 0x02, Words: 32768},
	{ID: AreaEM0, Name: "EM バンク 0（拡張データメモリ）", Prefix: "E0_", WordCode: 0xA0, BitCode: 0x20, Words: 32768},
}

func areaByID(id string) (areaDef, bool) {
	for _, a := range areaDefs {
		if a.ID == id {
			return a, true
		}
	}
	return areaDef{}, false
}

// areaByCode はエリア種別から、エリアと、ビットの種別かどうかを返す。
// CS / CJ の種別に加えて、古い機種（CV モード）の CIO の種別（0x80 / 0x00）と、EM のカレントバンク（0x98）も受け付ける
func areaByCode(code byte) (def areaDef, isBit, ok bool) {
	switch code {
	case 0x80:
		code = 0xB0
	case 0x00:
		code = 0x30
	case 0x98:
		code = 0xA0
	}
	for _, a := range areaDefs {
		if a.WordCode == code {
			return a, false, true
		}
		if a.BitCode == code {
			return a, true, true
		}
	}
	return areaDef{}, false, false
}

// memoryAreas は、ホストに見せるメモリエリアの一覧を返す（ワードのエリアのあとに、ビットのエリア）
func memoryAreas() []protocol.MemoryArea {
	areas := make([]protocol.MemoryArea, 0, len(areaDefs)*2)
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
	return areas
}

// wordLabel / bitLabel は、オムロンの書き方のアドレス（"D100"、"CIO 100.05"）を返す
func wordLabel(a areaDef, word int) string {
	return fmt.Sprintf("%s%d", a.Prefix, word)
}

func bitLabel(a areaDef, word, bit int) string {
	return fmt.Sprintf("%s%d.%02d", a.Prefix, word, bit)
}

// appendAddress は、エリア種別・アドレス（ワード 2 バイト + ビット 1 バイト）を足す
func appendAddress(dst []byte, code byte, word, bit int) []byte {
	dst = append(dst, code)
	dst = binary.BigEndian.AppendUint16(dst, uint16(word))
	return append(dst, byte(bit))
}

func endCodeText(code uint16) string {
	name := endCodeNames[code]
	if name == "" {
		name = "不明な終了コード"
	}
	return fmt.Sprintf("0x%04X %s", code, name)
}
