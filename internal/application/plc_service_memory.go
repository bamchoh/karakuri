package application

// === 汎用メモリ操作API ===

// GetMemoryAreas は利用可能なメモリエリアの一覧を返す
func (s *PLCService) GetMemoryAreas(protocolType string) []MemoryAreaDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return nil
	}

	areas := inst.dataStore.GetAreas()
	result := make([]MemoryAreaDTO, len(areas))
	for i, area := range areas {
		result[i] = MemoryAreaDTO{
			ID:             area.ID,
			DisplayName:    area.DisplayName,
			IsBit:          area.IsBit,
			Size:           int(area.Size),
			ReadOnly:       area.ReadOnly,
			ByteAddressing: area.ByteAddressing,
			OneOrigin:      area.OneOrigin,
			ShortName:      area.ShortName,
		}
	}
	return result
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
	// リモートプラグイン DataStore の場合、ホスト書き込みはプラグインから通知が来ないため
	// 自分で変数を同期する（VariableBackedDataStore の場合は WriteWord 内で自動的に同期済み）
	if inst.changeListener != nil {
		go inst.changeListener.SyncHostBitWriteToVariable(area, uint32(address))
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
	// リモートプラグイン DataStore の場合、ホスト書き込みはプラグインから通知が来ないため
	// 自分で変数を同期する（VariableBackedDataStore の場合は WriteWord 内で自動的に同期済み）
	if inst.changeListener != nil {
		go inst.changeListener.SyncHostWordWriteToVariable(area, uint32(address))
	}
	return nil
}
