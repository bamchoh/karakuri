package variable

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// ChangeListener は変数変更通知のリスナー
type ChangeListener interface {
	// OnVariableChanged は変数の値（またはマッピング・名前・データ型）が変わったときに呼ばれる。
	// 構造体のフィールドや配列の要素だけを書いた場合も、v.Value は変数全体の値。
	OnVariableChanged(v *Variable, mappings []ProtocolMapping)
}

// NodePublishing はノードベースプロトコルへの変数公開設定
type NodePublishing struct {
	Enabled    bool   `json:"enabled"`
	AccessMode string `json:"accessMode"` // "read" | "write" | "readwrite"
}

// VariableStore は中央変数ストア
type VariableStore struct {
	mu              sync.RWMutex
	variables       map[string]*Variable                  // id -> variable
	byName          map[string]*Variable                  // name -> variable
	mappings        map[string][]ProtocolMapping          // variableID -> mappings
	structTypes     map[string]*StructTypeDef             // 構造体型名 -> 型定義
	nodePublishings map[string]map[string]*NodePublishing // variableID -> protocolType -> NodePublishing
	listeners       []ChangeListener
}

// NewVariableStore は新しいVariableStoreを作成する
func NewVariableStore() *VariableStore {
	return &VariableStore{
		variables:       make(map[string]*Variable),
		byName:          make(map[string]*Variable),
		mappings:        make(map[string][]ProtocolMapping),
		structTypes:     make(map[string]*StructTypeDef),
		nodePublishings: make(map[string]map[string]*NodePublishing),
	}
}

// GetNodePublishing は変数のプロトコル公開設定を取得する
func (s *VariableStore) GetNodePublishing(variableID, protocolType string) *NodePublishing {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if m, ok := s.nodePublishings[variableID]; ok {
		return m[protocolType]
	}
	return nil
}

// SetNodePublishing は変数のプロトコル公開設定を設定する
func (s *VariableStore) SetNodePublishing(variableID, protocolType string, p *NodePublishing) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodePublishings[variableID]; !ok {
		s.nodePublishings[variableID] = make(map[string]*NodePublishing)
	}
	s.nodePublishings[variableID][protocolType] = p
}

// GetAllNodePublishings は指定プロトコルの全変数の公開設定を返す（variableID → NodePublishing）
func (s *VariableStore) GetAllNodePublishings(protocolType string) map[string]*NodePublishing {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*NodePublishing)
	for varID, protocols := range s.nodePublishings {
		if np, ok := protocols[protocolType]; ok {
			result[varID] = np
		}
	}
	return result
}

// ResolveStructWordCount はTypeResolverインターフェースの実装
func (s *VariableStore) ResolveStructWordCount(typeName string) (int, error) {
	st, ok := s.structTypes[typeName]
	if !ok {
		return 0, fmt.Errorf("struct type %s not found", typeName)
	}
	return st.WordCount, nil
}

// ResolveStructDef は構造体型定義を返す（TypeResolverインターフェース実装）
func (s *VariableStore) ResolveStructDef(typeName string) (*StructTypeDef, error) {
	st, ok := s.structTypes[typeName]
	if !ok {
		return nil, fmt.Errorf("struct type %s not found", typeName)
	}
	return st, nil
}

// RegisterStructType は構造体型を登録する
func (s *VariableStore) RegisterStructType(def *StructTypeDef) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.structTypes[def.Name]; exists {
		return fmt.Errorf("struct type %s already exists", def.Name)
	}
	s.structTypes[def.Name] = def
	return nil
}

// GetStructType は構造体型定義を取得する
func (s *VariableStore) GetStructType(name string) (*StructTypeDef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, ok := s.structTypes[name]
	if !ok {
		return nil, fmt.Errorf("struct type %s not found", name)
	}
	return st, nil
}

// GetAllStructTypes は全ての構造体型定義を返す
func (s *VariableStore) GetAllStructTypes() []*StructTypeDef {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*StructTypeDef, 0, len(s.structTypes))
	for _, st := range s.structTypes {
		result = append(result, st)
	}
	return result
}

// DeleteStructType は構造体型を削除する（使用中の変数がないかチェック）
func (s *VariableStore) DeleteStructType(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.structTypes[name]; !ok {
		return fmt.Errorf("struct type %s not found", name)
	}

	// 使用中の変数がないかチェック
	dt := DataType(name)
	for _, v := range s.variables {
		if v.DataType == dt {
			return fmt.Errorf("struct type %s is in use by variable %s", name, v.Name)
		}
	}

	delete(s.structTypes, name)
	return nil
}

// AddListener は変更リスナーを追加する
func (s *VariableStore) AddListener(l ChangeListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, l)
}

// RemoveListener は変更リスナーを削除する
func (s *VariableStore) RemoveListener(l ChangeListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, listener := range s.listeners {
		if listener == l {
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			return
		}
	}
}

// prepareNotify は、変数 v の変更をリスナーに通知する関数を返す（ロック済みで呼ぶ）。
// 返した関数は、ロックを外してから呼ぶ（リスナーの中から変数ストアを呼べるようにするため）。
// origin が nil でなければ、そのリスナーには通知しない（自分の書き込みを自分に戻さないため）。
func (s *VariableStore) prepareNotify(v *Variable, origin ChangeListener) func() {
	snapshot := v.Clone() // 通知はロックの外で行うので、この時点の内容のコピーを渡す
	mappings := s.getMappingsCopy(v.ID)
	listeners := make([]ChangeListener, len(s.listeners))
	copy(listeners, s.listeners)
	return func() {
		for _, l := range listeners {
			if origin != nil && l == origin {
				continue
			}
			l.OnVariableChanged(snapshot, mappings)
		}
	}
}

// generateDefaultValue はデータ型のデフォルト値を生成する（ロック済み前提）
func (s *VariableStore) generateDefaultValue(dataType DataType) (interface{}, error) {
	if dataType.IsValid() {
		return dataType.DefaultValue(), nil
	} else if dataType.IsArrayType() {
		elemType, size, err := ParseArrayType(dataType)
		if err != nil {
			return nil, err
		}
		if !elemType.IsValid() && !elemType.IsStructType() && !elemType.IsArrayType() {
			return nil, fmt.Errorf("invalid element type: %s", elemType)
		}
		arr := make([]interface{}, size)
		for i := range arr {
			elemVal, err := s.generateDefaultValue(elemType)
			if err != nil {
				return nil, err
			}
			arr[i] = elemVal
		}
		return arr, nil
	} else if dataType.IsStructType() {
		st, ok := s.structTypes[string(dataType)]
		if !ok {
			return nil, fmt.Errorf("struct type %s not found", dataType)
		}
		return st.DefaultValueWithResolver(s), nil
	}
	return nil, fmt.Errorf("invalid data type: %s", dataType)
}

// UpdateMetadata は変数の名前とデータタイプを更新する
// データタイプが変更された場合は値をデフォルト値にリセットする
func (s *VariableStore) UpdateMetadata(id string, newName string, newDataType DataType) (*Variable, error) {
	s.mu.Lock()

	v, exists := s.variables[id]
	if !exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("variable %s not found", id)
	}

	// 名前変更の場合は重複チェック
	if newName != v.Name {
		if _, nameExists := s.byName[newName]; nameExists {
			s.mu.Unlock()
			return nil, fmt.Errorf("variable %s already exists", newName)
		}
		delete(s.byName, v.Name)
		v.Name = newName
		s.byName[newName] = v
	}

	// データタイプ変更の場合は値をデフォルト値にリセット
	if newDataType != v.DataType {
		newValue, err := s.generateDefaultValue(newDataType)
		if err != nil {
			s.mu.Unlock()
			return nil, fmt.Errorf("failed to generate default value for %s: %w", newDataType, err)
		}
		v.DataType = newDataType
		v.Value = newValue
	}

	notify := s.prepareNotify(v, nil)
	s.mu.Unlock()
	notify()

	return v.Clone(), nil
}

// CreateVariable は新しい変数を作成する
func (s *VariableStore) CreateVariable(name string, dataType DataType, initialValue interface{}) (*Variable, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.byName[name]; exists {
		return nil, fmt.Errorf("variable %s already exists", name)
	}

	var value interface{}

	if dataType.IsValid() {
		// スカラー型
		converted, err := ConvertValue(initialValue, dataType)
		if err != nil {
			converted = dataType.DefaultValue()
		}
		value = converted
	} else if dataType.IsArrayType() {
		// 配列型
		elemType, size, err := ParseArrayType(dataType)
		if err != nil {
			return nil, err
		}
		if !elemType.IsValid() && !elemType.IsStructType() && !elemType.IsArrayType() {
			return nil, fmt.Errorf("invalid element type: %s", elemType)
		}
		if initialValue == nil {
			arr := make([]interface{}, size)
			for i := range arr {
				elemVal, genErr := s.generateDefaultValue(elemType)
				if genErr != nil {
					return nil, genErr
				}
				arr[i] = elemVal
			}
			value = arr
		} else {
			converted, err := ConvertValueWithResolver(initialValue, dataType, s)
			if err != nil {
				// デフォルト配列を生成
				arr := make([]interface{}, size)
				for i := range arr {
					elemVal, genErr := s.generateDefaultValue(elemType)
					if genErr != nil {
						return nil, genErr
					}
					arr[i] = elemVal
				}
				value = arr
			} else {
				value = converted
			}
		}
	} else if dataType.IsStructType() {
		// 構造体型
		st, ok := s.structTypes[string(dataType)]
		if !ok {
			return nil, fmt.Errorf("struct type %s not found", dataType)
		}
		if initialValue == nil {
			value = st.DefaultValueWithResolver(s)
		} else if _, ok := initialValue.(map[string]interface{}); ok {
			converted, err := ConvertValueWithResolver(initialValue, dataType, s)
			if err != nil {
				value = st.DefaultValueWithResolver(s)
			} else {
				value = converted
			}
		} else {
			value = st.DefaultValueWithResolver(s)
		}
	} else {
		return nil, fmt.Errorf("invalid data type: %s", dataType)
	}

	v := &Variable{
		ID:       uuid.New().String(),
		Name:     name,
		DataType: dataType,
		Value:    value,
	}

	s.variables[v.ID] = v
	s.byName[v.Name] = v
	s.mappings[v.ID] = nil

	return v.Clone(), nil
}

// GetVariable はIDで変数を取得する。
//
// 変数を返すメソッド（GetVariable / GetVariableByName / GetAllVariables / FindVariableByMapping）と
// リスナーへの通知は、その時点の内容のコピーを渡す。ストアの中の変数は、ロックの中でしか読み書きしない
// （呼び出し側がロックの外で読んでいる間に、別の書き込みが入っても競合しないようにするため）。
// 値（配列・構造体）そのものは書き換えず、更新のたびに新しい値に差し替えるので、コピーは浅くてよい。
func (s *VariableStore) GetVariable(id string) (*Variable, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, exists := s.variables[id]
	if !exists {
		return nil, fmt.Errorf("variable %s not found", id)
	}
	return v.Clone(), nil
}

// GetVariableByName は名前で変数を取得する
func (s *VariableStore) GetVariableByName(name string) (*Variable, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, exists := s.byName[name]
	if !exists {
		return nil, fmt.Errorf("variable %s not found", name)
	}
	return v.Clone(), nil
}

// GetAllVariables はすべての変数を取得する
func (s *VariableStore) GetAllVariables() []*Variable {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Variable, 0, len(s.variables))
	for _, v := range s.variables {
		result = append(result, v.Clone())
	}
	return result
}

// UpdateValue は変数の値を更新する
func (s *VariableStore) UpdateValue(id string, value interface{}) error {
	return s.UpdateValueFrom(id, value, nil)
}

// UpdateValueFrom は変数の値を更新し、origin 以外のリスナーに通知する。
// 変数と外部の値（レジスタなど）を同期するリスナーが、自分が持ち込んだ変更を
// 自分に通知させない（書き戻さない）ために使う。origin が nil なら全リスナーに通知する。
func (s *VariableStore) UpdateValueFrom(id string, value interface{}, origin ChangeListener) error {
	s.mu.Lock()
	v, exists := s.variables[id]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("variable %s not found", id)
	}

	converted, err := ConvertValueWithResolver(value, v.DataType, s)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to set %s: %w", v.Name, err)
	}

	v.Value = converted
	notify := s.prepareNotify(v, origin)
	s.mu.Unlock()
	notify()

	return nil
}

// UpdateValueByName は名前で変数の値を更新する
func (s *VariableStore) UpdateValueByName(name string, value interface{}) error {
	s.mu.RLock()
	v, exists := s.byName[name]
	if !exists {
		s.mu.RUnlock()
		return fmt.Errorf("variable %s not found", name)
	}
	id := v.ID
	s.mu.RUnlock()

	return s.UpdateValue(id, value)
}

// DeleteVariable は変数を削除する
func (s *VariableStore) DeleteVariable(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, exists := s.variables[id]
	if !exists {
		return fmt.Errorf("variable %s not found", id)
	}

	delete(s.byName, v.Name)
	delete(s.variables, id)
	delete(s.mappings, id)
	delete(s.nodePublishings, id)

	return nil
}

// GetMappings は変数のマッピングを取得する
func (s *VariableStore) GetMappings(variableID string) []ProtocolMapping {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getMappingsCopy(variableID)
}

// getMappingsCopy はマッピングのコピーを返す（ロック済み前提）
func (s *VariableStore) getMappingsCopy(variableID string) []ProtocolMapping {
	mappings := s.mappings[variableID]
	if mappings == nil {
		return nil
	}
	result := make([]ProtocolMapping, len(mappings))
	copy(result, mappings)
	return result
}

// SetMappings は変数のマッピングを設定する
func (s *VariableStore) SetMappings(variableID string, mappings []ProtocolMapping) error {
	s.mu.Lock()
	if _, exists := s.variables[variableID]; !exists {
		s.mu.Unlock()
		return fmt.Errorf("variable %s not found", variableID)
	}

	s.mappings[variableID] = mappings

	// マッピング変更時に現在の値をリスナーに通知
	v := s.variables[variableID]
	notify := s.prepareNotify(v, nil)
	s.mu.Unlock()
	notify()

	return nil
}

// FindVariableByMapping はプロトコル・エリア・アドレスに対応する変数を検索する
func (s *VariableStore) FindVariableByMapping(protocolType, area string, address uint32) (*Variable, *ProtocolMapping) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for varID, mappings := range s.mappings {
		for i := range mappings {
			m := mappings[i]
			if m.ProtocolType == protocolType && m.MemoryArea == area {
				v := s.variables[varID]
				wordCount := v.DataType.WordCountWithResolver(s)
				// アドレス範囲内かチェック
				if address >= m.Address && address < m.Address+uint32(wordCount) {
					return v.Clone(), &m
				}
			}
		}
	}
	return nil, nil
}

// GetAllMappingsForProtocol は指定プロトコルの全マッピングを返す
func (s *VariableStore) GetAllMappingsForProtocol(protocolType string) map[string][]ProtocolMapping {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]ProtocolMapping)
	for varID, mappings := range s.mappings {
		for _, m := range mappings {
			if m.ProtocolType == protocolType {
				result[varID] = append(result[varID], m)
			}
		}
	}
	return result
}

// Snapshot は変数ストアの状態をスナップショットとして返す
func (s *VariableStore) Snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vars := make([]map[string]interface{}, 0, len(s.variables))
	for _, v := range s.variables {
		varData := map[string]interface{}{
			"id":       v.ID,
			"name":     v.Name,
			"dataType": string(v.DataType),
			"value":    v.Value,
		}
		if mappings, ok := s.mappings[v.ID]; ok && len(mappings) > 0 {
			mappingData := make([]map[string]interface{}, len(mappings))
			for i, m := range mappings {
				mappingData[i] = map[string]interface{}{
					"protocolType": m.ProtocolType,
					"memoryArea":   m.MemoryArea,
					"address":      m.Address,
					"endianness":   m.Endianness,
				}
			}
			varData["mappings"] = mappingData
		}
		vars = append(vars, varData)
	}

	// 構造体型定義
	structTypesData := make([]map[string]interface{}, 0, len(s.structTypes))
	for _, st := range s.structTypes {
		fieldsData := make([]map[string]interface{}, len(st.Fields))
		for i, f := range st.Fields {
			fieldsData[i] = map[string]interface{}{
				"name":     f.Name,
				"dataType": string(f.DataType),
			}
		}
		structTypesData = append(structTypesData, map[string]interface{}{
			"name":   st.Name,
			"fields": fieldsData,
		})
	}

	// nodePublishings
	npData := make(map[string]interface{})
	for varID, protocols := range s.nodePublishings {
		perProtocol := make(map[string]interface{})
		for pt, np := range protocols {
			perProtocol[pt] = map[string]interface{}{
				"enabled":    np.Enabled,
				"accessMode": np.AccessMode,
			}
		}
		npData[varID] = perProtocol
	}

	return map[string]interface{}{
		"variables":       vars,
		"structTypes":     structTypesData,
		"nodePublishings": npData,
	}
}

// Restore はスナップショットから状態を復元する
func (s *VariableStore) Restore(data map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.variables = make(map[string]*Variable)
	s.byName = make(map[string]*Variable)
	s.mappings = make(map[string][]ProtocolMapping)
	s.structTypes = make(map[string]*StructTypeDef)
	s.nodePublishings = make(map[string]map[string]*NodePublishing)

	// 構造体型定義を先に復元
	if stData, ok := data["structTypes"].([]interface{}); ok {
		for _, item := range stData {
			stMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := stMap["name"].(string)
			fieldsData, _ := stMap["fields"].([]interface{})
			fields := make([]StructField, 0, len(fieldsData))
			for _, fItem := range fieldsData {
				fMap, ok := fItem.(map[string]interface{})
				if !ok {
					continue
				}
				fName, _ := fMap["name"].(string)
				fType, _ := fMap["dataType"].(string)
				fields = append(fields, StructField{
					Name:     fName,
					DataType: DataType(fType),
				})
			}
			def, err := NewStructTypeDef(name, fields, s)
			if err != nil {
				continue
			}
			s.structTypes[name] = def
		}
	}

	vars, ok := data["variables"].([]interface{})
	if !ok {
		return nil // variablesがなくても構造体型定義のみ復元は成功
	}

	for _, item := range vars {
		vMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		id, _ := vMap["id"].(string)
		name, _ := vMap["name"].(string)
		dataType, _ := vMap["dataType"].(string)
		value := vMap["value"]

		if id == "" {
			id = uuid.New().String()
		}

		dt := DataType(dataType)
		var finalValue interface{}
		if dt.IsArrayType() || dt.IsStructType() {
			// 配列・構造体はJSONデシリアライズ済みの値をそのまま使う
			finalValue = value
		} else {
			converted, err := ConvertValue(value, dt)
			if err != nil {
				converted = dt.DefaultValue()
			}
			finalValue = converted
		}

		v := &Variable{
			ID:       id,
			Name:     name,
			DataType: dt,
			Value:    finalValue,
		}

		s.variables[id] = v
		s.byName[name] = v

		// マッピングの復元
		if mappingsData, ok := vMap["mappings"].([]interface{}); ok {
			mappings := make([]ProtocolMapping, 0, len(mappingsData))
			for _, mItem := range mappingsData {
				mMap, ok := mItem.(map[string]interface{})
				if !ok {
					continue
				}
				protocolType, _ := mMap["protocolType"].(string)
				memoryArea, _ := mMap["memoryArea"].(string)
				address, _ := mMap["address"].(float64)
				endianness, _ := mMap["endianness"].(string)

				mappings = append(mappings, ProtocolMapping{
					ProtocolType: protocolType,
					MemoryArea:   memoryArea,
					Address:      uint32(address),
					Endianness:   endianness,
				})
			}
			s.mappings[id] = mappings
		}
	}

	// nodePublishings の復元
	if npData, ok := data["nodePublishings"].(map[string]interface{}); ok {
		for varID, protocols := range npData {
			protocolsMap, ok := protocols.(map[string]interface{})
			if !ok {
				continue
			}
			s.nodePublishings[varID] = make(map[string]*NodePublishing)
			for pt, npRaw := range protocolsMap {
				npMap, ok := npRaw.(map[string]interface{})
				if !ok {
					continue
				}
				enabled, _ := npMap["enabled"].(bool)
				accessMode, _ := npMap["accessMode"].(string)
				s.nodePublishings[varID][pt] = &NodePublishing{
					Enabled:    enabled,
					AccessMode: accessMode,
				}
			}
		}
	}

	return nil
}

// ---- フィールドパス操作 ----

// ClearAll はすべての変数をクリアする
func (s *VariableStore) ClearAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.variables = make(map[string]*Variable)
	s.byName = make(map[string]*Variable)
	s.mappings = make(map[string][]ProtocolMapping)
	s.structTypes = make(map[string]*StructTypeDef)
	s.nodePublishings = make(map[string]map[string]*NodePublishing)
}
