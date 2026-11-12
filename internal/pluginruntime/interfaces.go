package pluginruntime

import (
	"karakuri/internal/domain/protocol"

	"google.golang.org/grpc"
)

type plugin interface {
	Register(*grpc.Server)
}

type ChangeHookDataStore interface {
	protocol.DataStore
	SetChangeHook(hook DataChangeHook)
}

// Manifest は plugin.json の内容。
// ホスト側の plugin.PluginManifest と同じ JSON 形式で出力する。
type Manifest struct {
	Name         string               `json:"name"`
	EntryPoint   string               `json:"entrypoint"`
	Version      string               `json:"version"`
	ProtocolType string               `json:"protocol_type"`
	DisplayName  string               `json:"display_name"`
	Variants     []ManifestVariant    `json:"variants"`
	Capabilities ManifestCapabilities `json:"capabilities"`
}

// ManifestVariant は plugin.json の variants の 1 項目（ホスト側の plugin.ManifestVariant と同じ形）
type ManifestVariant struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// ManifestCapabilities は plugin.json の capabilities セクション（snake_case）
type ManifestCapabilities struct {
	SupportsUnitID         bool `json:"supports_unit_id"`
	UnitIDMin              int  `json:"unit_id_min"`
	UnitIDMax              int  `json:"unit_id_max"`
	SupportsNodePublishing bool `json:"supports_node_publishing"`
}

// NewManifest はファクトリーの情報から Manifest を作成する
func NewManifest(factory protocol.ServerFactory, entryPoint string) Manifest {
	caps := factory.GetProtocolCapabilities()
	return Manifest{
		Name:         factory.DisplayName() + " Plugin",
		EntryPoint:   entryPoint,
		Version:      factory.Version(),
		ProtocolType: string(factory.ProtocolType()),
		DisplayName:  factory.DisplayName(),
		Variants:     []ManifestVariant{},
		Capabilities: ManifestCapabilities{
			SupportsUnitID:         caps.SupportsUnitID,
			UnitIDMin:              caps.UnitIDMin,
			UnitIDMax:              caps.UnitIDMax,
			SupportsNodePublishing: caps.SupportsNodePublishing,
		},
	}
}
