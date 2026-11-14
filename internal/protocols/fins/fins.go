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

func endCodeText(code uint16) string {
	name := endCodeNames[code]
	if name == "" {
		name = "不明な終了コード"
	}
	return fmt.Sprintf("0x%04X %s", code, name)
}
