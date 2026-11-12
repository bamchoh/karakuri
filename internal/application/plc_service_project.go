package application

import (
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/script"
	"karakuri/internal/domain/variable"
)

// === プロジェクトExport/Import ===

// ExportProject はプロジェクト全体のデータをエクスポートする
func (s *PLCService) ExportProject() *ProjectDataDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 全サーバーのスナップショットを取得
	servers := make([]ServerSnapshotDTO, 0, len(s.servers))
	for _, inst := range s.servers {
		var settings map[string]interface{}
		if inst.config != nil {
			settings = inst.factory.ConfigToMap(inst.config)
		}

		var unitIDSettings *UnitIDSettingsDTO
		caps := inst.factory.GetProtocolCapabilities()
		if caps.SupportsUnitID {
			if us, ok := inst.server.(protocol.UnitIDConfigurable); ok {
				ids := us.GetDisabledUnitIDs()
				disabledIDs := make([]int, len(ids))
				for i, id := range ids {
					disabledIDs[i] = int(id)
				}
				unitIDSettings = &UnitIDSettingsDTO{
					Min:         caps.UnitIDMin,
					Max:         caps.UnitIDMax,
					DisabledIDs: disabledIDs,
				}
			}
		}

		servers = append(servers, ServerSnapshotDTO{
			ProtocolType:   string(inst.protocolType),
			Variant:        inst.variant,
			Settings:       settings,
			UnitIDSettings: unitIDSettings,
		})
	}

	// スクリプトを取得
	scripts := make([]*ScriptDTO, 0, len(s.scripts))
	for _, sc := range s.scripts {
		scripts = append(scripts, &ScriptDTO{
			ID:         sc.ID,
			Name:       sc.Name,
			Code:       sc.Code,
			IntervalMs: int(sc.Interval.Milliseconds()),
			IsRunning:  false,
		})
	}

	// モニタリング項目を取得
	monitoringItems := make([]*MonitoringItemDTO, 0, len(s.monitoringItems))
	for _, item := range s.monitoringItems {
		monitoringItems = append(monitoringItems, item)
	}

	// 構造体型定義を取得
	allStructTypes := s.variableStore.GetAllStructTypes()
	structTypeDTOs := make([]StructTypeDTO, len(allStructTypes))
	for i, st := range allStructTypes {
		structTypeDTOs[i] = structTypeToDTO(st)
	}

	// 変数を取得（s.mu.RLock() 保持中のため variableToDTO は使わず直接構築）
	vars := s.variableStore.GetAllVariables()
	variableDTOs := make([]*VariableDTO, 0, len(vars))
	for _, v := range vars {
		mappings := s.variableStore.GetMappings(v.ID)
		mappingDTOs := make([]ProtocolMappingDTO, len(mappings))
		for i, m := range mappings {
			mappingDTOs[i] = ProtocolMappingDTO{
				ProtocolType: m.ProtocolType,
				MemoryArea:   m.MemoryArea,
				Address:      int(m.Address),
				Endianness:   m.Endianness,
			}
		}
		var npDTOs []NodePublishingDTO
		for _, inst := range s.sortedServerInstances() {
			caps := inst.factory.GetProtocolCapabilities()
			if !caps.SupportsNodePublishing {
				continue
			}
			pt := string(inst.protocolType)
			np := s.variableStore.GetNodePublishing(v.ID, pt)
			enabled := false
			accessMode := "readwrite"
			if np != nil {
				enabled = np.Enabled
				accessMode = np.AccessMode
			}
			npDTOs = append(npDTOs, NodePublishingDTO{
				ProtocolType: pt,
				Enabled:      enabled,
				AccessMode:   accessMode,
			})
		}
		variableDTOs = append(variableDTOs, &VariableDTO{
			ID:              v.ID,
			Name:            v.Name,
			DataType:        string(v.DataType),
			Value:           normalizeVariableValueForJSON(v.Value, v.DataType),
			Mappings:        mappingDTOs,
			NodePublishings: npDTOs,
		})
	}

	return &ProjectDataDTO{
		Servers:         servers,
		Scripts:         scripts,
		MonitoringItems: monitoringItems,
		StructTypes:     structTypeDTOs,
		Variables:       variableDTOs,
	}
}

// ImportProject はプロジェクト全体のデータをインポートする
func (s *PLCService) ImportProject(data *ProjectDataDTO) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 実行中のスクリプトを全て停止
	if s.scriptEngine != nil {
		s.scriptEngine.StopAll()
	}

	// 全サーバーを停止・削除（RemoveServer と同じ後始末をする）。
	// プラグインのプロセスも止める。止めないと、読み込む前のレジスタの値や UnitID の設定が
	// プラグインに残り、読み込んだプロジェクトに含まれないプロトコルのプロセスも動き続ける
	for _, inst := range s.servers {
		if inst.server != nil {
			inst.server.Stop()
		}
		inst.detachSync()
		if stopper, ok := inst.factory.(pluginStopper); ok {
			stopper.StopProcess()
		}
	}
	s.servers = make(map[protocol.ProtocolType]*serverInstance)

	// 変数ストアをクリア（構造体型・変数・マッピング・公開設定をリセット）
	s.variableStore.ClearAll()

	// 構造体型を復元（変数より先に復元する必要がある）
	if data.StructTypes != nil {
		for _, stDTO := range data.StructTypes {
			fields := make([]variable.StructField, len(stDTO.Fields))
			for i, f := range stDTO.Fields {
				fields[i] = variable.StructField{
					Name:     f.Name,
					DataType: variable.DataType(f.DataType),
				}
			}
			st, err := variable.NewStructTypeDef(stDTO.Name, fields, s.variableStore)
			if err != nil {
				continue
			}
			s.variableStore.RegisterStructType(st)
		}
	}

	// 変数を復元（構造体型復元後・サーバー復元前）
	if data.Variables != nil {
		for _, dto := range data.Variables {
			v, err := s.variableStore.CreateVariable(dto.Name, variable.DataType(dto.DataType), dto.Value)
			if err != nil {
				continue
			}
			if len(dto.Mappings) > 0 {
				mappings := make([]variable.ProtocolMapping, len(dto.Mappings))
				for i, m := range dto.Mappings {
					mappings[i] = variable.ProtocolMapping{
						ProtocolType: m.ProtocolType,
						MemoryArea:   m.MemoryArea,
						Address:      uint32(m.Address),
						Endianness:   m.Endianness,
					}
				}
				_ = s.variableStore.SetMappings(v.ID, mappings)
			}
			for _, np := range dto.NodePublishings {
				s.variableStore.SetNodePublishing(v.ID, np.ProtocolType, &variable.NodePublishing{
					Enabled:    np.Enabled,
					AccessMode: np.AccessMode,
				})
			}
		}
	}

	for _, snap := range data.Servers {
		s.mu.Unlock()
		err := s.AddServer(snap.ProtocolType, snap.Variant)
		s.mu.Lock()
		if err != nil {
			return err
		}

		inst := s.servers[protocol.ProtocolType(snap.ProtocolType)]

		// 設定を更新
		if snap.Settings != nil && inst.factory != nil {
			newConfig, err := inst.factory.MapToConfig(snap.Variant, snap.Settings)
			if err == nil {
				if err := inst.server.UpdateConfig(newConfig); err == nil {
					inst.config = newConfig
				}
			}
		}

		// UnitID設定を復元
		if snap.UnitIDSettings != nil {
			if us, ok := inst.server.(protocol.UnitIDConfigurable); ok {
				uint8Ids := make([]uint8, len(snap.UnitIDSettings.DisabledIDs))
				for i, id := range snap.UnitIDSettings.DisabledIDs {
					uint8Ids[i] = uint8(id)
				}
				us.SetDisabledUnitIDs(uint8Ids)
			}
		}
	}

	// スクリプトを設定
	if data.Scripts != nil {
		s.nextOrder = 0
		s.scripts = make(map[string]*script.Script)
		for _, dto := range data.Scripts {
			sc := script.NewScript(
				dto.ID,
				dto.Name,
				dto.Code,
				time.Duration(dto.IntervalMs)*time.Millisecond,
				s.nextOrder,
			)
			s.scripts[dto.ID] = sc
			s.nextOrder++
		}
	}

	// モニタリング項目を設定
	if data.MonitoringItems != nil {
		s.monitoringItems = make(map[string]*MonitoringItemDTO)
		for _, item := range data.MonitoringItems {
			s.monitoringItems[item.ID] = item
		}
	}

	go s.emitServerChanged()
	go s.emitVariablesChanged()
	go s.emitScriptsChanged()

	return nil
}
