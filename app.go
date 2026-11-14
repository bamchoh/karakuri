package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"

	"karakuri/internal/application"
	"karakuri/internal/infrastructure/appconfig"
	"karakuri/internal/infrastructure/httpapi"
	"karakuri/internal/protocols"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"go.bug.st/serial"
)

const defaultHTTPAPIPort = 8765

// アプリ全体のログの出どころ
const (
	logSourceSystem  = "システム"
	logSourceHTTPAPI = "HTTP API"
	logSourceProject = "プロジェクト"
)

// App struct
type App struct {
	ctx         context.Context
	plcService  *application.PLCService
	httpAPI     *httpapi.Server
	httpAPIPort int
	// 最後に開いた（保存した）プロジェクトのファイル。保存のダイアログの最初の場所と名前に使う
	projectPath string
}

// NewApp creates a new App application struct
func NewApp() *App {
	svc := application.NewPLCService()
	port := loadHTTPAPIPort()
	return &App{
		plcService:  svc,
		httpAPI:     httpapi.NewServer(svc, port),
		httpAPIPort: port,
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// アプリケーション状態イベントエミッターを設定
	appEmitter := application.NewWailsAppStateEmitter(ctx)
	a.plcService.SetAppStateEmitter(appEmitter)

	// コンソールログプッシュ通知を設定
	a.plcService.SetConsoleLogCallback(func(entry application.ConsoleLogDTO) {
		appEmitter.EmitConsoleLogAdded(entry)
	})

	// アプリ全体のログを、画面に知らせる。標準のログ（プロトコルの実装やライブラリが出すもの）も取り込む
	// ログは、ロックを持ったまま追加されることがあるので、画面への通知は別のゴルーチンから順番に送る
	// entry が nil なら「すべて消した」の知らせ
	logEvents := make(chan *application.AppLogDTO, 1024)
	go func() {
		for entry := range logEvents {
			if entry == nil {
				appEmitter.EmitAppLogCleared()
			} else {
				appEmitter.EmitAppLogAdded(*entry)
			}
		}
	}()
	a.plcService.SetAppLogCallbacks(func(entry application.AppLogDTO) {
		select {
		case logEvents <- &entry:
		default: // あふれた分は通知しない（ログ自体は残っていて、画面を開き直すと読める）
		}
	}, func() {
		logEvents <- nil
	})
	log.SetOutput(io.MultiWriter(os.Stderr, a.plcService.AppLog().Writer(logSourceSystem)))

	// 組み込みのプロトコルを登録する
	for _, factory := range protocols.Factories() {
		a.plcService.RegisterFactory(factory)
	}
	for _, factory := range protocols.ClientFactories() {
		a.plcService.RegisterClientFactory(factory)
	}

	// REST HTTP API サーバーを起動
	if err := a.httpAPI.Start(); err != nil {
		a.plcService.AppLog().Errorf(logSourceHTTPAPI, "HTTP API サーバーの起動に失敗しました: %v", err)
	} else {
		a.plcService.AppLog().Infof(logSourceHTTPAPI, "HTTP API サーバーを起動しました: http://localhost:%d/api", a.httpAPIPort)
	}
}

// shutdown is called when the app closes
func (a *App) shutdown(ctx context.Context) {
	a.httpAPI.Shutdown(ctx) //nolint:errcheck
	a.plcService.Shutdown()
}

// === HTTP API 設定 ===

// GetHTTPAPIPort は現在のHTTP APIポート番号を返す
func (a *App) GetHTTPAPIPort() int {
	return a.httpAPIPort
}

// SetHTTPAPIPort はHTTP APIポートを変更してサーバーを再起動する
func (a *App) SetHTTPAPIPort(port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("ポート番号は1024〜65535の範囲で指定してください")
	}
	if err := saveHTTPAPIPort(port); err != nil {
		return err
	}
	if err := a.httpAPI.Restart(port); err != nil {
		err = fmt.Errorf("ポート %d でHTTP APIサーバーを起動できませんでした: %w", port, err)
		a.plcService.AppLog().Errorf(logSourceHTTPAPI, "%v", err)
		return err
	}
	a.httpAPIPort = port
	a.plcService.AppLog().Infof(logSourceHTTPAPI, "ポートを %d に変更しました: http://localhost:%d/api", port, port)
	return nil
}

// --- HTTP API 設定ファイルヘルパー ---

func httpAPIConfigPath() (string, error) {
	return appconfig.Path("httpapi_config.json")
}

func loadHTTPAPIPort() int {
	path, err := httpAPIConfigPath()
	if err != nil {
		return defaultHTTPAPIPort
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultHTTPAPIPort
	}
	var cfg struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil || cfg.Port == 0 {
		return defaultHTTPAPIPort
	}
	return cfg.Port
}

func saveHTTPAPIPort(port int) error {
	path, err := httpAPIConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Port int `json:"port"`
	}{Port: port}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// === サーバーインスタンス管理 ===

// GetServerInstances はサーバーインスタンス一覧を返す
func (a *App) GetServerInstances() []application.ServerInstanceDTO {
	return a.plcService.GetServerInstances()
}

// AddServer は新しいサーバーインスタンスを追加する
func (a *App) AddServer(protocolType string, variantID string) error {
	return a.plcService.AddServer(protocolType, variantID)
}

// RemoveServer はサーバーインスタンスを削除する
func (a *App) RemoveServer(protocolType string) error {
	return a.plcService.RemoveServer(protocolType)
}

// === サーバー管理 ===

// StartServer はサーバーを起動する
func (a *App) StartServer(protocolType string) error {
	return a.plcService.StartServer(protocolType)
}

// StopServer はサーバーを停止する
func (a *App) StopServer(protocolType string) error {
	return a.plcService.StopServer(protocolType)
}

// GetServerStatus はサーバーのステータスを返す
func (a *App) GetServerStatus(protocolType string) string {
	return a.plcService.GetServerStatus(protocolType)
}

// GetNodeAddresses は公開変数のノードアドレス（OPC UA なら NodeID）を返す
func (a *App) GetNodeAddresses(protocolType string, variableIDs []string) []application.NodeAddressDTO {
	return a.plcService.GetNodeAddresses(protocolType, variableIDs)
}

// === プロトコル管理API ===

// GetAvailableProtocols は利用可能なプロトコル一覧を返す
func (a *App) GetAvailableProtocols() []application.ProtocolInfoDTO {
	return a.plcService.GetAvailableProtocols()
}

// GetProtocolSchema はプロトコルスキーマを返す
func (a *App) GetProtocolSchema(protocolType string) (*application.ProtocolSchemaDTO, error) {
	return a.plcService.GetProtocolSchema(protocolType)
}

// GetServerConfig は指定サーバーの現在の設定を返す
func (a *App) GetServerConfig(protocolType string) *application.ServerConfigDTO {
	return a.plcService.GetServerConfig(protocolType)
}

// UpdateServerConfig はサーバーの設定を更新する
func (a *App) UpdateServerConfig(dto *application.ServerConfigDTO) error {
	return a.plcService.UpdateServerConfig(dto)
}

// === UnitID設定API ===

// GetUnitIDSettings はUnitID設定を返す
func (a *App) GetUnitIDSettings(protocolType string) *application.UnitIDSettingsDTO {
	return a.plcService.GetUnitIDSettings(protocolType)
}

// SetUnitIDEnabled は指定したUnitIdの応答を有効/無効にする
func (a *App) SetUnitIDEnabled(protocolType string, unitId int, enabled bool) error {
	return a.plcService.SetUnitIDEnabled(protocolType, unitId, enabled)
}

// GetDisabledUnitIDs は無効化されたUnitIDのリストを返す
func (a *App) GetDisabledUnitIDs(protocolType string) []int {
	return a.plcService.GetDisabledUnitIDs(protocolType)
}

// SetDisabledUnitIDs は無効化するUnitIDのリストを設定する
func (a *App) SetDisabledUnitIDs(protocolType string, ids []int) error {
	return a.plcService.SetDisabledUnitIDs(protocolType, ids)
}

// === 汎用メモリ操作API ===

// GetMemoryAreas は利用可能なメモリエリアの一覧を返す
func (a *App) GetMemoryAreas(protocolType string) []application.MemoryAreaDTO {
	return a.plcService.GetMemoryAreas(protocolType)
}

// ReadBits は指定エリアの複数ビット値を読み込む
func (a *App) ReadBits(protocolType, area string, address, count int) ([]bool, error) {
	return a.plcService.ReadBits(protocolType, area, address, count)
}

// WriteBit は指定エリアのビット値を書き込む
func (a *App) WriteBit(protocolType, area string, address int, value bool) error {
	return a.plcService.WriteBit(protocolType, area, address, value)
}

// ReadWords は指定エリアの複数ワード値を読み込む
func (a *App) ReadWords(protocolType, area string, address, count int) ([]int, error) {
	return a.plcService.ReadWords(protocolType, area, address, count)
}

// WriteWord は指定エリアのワード値を書き込む
func (a *App) WriteWord(protocolType, area string, address int, value int) error {
	return a.plcService.WriteWord(protocolType, area, address, value)
}

// === スクリプト管理 ===

// CreateScript は新しいスクリプトを作成する
func (a *App) CreateScript(name, code string, intervalMs int) (*application.ScriptDTO, error) {
	return a.plcService.CreateScript(name, code, intervalMs)
}

// UpdateScript はスクリプトを更新する
func (a *App) UpdateScript(id, name, code string, intervalMs int) error {
	return a.plcService.UpdateScript(id, name, code, intervalMs)
}

// DeleteScript はスクリプトを削除する
func (a *App) DeleteScript(id string) error {
	return a.plcService.DeleteScript(id)
}

// GetScripts は全てのスクリプトを取得する
func (a *App) GetScripts() []*application.ScriptDTO {
	return a.plcService.GetScripts()
}

// GetScript は特定のスクリプトを取得する
func (a *App) GetScript(id string) (*application.ScriptDTO, error) {
	return a.plcService.GetScript(id)
}

// StartScript はスクリプトを開始する
func (a *App) StartScript(id string) error {
	return a.plcService.StartScript(id)
}

// StopScript はスクリプトを停止する
func (a *App) StopScript(id string) error {
	return a.plcService.StopScript(id)
}

// RunScriptOnce はコードを 1 回だけ実行する（結果とエラーはコンソールにも出る）
func (a *App) RunScriptOnce(code string) (interface{}, error) {
	return a.plcService.RunScriptOnce(code)
}

// GetAppLogs はアプリ全体のログ（サーバーの起動・停止、クライアントの通信エラーなど）を古い順に返す
func (a *App) GetAppLogs() []application.AppLogDTO {
	return a.plcService.GetAppLogs()
}

// ClearAppLogs はアプリ全体のログをすべて消す
func (a *App) ClearAppLogs() {
	a.plcService.ClearAppLogs()
}

// GetConsoleLogs はスクリプトのコンソールログを返す
func (a *App) GetConsoleLogs() []application.ConsoleLogDTO {
	return a.plcService.GetConsoleLogs()
}

// ClearConsoleLogs はコンソールログをクリアする
func (a *App) ClearConsoleLogs() {
	a.plcService.ClearConsoleLogs()
}

// GetIntervalPresets は周期プリセットを取得する
func (a *App) GetIntervalPresets() []application.IntervalPresetDTO {
	return a.plcService.GetIntervalPresets()
}

// === クライアント（外部の機器へつなぐ側） ===

// GetClientProtocols はクライアントとして使えるプロトコル（接続先の設定項目・コマンドの定義）を返す
func (a *App) GetClientProtocols() []application.ClientProtocolDTO {
	return a.plcService.GetClientProtocols()
}

// GetClients は接続先の一覧を返す
func (a *App) GetClients() []application.ClientDTO {
	return a.plcService.GetClients()
}

// AddClient は接続先を追加する（設定は既定値）
func (a *App) AddClient(protocolType string, name string) (*application.ClientDTO, error) {
	return a.plcService.AddClient(protocolType, name, nil)
}

// UpdateClient は接続先の名前と設定を変える
func (a *App) UpdateClient(id string, name string, settings map[string]interface{}) error {
	return a.plcService.UpdateClient(id, name, settings)
}

// RemoveClient は接続先を削除する
func (a *App) RemoveClient(id string) error {
	return a.plcService.RemoveClient(id)
}

// DisconnectClient は接続先との接続を閉じる
func (a *App) DisconnectClient(id string) error {
	return a.plcService.DisconnectClient(id)
}

// BuildClientRequest はコマンドと項目の値からリクエストを組み立て、16 進の文字列で返す
func (a *App) BuildClientRequest(protocolType string, commandID string, params map[string]interface{}) (string, error) {
	return a.plcService.BuildClientRequest(protocolType, commandID, params)
}

// SendClientRequest は 16 進で指定したリクエストを接続先に送り、結果を返す
func (a *App) SendClientRequest(id string, requestHex string) (*application.ClientCommandLogDTO, error) {
	return a.plcService.SendClientRequest(id, requestHex)
}

// GetClientCommandLogs は送ったコマンドの履歴を新しい順に返す
func (a *App) GetClientCommandLogs() []application.ClientCommandLogDTO {
	return a.plcService.GetClientCommandLogs()
}

// ClearClientCommandLogs はコマンドの履歴を消す
func (a *App) ClearClientCommandLogs() {
	a.plcService.ClearClientCommandLogs()
}

// GetClientPolling は接続先のポーリングの状態（周期・読み取り項目と最後の結果）を返す
func (a *App) GetClientPolling(id string) (*application.ClientPollingDTO, error) {
	return a.plcService.GetClientPolling(id)
}

// AddClientPollItem はポーリングの読み取り項目を足す
func (a *App) AddClientPollItem(id string, commandID string, params map[string]interface{}, format string) (*application.ClientPollItemDTO, error) {
	return a.plcService.AddClientPollItem(id, commandID, params, format)
}

// UpdateClientPollItem はポーリングの読み取り項目を変える
func (a *App) UpdateClientPollItem(id string, itemID string, commandID string, params map[string]interface{}, format string) error {
	return a.plcService.UpdateClientPollItem(id, itemID, commandID, params, format)
}

// RemoveClientPollItem はポーリングの読み取り項目を削除する
func (a *App) RemoveClientPollItem(id string, itemID string) error {
	return a.plcService.RemoveClientPollItem(id, itemID)
}

// SetClientPollInterval はポーリングの周期（ms）を変える
func (a *App) SetClientPollInterval(id string, intervalMs int) error {
	return a.plcService.SetClientPollInterval(id, intervalMs)
}

// StartClientPolling はポーリングを始める
func (a *App) StartClientPolling(id string) error {
	return a.plcService.StartClientPolling(id)
}

// StopClientPolling はポーリングを止める
func (a *App) StopClientPolling(id string) error {
	return a.plcService.StopClientPolling(id)
}

// BrowseClient は、接続先の機器が持つアドレス（OPC UA ならノード）のうち、parent の子を返す（parent が空なら、いちばん上）
func (a *App) BrowseClient(id string, parent string) ([]application.ClientBrowseEntryDTO, error) {
	return a.plcService.BrowseClient(id, parent)
}

// GetClientSubscription は接続先のサブスクリプション（値の変化の通知）の状態を返す
func (a *App) GetClientSubscription(id string) (*application.ClientSubscriptionDTO, error) {
	return a.plcService.GetClientSubscription(id)
}

// AddClientSubscriptionItems はサブスクリプションの監視項目を足す（1 回の入力から、いくつも足されることがある）
func (a *App) AddClientSubscriptionItems(id string, params map[string]interface{}) ([]application.ClientSubscriptionItemDTO, error) {
	return a.plcService.AddClientSubscriptionItems(id, params)
}

// RemoveClientSubscriptionItem はサブスクリプションの監視項目を削除する
func (a *App) RemoveClientSubscriptionItem(id string, itemID string) error {
	return a.plcService.RemoveClientSubscriptionItem(id, itemID)
}

// SetClientSubscriptionInterval はサブスクリプションの通知の間隔（ms）を変える
func (a *App) SetClientSubscriptionInterval(id string, intervalMs int) error {
	return a.plcService.SetClientSubscriptionInterval(id, intervalMs)
}

// StartClientSubscription はサブスクリプションの監視を始める
func (a *App) StartClientSubscription(id string) error {
	return a.plcService.StartClientSubscription(id)
}

// StopClientSubscription はサブスクリプションの監視を止める
func (a *App) StopClientSubscription(id string) error {
	return a.plcService.StopClientSubscription(id)
}

// SetClientFailThreshold は、読み取りが続けて何回失敗したら「通信エラー」にするかを変える
func (a *App) SetClientFailThreshold(id string, n int) error {
	return a.plcService.SetClientFailThreshold(id, n)
}

// GetClientSavedCommands は接続先の保存したコマンドを返す
func (a *App) GetClientSavedCommands(id string) ([]application.ClientSavedCommandDTO, error) {
	return a.plcService.GetClientSavedCommands(id)
}

// AddClientSavedCommand はコマンドを保存する
func (a *App) AddClientSavedCommand(id string, in application.ClientSavedCommandInputDTO) (*application.ClientSavedCommandDTO, error) {
	return a.plcService.AddClientSavedCommand(id, in)
}

// UpdateClientSavedCommand は保存したコマンドを置き換える
func (a *App) UpdateClientSavedCommand(id string, commandID string, in application.ClientSavedCommandInputDTO) error {
	return a.plcService.UpdateClientSavedCommand(id, commandID, in)
}

// RenameClientSavedCommand は保存したコマンドの名前を変える
func (a *App) RenameClientSavedCommand(id string, commandID string, name string) error {
	return a.plcService.RenameClientSavedCommand(id, commandID, name)
}

// RemoveClientSavedCommand は保存したコマンドを削除する
func (a *App) RemoveClientSavedCommand(id string, commandID string) error {
	return a.plcService.RemoveClientSavedCommand(id, commandID)
}

// SendClientSavedCommand は保存したコマンドを送る
func (a *App) SendClientSavedCommand(id string, commandID string) (*application.ClientCommandLogDTO, error) {
	return a.plcService.SendClientSavedCommand(id, commandID)
}

// GetMappingTargets は変数をマッピングできる先（サーバーと、接続先）を返す
func (a *App) GetMappingTargets() []application.MappingTargetDTO {
	return a.plcService.GetMappingTargets()
}

// GetVariableDeviceStatuses は、接続先にマッピングした変数の同期の状態を返す
func (a *App) GetVariableDeviceStatuses() []application.VariableDeviceStatusDTO {
	return a.plcService.GetVariableDeviceStatuses()
}

// === プロジェクト管理 ===

// ExportProject はプロジェクトをファイルにエクスポートする
//
// 保存したファイルのパスを返す（ダイアログでやめたときは空）。画面はこれをタイトルバーに出す。
func (a *App) ExportProject() (string, error) {
	// ファイル保存ダイアログを表示（前に開いた・保存したファイルがあれば、そこから始める）
	defaultName, defaultDir := "project.json", ""
	if a.projectPath != "" {
		defaultName, defaultDir = filepath.Base(a.projectPath), filepath.Dir(a.projectPath)
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "プロジェクトをエクスポート",
		DefaultFilename:  defaultName,
		DefaultDirectory: defaultDir,
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON Files (*.json)", Pattern: "*.json"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // キャンセルされた
	}

	// プロジェクトデータを取得
	data := a.plcService.ExportProject()

	// JSONに変換
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}

	// ファイルに書き込み
	err = os.WriteFile(path, jsonData, 0644)
	a.plcService.AppLog().Result(logSourceProject, "プロジェクトを保存しました: "+path, "プロジェクトを保存できませんでした", err)
	if err != nil {
		return "", err
	}
	a.projectPath = path
	return path, nil
}

// ImportProject はファイルからプロジェクトをインポートする
//
// 読み込んだファイルのパスを返す（ダイアログでやめたときは空）。
func (a *App) ImportProject() (string, error) {
	// ファイル選択ダイアログを表示
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "プロジェクトをインポート",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON Files (*.json)", Pattern: "*.json"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // キャンセルされた
	}

	// ファイルを読み込み
	jsonData, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// JSONをパース
	var data application.ProjectDataDTO
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return "", err
	}

	// プロジェクトをインポート
	if err := a.plcService.ImportProject(&data); err != nil {
		return "", err
	}
	a.projectPath = path

	runtime.EventsEmit(a.ctx, "project:imported") // インポート完了イベントをフロントエンドに通知

	return path, nil
}

// === モニタリング管理 ===

// GetMonitoringItems はモニタリング項目一覧を返す
func (a *App) GetMonitoringItems() []*application.MonitoringItemDTO {
	return a.plcService.GetMonitoringItems()
}

// AddMonitoringItem はモニタリング項目を追加する
func (a *App) AddMonitoringItem(item *application.MonitoringItemDTO) (*application.MonitoringItemDTO, error) {
	return a.plcService.AddMonitoringItem(item)
}

// UpdateMonitoringItem はモニタリング項目を更新する
func (a *App) UpdateMonitoringItem(item *application.MonitoringItemDTO) error {
	return a.plcService.UpdateMonitoringItem(item)
}

// DeleteMonitoringItem はモニタリング項目を削除する
func (a *App) DeleteMonitoringItem(id string) error {
	return a.plcService.DeleteMonitoringItem(id)
}

// MoveMonitoringItem はモニタリング項目を移動する
func (a *App) MoveMonitoringItem(id string, direction string) error {
	return a.plcService.MoveMonitoringItem(id, direction)
}

// ReorderMonitoringItem はモニタリング項目を指定したインデックスに移動する
func (a *App) ReorderMonitoringItem(id string, newIndex int) error {
	return a.plcService.ReorderMonitoringItem(id, newIndex)
}

// ClearMonitoringItems は全モニタリング項目を削除する
func (a *App) ClearMonitoringItems() {
	a.plcService.ClearMonitoringItems()
}

// === シリアルポート ===

// GetSerialPorts はシステムで利用可能なシリアルポートの一覧を返す
func (a *App) GetSerialPorts() []string {
	ports, err := serial.GetPortsList()
	if err != nil {
		return []string{}
	}
	sort.Strings(ports)
	return ports
}

// === 変数管理 ===

// GetVariables はすべての変数を返す
func (a *App) GetVariables() []*application.VariableDTO {
	return a.plcService.GetVariables()
}

// CreateVariable は新しい変数を作成する
func (a *App) CreateVariable(name, dataType string, value interface{}) (*application.VariableDTO, error) {
	return a.plcService.CreateVariable(name, dataType, value)
}

// UpdateVariableValue は変数の値を更新する
func (a *App) UpdateVariableValue(id string, value interface{}) error {
	return a.plcService.UpdateVariableValue(id, value)
}

// UpdateVariableFieldValue は変数の特定フィールド/要素のみを更新する。
// fieldPath は外部インデックス（表示ベース）のパス文字列（例: "motor.speed", "items[1]"）
func (a *App) UpdateVariableFieldValue(id, fieldPath string, value interface{}) error {
	return a.plcService.UpdateVariableFieldValue(id, fieldPath, value)
}

// DeleteVariable は変数を削除する
func (a *App) DeleteVariable(id string) error {
	return a.plcService.DeleteVariable(id)
}

// UpdateVariable は変数の名前とデータタイプを更新する
func (a *App) UpdateVariable(id, name, dataType string) (*application.VariableDTO, error) {
	return a.plcService.UpdateVariable(id, name, dataType)
}

// GetDataTypes はデータ型一覧を返す
func (a *App) GetDataTypes() *application.DataTypesDTO {
	return a.plcService.GetDataTypes()
}

// GetVariableMappings は変数のマッピングを返す
func (a *App) GetVariableMappings(id string) ([]application.ProtocolMappingDTO, error) {
	return a.plcService.GetVariableMappings(id)
}

// UpdateVariableMappings は変数のマッピングを更新する
func (a *App) UpdateVariableMappings(id string, mappings []application.ProtocolMappingDTO) error {
	return a.plcService.UpdateVariableMappings(id, mappings)
}

// UpdateVariableNodePublishing は変数のプロトコル公開設定を更新する
func (a *App) UpdateVariableNodePublishing(variableID, protocolType string, dto *application.NodePublishingDTO) error {
	return a.plcService.UpdateVariableNodePublishing(variableID, protocolType, dto)
}

// === 構造体型管理 ===

// RegisterStructType は構造体型を登録する
func (a *App) RegisterStructType(dto application.StructTypeDTO) (*application.StructTypeDTO, error) {
	return a.plcService.RegisterStructType(dto)
}

// GetStructTypes は全構造体型を返す
func (a *App) GetStructTypes() []application.StructTypeDTO {
	return a.plcService.GetStructTypes()
}

// DeleteStructType は構造体型を削除する
func (a *App) DeleteStructType(name string) error {
	return a.plcService.DeleteStructType(name)
}
