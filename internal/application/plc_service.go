package application

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/script"
	"karakuri/internal/domain/variable"
	"karakuri/internal/infrastructure/adapter"
	plugininfra "karakuri/internal/infrastructure/plugin"
	"karakuri/internal/infrastructure/scripting"
)

// serverInstance はサーバーインスタンスの内部表現
type serverInstance struct {
	protocolType   protocol.ProtocolType
	variant        string
	factory        protocol.ServerFactory
	dataStore      protocol.DataStore
	config         protocol.ProtocolConfig
	server         protocol.ProtocolServer
	changeListener *plugininfra.RemoteVariableChangeListener
	cancelChange   context.CancelFunc
	addedOrder     int // サーバー登録順（表示順の固定化に使用）
}

// PLCService はPLCシミュレーターのメインサービス
type PLCService struct {
	mu sync.RWMutex

	// 登録済みプロトコルファクトリー（protocolType → factory）
	factories map[protocol.ProtocolType]protocol.ServerFactory

	// 中央変数ストア
	variableStore *variable.VariableStore

	// VariableStoreAccessor（AddServer 時にファクトリーへ注入）
	vsAccessor protocol.VariableStoreAccessor

	// 複数サーバーインスタンス（protocolType → instance）
	servers   map[protocol.ProtocolType]*serverInstance
	serverSeq int // AddServer 呼び出しごとに増加する登録順カウンター

	// ホスト側 gRPC サーバー（OPC UA 等のプラグインから変数アクセスに使用）
	hostGrpcServer *plugininfra.HostGrpcServer

	// プラグインプロセスマネージャー
	pluginManager *plugininfra.PluginProcessManager

	// スクリプト
	scriptEngine *scripting.ScriptEngine
	scripts      map[string]*script.Script
	nextOrder    int // スクリプトの表示順を管理するカウンター

	// モニタリング
	monitoringItems map[string]*MonitoringItemDTO
	// モニタリング設定の自動保存（plc_service_monitoring.go）
	monitoringSaveSeq   uint64         // 保存する内容の通し番号（mu で保護）
	monitoringSaveMu    sync.Mutex     // ファイルへの書き込みを 1 つずつにする
	monitoringSavedSeq  uint64         // 書き込み済みの通し番号（monitoringSaveMu で保護）
	monitoringSaveGroup sync.WaitGroup // 実行中の自動保存

	// アプリケーション状態イベント
	appEmitter        AppStateEmitter
	varChangeListener *variableChangeListener
}

// NewPLCService は新しいPLCServiceを作成する
func NewPLCService() *PLCService {
	varStore := variable.NewVariableStore()

	service := &PLCService{
		factories:       make(map[protocol.ProtocolType]protocol.ServerFactory),
		variableStore:   varStore,
		vsAccessor:      adapter.NewVariableStoreAccessor(varStore),
		servers:         make(map[protocol.ProtocolType]*serverInstance),
		scriptEngine:    scripting.NewScriptEngine(varStore),
		scripts:         make(map[string]*script.Script),
		monitoringItems: make(map[string]*MonitoringItemDTO),
	}

	// モニタリング設定を読み込み
	_ = service.LoadMonitoringConfig()

	return service
}

// RegisterPluginFactory はプラグインプロセスから取得したファクトリーを登録する
func (s *PLCService) RegisterPluginFactory(factory protocol.ServerFactory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.factories[factory.ProtocolType()] = factory
}

// StartHostGrpcServer はホスト側 gRPC サーバーを起動してアドレスを返す
func (s *PLCService) StartHostGrpcServer() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	srv := plugininfra.NewHostGrpcServer(s.vsAccessor, s.variableStore)
	port, err := srv.Start()
	if err != nil {
		return "", fmt.Errorf("HostGrpcServer 起動失敗: %w", err)
	}
	s.hostGrpcServer = srv
	return fmt.Sprintf("127.0.0.1:%d", port), nil
}

// GetHostGrpcAddr はホスト側 gRPC サーバーのアドレスを返す
func (s *PLCService) GetHostGrpcAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.hostGrpcServer == nil {
		return ""
	}
	return s.hostGrpcServer.Addr()
}

// InitPlugins はプラグインディレクトリを検索してマニフェストを読み込み、
// LazyRemoteServerFactory を登録する。プロセスはこの時点では起動しない。
func (s *PLCService) InitPlugins(pluginsDir string) error {
	hostAddr := s.GetHostGrpcAddr()

	mgr := plugininfra.NewPluginProcessManager(hostAddr)
	entries, err := mgr.DiscoverManifests(pluginsDir)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.pluginManager = mgr
	s.mu.Unlock()

	for _, entry := range entries {
		factory := plugininfra.NewLazyRemoteServerFactory(entry, mgr)
		s.RegisterPluginFactory(factory)
	}

	return nil
}

// === サーバーインスタンス管理 ===

// GetServerInstances はサーバーインスタンス一覧を返す
func (s *PLCService) GetServerInstances() []ServerInstanceDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ServerInstanceDTO, 0, len(s.servers))
	for _, inst := range s.servers {
		status := "Stopped"
		if inst.server != nil {
			status = inst.server.Status().String()
		}
		caps := inst.factory.GetProtocolCapabilities()
		result = append(result, ServerInstanceDTO{
			ProtocolType:           string(inst.protocolType),
			DisplayName:            inst.factory.DisplayName(),
			Variant:                inst.variant,
			Status:                 status,
			SupportsNodePublishing: caps.SupportsNodePublishing,
		})
	}

	// プロトコルタイプ順にソート（安定した順序）
	sort.Slice(result, func(i, j int) bool {
		return result[i].ProtocolType < result[j].ProtocolType
	})

	return result
}

// AddServer は新しいサーバーインスタンスを追加する
func (s *PLCService) AddServer(protocolType string, variantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pt := protocol.ProtocolType(protocolType)

	// 既に存在する場合はエラー
	if _, exists := s.servers[pt]; exists {
		return fmt.Errorf("server already exists for protocol: %s", protocolType)
	}

	// ファクトリーを取得
	factory, ok := s.factories[pt]
	if !ok {
		return fmt.Errorf("protocol not found: %s", protocolType)
	}

	// プラグインプロセスをオンデマンドで起動（LazyRemoteServerFactory の場合）
	if starter, ok := factory.(pluginStarter); ok {
		if err := starter.EnsureStarted(); err != nil {
			return fmt.Errorf("プラグイン起動失敗: %w", err)
		}
	}

	// VariableStoreAccessor を注入（NodePublishing 対応プロトコル向け）
	if injector, ok := factory.(protocol.VariableStoreInjector); ok {
		injector.InjectVariableStore(s.vsAccessor)
	}

	// サーバーインスタンスを作成（DataStore・サーバー・変数との同期）
	inst := &serverInstance{
		protocolType: pt,
		variant:      variantID,
		factory:      factory,
		config:       factory.CreateConfigFromVariant(variantID),
	}
	if err := s.attachServer(inst); err != nil {
		return err
	}

	s.serverSeq++
	inst.addedOrder = s.serverSeq
	s.servers[pt] = inst

	go s.emitServerChanged()

	return nil
}

// RemoveServer はサーバーインスタンスを削除する
func (s *PLCService) RemoveServer(protocolType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pt := protocol.ProtocolType(protocolType)
	inst, exists := s.servers[pt]
	if !exists {
		return fmt.Errorf("server not found for protocol: %s", protocolType)
	}

	if inst.server != nil {
		if err := inst.server.Stop(); err != nil {
			if checker, ok := inst.factory.(pluginCrashChecker); ok && checker.IsProcessCrashed() {
				// プラグインプロセスがクラッシュしている場合、gRPC Stop は失敗する可能性がある。
				// その場合でも後続のプロセスクリーンアップを続行する。
			} else {
				return fmt.Errorf("failed to stop server: %w", err)
			}
		}
	}

	// 変数と DataStore の同期を止める
	inst.detachSync()

	if stopper, ok := inst.factory.(pluginStopper); ok {
		stopper.StopProcess()
	}

	delete(s.servers, pt)

	go s.emitServerChanged()

	return nil
}

// getServerInstance は指定プロトコルのサーバーインスタンスを取得する（ロック済み前提）
func (s *PLCService) getServerInstance(protocolType string) (*serverInstance, error) {
	pt := protocol.ProtocolType(protocolType)
	inst, exists := s.servers[pt]
	if !exists {
		return nil, fmt.Errorf("server not found for protocol: %s", protocolType)
	}
	return inst, nil
}

// Shutdown はサービスをシャットダウンする
func (s *PLCService) Shutdown() {
	// 実行中のモニタリング設定の保存を終わらせる
	s.monitoringSaveGroup.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.scriptEngine != nil {
		s.scriptEngine.StopAll()
	}
	for _, inst := range s.servers {
		inst.detachSync()
		if inst.server != nil {
			inst.server.Stop()
		}
	}
	if s.hostGrpcServer != nil {
		s.hostGrpcServer.Stop()
	}
	if s.pluginManager != nil {
		s.pluginManager.Shutdown()
	}
}

// SetAppStateEmitter はアプリケーション状態イベントエミッターを設定する
func (s *PLCService) SetAppStateEmitter(emitter AppStateEmitter) {
	s.mu.Lock()

	// 既存のリスナーを解除
	if s.varChangeListener != nil {
		s.variableStore.RemoveListener(s.varChangeListener)
	}

	s.appEmitter = emitter

	// 変数ストアの変更をデバウンスしてUIへプッシュするリスナーを登録（300ms デバウンス）
	listener := newVariableChangeListener(s.emitVariablesChanged, 300*time.Millisecond)
	s.varChangeListener = listener
	s.variableStore.AddListener(listener)

	s.mu.Unlock()
}

// emitServerChanged はサーバー状態変化イベントを発行する（ロック不要・内部で取得）
func (s *PLCService) emitServerChanged() {
	s.mu.RLock()
	emitter := s.appEmitter
	s.mu.RUnlock()
	if emitter == nil {
		return
	}
	instances := s.GetServerInstances()
	protocols := s.GetAvailableProtocols()
	emitter.EmitServerChanged(instances, protocols)
}

// emitVariablesChanged は変数一覧変化イベントを発行する（ロック不要・内部で取得）
func (s *PLCService) emitVariablesChanged() {
	s.mu.RLock()
	emitter := s.appEmitter
	s.mu.RUnlock()
	if emitter == nil {
		return
	}
	variables := s.GetVariables()
	emitter.EmitVariablesChanged(variables)
}

// emitScriptsChanged はスクリプト一覧変化イベントを発行する（ロック不要・内部で取得）
func (s *PLCService) emitScriptsChanged() {
	s.mu.RLock()
	emitter := s.appEmitter
	s.mu.RUnlock()
	if emitter == nil {
		return
	}
	scripts := s.GetScripts()
	emitter.EmitScriptsChanged(scripts)
}
