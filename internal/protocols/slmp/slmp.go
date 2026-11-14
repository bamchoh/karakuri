// Package slmp は、三菱電機の SLMP（Seamless Message Protocol。MC プロトコルの 3E / 4E フレーム）のサーバーとクライアント。
//
// 対応するのは、TCP と UDP の、バイナリコードの 3E フレームと 4E フレーム:
//
//	3E 要求: 50 00 | ネットワーク番号(1) 局番(1) 要求先ユニット I/O 番号(2) マルチドロップ局番(1) | データ長(2) | 監視タイマ(2) コマンド(2) サブコマンド(2) データ
//	3E 応答: D0 00 | （同じ 5 バイト）                                                        | データ長(2) | 終了コード(2) データ
//	4E     : サブヘッダーが 54 00（応答は D4 00）で、そのあとにシリアル番号(2) と 00 00 が入る。ほかは 3E と同じ
//
// 数値はすべてリトルエンディアン。データ長は、そのあと（監視タイマ・終了コードから終わりまで）のバイト数。
// 異常終了の応答は、終了コードのあとに、要求の 5 バイト・コマンド・サブコマンドが続く。
//
// ASCII コードのフレームには対応していない。
package slmp

import (
	"encoding/binary"
	"fmt"
	"io"

	"karakuri/internal/domain/protocol"
)

// ProtocolSLMP は SLMP のプロトコルタイプ（サーバーとクライアントで共通）
const ProtocolSLMP protocol.ProtocolType = "slmp"

const (
	defaultPort = 5007

	sub3ERequest  = 0x50
	sub3EResponse = 0xD0
	sub4ERequest  = 0x54
	sub4EResponse = 0xD4

	routeLength = 5 // ネットワーク番号・局番・ユニット I/O 番号(2)・マルチドロップ局番

	// maxDataLength は、データ長の欄に入れられる値の上限として受け付ける大きさ
	// （いちばん大きい要求は、ワードの一括書き込み 960 点で 2000 バイトほど）
	maxDataLength = 8192

	// 1 回の要求で読み書きできる点数（バイナリコード）
	maxWordPoints   = 960  // ワード単位
	maxBitPoints    = 7168 // ビット単位
	maxRandomPoints = 192  // ランダム読み出し（ワードとダブルワードの合計）
	maxRandomWrite  = 160  // ランダム書き込み（ワード × 12 + ダブルワード × 14 ≦ 1920）
	maxRandomBits   = 188  // ランダム書き込み（ビット単位）
)

// コマンド
const (
	cmdBatchRead   uint16 = 0x0401
	cmdBatchWrite  uint16 = 0x1401
	cmdRandomRead  uint16 = 0x0403
	cmdRandomWrite uint16 = 0x1402
	cmdTypeName    uint16 = 0x0101
	cmdSelfTest    uint16 = 0x0619
)

// サブコマンドのビット
const (
	subBitUnits uint16 = 0x0001 // ビット単位
	subLong     uint16 = 0x0002 // デバイス番号 4 バイト・デバイスコード 2 バイト（MELSEC iQ-R の形）
)

// 終了コード
const (
	endOK       uint16 = 0x0000
	endPoints   uint16 = 0xC051 // 点数が範囲外
	endRange    uint16 = 0xC056 // 最大のアドレスを超える
	endCommand  uint16 = 0xC059 // コマンド・サブコマンドの誤り（対応していない）
	endDevice   uint16 = 0xC05B // 指定したデバイスを読み書きできない
	endRequest  uint16 = 0xC05C // 要求の内容の誤り
	endLength   uint16 = 0xC061 // 要求のデータ長が合わない
	endSelfTest uint16 = 0xC052 // （折り返しテストのデータ数など）点数が範囲外
)

var commandNames = map[uint16]string{
	cmdBatchRead:   "一括読み出し",
	cmdBatchWrite:  "一括書き込み",
	cmdRandomRead:  "ランダム読み出し",
	cmdRandomWrite: "ランダム書き込み",
	cmdTypeName:    "形名の読み出し",
	cmdSelfTest:    "折り返しテスト",
}

var endCodeNames = map[uint16]string{
	0x0055: "RUN 中書き込みが許可されていない",
	0xC050: "ASCII コードの変換ができない",
	0xC051: "点数が範囲外",
	0xC052: "点数が範囲外",
	0xC053: "点数が範囲外",
	0xC054: "点数が範囲外",
	0xC056: "最大のアドレスを超えている",
	0xC058: "要求のデータ長が合わない",
	0xC059: "コマンド・サブコマンドの誤り（対応していない）",
	0xC05B: "指定したデバイスを読み書きできない",
	0xC05C: "要求の内容の誤り",
	0xC05D: "モニタの登録がされていない",
	0xC05F: "対象のユニットでは実行できない",
	0xC060: "要求の内容の誤り（ビットデバイスのデータなど）",
	0xC061: "要求のデータ長が合わない",
	0xC06F: "フレームのコード（ASCII / バイナリ）が設定と違う",
	0xC0D8: "ブロック数が範囲外",
	0xC200: "リモートパスワードの誤り",
	0xC201: "リモートパスワードでロックされている",
}

func endCodeText(code uint16) string {
	name := endCodeNames[code]
	if name == "" {
		name = "不明な終了コード"
	}
	return fmt.Sprintf("0x%04X %s", code, name)
}

// frame は、要求または応答のフレーム 1 つ（サブヘッダーからデータ長までを読んだもの）
type frame struct {
	Kind   byte // サブヘッダーの 1 バイト目（50 / D0 / 54 / D4）
	Serial uint16
	Route  [routeLength]byte
	Body   []byte // データ長のあと（要求は 監視タイマ以降、応答は 終了コード以降）
}

func (f frame) is4E() bool {
	return f.Kind == sub4ERequest || f.Kind == sub4EResponse
}

func (f frame) isRequest() bool {
	return f.Kind == sub3ERequest || f.Kind == sub4ERequest
}

// headLength は、サブヘッダーの 1 バイト目から、データ長までのバイト数を返す（知らないサブヘッダーなら 0）
func headLength(kind byte) int {
	switch kind {
	case sub3ERequest, sub3EResponse:
		return 2 + routeLength + 2
	case sub4ERequest, sub4EResponse:
		return 2 + 4 + routeLength + 2
	}
	return 0
}

// parseFrame は、1 つのフレームが入ったバイト列（UDP のデータグラムなど）を読む
func parseFrame(b []byte) (frame, error) {
	if len(b) < 2 {
		return frame{}, fmt.Errorf("フレームが短すぎます（%d バイト）", len(b))
	}
	n := headLength(b[0])
	if n == 0 || b[1] != 0x00 {
		return frame{}, fmt.Errorf("SLMP の 3E / 4E フレーム（バイナリコード）ではありません（先頭 %02X %02X）", b[0], b[1])
	}
	if len(b) < n {
		return frame{}, fmt.Errorf("フレームが短すぎます（%d バイト）", len(b))
	}
	f := frame{Kind: b[0]}
	p := b[2:]
	if f.is4E() {
		f.Serial = binary.LittleEndian.Uint16(p)
		p = p[4:]
	}
	copy(f.Route[:], p)
	length := int(binary.LittleEndian.Uint16(p[routeLength:]))
	if len(b) != n+length {
		return frame{}, fmt.Errorf("データ長（%d）が、フレームの長さ（%d バイト）と合いません", length, len(b))
	}
	f.Body = b[n:]
	return f, nil
}

// readFrame は、ストリーム（TCP）からフレームを 1 つ読み、そのバイト列を返す
func readFrame(r io.Reader) ([]byte, error) {
	head := make([]byte, 2, 16)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, err
	}
	n := headLength(head[0])
	if n == 0 || head[1] != 0x00 {
		return head, fmt.Errorf("SLMP の 3E / 4E フレーム（バイナリコード）ではありません（先頭 %02X %02X）", head[0], head[1])
	}
	head = head[:n]
	if _, err := io.ReadFull(r, head[2:]); err != nil {
		return head, err
	}
	length := int(binary.LittleEndian.Uint16(head[n-2:]))
	if length > maxDataLength {
		return head, fmt.Errorf("データ長（%d）が大きすぎます", length)
	}
	raw := make([]byte, n+length)
	copy(raw, head)
	if _, err := io.ReadFull(r, raw[n:]); err != nil {
		return raw[:n], err
	}
	return raw, nil
}

// appendFrame は、フレームを足す。kind はサブヘッダーの 1 バイト目、body はデータ長のあと
func appendFrame(dst []byte, kind byte, serial uint16, route [routeLength]byte, body []byte) []byte {
	dst = append(dst, kind, 0x00)
	if kind == sub4ERequest || kind == sub4EResponse {
		dst = binary.LittleEndian.AppendUint16(dst, serial)
		dst = append(dst, 0x00, 0x00)
	}
	dst = append(dst, route[:]...)
	dst = binary.LittleEndian.AppendUint16(dst, uint16(len(body)))
	return append(dst, body...)
}

// reply は、要求 f への応答のフレームを作る。異常終了なら、終了コードのあとに要求の内容（経路・コマンド・サブコマンド）を付ける
func (f frame) reply(end uint16, command, sub uint16, data []byte) []byte {
	kind := byte(sub3EResponse)
	if f.is4E() {
		kind = sub4EResponse
	}
	body := binary.LittleEndian.AppendUint16(make([]byte, 0, 2+len(data)+9), end)
	if end != endOK {
		body = append(body, f.Route[:]...)
		body = binary.LittleEndian.AppendUint16(body, command)
		body = binary.LittleEndian.AppendUint16(body, sub)
	} else {
		body = append(body, data...)
	}
	return appendFrame(make([]byte, 0, 16+len(body)), kind, f.Serial, f.Route, body)
}
