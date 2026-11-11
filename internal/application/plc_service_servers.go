package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/infrastructure/adapter"
	plugininfra "karakuri/internal/infrastructure/plugin"
)

// === サーバー管理 ===

// StartServer はサーバーを起動する
func (s *PLCService) StartServer(protocolType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return err
	}
	if inst.server == nil {
		return fmt.Errorf("server not initialized")
	}

	startErr := inst.server.Start(context.Background())
	if startErr == nil {
		go s.emitServerChanged()
		return nil
	}

	// Start 失敗時: 再接続をサポートするファクトリー（debug_port 等）の場合は
	// 接続をリセットしてサーバーインスタンスを再構築してからリトライする
	type pluginReconnector interface{ ForceReconnect() error }
	reconnector, ok := inst.factory.(pluginReconnector)
	if !ok {
		return fmt.Errorf("サーバーの起動に失敗しました: %w", startErr)
	}

	fmt.Fprintf(os.Stderr, "[INFO] StartServer 失敗。再接続を試みます (protocol=%s)\n", protocolType)
	if rerr := reconnector.ForceReconnect(); rerr != nil {
		return fmt.Errorf("再接続失敗 (start=%v, reconnect=%w)", startErr, rerr)
	}

	if rerr := s.rebuildServerInstance(inst); rerr != nil {
		return fmt.Errorf("サーバー再構築失敗: %w", rerr)
	}

	if err := inst.server.Start(context.Background()); err != nil {
		return err
	}
	go s.emitServerChanged()
	return nil
}

// rebuildServerInstance は既存の serverInstance の DataStore・Server を再作成する。
// プラグイン再接続後に呼び出す（ロック済み前提）。
func (s *PLCService) rebuildServerInstance(inst *serverInstance) error {
	// 古いリスナーをクリーンアップ
	if inst.cancelChange != nil {
		inst.cancelChange()
		inst.cancelChange = nil
	}
	if inst.changeListener != nil {
		inst.changeListener.Detach()
		inst.changeListener = nil
	}

	factory := inst.factory
	protocolType := string(inst.protocolType)

	innerDataStore := factory.CreateDataStore()

	var dataStore protocol.DataStore
	var changeListener *plugininfra.RemoteVariableChangeListener
	var cancelChange context.CancelFunc

	if remoteDS, isRemote := innerDataStore.(*plugininfra.RemoteDataStore); isRemote {
		dataStore = innerDataStore
		changeListener = plugininfra.NewRemoteVariableChangeListener(remoteDS, s.variableStore, protocolType)
		ctx, cancel := context.WithCancel(context.Background())
		cancelChange = cancel
		go changeListener.StartChangeSubscription(ctx)
	} else {
		dataStore = adapter.NewVariableBackedDataStore(innerDataStore, s.variableStore, protocolType)
	}

	server, err := factory.CreateServer(inst.config, dataStore)
	if err != nil {
		return err
	}

	if s.hostGrpcServer != nil {
		type hostAddrSetter interface{ SetHostGrpcAddr(addr string) }
		if setter, ok := server.(hostAddrSetter); ok {
			setter.SetHostGrpcAddr(s.hostGrpcServer.Addr())
		}
	}

	inst.dataStore = dataStore
	inst.server = server
	inst.changeListener = changeListener
	inst.cancelChange = cancelChange

	if s.eventEmitter != nil {
		s.setEmitterToServerInstance(inst)
	}

	return nil
}

// StopServer はサーバーを停止する
func (s *PLCService) StopServer(protocolType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, err := s.getServerInstance(protocolType)
	if err != nil {
		return err
	}
	if inst.server == nil {
		return nil
	}

	type pluginStopper interface{ StopProcess() }
	type pluginCrashChecker interface{ IsProcessCrashed() bool }
	if checker, ok := inst.factory.(pluginCrashChecker); ok && checker.IsProcessCrashed() {
		if stopper, ok2 := inst.factory.(pluginStopper); ok2 {
			stopper.StopProcess()
		}
		go s.emitServerChanged()
		return nil
	}

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
// アドレスの書式と、公開できない変数の判定はプロトコルのプラグインが決める。
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

// GetAvailableProtocols は利用可能なプロトコル一覧を返す
func (s *PLCService) GetAvailableProtocols() []ProtocolInfoDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ProtocolInfoDTO, 0, len(s.factories))
	for _, factory := range s.factories {
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
	sort.Slice(result, func(i, j int) bool {
		return result[i].Type < result[j].Type
	})
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
		fields := factory.GetConfigFields(v.ID)
		fieldDTOs := make([]FieldDTO, len(fields))
		for j, f := range fields {
			fieldDTOs[j] = FieldDTO{
				Name:        f.Name,
				Label:       f.Label,
				Description: f.Description,
				Type:        f.Type,
				Required:    f.Required,
				Default:     f.Default,
				Min:         f.Min,
				Max:         f.Max,
				Category:    f.Category,
			}
			if f.Options != nil {
				fieldDTOs[j].Options = make([]OptionDTO, len(f.Options))
				for k, o := range f.Options {
					fieldDTOs[j].Options[k] = OptionDTO{Value: o.Value, Label: o.Label}
				}
			}
			if f.Condition != nil {
				fieldDTOs[j].ShowWhen = &ConditionDTO{Field: f.Condition.Field, Value: f.Condition.Value}
			}
		}
		variantDTOs[i] = VariantDTO{
			ID:          v.ID,
			DisplayName: v.DisplayName,
			Fields:      fieldDTOs,
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
func (s *PLCService) UpdateServerConfig(dto *ServerConfigDTO) error {
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

		if s.eventEmitter != nil {
			s.setEmitterToServerInstance(inst)
		}
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
