package fins

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

// Host Link（SYSMAC WAY）の上に FINS のコマンドを載せる形（ヘッダーコード "FA"）。シリアルで FINS を使うときの運び方。
//
// フレームは文字（ASCII）で、数値は 16 進の大文字:
//
//	コマンド: '@' 号機番号(10 進 2 桁) "FA" 応答待ち時間(16 進 1 桁) FINS(16 進) FCS(2 桁) '*' CR
//	応答:     '@' 号機番号(10 進 2 桁) "FA" 終了コード(2 桁) FINS(16 進) FCS(2 桁) '*' CR
//
//   - FINS の部分は、ICF の最上位ビット（ゲートウェイを使う）で 2 つの形がある。
//     0 なら短い形（ICF DA2 SA2 SID の 4 バイト。シリアルでつながった CPU 宛て）、1 なら FINS のヘッダー 10 バイトすべて
//     （ICF RSV GCT DNA DA1 DA2 SNA SA1 SA2 SID。ネットワークの先の宛て）。応答も、コマンドと同じ形で返す
//   - 応答の "FA" の直後の 2 桁は Host Link の終了コード（00 が正常）
//   - FCS は、'@' から FCS の直前までの文字の XOR を、16 進 2 桁で書いたもの
//   - 1 フレームに入らない長い応答は、複数のフレームに分けて送られる。途中のフレームは '*' を付けずに FCS CR で終わり、
//     受け取った側が CR を送ると次のフレームが来る（2 つ目からは '@' 号機番号・ヘッダーコードがなく、続きの文字だけ）
//
// 確かめた資料: 実機の通信の例（コマンド "@00FA5800007013C...0F*" と応答 "@00FA00C000020...46*"。FCS の計算が合う）と、
// 公開されている実装（短い形のコマンドと、応答の "FA" "00" "40"(ICF) DA2 SA2 SID の並び）。
// オムロンのマニュアル（W342）とは突き合わせていない。

const (
	hostLinkStart     = '@'
	hostLinkCR        = '\r'
	hostLinkHeaderFA  = "FA" // FINS のコマンドを載せる
	hostLinkHeaderIC  = "IC" // 未定義のコマンドへの応答
	hostLinkEndNormal = "00"
	hostLinkEndFCS    = "13" // FCS の誤り
	hostLinkEndFormat = "14" // 形式の誤り

	// maxHostLinkUnit は号機番号の最大（00〜31）
	maxHostLinkUnit = 31
	// shortHeaderLength は短い形の FINS のヘッダー（ICF DA2 SA2 SID）の長さ
	shortHeaderLength = 4
	// icfGateway は ICF の最上位ビット。1 なら FINS のヘッダー 10 バイトすべてを載せる
	icfGateway = 0x80
	// maxHostLinkText は、受け付ける 1 つのフレーム（分けて届いたものはつないだもの）の最大の文字数
	maxHostLinkText = 2 * (maxFrameLength + 16)
)

var hostLinkEndNames = map[string]string{
	"00": "正常",
	"13": "FCS の誤り",
	"14": "形式の誤り",
	"18": "フレームの長さの誤り",
}

func hostLinkEndText(code string) string {
	name := hostLinkEndNames[code]
	if name == "" {
		name = "不明な終了コード"
	}
	return code + " " + name
}

// hostLinkFCS は、b の文字の XOR
func hostLinkFCS(b []byte) byte {
	var x byte
	for _, c := range b {
		x ^= c
	}
	return x
}

// appendHostLinkTail は、FCS と '*' CR を付ける（FCS は dst[start:] の文字から計算する）
func appendHostLinkTail(dst []byte, start int) []byte {
	dst = fmt.Appendf(dst, "%02X", hostLinkFCS(dst[start:]))
	return append(dst, '*', hostLinkCR)
}

func appendHostLinkHead(dst []byte, unit int, code string) []byte {
	dst = append(dst, hostLinkStart)
	dst = fmt.Appendf(dst, "%02d", unit)
	return append(dst, code...)
}

func appendUpperHex(dst, b []byte) []byte {
	return fmt.Appendf(dst, "%X", b)
}

// finsToHostLink は、FINS のフレーム（ヘッダー 10 バイト付き）を Host Link に載せる形にする。
// ICF の最上位ビットが 0 なら、ヘッダーを短い形（ICF DA2 SA2 SID）にする
func finsToHostLink(frame []byte) []byte {
	if frame[0]&icfGateway != 0 {
		return frame
	}
	h := parseHeader(frame)
	out := make([]byte, 0, shortHeaderLength+len(frame)-headerLength)
	out = append(out, h.ICF, h.DA2, h.SA2, h.SID)
	return append(out, frame[headerLength:]...)
}

// finsFromHostLink は、Host Link に載っていた FINS（短い形か、ヘッダー 10 バイト）を、ヘッダー 10 バイトの形に戻す
func finsFromHostLink(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, errors.New("FINS の部分がありません")
	}
	if b[0]&icfGateway != 0 {
		if len(b) < headerLength {
			return nil, fmt.Errorf("FINS のヘッダーが短すぎます（%d バイト）", len(b))
		}
		return b, nil
	}
	if len(b) < shortHeaderLength {
		return nil, fmt.Errorf("FINS のヘッダーが短すぎます（%d バイト）", len(b))
	}
	out := make([]byte, 0, headerLength+len(b)-shortHeaderLength)
	out = header{ICF: b[0], GCT: 0x02, DA2: b[1], SA2: b[2], SID: b[3]}.append(out)
	return append(out, b[shortHeaderLength:]...), nil
}

// encodeHostLinkCommand は、FINS のフレーム（ヘッダー 10 バイト付き）を Host Link のコマンドのフレームにする
func encodeHostLinkCommand(unit int, wait byte, frame []byte) []byte {
	out := appendHostLinkHead(nil, unit, hostLinkHeaderFA)
	out = append(out, "0123456789ABCDEF"[wait&0x0F])
	out = appendUpperHex(out, finsToHostLink(frame))
	return appendHostLinkTail(out, 0)
}

// encodeHostLinkResponse は、FINS の応答（ヘッダー 10 バイト付き）を Host Link の応答のフレームにする
func encodeHostLinkResponse(unit int, frame []byte) []byte {
	out := appendHostLinkHead(nil, unit, hostLinkHeaderFA)
	out = append(out, hostLinkEndNormal...)
	out = appendUpperHex(out, finsToHostLink(frame))
	return appendHostLinkTail(out, 0)
}

// encodeHostLinkError は、Host Link の誤りの応答（FA と終了コード、または IC）を作る
func encodeHostLinkError(unit int, code, end string) []byte {
	out := appendHostLinkHead(nil, unit, code)
	out = append(out, end...)
	return appendHostLinkTail(out, 0)
}

// hostLinkFrame は、読み取った Host Link のフレーム 1 つ
type hostLinkFrame struct {
	unit int
	code string // ヘッダーコード（"FA" など）
	text []byte // ヘッダーコードのあと、FCS の前までの文字
}

var (
	errHostLinkFCS    = errors.New("FCS が合いません")
	errHostLinkFormat = errors.New("Host Link のフレームの形ではありません")
)

// parseHostLinkFrame は、'@' から '*' CR までの 1 つのフレームを読む（FCS を確かめる）
func parseHostLinkFrame(b []byte) (hostLinkFrame, error) {
	// '@' 号機番号 2・ヘッダーコード 2・FCS 2・'*' CR
	if len(b) < 9 || b[0] != hostLinkStart || !bytes.HasSuffix(b, []byte{'*', hostLinkCR}) {
		return hostLinkFrame{}, errHostLinkFormat
	}
	unit, err := strconv.Atoi(string(b[1:3]))
	if err != nil || unit < 0 {
		return hostLinkFrame{}, errHostLinkFormat
	}
	f := hostLinkFrame{unit: unit, code: string(b[3:5]), text: b[5 : len(b)-4]}
	fcs, err := strconv.ParseUint(string(b[len(b)-4:len(b)-2]), 16, 8)
	if err != nil {
		return f, errHostLinkFormat
	}
	if byte(fcs) != hostLinkFCS(b[:len(b)-4]) {
		return f, errHostLinkFCS
	}
	return f, nil
}

// decodeHex は、16 進の文字をバイト列にする（小文字も読む）
func decodeHex(b []byte) ([]byte, error) {
	if len(b)%2 != 0 {
		return nil, errors.New("16 進の文字の数が奇数です")
	}
	out := make([]byte, len(b)/2)
	if _, err := hex.Decode(out, b); err != nil {
		return nil, errors.New("16 進ではない文字があります")
	}
	return out, nil
}
