package application

import (
	"karakuri/internal/infrastructure/adapter"
)

// attachServer は inst の factory / config から DataStore とサーバーを作り、inst に設定する（mu をロック済みで呼ぶ）。
// サーバーは起動しない。
//
// DataStore は VariableBackedDataStore で包み、変数と双方向に同期する:
//   - 変数が変わると、マッピング先のレジスタに書く
//   - レジスタに書かれると（プロトコルのクライアント・画面・スクリプトのどれからでも）、マッピング元の変数に反映する
//
// DataStore とサーバーは、サーバーを削除するまで同じものを使い続ける（起動・停止では作り直さない）。
func (s *PLCService) attachServer(inst *serverInstance) error {
	dataStore := adapter.NewVariableBackedDataStore(inst.factory.CreateDataStore(), s.variableStore, string(inst.protocolType))

	server, err := inst.factory.CreateServer(inst.config, dataStore)
	if err != nil {
		dataStore.Detach()
		return err
	}

	inst.dataStore = dataStore
	inst.server = server
	return nil
}

// detachSync は変数と DataStore の同期を止める（サーバーは止めない）
func (inst *serverInstance) detachSync() {
	if ds, ok := inst.dataStore.(*adapter.VariableBackedDataStore); ok {
		ds.Detach()
	}
}
