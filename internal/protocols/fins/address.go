package fins

import (
	"fmt"
	"strconv"
	"strings"

	"karakuri/internal/domain/protocol"
)

// addressPrefix は、アドレスの前に付ける文字と、それが指すエリア
type addressPrefix struct {
	text string
	def  areaDef
	flag bool // タイマ・カウンタのアップフラグ
}

// addressPrefixes は、受け付けるアドレスの前の文字（オムロンの書き方 "D"・"W"・"E0_" と、エリアの ID "DM"・"WR"・"EM0_"）
var addressPrefixes = func() []addressPrefix {
	var list []addressPrefix
	for _, a := range areaDefs {
		names := []string{strings.TrimSpace(a.Prefix)}
		if strings.HasPrefix(a.ID, "EM") {
			names = append(names, a.ID+"_")
		} else if a.ID != strings.TrimSpace(a.Prefix) {
			names = append(names, a.ID)
		}
		for _, n := range names {
			list = append(list, addressPrefix{text: n, def: a})
		}
		if a.Flags {
			list = append(list, addressPrefix{text: a.ID + "F", def: a, flag: true})
		}
	}
	return list
}()

// parseAddress は、オムロンの書き方のアドレスを、DataStore のエリアとアドレスに直す。
//
//   - ワード: "D100"、"DM100"、"W10"、"H10"、"A10"、"CIO 100"、"T5"（タイマの現在値）、"C5"、"E0_100"（EM バンク 0）
//   - ワードの中のビット: 「.」のあとに 10 進で 0〜15（"CIO 100.05"、"W10.15"、"D100.3"）。アドレスは ワード × 16 + ビット。
//     CIO は前の文字を省いてもよい（"100.05"）
//   - タイマ・カウンタのアップフラグ: "TF5"、"CF5"
//
// 大文字・小文字と空白は区別しない。前の文字は、長いほうを優先する（"DM100" は DM、"D100" も DM）
func parseAddress(text string) (protocol.DeviceAddress, error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(text), " ", ""))
	var p addressPrefix
	for _, c := range addressPrefixes {
		if strings.HasPrefix(s, c.text) && len(c.text) > len(p.text) {
			p = c
		}
	}
	if p.text == "" {
		if s == "" || s[0] < '0' || s[0] > '9' {
			return protocol.DeviceAddress{}, fmt.Errorf("エリアが読めません: %q（D100、CIO 100.05、W10.15、T5 のように書きます）", text)
		}
		def, _ := areaByID(AreaCIO)
		p = addressPrefix{def: def}
	}
	numText, bitText, hasBit := strings.Cut(s[len(p.text):], ".")
	n, err := strconv.ParseUint(numText, 10, 32)
	if err != nil || n >= uint64(p.def.Words) {
		return protocol.DeviceAddress{}, fmt.Errorf("番号が読めません: %q（%s は 0〜%d）", text, p.def.ID, p.def.Words-1)
	}
	if p.flag {
		if hasBit {
			return protocol.DeviceAddress{}, fmt.Errorf("アップフラグには、ビットの位置（.）は付けられません: %q", text)
		}
		return protocol.DeviceAddress{Area: p.def.ID + flagSuffix, Address: uint32(n), Bit: true}, nil
	}
	if !hasBit {
		return protocol.DeviceAddress{Area: p.def.ID, Address: uint32(n)}, nil
	}
	b, err := strconv.ParseUint(bitText, 10, 8)
	if err != nil || b > 15 || len(bitText) > 2 {
		return protocol.DeviceAddress{}, fmt.Errorf("ビットの位置は 10 進で 0〜15 です: %q", text)
	}
	return protocol.DeviceAddress{Area: p.def.ID, Address: uint32(n)*16 + uint32(b), Bit: true}, nil
}

// ParseAddress は protocol.AddressParser の実装（HTTP API で "CIO 100.05" のようにアドレスを書けるようにする）
func (f *Factory) ParseAddress(text string) (protocol.DeviceAddress, error) {
	return parseAddress(text)
}

// ParseAddress は protocol.AddressParser の実装（エリアは FINS と同じ）
func (f *HostLinkFactory) ParseAddress(text string) (protocol.DeviceAddress, error) {
	return parseAddress(text)
}

var (
	_ protocol.AddressParser = (*Factory)(nil)
	_ protocol.AddressParser = (*HostLinkFactory)(nil)
)
