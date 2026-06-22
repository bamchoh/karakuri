package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"google.golang.org/grpc"

	pb "modbus_simulator/pb/pluginpb"

	"modbus_simulator/internal/domain/protocol"
)

// LazyRemoteServerFactory は plugin.json から読み込んだマニフェストをもとに
// プロトコル情報を返し、gRPC が必要な処理ではプラグインプロセスをオンデマンドで起動する。
type LazyRemoteServerFactory struct {
	manifest    *PluginManifest
	manifestDir string
	manager     *PluginProcessManager

	mu     sync.Mutex
	proc   *PluginProcess // nil = 未起動
	client pb.PluginServiceClient
	conn   *grpc.ClientConn
}

// NewLazyRemoteServerFactory は LazyRemoteServerFactory を作成する。
func NewLazyRemoteServerFactory(entry *PluginManifestEntry, manager *PluginProcessManager) *LazyRemoteServerFactory {
	return &LazyRemoteServerFactory{
		manifest:    entry.Manifest,
		manifestDir: entry.Dir,
		manager:     manager,
	}
}

// ---- マニフェストから返すメソッド（gRPC 不要） ----

func (f *LazyRemoteServerFactory) Version() string { return "1.0.0" }

func (f *LazyRemoteServerFactory) ProtocolType() protocol.ProtocolType {
	return protocol.ProtocolType(f.manifest.ProtocolType)
}

func (f *LazyRemoteServerFactory) DisplayName() string {
	return f.manifest.DisplayName
}

func (f *LazyRemoteServerFactory) ConfigVariants() []protocol.ConfigVariant {
	variants := make([]protocol.ConfigVariant, len(f.manifest.Variants))
	for i, v := range f.manifest.Variants {
		variants[i] = protocol.ConfigVariant{ID: v.ID, DisplayName: v.DisplayName}
	}
	return variants
}

func (f *LazyRemoteServerFactory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	c := f.manifest.Capabilities
	return protocol.ProtocolCapabilities{
		SupportsUnitID:         c.SupportsUnitID,
		UnitIDMin:              c.UnitIDMin,
		UnitIDMax:              c.UnitIDMax,
		SupportsNodePublishing: c.SupportsNodePublishing,
	}
}

// ---- プロセス管理 ----

// EnsureStarted はプラグインプロセスが起動していなければ起動する。
// 既に起動済みかつクラッシュしていなければ何もしない。
// plugin.json に debug_port が指定されている場合は新規プロセスを起動せず、
// その port で既に動いているプロセスに接続する（デバッグ用）。
func (f *LazyRemoteServerFactory) EnsureStarted() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.proc != nil && !f.proc.IsCrashed() {
		return nil
	}

	var (
		proc *PluginProcess
		err  error
	)

	if f.manifest.DebugPort > 0 {
		fmt.Fprintf(os.Stderr, "[DEBUG] debug_port=%d が指定されているため既存プロセスへの接続を試みます (%s)\n", f.manifest.DebugPort, f.manifest.Name)
		proc, err = f.manager.ConnectToExisting(f.manifest.DebugPort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[DEBUG] 既存プロセスへの接続失敗 (port=%d, err=%v)。通常起動にフォールバックします\n", f.manifest.DebugPort, err)
		}
	}

	if proc == nil {
		entrypoint := filepath.Join(f.manifestDir, f.manifest.Entrypoint)
		fmt.Println("Starting plugin process:", entrypoint)
		proc, err = f.manager.Launch(entrypoint, "--protocol-type="+f.manifest.ProtocolType)
		if err != nil {
			return fmt.Errorf("プラグイン起動失敗 (%s): %w", f.manifest.Name, err)
		}
	}

	f.proc = proc
	f.conn = proc.conn
	f.client = pb.NewPluginServiceClient(proc.conn)
	return nil
}

// StopProcess はプラグインプロセスを停止してリセットする。
// RemoveServer 時に呼び出す。次回 EnsureStarted() で再起動可能になる。
func (f *LazyRemoteServerFactory) StopProcess() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.proc != nil {
		f.manager.RemovePlugin(f.proc)
		f.proc = nil
		f.conn = nil
		f.client = nil
	}
}

// ForceReconnect は現在の接続をリセットして再接続する。
// debug_port が設定されている場合は既存プロセスへの再接続を試みる。
// debug_port が設定されていない場合は通常のプロセス起動にフォールバックする。
// プラグインプロセスが再起動した後に StartServer が失敗した場合に使用する。
func (f *LazyRemoteServerFactory) ForceReconnect() error {
	f.mu.Lock()
	if f.conn != nil {
		_ = f.conn.Close()
	}
	f.proc = nil
	f.conn = nil
	f.client = nil
	f.mu.Unlock()

	return f.EnsureStarted()
}

// ---- gRPC を使うメソッド ----

func (f *LazyRemoteServerFactory) DefaultConfig() protocol.ProtocolConfig {
	variants := f.ConfigVariants()
	if len(variants) == 0 {
		return &remoteProtocolConfig{protocolType: f.ProtocolType(), variantID: ""}
	}
	return f.CreateConfigFromVariant(variants[0].ID)
}

func (f *LazyRemoteServerFactory) CreateConfigFromVariant(variantID string) protocol.ProtocolConfig {
	if err := f.EnsureStarted(); err != nil {
		return &remoteProtocolConfig{protocolType: f.ProtocolType(), variantID: variantID, settingsJSON: "{}"}
	}
	resp, err := f.client.GetDefaultConfig(backgroundCtx(), &pb.GetDefaultConfigRequest{VariantId: variantID})
	if err != nil {
		return &remoteProtocolConfig{protocolType: f.ProtocolType(), variantID: variantID, settingsJSON: "{}"}
	}
	return &remoteProtocolConfig{
		protocolType: f.ProtocolType(),
		variantID:    resp.VariantId,
		settingsJSON: resp.SettingsJson,
	}
}

func (f *LazyRemoteServerFactory) GetConfigFields(variantID string) []protocol.ConfigField {
	if err := f.EnsureStarted(); err != nil {
		return nil
	}
	resp, err := f.client.GetConfigFields(backgroundCtx(), &pb.GetConfigFieldsRequest{VariantId: variantID})
	if err != nil {
		return nil
	}
	fields := make([]protocol.ConfigField, len(resp.Fields))
	for i, pbf := range resp.Fields {
		fields[i] = pbConfigFieldToProtocol(pbf)
	}
	return fields
}

func (f *LazyRemoteServerFactory) CreateDataStore() protocol.DataStore {
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	return NewRemoteDataStore(pb.NewDataStoreServiceClient(conn))
}

func (f *LazyRemoteServerFactory) CreateServer(config protocol.ProtocolConfig, store protocol.DataStore) (protocol.ProtocolServer, error) {
	f.mu.Lock()
	client := f.client
	conn := f.conn
	f.mu.Unlock()
	return NewRemoteProtocolServer(client, conn, config), nil
}

func (f *LazyRemoteServerFactory) ConfigToMap(config protocol.ProtocolConfig) map[string]interface{} {
	rc, ok := config.(*remoteProtocolConfig)
	if !ok {
		return map[string]interface{}{}
	}
	if err := f.EnsureStarted(); err != nil {
		return map[string]interface{}{}
	}
	resp, err := f.client.ConfigToMap(backgroundCtx(), &pb.ConfigToMapRequest{
		VariantId:    rc.variantID,
		SettingsJson: rc.settingsJSON,
	})
	if err != nil {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(resp.SettingsJson), &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

func (f *LazyRemoteServerFactory) MapToConfig(variantID string, settings map[string]interface{}) (protocol.ProtocolConfig, error) {
	if err := f.EnsureStarted(); err != nil {
		return nil, err
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("設定の JSON 変換に失敗: %w", err)
	}
	resp, err := f.client.MapToConfig(backgroundCtx(), &pb.MapToConfigRequest{
		VariantId:    variantID,
		SettingsJson: string(settingsJSON),
	})
	if err != nil {
		return nil, fmt.Errorf("MapToConfig RPC 失敗: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("設定検証エラー: %s", resp.Error)
	}
	return &remoteProtocolConfig{
		protocolType: f.ProtocolType(),
		variantID:    resp.VariantId,
		settingsJSON: resp.SettingsJson,
	}, nil
}

// pbConfigFieldToProtocol は protobuf の ConfigField を domain の ConfigField に変換する
func pbConfigFieldToProtocol(pbf *pb.ConfigField) protocol.ConfigField {
	field := protocol.ConfigField{
		Name:        pbf.Name,
		Label:       pbf.Label,
		Description: pbf.Description,
		Type:        pbf.Type,
		Required:    pbf.Required,
		Options:     make([]protocol.FieldOption, len(pbf.Options)),
	}
	if pbf.DefaultJson != "" {
		var def interface{}
		if err := json.Unmarshal([]byte(pbf.DefaultJson), &def); err == nil {
			field.Default = def
		}
	}
	for j, opt := range pbf.Options {
		field.Options[j] = protocol.FieldOption{Value: opt.Value, Label: opt.Label}
	}
	if pbf.HasMin {
		v := int(pbf.Min)
		field.Min = &v
	}
	if pbf.HasMax {
		v := int(pbf.Max)
		field.Max = &v
	}
	if pbf.Condition != nil {
		field.Condition = &protocol.FieldCondition{
			Field: pbf.Condition.Field,
			Value: pbf.Condition.Value,
		}
	}
	field.Category = pbf.Category
	return field
}
