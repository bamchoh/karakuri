package application

import (
	"fmt"
	"strings"

	"karakuri/internal/domain/protocol"
)

// === 汎用メモリ操作API ===

// GetMemoryAreas は利用可能なメモリエリアの一覧を返す。
// protocolType が接続先へのマッピングのキー（"client:<ID>"）なら、その接続先のマッピングできるエリアを返す
func (s *PLCService) GetMemoryAreas(protocolType string) []MemoryAreaDTO {
	if clientID, ok := clientIDFromMappingKey(protocolType); ok {
		return s.clients.clientMemoryAreas(clientID)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return nil
	}
	return toMemoryAreaDTOs(inst.dataStore.GetAreas())
}

// ReadBits は指定エリアの複数ビット値を読み込む
func (s *PLCService) ReadBits(protocolType, area string, address, count int) ([]bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return nil, err
	}
	return inst.dataStore.ReadBits(area, uint32(address), uint16(count))
}

// WriteBit は指定エリアのビット値を書き込む
func (s *PLCService) WriteBit(protocolType, area string, address int, value bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return err
	}
	if err := inst.dataStore.WriteBit(area, uint32(address), value); err != nil {
		return err
	}
	return nil
}

// ReadWords は指定エリアの複数ワード値を読み込む
func (s *PLCService) ReadWords(protocolType, area string, address, count int) ([]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return nil, err
	}

	vals, err := inst.dataStore.ReadWords(area, uint32(address), uint16(count))
	if err != nil {
		return nil, err
	}
	result := make([]int, len(vals))
	for i, v := range vals {
		result[i] = int(v)
	}
	return result, nil
}

// WriteWord は指定エリアのワード値を書き込む
func (s *PLCService) WriteWord(protocolType, area string, address int, value int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return err
	}
	if err := inst.dataStore.WriteWord(area, uint32(address), uint16(value)); err != nil {
		return err
	}
	return nil
}

// WriteWords は指定エリアの address から続けて複数ワードを書き込む。
// 先にエリアの範囲に収まるかを確かめるので、範囲外なら 1 つも書かない
func (s *PLCService) WriteWords(protocolType, area string, address int, values []uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return err
	}
	if err := checkAreaRange(inst.dataStore.GetAreas(), area, address, len(values), false); err != nil {
		return err
	}
	return inst.dataStore.WriteWords(area, uint32(address), values)
}

// WriteBits は指定エリアの address から続けて複数ビットを書き込む。
// 先にエリアの範囲に収まるかを確かめるので、範囲外なら 1 つも書かない
func (s *PLCService) WriteBits(protocolType, area string, address int, values []bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return err
	}
	if err := checkAreaRange(inst.dataStore.GetAreas(), area, address, len(values), true); err != nil {
		return err
	}
	return inst.dataStore.WriteBits(area, uint32(address), values)
}

// checkAreaRange は、エリアの address から count 個が、エリアの大きさに収まるかを確かめる。
// bits が true ならビットで数える（ワードのエリアなら、address はビットの番号で、大きさは ワード数 × 16）。
// false ならワードで数える（ビットのエリアなら、1 ワードが 16 点）。DataStore の決まり（protocol.DataStore）と同じ
func checkAreaRange(areas []protocol.MemoryArea, areaID string, address, count int, bits bool) error {
	for _, a := range areas {
		if a.ID != areaID {
			continue
		}
		size, need := uint64(a.Size), uint64(count)
		switch {
		case bits && !a.IsBit:
			size *= 16
		case !bits && a.IsBit:
			need *= 16
		}
		if address < 0 || count < 0 || uint64(address)+need > size {
			unit := "ワード"
			if bits {
				unit = "ビット"
			}
			return fmt.Errorf("エリア %s の範囲外です（アドレス %d から %d %s）", areaID, address, count, unit)
		}
		return nil
	}
	return fmt.Errorf("エリアがありません: %s", areaID)
}

// ResolveMemoryAddress は、利用者が書くアドレスの文字（三菱の "D10.3"・"X1F" など。書き方はプロトコルが決める）を、
// エリアと 0 始まりのアドレスに直す。
//
//   - bits が true: ビットで読み書きするときのアドレス。ワードを指していれば、そのワードのビット 0（"D10" は D10.0）
//   - bits が false: ワードで読み書きするときのアドレス。ビットのデバイスなら、そこから 16 点を 1 ワードにする（"X10" は X10〜X1F）。
//     ワードの中のビット（"D10.3"）はエラー
func (s *PLCService) ResolveMemoryAddress(protocolType, text string, bits bool) (area string, address int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return "", 0, err
	}
	parser, ok := inst.factory.(protocol.AddressParser)
	if !ok {
		return "", 0, fmt.Errorf("%s は、アドレスを文字で指定できません", protocolType)
	}
	da, err := parser.ParseAddress(strings.TrimSpace(text))
	if err != nil {
		return "", 0, err
	}
	isBitArea := false
	found := false
	for _, a := range inst.dataStore.GetAreas() {
		if a.ID == da.Area {
			isBitArea, found = a.IsBit, true
		}
	}
	if !found {
		return "", 0, fmt.Errorf("エリアがありません: %s", da.Area)
	}
	switch {
	case bits && !da.Bit && !isBitArea:
		return da.Area, int(da.Address) * 16, nil
	case !bits && da.Bit && !isBitArea:
		return "", 0, fmt.Errorf("%s はワードの中のビットです。ワードで読み書きするときは、ビットの位置を付けずに指定してください", text)
	}
	return da.Area, int(da.Address), nil
}
