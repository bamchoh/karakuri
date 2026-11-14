package slmp

import (
	"encoding/binary"

	"karakuri/internal/domain/protocol"
)

// CPU の形名（0101 の応答）。形名は 16 バイト（空白で埋める）、そのあとに形名コード 2 バイト
const (
	cpuModel     = "Q03UDECPU"
	cpuModelCode = 0x0268
)

// handler は SLMP の要求を処理する（UDP と TCP で共通）。
// メモリの読み書きは、渡された DataStore（変数と同期する包み）に対して行う
type handler struct {
	store protocol.DataStore
}

func newHandler(store protocol.DataStore) *handler {
	return &handler{store: store}
}

// handle は、要求のフレーム 1 つを処理し、応答のフレームを返す。応答しないときは nil を返す
// （フレームとして読めない・応答のフレームが来た）
func (h *handler) handle(raw []byte) []byte {
	f, err := parseFrame(raw)
	if err != nil || !f.isRequest() {
		return nil
	}
	// 監視タイマ(2) コマンド(2) サブコマンド(2) が要る
	if len(f.Body) < 6 {
		return f.reply(endLength, 0, 0, nil)
	}
	command := binary.LittleEndian.Uint16(f.Body[2:])
	sub := binary.LittleEndian.Uint16(f.Body[4:])
	end, data := h.execute(command, sub, f.Body[6:])
	return f.reply(end, command, sub, data)
}

func (h *handler) execute(command, sub uint16, p []byte) (uint16, []byte) {
	// デバイスを指定するコマンドのサブコマンドは、ビット単位と、長い形の指定だけ
	deviceSub := sub&^(subBitUnits|subLong) == 0
	bitUnits, long := sub&subBitUnits != 0, sub&subLong != 0
	switch command {
	case cmdBatchRead:
		if !deviceSub {
			return endCommand, nil
		}
		return h.batchRead(p, bitUnits, long)
	case cmdBatchWrite:
		if !deviceSub {
			return endCommand, nil
		}
		return h.batchWrite(p, bitUnits, long)
	case cmdRandomRead:
		if !deviceSub || bitUnits {
			return endCommand, nil
		}
		return h.randomRead(p, long)
	case cmdRandomWrite:
		if !deviceSub {
			return endCommand, nil
		}
		if bitUnits {
			return h.randomWriteBits(p, long)
		}
		return h.randomWriteWords(p, long)
	case cmdTypeName:
		if sub != 0 {
			return endCommand, nil
		}
		if len(p) != 0 {
			return endLength, nil
		}
		out := make([]byte, 16, 18)
		for i := range out {
			out[i] = ' '
		}
		copy(out, cpuModel)
		return endOK, binary.LittleEndian.AppendUint16(out, cpuModelCode)
	case cmdSelfTest:
		if sub != 0 {
			return endCommand, nil
		}
		return selfTest(p)
	}
	return endCommand, nil
}

// --- メモリの読み書き ---

// readBits は t から count 点のビットを読む
func (h *handler) readBits(t target, count int) ([]bool, uint16) {
	bits, err := h.store.ReadBits(t.def.ID, t.number, uint16(count))
	if err != nil {
		return nil, endRange
	}
	return bits, endOK
}

// readWords は t から count ワードを読む。ビットデバイスなら、16 点ずつをワードにまとめる（最初の点が、ワードのビット 0）。
//
// ビットデバイスは、DataStore にはビットとして読み書きする
// （ビットのエリアにワードで書くと、変数と同期する包みが、書かれた範囲を取り違えるため）
func (h *handler) readWords(t target, count int) ([]uint16, uint16) {
	if t.def.IsBit {
		bits, end := h.readBits(t, count*16)
		if end != endOK {
			return nil, end
		}
		words := make([]uint16, count)
		for i, b := range bits {
			if b {
				words[i/16] |= 1 << (i % 16)
			}
		}
		return words, endOK
	}
	words, err := h.store.ReadWords(t.def.ID, t.number, uint16(count))
	if err != nil {
		return nil, endRange
	}
	return words, endOK
}

func (h *handler) writeBits(t target, bits []bool) uint16 {
	if err := h.store.WriteBits(t.def.ID, t.number, bits); err != nil {
		return endRange
	}
	return endOK
}

func (h *handler) writeWords(t target, words []uint16) uint16 {
	if t.def.IsBit {
		bits := make([]bool, len(words)*16)
		for i := range bits {
			bits[i] = words[i/16]&(1<<(i%16)) != 0
		}
		return h.writeBits(t, bits)
	}
	if err := h.store.WriteWords(t.def.ID, t.number, words); err != nil {
		return endRange
	}
	return endOK
}

// wordSpan は、ワード単位で count 個を読み書きするときに、デバイスの上で使う点数を返す（ビットデバイスは 16 点ずつ）
func wordSpan(t target, count int) int {
	if t.def.IsBit {
		return count * 16
	}
	return count
}

// batchTarget は、一括読み出し・一括書き込みの先頭（デバイスの指定 + 点数(2)）を読み、範囲を確かめる
func batchTarget(p []byte, bitUnits, long bool) (t target, count int, rest []byte, end uint16) {
	n := deviceLength(long)
	if len(p) < n+2 {
		return t, 0, nil, endLength
	}
	t, end = parseTarget(p, long)
	if end != endOK {
		return t, 0, nil, end
	}
	count = int(binary.LittleEndian.Uint16(p[n:]))
	rest = p[n+2:]
	if bitUnits {
		switch {
		case !t.def.IsBit:
			return t, 0, nil, endRequest // ワードデバイスは、ビット単位では読み書きできない
		case count < 1 || count > maxBitPoints:
			return t, 0, nil, endPoints
		case count > t.remaining():
			return t, 0, nil, endRange
		}
		return t, count, rest, endOK
	}
	switch {
	case count < 1 || count > maxWordPoints:
		return t, 0, nil, endPoints
	case wordSpan(t, count) > t.remaining():
		return t, 0, nil, endRange
	}
	return t, count, rest, endOK
}

// 0401: デバイスの指定 + 点数(2)。
// ビット単位の応答は 1 点を 4 ビットで表し、2 点を 1 バイトに詰める（先の点が上位）。ワード単位の応答は 1 点 2 バイト
func (h *handler) batchRead(p []byte, bitUnits, long bool) (uint16, []byte) {
	t, count, rest, end := batchTarget(p, bitUnits, long)
	if end != endOK {
		return end, nil
	}
	if len(rest) != 0 {
		return endLength, nil
	}
	if bitUnits {
		bits, end := h.readBits(t, count)
		if end != endOK {
			return end, nil
		}
		return endOK, packNibbles(bits)
	}
	words, end := h.readWords(t, count)
	if end != endOK {
		return end, nil
	}
	return endOK, appendWords(make([]byte, 0, len(words)*2), words)
}

// 1401: デバイスの指定 + 点数(2) + データ
func (h *handler) batchWrite(p []byte, bitUnits, long bool) (uint16, []byte) {
	t, count, data, end := batchTarget(p, bitUnits, long)
	if end != endOK {
		return end, nil
	}
	if bitUnits {
		if len(data) != (count+1)/2 {
			return endLength, nil
		}
		return h.writeBits(t, unpackNibbles(data, count)), nil
	}
	if len(data) != count*2 {
		return endLength, nil
	}
	return h.writeWords(t, parseWords(data)), nil
}

// 0403: ワードの点数(1) ダブルワードの点数(1) デバイスの指定の並び。応答は、ワード（2 バイトずつ）、ダブルワード（4 バイトずつ）の順
func (h *handler) randomRead(p []byte, long bool) (uint16, []byte) {
	if len(p) < 2 {
		return endLength, nil
	}
	wordCount, dwordCount := int(p[0]), int(p[1])
	n := deviceLength(long)
	switch {
	case wordCount+dwordCount < 1 || wordCount+dwordCount > maxRandomPoints:
		return endPoints, nil
	case len(p) != 2+(wordCount+dwordCount)*n:
		return endLength, nil
	}
	out := make([]byte, 0, wordCount*2+dwordCount*4)
	p = p[2:]
	for i := 0; i < wordCount+dwordCount; i++ {
		t, end := parseTarget(p[i*n:], long)
		if end != endOK {
			return end, nil
		}
		words := 1
		if i >= wordCount {
			words = 2
		}
		if wordSpan(t, words) > t.remaining() {
			return endRange, nil
		}
		values, end := h.readWords(t, words)
		if end != endOK {
			return end, nil
		}
		out = appendWords(out, values)
	}
	return endOK, out
}

// 1402（ワード単位）: ワードの点数(1) ダブルワードの点数(1) （デバイスの指定 + 値 2 バイト）の並び （デバイスの指定 + 値 4 バイト）の並び
func (h *handler) randomWriteWords(p []byte, long bool) (uint16, []byte) {
	if len(p) < 2 {
		return endLength, nil
	}
	wordCount, dwordCount := int(p[0]), int(p[1])
	n := deviceLength(long)
	switch {
	case wordCount+dwordCount < 1 || wordCount+dwordCount > maxRandomWrite:
		return endPoints, nil
	case len(p) != 2+wordCount*(n+2)+dwordCount*(n+4):
		return endLength, nil
	}
	type write struct {
		t      target
		values []uint16
	}
	// 全部を読んで確かめてから書く（途中まで書いて異常終了にしない）
	writes := make([]write, 0, wordCount+dwordCount)
	p = p[2:]
	for i := 0; i < wordCount+dwordCount; i++ {
		size := 2
		if i >= wordCount {
			size = 4
		}
		t, end := parseTarget(p, long)
		if end != endOK {
			return end, nil
		}
		if wordSpan(t, size/2) > t.remaining() {
			return endRange, nil
		}
		writes = append(writes, write{t: t, values: parseWords(p[n : n+size])})
		p = p[n+size:]
	}
	for _, w := range writes {
		if end := h.writeWords(w.t, w.values); end != endOK {
			return end, nil
		}
	}
	return endOK, nil
}

// 1402（ビット単位）: 点数(1) （デバイスの指定 + ON / OFF）の並び。ON / OFF は 1 バイト（長い形は 2 バイト）
func (h *handler) randomWriteBits(p []byte, long bool) (uint16, []byte) {
	if len(p) < 1 {
		return endLength, nil
	}
	count := int(p[0])
	n := deviceLength(long)
	valueLength := 1
	if long {
		valueLength = 2
	}
	switch {
	case count < 1 || count > maxRandomBits:
		return endPoints, nil
	case len(p) != 1+count*(n+valueLength):
		return endLength, nil
	}
	type write struct {
		t  target
		on bool
	}
	writes := make([]write, 0, count)
	p = p[1:]
	for i := 0; i < count; i++ {
		t, end := parseTarget(p, long)
		if end != endOK {
			return end, nil
		}
		if !t.def.IsBit {
			return endRequest, nil
		}
		writes = append(writes, write{t: t, on: p[n] != 0})
		p = p[n+valueLength:]
	}
	for _, w := range writes {
		if end := h.writeBits(w.t, []bool{w.on}); end != endOK {
			return end, nil
		}
	}
	return endOK, nil
}

// 0619: データのバイト数(2) + データ。同じものを返す
func selfTest(p []byte) (uint16, []byte) {
	if len(p) < 2 {
		return endLength, nil
	}
	count := int(binary.LittleEndian.Uint16(p))
	switch {
	case count < 1 || count > maxWordPoints:
		return endSelfTest, nil
	case len(p) != 2+count:
		return endLength, nil
	}
	return endOK, append([]byte(nil), p...)
}

// --- データの詰め方 ---

// packNibbles は、ビットを 1 点 4 ビットで詰める（2 点で 1 バイト。先の点が上位。点数が奇数なら、最後の下位は 0）
func packNibbles(bits []bool) []byte {
	out := make([]byte, (len(bits)+1)/2)
	for i, b := range bits {
		if !b {
			continue
		}
		if i%2 == 0 {
			out[i/2] |= 0x10
		} else {
			out[i/2] |= 0x01
		}
	}
	return out
}

// unpackNibbles は、packNibbles の逆（count 点を取り出す）
func unpackNibbles(data []byte, count int) []bool {
	bits := make([]bool, count)
	for i := range bits {
		if i%2 == 0 {
			bits[i] = data[i/2]&0xF0 != 0
		} else {
			bits[i] = data[i/2]&0x0F != 0
		}
	}
	return bits
}

func appendWords(dst []byte, words []uint16) []byte {
	for _, w := range words {
		dst = binary.LittleEndian.AppendUint16(dst, w)
	}
	return dst
}

func parseWords(data []byte) []uint16 {
	words := make([]uint16, len(data)/2)
	for i := range words {
		words[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	return words
}
