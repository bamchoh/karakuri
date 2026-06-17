package protocol

import (
	"context"
)

// ProtocolType はプロトコルの種類を表す
type ProtocolType string

const (
	ProtocolModbusTCP   ProtocolType = "modbus-tcp"
	ProtocolModbusRTU   ProtocolType = "modbus-rtu"
	ProtocolModbusASCII ProtocolType = "modbus-ascii"
)

// ServerStatus はサーバーの状態を表す
type ServerStatus int

const (
	StatusStopped ServerStatus = iota
	StatusRunning
	StatusError
)

func (s ServerStatus) String() string {
	switch s {
	case StatusStopped:
		return "Stopped"
	case StatusRunning:
		return "Running"
	case StatusError:
		return "Error"
	default:
		return "Unknown"
	}
}

// ProtocolConfig はプロトコル設定の共通インターフェース
type ProtocolConfig interface {
	// ProtocolType はプロトコルの種類を返す
	ProtocolType() ProtocolType
	// Variant はバリアント名を返す（例: "tcp", "rtu"）
	Variant() string
	// Validate は設定を検証する
	Validate() error
	// Clone は設定のコピーを作成する
	Clone() ProtocolConfig
}

// ProtocolServer はプロトコルサーバーの共通インターフェース
type ProtocolServer interface {
	// Start はサーバーを起動する
	Start(ctx context.Context) error
	// Stop はサーバーを停止する
	Stop() error
	// Status はサーバーの状態を返す
	Status() ServerStatus
	// ProtocolType はプロトコルの種類を返す
	ProtocolType() ProtocolType
	// Config は現在の設定を返す
	Config() ProtocolConfig
	// UpdateConfig は設定を更新する（停止中のみ）
	UpdateConfig(config ProtocolConfig) error
}

// ConfigVariant は設定バリアントの情報
type ConfigVariant struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// ServerFactory はプロトコルサーバーを作成するファクトリーインターフェース(プラグイン作者が実装すること)
type ServerFactory interface {
	// ProtocolType はファクトリーが作成するプロトコルの種類を返す
	ProtocolType() ProtocolType
	// DisplayName はプロトコルの表示名を返す
	DisplayName() string
	// CreateServer はサーバーを作成する
	CreateServer(config ProtocolConfig, store DataStore) (ProtocolServer, error)
	// CreateDataStore はプロトコル用のデータストアを作成する
	CreateDataStore() DataStore
	// DefaultConfig はデフォルト設定を返す
	DefaultConfig() ProtocolConfig
	// ConfigVariants は利用可能な設定バリアントを返す
	ConfigVariants() []ConfigVariant
	// CreateConfigFromVariant は指定バリアントの設定を作成する
	CreateConfigFromVariant(variantID string) ProtocolConfig
	// GetConfigFields は指定バリアントの設定フィールドを返す
	GetConfigFields(variantID string) []ConfigField
	// GetProtocolCapabilities はプロトコルの機能情報を返す
	GetProtocolCapabilities() ProtocolCapabilities
	// ConfigToMap は設定をmapに変換する
	ConfigToMap(config ProtocolConfig) map[string]interface{}
	// MapToConfig はmapから設定を作成する
	MapToConfig(variantID string, settings map[string]interface{}) (ProtocolConfig, error)
}

// DataStore はプロトコルサーバーファクトリーで使用するインターフェース
// 実際の定義は datastore パッケージにある
type DataStore interface {
	GetAreas() []MemoryArea
	ReadBit(area string, address uint32) (bool, error)
	WriteBit(area string, address uint32, value bool) error
	ReadBits(area string, address uint32, count uint16) ([]bool, error)
	WriteBits(area string, address uint32, values []bool) error
	ReadWord(area string, address uint32) (uint16, error)
	WriteWord(area string, address uint32, value uint16) error
	ReadWords(area string, address uint32, count uint16) ([]uint16, error)
	WriteWords(area string, address uint32, values []uint16) error
	Snapshot() map[string]interface{}
	Restore(data map[string]interface{}) error
	ClearAll()
}

// MemoryArea はメモリエリアの定義
type MemoryArea struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	IsBit          bool   `json:"isBit"`
	Size           uint32 `json:"size"`
	ReadOnly       bool   `json:"readOnly"`
	ByteAddressing bool   `json:"byteAddressing"` // trueの場合、アドレスをバイトベースで表示
	OneOrigin      bool   `json:"oneOrigin"`      // trueの場合、UIでのアドレス表示を1オリジンにする
}

// ConfigField は設定フィールドの定義
type ConfigField struct {
	Name        string          `json:"name"`
	Label       string          `json:"label"`
	Description string          `json:"description,omitempty"` // 設定項目の説明文
	Type        string          `json:"type"`                  // "text", "number", "select"
	Required    bool            `json:"required"`
	Default     interface{}     `json:"default"`
	Options     []FieldOption   `json:"options,omitempty"`
	Min         *int            `json:"min,omitempty"`
	Max         *int            `json:"max,omitempty"`
	Condition   *FieldCondition `json:"condition,omitempty"` // 表示条件
	Category    string          `json:"category,omitempty"`  // カテゴリ名（空の場合はカテゴリなし）
}

// FieldOption はセレクトフィールドのオプション
type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// FieldCondition はフィールドの表示条件
type FieldCondition struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// ProtocolCapabilities はプロトコルの機能情報
type ProtocolCapabilities struct {
	SupportsUnitID         bool `json:"supportsUnitId"`
	UnitIDMin              int  `json:"unitIdMin,omitempty"`
	UnitIDMax              int  `json:"unitIdMax,omitempty"`
	SupportsNodePublishing bool `json:"supportsNodePublishing"` // OPC UA, MQTT 等のノードベースプロトコル向け汎用フラグ
}

// NodePublishingInfo はノード公開設定を持つ変数の情報（プロトコル非依存）
type NodePublishingInfo struct {
	VariableID   string
	VariableName string
	DataType     string // "INT", "REAL", "STRING[20]" 等のプリミティブ文字列表現
	AccessMode   string // "read" | "write" | "readwrite"
}

// StructFieldInfo は OPC UA ノードツリー構築用の構造体フィールド情報
type StructFieldInfo struct {
	Name     string
	DataType string
}

// VariableStoreAccessor はプロトコルが VariableStore にアクセスするための汎用インターフェース。
// plugin 化時はこのインターフェースを gRPC 越しに提供することを想定している。
type VariableStoreAccessor interface {
	GetEnabledNodePublishings(protocolType string) []NodePublishingInfo
	ReadVariableValue(variableID string) (interface{}, error)
	WriteVariableValue(variableID string, value interface{}) error
	// WriteVariableFieldValue は変数の特定フィールド/要素のみをアトミックに更新する。
	// fieldPath は外部インデックス（表示ベース）のパス文字列
	// 例: "motor.speed", "items[1]"（ARRAY[1..10] の場合）, "items[2].name"
	WriteVariableFieldValue(variableID string, fieldPath string, value interface{}) error
	// GetStructFields は指定した構造体型のフィールド一覧を返す（子ノードブラウズ・サブスクリプション用）
	GetStructFields(typeName string) []StructFieldInfo
}

// VariableStoreInjector は VariableStoreAccessor の遅延注入を受け付ける ServerFactory 用インターフェース。
// init() で登録した後、AddServer 時に PLCService から注入される。
type VariableStoreInjector interface {
	InjectVariableStore(accessor VariableStoreAccessor)
}

// NodePublishingAware はノード公開設定変更通知を受け付ける ProtocolServer 用インターフェース
type NodePublishingAware interface {
	OnNodePublishingUpdated()
}
