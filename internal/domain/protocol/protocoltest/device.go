package protocoltest

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"

	"karakuri/internal/domain/protocol"
)

// ===== 変数をマッピングできるクライアント（フェイクの機器つき） =====

// DeviceAreas はフェイクの機器のエリア。"bits" / "words" は読み書きできる、"inputs" / "inputWords" は読むだけ
var DeviceAreas = []protocol.MemoryArea{
	{ID: "bits", DisplayName: "ビット", IsBit: true, Size: 1000, ShortName: "B"},
	{ID: "inputs", DisplayName: "入力ビット", IsBit: true, Size: 1000, ReadOnly: true, ShortName: "I"},
	{ID: "words", DisplayName: "ワード", Size: 1000, ShortName: "W"},
	{ID: "inputWords", DisplayName: "入力ワード", Size: 1000, ReadOnly: true, ShortName: "IW"},
}

// Device はフェイクの機器（メモリ）。クライアントのリクエストを処理する
type Device struct {
	mu    sync.Mutex
	bits  map[string][]bool
	words map[string][]uint16
	// Down が true の間、すべてのリクエストをタイムアウトにする
	Down atomic.Bool
	// Writes は書き込みのリクエストを受けた回数
	Writes atomic.Int32
}

// NewDevice は空のフェイクの機器を作る
func NewDevice() *Device {
	d := &Device{bits: map[string][]bool{}, words: map[string][]uint16{}}
	for _, a := range DeviceAreas {
		if a.IsBit {
			d.bits[a.ID] = make([]bool, a.Size)
		} else {
			d.words[a.ID] = make([]uint16, a.Size)
		}
	}
	return d
}

// SetWord / Word / SetBit / Bit は機器のメモリを直接読み書きする（テストの準備と確認）
func (d *Device) SetWord(area string, addr int, v uint16) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.words[area][addr] = v
}

func (d *Device) Word(area string, addr int) uint16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.words[area][addr]
}

func (d *Device) SetBit(area string, addr int, v bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bits[area][addr] = v
}

func (d *Device) Bit(area string, addr int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bits[area][addr]
}

var areaIndex = map[string]byte{"bits": 0, "inputs": 1, "words": 2, "inputWords": 3}

func areaByIndex(i byte) (protocol.MemoryArea, bool) {
	if int(i) >= len(DeviceAreas) {
		return protocol.MemoryArea{}, false
	}
	return DeviceAreas[i], true
}

// リクエストの形: ['R' | 'W', エリア, アドレス(2), 個数(2), データ...]（ワードは 2 バイトずつ、ビットは 1 バイトずつ）
func (d *Device) handle(req []byte) protocol.CommandResult {
	if d.Down.Load() {
		return protocol.CommandResult{Sent: req, Status: protocol.CommandTimeout, Error: "応答がありません（フェイク）"}
	}
	if len(req) < 6 {
		return protocol.CommandResult{Sent: req, Status: protocol.CommandError, Error: "リクエストが短い"}
	}
	area, ok := areaByIndex(req[1])
	if !ok {
		return protocol.CommandResult{Sent: req, Status: protocol.CommandException, Error: "不明なエリア"}
	}
	addr := int(binary.BigEndian.Uint16(req[2:4]))
	count := int(binary.BigEndian.Uint16(req[4:6]))
	if addr+count > int(area.Size) {
		return protocol.CommandResult{Sent: req, Status: protocol.CommandException, Error: "範囲外"}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	switch req[0] {
	case 'R':
		values := make([]protocol.ResultValue, count)
		for i := 0; i < count; i++ {
			if area.IsBit {
				b := d.bits[area.ID][addr+i]
				values[i] = protocol.ResultValue{Label: fmt.Sprint(addr + i), Bit: &b}
			} else {
				w := d.words[area.ID][addr+i]
				values[i] = protocol.ResultValue{Label: fmt.Sprint(addr + i), Word: &w}
			}
		}
		return protocol.CommandResult{Sent: req, Status: protocol.CommandOK, Values: values}
	case 'W':
		if area.ReadOnly {
			return protocol.CommandResult{Sent: req, Status: protocol.CommandException, Error: "書けないエリア"}
		}
		d.Writes.Add(1)
		data := req[6:]
		for i := 0; i < count; i++ {
			if area.IsBit {
				d.bits[area.ID][addr+i] = data[i] != 0
			} else {
				d.words[area.ID][addr+i] = binary.BigEndian.Uint16(data[i*2:])
			}
		}
		return protocol.CommandResult{Sent: req, Status: protocol.CommandOK}
	}
	return protocol.CommandResult{Sent: req, Status: protocol.CommandException, Error: "不明な操作"}
}

// DeviceClientFactory は、フェイクの機器につなぐクライアントのファクトリー（MappableClientFactory を実装する）。
// 読み書きの上限は MaxCount（既定 4）。分けて送ることを確かめやすいよう小さくしている
type DeviceClientFactory struct {
	ClientFactory
	Device   *Device
	MaxCount int
}

// NewDeviceClientFactory はフェイクの機器と、それにつなぐクライアントのファクトリーを作る
func NewDeviceClientFactory(pt protocol.ProtocolType) *DeviceClientFactory {
	f := &DeviceClientFactory{ClientFactory: ClientFactory{Type: pt}, Device: NewDevice(), MaxCount: 4}
	f.ClientFactory.SetRespond(f.Device.handle)
	return f
}

var _ protocol.MappableClientFactory = (*DeviceClientFactory)(nil)

func (f *DeviceClientFactory) MappingAreas() []protocol.MemoryArea {
	return append([]protocol.MemoryArea(nil), DeviceAreas...)
}

func (f *DeviceClientFactory) MaxReadCount(string) int { return f.MaxCount }

func (f *DeviceClientFactory) MaxWriteCount(area string) int {
	for _, a := range DeviceAreas {
		if a.ID == area && !a.ReadOnly {
			return f.MaxCount
		}
	}
	return 0
}

func header(op byte, area string, address uint32, count int) ([]byte, error) {
	idx, ok := areaIndex[area]
	if !ok {
		return nil, fmt.Errorf("不明なエリア: %s", area)
	}
	b := []byte{op, idx}
	b = binary.BigEndian.AppendUint16(b, uint16(address))
	return binary.BigEndian.AppendUint16(b, uint16(count)), nil
}

func (f *DeviceClientFactory) BuildReadRequest(area string, address uint32, count int) ([]byte, error) {
	if count < 1 || count > f.MaxCount {
		return nil, fmt.Errorf("個数 %d", count)
	}
	return header('R', area, address, count)
}

func (f *DeviceClientFactory) BuildWriteRequest(area string, address uint32, words []uint16, bits []bool) ([]byte, error) {
	n := len(words)
	if bits != nil {
		n = len(bits)
	}
	if n < 1 || n > f.MaxCount {
		return nil, fmt.Errorf("個数 %d", n)
	}
	b, err := header('W', area, address, n)
	if err != nil {
		return nil, err
	}
	for _, x := range bits {
		if x {
			b = append(b, 1)
		} else {
			b = append(b, 0)
		}
	}
	for _, w := range words {
		b = binary.BigEndian.AppendUint16(b, w)
	}
	return b, nil
}
