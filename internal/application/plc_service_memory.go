package application

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
