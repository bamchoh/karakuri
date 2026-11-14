package application

import (
	"context"
	"errors"
	"fmt"

	"karakuri/internal/domain/protocol"
)

// === サーバー管理 ===

// StartServer はサーバーを起動する
func (s *PLCService) StartServer(protocolType string) (err error) {
	defer func() {
		// 失敗の理由は「サーバーの起動に失敗しました: ...」の形で返るので、そのまま出す
		if err != nil {
			s.serverLog(protocolType).Errorf("%v", err)
		} else {
			s.serverLog(protocolType).Infof("サーバーを起動しました")
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return err
	}
	if inst.server == nil {
		return fmt.Errorf("server not initialized")
	}

	if err := inst.server.Start(context.Background()); err != nil {
		return fmt.Errorf("サーバーの起動に失敗しました: %w", err)
	}
	go s.emitServerChanged()
	return nil
}

// StopServer はサーバーを停止する
func (s *PLCService) StopServer(protocolType string) (err error) {
	// 止まっているサーバーを止めても、ログは出さない
	wasRunning := false
	defer func() {
		if err != nil {
			s.serverLog(protocolType).Errorf("サーバーを停止できませんでした: %v", err)
		} else if wasRunning {
			s.serverLog(protocolType).Infof("サーバーを停止しました")
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return err
	}
	if inst.server == nil {
		return nil
	}
	wasRunning = inst.server.Status() == protocol.StatusRunning

	if err := inst.server.Stop(); err != nil {
		return err
	}
	go s.emitServerChanged()
	return nil
}

// GetServerStatus はサーバーのステータスを返す
func (s *PLCService) GetServerStatus(protocolType string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return "Stopped"
	}
	if inst.server != nil {
		return inst.server.Status().String()
	}
	return "Stopped"
}

// GetNodeAddresses は公開変数のノードアドレス（OPC UA なら NodeID）を返す。
// アドレスの書式と、公開できない変数の判定はプロトコルの実装が決める。
// 対応していないプロトコルでは空のスライスを返す。
func (s *PLCService) GetNodeAddresses(protocolType string, variableIDs []string) []NodeAddressDTO {
	// アドレスに変数名を使うプロトコルのため、名前も渡す
	variables := make([]protocol.NodeVariable, 0, len(variableIDs))
	for _, id := range variableIDs {
		if v, err := s.variableStore.GetVariable(id); err == nil {
			variables = append(variables, protocol.NodeVariable{ID: id, Name: v.Name})
		}
	}
	addrs := s.resolveNodeAddresses(protocolType, variables)
	result := make([]NodeAddressDTO, 0, len(addrs))
	for _, v := range variables {
		if a, ok := addrs[v.ID]; ok {
			result = append(result, NodeAddressDTO{VariableID: v.ID, Address: a.Address, Error: a.Error})
		}
	}
	return result
}

// resolveNodeAddresses は NodeAddressResolver を実装するサーバーにノードアドレスを問い合わせる
func (s *PLCService) resolveNodeAddresses(protocolType string, variables []protocol.NodeVariable) map[string]protocol.NodeAddress {
	s.mu.RLock()
	inst, err := s.getServerInstance(protocolType)
	var srv protocol.ProtocolServer
	var factory protocol.ServerFactory
	var config protocol.ProtocolConfig
	if err == nil {
		srv, factory, config = inst.server, inst.factory, inst.config
	}
	s.mu.RUnlock()

	if err != nil || len(variables) == 0 {
		return nil
	}
	// 一度も起動していないサーバーは未作成なので、現在の設定で作って問い合わせる（起動はしない）
	if srv == nil {
		if srv, err = factory.CreateServer(config, nil); err != nil {
			return nil
		}
	}
	if resolver, ok := srv.(protocol.NodeAddressResolver); ok {
		return resolver.NodeAddresses(variables)
	}
	return nil
}

// checkNodePublishable は変数を name の名前で protocolType に公開できるか確認する（できなければ理由をエラーで返す）
func (s *PLCService) checkNodePublishable(protocolType, variableID, name string) error {
	addrs := s.resolveNodeAddresses(protocolType, []protocol.NodeVariable{{ID: variableID, Name: name}})
	if a, ok := addrs[variableID]; ok && a.Error != "" {
		return errors.New(a.Error)
	}
	return nil
}

// === プロトコル管理API ===

// GetAvailableProtocols は利用可能なプロトコル一覧を、登録した順に返す
func (s *PLCService) GetAvailableProtocols() []ProtocolInfoDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ProtocolInfoDTO, 0, len(s.factories))
	for _, pt := range s.factoryOrder {
		factory := s.factories[pt]
		variants := factory.ConfigVariants()
		variantDTOs := make([]ConfigVariantDTO, len(variants))
		for j, v := range variants {
			variantDTOs[j] = ConfigVariantDTO{
				ID:          v.ID,
				DisplayName: v.DisplayName,
			}
		}
		result = append(result, ProtocolInfoDTO{
			Type:        string(factory.ProtocolType()),
			DisplayName: factory.DisplayName(),
			Variants:    variantDTOs,
		})
	}
	return result
}

// GetProtocolSchema はプロトコルスキーマを返す
func (s *PLCService) GetProtocolSchema(protocolType string) (*ProtocolSchemaDTO, error) {
	s.mu.RLock()
	factory, ok := s.factories[protocol.ProtocolType(protocolType)]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("protocol not found: %s", protocolType)
	}

	variants := factory.ConfigVariants()
	// バリアントがない場合（OPC UA 等）は空 ID で1つ生成してフィールドを取得する
	if len(variants) == 0 {
		variants = []protocol.ConfigVariant{{ID: "", DisplayName: ""}}
	}
	variantDTOs := make([]VariantDTO, len(variants))
	for i, v := range variants {
		variantDTOs[i] = VariantDTO{
			ID:          v.ID,
			DisplayName: v.DisplayName,
			Fields:      toFieldDTOs(factory.GetConfigFields(v.ID)),
		}
	}

	caps := factory.GetProtocolCapabilities()
	return &ProtocolSchemaDTO{
		ProtocolType: string(factory.ProtocolType()),
		DisplayName:  factory.DisplayName(),
		Variants:     variantDTOs,
		Capabilities: CapabilitiesDTO{
			SupportsUnitID: caps.SupportsUnitID,
			UnitIDMin:      caps.UnitIDMin,
			UnitIDMax:      caps.UnitIDMax,
		},
	}, nil
}

// GetServerConfig は指定サーバーの現在の設定を返す
func (s *PLCService) GetServerConfig(protocolType string) *ServerConfigDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return nil
	}

	return &ServerConfigDTO{
		ProtocolType: protocolType,
		Variant:      inst.variant,
		Settings:     inst.factory.ConfigToMap(inst.config),
	}
}

// UpdateServerConfig はサーバーの設定を更新する
func (s *PLCService) UpdateServerConfig(dto *ServerConfigDTO) (err error) {
	defer func() {
		s.serverLog(dto.ProtocolType).Result("設定を変更しました", "設定を変更できませんでした", err)
	}()
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(dto.ProtocolType)
	if err != nil {
		return err
	}

	if inst.server != nil && inst.server.Status() == protocol.StatusRunning {
		return fmt.Errorf("cannot update config while server is running")
	}

	// バリアントが変わる場合は新しいサーバーを作成
	if dto.Variant != inst.variant {
		config := inst.factory.CreateConfigFromVariant(dto.Variant)
		server, err := inst.factory.CreateServer(config, inst.dataStore)
		if err != nil {
			return err
		}
		inst.variant = dto.Variant
		inst.config = config
		inst.server = server
	}

	// 設定を更新
	newConfig, err := inst.factory.MapToConfig(inst.variant, dto.Settings)
	if err != nil {
		return err
	}

	if err := inst.server.UpdateConfig(newConfig); err != nil {
		return err
	}

	inst.config = newConfig
	go s.emitServerChanged()
	return nil
}

// === UnitID設定 ===

// GetUnitIDSettings はUnitID設定を返す
func (s *PLCService) GetUnitIDSettings(protocolType string) *UnitIDSettingsDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return nil
	}

	caps := inst.factory.GetProtocolCapabilities()
	if !caps.SupportsUnitID {
		return nil
	}

	var disabledIDs []int
	if us, ok := inst.server.(protocol.UnitIDConfigurable); ok {
		ids := us.GetDisabledUnitIDs()
		disabledIDs = make([]int, len(ids))
		for i, id := range ids {
			disabledIDs[i] = int(id)
		}
	}

	return &UnitIDSettingsDTO{
		Min:         caps.UnitIDMin,
		Max:         caps.UnitIDMax,
		DisabledIDs: disabledIDs,
	}
}

// SetUnitIDEnabled は指定したUnitIdの応答を有効/無効にする
func (s *PLCService) SetUnitIDEnabled(protocolType string, unitId int, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return err
	}

	if us, ok := inst.server.(protocol.UnitIDConfigurable); ok {
		us.SetUnitIdEnabled(uint8(unitId), enabled)
		return nil
	}

	return fmt.Errorf("protocol does not support unit ID")
}

// GetDisabledUnitIDs は無効化されたUnitIDのリストを返す
func (s *PLCService) GetDisabledUnitIDs(protocolType string) []int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return nil
	}

	if us, ok := inst.server.(protocol.UnitIDConfigurable); ok {
		ids := us.GetDisabledUnitIDs()
		result := make([]int, len(ids))
		for i, id := range ids {
			result[i] = int(id)
		}
		return result
	}
	return nil
}

// SetDisabledUnitIDs は無効化するUnitIDのリストを設定する
func (s *PLCService) SetDisabledUnitIDs(protocolType string, ids []int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return err
	}

	if us, ok := inst.server.(protocol.UnitIDConfigurable); ok {
		uint8Ids := make([]uint8, len(ids))
		for i, id := range ids {
			uint8Ids[i] = uint8(id)
		}
		us.SetDisabledUnitIDs(uint8Ids)
		return nil
	}

	return fmt.Errorf("protocol does not support unit ID")
}
