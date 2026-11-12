package rtu

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// ASCII フレーム定数
const (
	ASCIIFrameStart = ':'  // 0x3A
	ASCIIFrameCR    = '\r' // 0x0D
	ASCIIFrameLF    = '\n' // 0x0A
)

// ParseASCIIFrame はASCIIフレームを解析してバイナリデータを返す
// 入力: ":AABBCCDD...LRC\r\n" 形式の文字列
// 出力: バイナリデータ（LRC検証済み）
func ParseASCIIFrame(frame []byte) ([]byte, error) {
	// 最小長チェック: ':' + UnitID(2) + FC(2) + LRC(2) + CR + LF = 9
	if len(frame) < 9 {
		return nil, ErrFrameTooShort
	}

	// 開始文字チェック
	if frame[0] != ASCIIFrameStart {
		return nil, fmt.Errorf("invalid start character: expected ':', got '%c'", frame[0])
	}

	// 終了文字チェック
	if frame[len(frame)-2] != ASCIIFrameCR || frame[len(frame)-1] != ASCIIFrameLF {
		return nil, fmt.Errorf("invalid end characters: expected CR LF")
	}

	// HEX部分を抽出（':'とCR LFを除く）
	hexStr := string(frame[1 : len(frame)-2])

	// HEX文字列の長さは偶数でなければならない
	if len(hexStr)%2 != 0 {
		return nil, fmt.Errorf("invalid hex string length")
	}

	// HEX文字列をバイナリに変換
	data, err := hex.DecodeString(strings.ToUpper(hexStr))
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex: %w", err)
	}

	// 最低限のデータ長チェック（UnitID + FC + LRC = 3バイト）
	if len(data) < 3 {
		return nil, ErrFrameTooShort
	}

	// LRC検証
	dataWithoutLRC := data[:len(data)-1]
	receivedLRC := data[len(data)-1]
	if !CheckLRC(dataWithoutLRC, receivedLRC) {
		return nil, fmt.Errorf("LRC check failed")
	}

	return dataWithoutLRC, nil
}

// ParseASCIIRequest はASCIIフレームからリクエストを解析する
func ParseASCIIRequest(frame []byte) (*Request, error) {
	// ASCIIフレームをバイナリに変換
	data, err := ParseASCIIFrame(frame)
	if err != nil {
		return nil, err
	}
	return parseRequestBody(data)
}

// BuildASCIIFrame はバイナリデータからASCIIフレームを構築する
func BuildASCIIFrame(data []byte) []byte {
	// LRCを計算（呼び出し側のスライスを書き換えないよう、新しいスライスに入れる）
	dataWithLRC := make([]byte, 0, len(data)+1)
	dataWithLRC = append(dataWithLRC, data...)
	dataWithLRC = append(dataWithLRC, LRC(data))

	// HEX文字列に変換
	hexStr := strings.ToUpper(hex.EncodeToString(dataWithLRC))

	// フレームを構築: ':' + HEX + CR + LF
	frame := make([]byte, 1+len(hexStr)+2)
	frame[0] = ASCIIFrameStart
	copy(frame[1:], hexStr)
	frame[len(frame)-2] = ASCIIFrameCR
	frame[len(frame)-1] = ASCIIFrameLF

	return frame
}
