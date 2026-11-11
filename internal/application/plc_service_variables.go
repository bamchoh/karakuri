package application

import (
	"fmt"
	"sort"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
)

// === 変数管理API ===

// GetVariableStore は変数ストアを返す
func (s *PLCService) GetVariableStore() *variable.VariableStore {
	return s.variableStore
}

// GetVariables はすべての変数を返す
func (s *PLCService) GetVariables() []*VariableDTO {
	vars := s.variableStore.GetAllVariables()
	result := make([]*VariableDTO, len(vars))
	for i, v := range vars {
		result[i] = s.variableToDTO(v)
	}
	// 名前順でソート
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// CreateVariable は新しい変数を作成する
func (s *PLCService) CreateVariable(name, dataType string, initialValue interface{}) (*VariableDTO, error) {
	dt := variable.DataType(dataType)
	v, err := s.variableStore.CreateVariable(name, dt, initialValue)
	if err != nil {
		return nil, err
	}
	go s.emitVariablesChanged()
	return s.variableToDTO(v), nil
}

// UpdateVariableValue は変数の値を更新する
func (s *PLCService) UpdateVariableValue(id string, value interface{}) error {
	if err := s.variableStore.UpdateValue(id, value); err != nil {
		return err
	}
	go s.emitVariablesChanged()
	return nil
}

// UpdateVariableFieldValue は変数の特定フィールド/要素のみを更新する。
// fieldPath は外部インデックス（表示ベース）のパス文字列（例: "motor.speed", "items[1]"）
func (s *PLCService) UpdateVariableFieldValue(id, fieldPath string, value interface{}) error {
	if err := s.variableStore.UpdateFieldValue(id, fieldPath, value); err != nil {
		return err
	}
	go s.emitVariablesChanged()
	return nil
}

// DeleteVariable は変数を削除する
func (s *PLCService) DeleteVariable(id string) error {
	if err := s.variableStore.DeleteVariable(id); err != nil {
		return err
	}
	go s.emitVariablesChanged()
	return nil
}

// UpdateVariable は変数の名前とデータタイプを更新する
// データタイプが変更された場合は値をデフォルト値にリセットする
func (s *PLCService) UpdateVariable(id, name, dataType string) (*VariableDTO, error) {
	// 公開中の変数は、新しい名前でも公開できるか先に確認する（ノードアドレスに変数名を使うプロトコルのため）
	if old, err := s.variableStore.GetVariable(id); err == nil && old.Name != name {
		s.mu.RLock()
		protocolTypes := make([]string, 0, len(s.servers))
		for pt := range s.servers {
			protocolTypes = append(protocolTypes, string(pt))
		}
		s.mu.RUnlock()
		for _, pt := range protocolTypes {
			if pub := s.variableStore.GetNodePublishing(id, pt); pub == nil || !pub.Enabled {
				continue
			}
			if err := s.checkNodePublishable(pt, id, name); err != nil {
				return nil, err
			}
		}
	}

	v, err := s.variableStore.UpdateMetadata(id, name, variable.DataType(dataType))
	if err != nil {
		return nil, err
	}

	// NodePublishingAware なサーバー全てに変更通知を送信
	s.mu.RLock()
	for _, inst := range s.servers {
		if inst.server != nil {
			if aware, ok := inst.server.(protocol.NodePublishingAware); ok {
				aware.OnNodePublishingUpdated()
			}
		}
	}
	s.mu.RUnlock()

	go s.emitVariablesChanged()

	return s.variableToDTO(v), nil
}

// GetVariableMappings は変数のマッピングを返す
func (s *PLCService) GetVariableMappings(id string) ([]ProtocolMappingDTO, error) {
	mappings := s.variableStore.GetMappings(id)
	result := make([]ProtocolMappingDTO, len(mappings))
	for i, m := range mappings {
		result[i] = ProtocolMappingDTO{
			ProtocolType: m.ProtocolType,
			MemoryArea:   m.MemoryArea,
			Address:      int(m.Address),
			Endianness:   m.Endianness,
		}
	}
	return result, nil
}

// UpdateVariableNodePublishing は変数のプロトコル公開設定を更新する
func (s *PLCService) UpdateVariableNodePublishing(variableID, protocolType string, dto *NodePublishingDTO) error {
	if dto.Enabled {
		if v, err := s.variableStore.GetVariable(variableID); err == nil {
			if err := s.checkNodePublishable(protocolType, variableID, v.Name); err != nil {
				return err
			}
		}
	}
	s.variableStore.SetNodePublishing(variableID, protocolType, &variable.NodePublishing{
		Enabled:    dto.Enabled,
		AccessMode: dto.AccessMode,
	})

	// 対象サーバーが NodePublishingAware なら通知
	s.mu.RLock()
	inst, ok := s.servers[protocol.ProtocolType(protocolType)]
	s.mu.RUnlock()
	if ok && inst.server != nil {
		if aware, ok := inst.server.(protocol.NodePublishingAware); ok {
			aware.OnNodePublishingUpdated()
		}
	}
	go s.emitVariablesChanged()
	return nil
}

// UpdateVariableMappings は変数のマッピングを更新する
func (s *PLCService) UpdateVariableMappings(id string, mappingDTOs []ProtocolMappingDTO) error {
	mappings := make([]variable.ProtocolMapping, len(mappingDTOs))
	for i, dto := range mappingDTOs {
		mappings[i] = variable.ProtocolMapping{
			ProtocolType: dto.ProtocolType,
			MemoryArea:   dto.MemoryArea,
			Address:      uint32(dto.Address),
			Endianness:   dto.Endianness,
		}
	}
	if err := s.variableStore.SetMappings(id, mappings); err != nil {
		return err
	}
	go s.emitVariablesChanged()
	return nil
}

// GetDataTypes はデータ型一覧を返す
func (s *PLCService) GetDataTypes() *DataTypesDTO {
	types := variable.AllDataTypes()
	result := make([]DataTypeInfoDTO, 0, len(types))
	for _, dt := range types {
		// STRING は STRING[n] としてUIで動的生成するため除外
		if dt == variable.TypeSTRING {
			continue
		}
		result = append(result, DataTypeInfoDTO{
			ID:          string(dt),
			DisplayName: string(dt),
			Description: dataTypeDescription(dt),
			WordCount:   dt.WordCount(),
		})
	}

	// 構造体型情報を含める
	structTypes := s.variableStore.GetAllStructTypes()
	structDTOs := make([]StructTypeDTO, len(structTypes))
	for i, st := range structTypes {
		structDTOs[i] = structTypeToDTO(st)
	}

	return &DataTypesDTO{Types: result, StructTypes: structDTOs}
}

// === 構造体型管理 ===

// RegisterStructType は構造体型を登録する
func (s *PLCService) RegisterStructType(dto StructTypeDTO) (*StructTypeDTO, error) {
	fields := make([]variable.StructField, len(dto.Fields))
	for i, f := range dto.Fields {
		fields[i] = variable.StructField{
			Name:     f.Name,
			DataType: variable.DataType(f.DataType),
		}
	}

	st, err := variable.NewStructTypeDef(dto.Name, fields, s.variableStore)
	if err != nil {
		return nil, err
	}

	if err := s.variableStore.RegisterStructType(st); err != nil {
		return nil, err
	}

	go s.emitVariablesChanged()

	result := structTypeToDTO(st)
	return &result, nil
}

// GetStructTypes は全構造体型を返す
func (s *PLCService) GetStructTypes() []StructTypeDTO {
	structTypes := s.variableStore.GetAllStructTypes()
	result := make([]StructTypeDTO, len(structTypes))
	for i, st := range structTypes {
		result[i] = structTypeToDTO(st)
	}
	return result
}

// DeleteStructType は構造体型を削除する
func (s *PLCService) DeleteStructType(name string) error {
	if err := s.variableStore.DeleteStructType(name); err != nil {
		return err
	}
	go s.emitVariablesChanged()
	return nil
}

// structTypeToDTO は構造体型をDTOに変換する
func structTypeToDTO(st *variable.StructTypeDef) StructTypeDTO {
	fields := make([]StructFieldDTO, len(st.Fields))
	for i, f := range st.Fields {
		fields[i] = StructFieldDTO{
			Name:     f.Name,
			DataType: string(f.DataType),
			Offset:   f.Offset,
		}
	}
	return StructTypeDTO{
		Name:      st.Name,
		Fields:    fields,
		WordCount: st.WordCount,
	}
}

// variableToDTO は変数をDTOに変換する
func (s *PLCService) variableToDTO(v *variable.Variable) *VariableDTO {
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

	// NodePublishings: 全プロトコルの公開設定を収集（登録順で固定）
	var npDTOs []NodePublishingDTO
	s.mu.RLock()
	sortedInsts := s.sortedServerInstances()
	for _, inst := range sortedInsts {
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
	s.mu.RUnlock()

	return &VariableDTO{
		ID:              v.ID,
		Name:            v.Name,
		DataType:        string(v.DataType),
		Value:           normalizeVariableValueForJSON(v.Value, v.DataType),
		Mappings:        mappingDTOs,
		NodePublishings: npDTOs,
	}
}

// sortedServerInstances は s.servers を addedOrder 順に並べたスライスを返す。
// Go の map 反復が非決定的なため、表示順を安定させるために使用する。
// 呼び出し側は必要に応じて s.mu を保持すること。
func (s *PLCService) sortedServerInstances() []*serverInstance {
	insts := make([]*serverInstance, 0, len(s.servers))
	for _, inst := range s.servers {
		insts = append(insts, inst)
	}
	sort.Slice(insts, func(i, j int) bool {
		return insts[i].addedOrder < insts[j].addedOrder
	})
	return insts
}

// normalizeVariableValueForJSON はJSONシリアライズ時の精度損失を防ぐため
// LINT/ULINT の int64/uint64 値を文字列に変換する
// JavaScript の Number は ±2^53 を超える整数を正確に表現できないため
func normalizeVariableValueForJSON(value interface{}, dt variable.DataType) interface{} {
	switch dt {
	case variable.TypeLINT:
		if val, ok := value.(int64); ok {
			return fmt.Sprintf("%d", val)
		}
	case variable.TypeULINT:
		if val, ok := value.(uint64); ok {
			return fmt.Sprintf("%d", val)
		}
	default:
		// ARRAY[LINT;n] / ARRAY[ULINT;n] の要素も文字列化する
		if dt.IsArrayType() {
			elemType, _, err := variable.ParseArrayType(dt)
			if err != nil {
				return value
			}
			if elemType != variable.TypeLINT && elemType != variable.TypeULINT {
				return value
			}
			arr, ok := value.([]interface{})
			if !ok {
				return value
			}
			result := make([]interface{}, len(arr))
			for i, v := range arr {
				result[i] = normalizeVariableValueForJSON(v, elemType)
			}
			return result
		}
	}
	return value
}

// dataTypeDescription はデータ型の説明を返す
func dataTypeDescription(dt variable.DataType) string {
	switch dt {
	case variable.TypeBOOL:
		return "ブール値 (1ビット)"
	case variable.TypeSINT:
		return "符号付き8ビット整数"
	case variable.TypeINT:
		return "符号付き16ビット整数"
	case variable.TypeDINT:
		return "符号付き32ビット整数"
	case variable.TypeLINT:
		return "符号付き64ビット整数"
	case variable.TypeUSINT:
		return "符号なし8ビット整数"
	case variable.TypeUINT:
		return "符号なし16ビット整数"
	case variable.TypeUDINT:
		return "符号なし32ビット整数"
	case variable.TypeULINT:
		return "符号なし64ビット整数"
	case variable.TypeREAL:
		return "32ビット浮動小数点"
	case variable.TypeLREAL:
		return "64ビット浮動小数点"
	case variable.TypeSTRING:
		return "文字列"
	case variable.TypeTIME:
		return "経過時間"
	case variable.TypeDATE:
		return "日付"
	case variable.TypeTIME_OF_DAY:
		return "時刻"
	case variable.TypeDATE_AND_TIME:
		return "日時"
	default:
		return ""
	}
}
