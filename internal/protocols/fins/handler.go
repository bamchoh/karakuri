package fins

import (
	"encoding/binary"
	"time"

	"karakuri/internal/domain/protocol"
)

// handler は FINS のリクエストを処理する（UDP と TCP で共通）。
// メモリの読み書きは、渡された DataStore（変数と同期する包み）に対して行う
type handler struct {
	store protocol.DataStore
	now   func() time.Time
}

func newHandler(store protocol.DataStore) *handler {
	return &handler{store: store, now: time.Now}
}

// handle は FINS フレーム 1 つを処理し、応答のフレームを返す。応答しないときは nil を返す
// （短すぎて読めない・応答のフレームが来た・応答不要のリクエスト）
func (h *handler) handle(frame []byte) []byte {
	if len(frame) < headerLength+2 || len(frame) > maxFrameLength {
		return nil
	}
	hdr := parseHeader(frame)
	if hdr.ICF&icfResponse != 0 {
		return nil
	}
	cmd := binary.BigEndian.Uint16(frame[headerLength:])
	end, data := h.execute(cmd, frame[headerLength+2:])
	if hdr.ICF&icfNoResponse != 0 {
		return nil
	}
	resp := make([]byte, 0, headerLength+4+len(data))
	resp = hdr.reply().append(resp)
	resp = binary.BigEndian.AppendUint16(resp, cmd)
	resp = binary.BigEndian.AppendUint16(resp, end)
	return append(resp, data...)
}

func (h *handler) execute(cmd uint16, p []byte) (uint16, []byte) {
	switch cmd {
	case cmdMemoryRead:
		return h.memoryRead(p)
	case cmdMemoryWrite:
		return h.memoryWrite(p)
	case cmdMemoryFill:
		return h.memoryFill(p)
	case cmdMultipleRead:
		return h.multipleRead(p)
	case cmdCPUUnitData:
		return endNormal, cpuUnitData()
	case cmdCPUUnitStatus:
		return endNormal, cpuUnitStatus()
	case cmdClockRead:
		t := h.now()
		return endNormal, []byte{bcd(t.Year() % 100), bcd(int(t.Month())), bcd(t.Day()), bcd(t.Hour()), bcd(t.Minute()), bcd(t.Second()), byte(t.Weekday())}
	}
	return endUndefinedCmd, nil
}

// target は、リクエストの中のエリア種別とアドレスが指す場所
type target struct {
	def   areaDef
	isBit bool
	word  int
	bit   int
}

// storeArea は、DataStore に渡すエリアの ID を返す。ビットのエリアを持つエリアのビットは、ビットのエリアの ID にする
// （ビットのエリアにマッピングした変数に、書き込みが反映されるようにするため）
func (t target) storeArea() string {
	if t.isBit && t.def.BitArea {
		return t.def.ID + bitSuffix
	}
	return t.def.ID
}

func (t target) bitAddress() uint32 {
	return uint32(t.word)*16 + uint32(t.bit)
}

// parseTarget は、エリア種別 1 バイト + アドレス 3 バイトを読み、count 個が範囲に入るかを確かめる
func parseTarget(p []byte, count int) (target, uint16) {
	def, isBit, ok := areaByCode(p[0])
	if !ok {
		return target{}, endNoAreaType
	}
	t := target{def: def, isBit: isBit, word: int(binary.BigEndian.Uint16(p[1:3])), bit: int(p[3])}
	if t.word >= def.Words || t.bit > 15 || (!isBit && t.bit != 0) {
		return t, endAddressRange
	}
	if isBit {
		if int(t.bitAddress())+count > def.Words*16 {
			return t, endAddressExceeds
		}
	} else if t.word+count > def.Words {
		return t, endAddressExceeds
	}
	return t, endNormal
}

// read は t から count 個を読み、応答に入れる形（ワードは 2 バイト、ビットは 1 バイト）で返す
func (h *handler) read(t target, count int) ([]byte, uint16) {
	if t.isBit {
		bits, err := h.store.ReadBits(t.storeArea(), t.bitAddress(), uint16(count))
		if err != nil {
			return nil, endAddressRange
		}
		out := make([]byte, len(bits))
		for i, b := range bits {
			if b {
				out[i] = 1
			}
		}
		return out, endNormal
	}
	words, err := h.store.ReadWords(t.storeArea(), uint32(t.word), uint16(count))
	if err != nil {
		return nil, endAddressRange
	}
	out := make([]byte, 0, len(words)*2)
	for _, w := range words {
		out = binary.BigEndian.AppendUint16(out, w)
	}
	return out, endNormal
}

// 0101: エリア種別(1) アドレス(3) 個数(2)
func (h *handler) memoryRead(p []byte) (uint16, []byte) {
	switch {
	case len(p) < 6:
		return endTooShort, nil
	case len(p) > 6:
		return endTooLong, nil
	}
	count := int(binary.BigEndian.Uint16(p[4:6]))
	if count > maxReadCount {
		return endResponseLong, nil
	}
	t, end := parseTarget(p, count)
	if end != endNormal {
		return end, nil
	}
	data, end := h.read(t, count)
	return end, data
}

// 0102: エリア種別(1) アドレス(3) 個数(2) データ（ワードは 2 バイトずつ、ビットは 1 バイトずつ）
func (h *handler) memoryWrite(p []byte) (uint16, []byte) {
	if len(p) < 6 {
		return endTooShort, nil
	}
	count := int(binary.BigEndian.Uint16(p[4:6]))
	data := p[6:]
	t, end := parseTarget(p, count)
	if end != endNormal {
		return end, nil
	}
	if count == 0 {
		return endNormal, nil
	}
	if t.isBit {
		if len(data) != count {
			return endCountMismatch, nil
		}
		bits := make([]bool, count)
		for i, b := range data {
			bits[i] = b&1 != 0
		}
		if err := h.store.WriteBits(t.storeArea(), t.bitAddress(), bits); err != nil {
			return endAddressRange, nil
		}
		return endNormal, nil
	}
	if len(data) != count*2 {
		return endCountMismatch, nil
	}
	words := make([]uint16, count)
	for i := range words {
		words[i] = binary.BigEndian.Uint16(data[i*2:])
	}
	if err := h.store.WriteWords(t.storeArea(), uint32(t.word), words); err != nil {
		return endAddressRange, nil
	}
	return endNormal, nil
}

// 0103: エリア種別(1) アドレス(3) 個数(2) データ(2)。ワードのエリアだけ
func (h *handler) memoryFill(p []byte) (uint16, []byte) {
	switch {
	case len(p) < 8:
		return endTooShort, nil
	case len(p) > 8:
		return endTooLong, nil
	}
	count := int(binary.BigEndian.Uint16(p[4:6]))
	t, end := parseTarget(p, count)
	if end != endNormal {
		return end, nil
	}
	if t.isBit {
		return endNoAreaType, nil
	}
	if count == 0 {
		return endNormal, nil
	}
	words := make([]uint16, count)
	v := binary.BigEndian.Uint16(p[6:8])
	for i := range words {
		words[i] = v
	}
	if err := h.store.WriteWords(t.storeArea(), uint32(t.word), words); err != nil {
		return endAddressRange, nil
	}
	return endNormal, nil
}

// 0104: （エリア種別(1) アドレス(3)）の繰り返し。応答は（エリア種別(1) データ）の繰り返し
func (h *handler) multipleRead(p []byte) (uint16, []byte) {
	if len(p) < 4 {
		return endTooShort, nil
	}
	if len(p)%4 != 0 {
		return endCountMismatch, nil
	}
	var out []byte
	for i := 0; i < len(p); i += 4 {
		t, end := parseTarget(p[i:i+4], 1)
		if end != endNormal {
			return end, nil
		}
		data, end := h.read(t, 1)
		if end != endNormal {
			return end, nil
		}
		out = append(out, p[i])
		out = append(out, data...)
	}
	return endNormal, out
}

const (
	cpuModel   = "CJ2M-CPU31"
	cpuVersion = "02.01"
)

// cpuUnitData は 0501 の応答（形式 20 バイト・バージョン 20 バイト・システム予約 40 バイト・エリア情報 12 バイト・
// CPU 高機能ユニットの構成 64 バイト・リモート I/O 1 バイト・PC の状態 1 バイト）
func cpuUnitData() []byte {
	out := make([]byte, 0, 158)
	out = append(out, padSpaces(cpuModel, 20)...)
	out = append(out, padSpaces(cpuVersion, 20)...)
	out = append(out, make([]byte, 40)...)
	// エリア情報: プログラムの大きさ(2) IOM の大きさ(1) DM のワード数(2) タイマ・カウンタ(1) EM のバンク数(1) 予約(2) メモリカード(1) その大きさ(2)
	out = append(out, 0x00, 0x14, 0x17, 0x80, 0x00, 0x08, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00)
	out = append(out, make([]byte, 64)...)
	return append(out, 0x00, 0x00)
}

// cpuUnitStatus は 0601 の応答（運転中・運転モード）。エラーはなし
func cpuUnitStatus() []byte {
	out := make([]byte, 26)
	out[0] = 0x01 // 運転中
	out[1] = 0x04 // 運転モード
	for i := 10; i < 26; i++ {
		out[i] = ' ' // エラーメッセージ（なし）
	}
	return out
}

func padSpaces(s string, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	copy(out, s)
	return out
}

func bcd(n int) byte {
	return byte(n/10<<4 | n%10)
}
