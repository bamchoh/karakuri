package application

import (
	"context"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/infrastructure/adapter"
	plugininfra "karakuri/internal/infrastructure/plugin"
)

// プラグインのプロセスを操作できるファクトリー用のインターフェース（LazyRemoteServerFactory が実装する）。
// テスト用のインプロセスのファクトリーは実装しないため、型アサーションで確かめて使う。
type (
	// pluginStarter はプラグインのプロセスをオンデマンドで起動できる
	pluginStarter interface{ EnsureStarted() error }
	// pluginStopper はプラグインのプロセスを停止できる
	pluginStopper interface{ StopProcess() }
	// pluginCrashChecker はプラグインのプロセスが異常終了したかを調べられる
	pluginCrashChecker interface{ IsProcessCrashed() bool }
	// pluginRunningChecker はプラグインのプロセスが動いているかを調べられる
	pluginRunningChecker interface{ IsProcessRunning() bool }
	// pluginReconnector はプラグインへの接続を張り直せる（debug_port などで起動したプラグイン向け）
	pluginReconnector interface{ ForceReconnect() error }
)

// hostAddrSetter はホストの gRPC サーバーのアドレスを受け取るサーバー（ノード公開型プロトコル向け）
type hostAddrSetter interface{ SetHostGrpcAddr(addr string) }

// attachServer は inst の factory / config から DataStore とサーバーを作り、inst に設定する（mu をロック済みで呼ぶ）。
// 変数と DataStore の双方向同期もここで始める。サーバーは起動しない。
//   - RemoteDataStore（プラグインプロセス）: RemoteVariableChangeListener で同期する
//   - インプロセスの DataStore（テスト用）: VariableBackedDataStore で包んで同期する
func (s *PLCService) attachServer(inst *serverInstance) error {
	protocolType := string(inst.protocolType)
	innerDataStore := inst.factory.CreateDataStore()

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

	server, err := inst.factory.CreateServer(inst.config, dataStore)
	if err != nil {
		// 作りかけの同期を止める
		(&serverInstance{dataStore: dataStore, changeListener: changeListener, cancelChange: cancelChange}).detachSync()
		return err
	}

	// ホストの gRPC アドレスを渡す（ノード公開型プロトコルが変数にアクセスするため）
	if s.hostGrpcServer != nil {
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

// detachSync は変数と DataStore の同期を止める（サーバーは止めない）
func (inst *serverInstance) detachSync() {
	if inst.cancelChange != nil {
		inst.cancelChange()
		inst.cancelChange = nil
	}
	if inst.changeListener != nil {
		inst.changeListener.Detach()
		inst.changeListener = nil
	}
	if ds, ok := inst.dataStore.(*adapter.VariableBackedDataStore); ok {
		ds.Detach()
	}
}
