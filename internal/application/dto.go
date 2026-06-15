package application

// === プロトコルスキーマDTO ===

// ProtocolSchemaDTO はプロトコル設定スキーマ
type ProtocolSchemaDTO struct {
	ProtocolType string          `json:"protocolType"`
	DisplayName  string          `json:"displayName"`
	Variants     []VariantDTO    `json:"variants"`
	Capabilities CapabilitiesDTO `json:"capabilities"`
}

// VariantDTO はバリアント情報
type VariantDTO struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"displayName"`
	Fields      []FieldDTO `json:"fields"`
}

// FieldDTO は設定フィールド情報
type FieldDTO struct {
	Name        string        `json:"name"`
	Label       string        `json:"label"`
	Description string        `json:"description,omitempty"` // 設定項目の説明文
	Type        string        `json:"type"`
	Required    bool          `json:"required"`
	Default     interface{}   `json:"default"`
	Options     []OptionDTO   `json:"options,omitempty"`
	Min         *int          `json:"min,omitempty"`
	Max         *int          `json:"max,omitempty"`
	ShowWhen    *ConditionDTO `json:"showWhen,omitempty"`
	Category    string        `json:"category,omitempty"` // カテゴリ名（空の場合はカテゴリなし）
}

// OptionDTO はセレクトフィールドのオプション
type OptionDTO struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ConditionDTO はフィールドの表示条件
type ConditionDTO struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// CapabilitiesDTO はプロトコルの機能情報
type CapabilitiesDTO struct {
	SupportsUnitID bool `json:"supportsUnitId"`
	UnitIDMin      int  `json:"unitIdMin,omitempty"`
	UnitIDMax      int  `json:"unitIdMax,omitempty"`
}

// === 汎用プロトコル設定DTO ===

// ProtocolConfigDTO は汎用プロトコル設定のDTO
type ProtocolConfigDTO struct {
	ProtocolType string                 `json:"protocolType"`
	Variant      string                 `json:"variant"`
	Settings     map[string]interface{} `json:"settings"`
}

// === プロトコル情報DTO ===

// ProtocolInfoDTO はプロトコル情報のDTO
type ProtocolInfoDTO struct {
	Type        string             `json:"type"`
	DisplayName string             `json:"displayName"`
	Variants    []ConfigVariantDTO `json:"variants"`
}

// ConfigVariantDTO は設定バリアント情報のDTO
type ConfigVariantDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// === メモリ操作DTO ===

// MemoryAreaDTO はメモリエリア情報のDTO
type MemoryAreaDTO struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	IsBit          bool   `json:"isBit"`
	Size           int    `json:"size"`
	ReadOnly       bool   `json:"readOnly"`
	ByteAddressing bool   `json:"byteAddressing"`
	OneOrigin      bool   `json:"oneOrigin"`
}

// === UnitID設定DTO ===

// UnitIDSettingsDTO はUnitID設定のDTO
type UnitIDSettingsDTO struct {
	Min         int   `json:"min"`
	Max         int   `json:"max"`
	DisabledIDs []int `json:"disabledIds"`
}

// === スクリプトDTO ===

// ConsoleLogDTO はconsole.logの1エントリのDTO
type ConsoleLogDTO struct {
	ScriptID   string `json:"scriptId"`
	ScriptName string `json:"scriptName"`
	Message    string `json:"message"`
	At         int64  `json:"at"` // Unix ミリ秒
}

// ScriptDTO はスクリプトのDTO
type ScriptDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	IntervalMs int    `json:"intervalMs"`
	IsRunning  bool   `json:"isRunning"`
	LastError  string `json:"lastError"`
	ErrorAt    int64  `json:"errorAt"`
	Order      int    `json:"order"`
}

// IntervalPresetDTO は周期プリセットのDTO
type IntervalPresetDTO struct {
	Label string `json:"label"`
	Ms    int    `json:"ms"`
}

// === サーバーインスタンスDTO ===

// ServerInstanceDTO はサーバーインスタンス一覧表示用
type ServerInstanceDTO struct {
	ProtocolType           string `json:"protocolType"`
	DisplayName            string `json:"displayName"`
	Variant                string `json:"variant"`
	Status                 string `json:"status"` // "Running" | "Stopped" | "Error"
	SupportsNodePublishing bool   `json:"supportsNodePublishing"`
}

// ServerConfigDTO は特定サーバーの設定
type ServerConfigDTO struct {
	ProtocolType string                 `json:"protocolType"`
	Variant      string                 `json:"variant"`
	Settings     map[string]interface{} `json:"settings"`
}

// ServerSnapshotDTO は Export/Import 用の単一サーバースナップショット
type ServerSnapshotDTO struct {
	ProtocolType   string                 `json:"protocolType"`
	Variant        string                 `json:"variant"`
	Settings       map[string]interface{} `json:"settings"`
	UnitIDSettings *UnitIDSettingsDTO     `json:"unitIdSettings,omitempty"`
}

// === モニタリングDTO ===

// MonitoringItemDTO はモニタリング項目のDTO
type MonitoringItemDTO struct {
	ID            string `json:"id"`
	Order         int    `json:"order"`
	ProtocolType  string `json:"protocolType"`
	MemoryArea    string `json:"memoryArea"`
	Address       int    `json:"address"`
	BitWidth      int    `json:"bitWidth"`
	Endianness    string `json:"endianness"`
	DisplayFormat string `json:"displayFormat"`
}

// MonitoringConfigDTO はモニタリング設定全体のDTO
type MonitoringConfigDTO struct {
	Version int                  `json:"version"`
	Items   []*MonitoringItemDTO `json:"items"`
}

// === 変数DTO ===

// NodePublishingDTO はノード公開設定のDTO（プロトコル非依存）
type NodePublishingDTO struct {
	ProtocolType string `json:"protocolType"`
	Enabled      bool   `json:"enabled"`
	AccessMode   string `json:"accessMode"` // "read" | "write" | "readwrite"
}

// VariableDTO は変数のDTO
type VariableDTO struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	DataType        string               `json:"dataType"`
	Value           interface{}          `json:"value"`
	Mappings        []ProtocolMappingDTO `json:"mappings,omitempty"`
	NodePublishings []NodePublishingDTO  `json:"nodePublishings,omitempty"`
}

// ProtocolMappingDTO はプロトコルマッピングのDTO
type ProtocolMappingDTO struct {
	ProtocolType string `json:"protocolType"`
	MemoryArea   string `json:"memoryArea"`
	Address      int    `json:"address"`
	Endianness   string `json:"endianness"`
}

// DataTypesDTO はデータ型一覧のDTO
type DataTypesDTO struct {
	Types       []DataTypeInfoDTO `json:"types"`
	StructTypes []StructTypeDTO   `json:"structTypes,omitempty"`
}

// StructFieldDTO は構造体フィールドのDTO
type StructFieldDTO struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Offset   int    `json:"offset"`
}

// StructTypeDTO は構造体型定義のDTO
type StructTypeDTO struct {
	Name      string           `json:"name"`
	Fields    []StructFieldDTO `json:"fields"`
	WordCount int              `json:"wordCount"`
}

// DataTypeInfoDTO はデータ型情報のDTO
type DataTypeInfoDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	WordCount   int    `json:"wordCount"`
}

// === プロジェクトエクスポート/インポートDTO ===

// ProjectDataDTO はプロジェクト全体のエクスポート/インポート用DTO
type ProjectDataDTO struct {
	Servers         []ServerSnapshotDTO  `json:"servers,omitempty"`
	Scripts         []*ScriptDTO         `json:"scripts"`
	MonitoringItems []*MonitoringItemDTO `json:"monitoringItems,omitempty"`
	Variables       []*VariableDTO       `json:"variables,omitempty"`
	StructTypes     []StructTypeDTO      `json:"structTypes,omitempty"`
}
