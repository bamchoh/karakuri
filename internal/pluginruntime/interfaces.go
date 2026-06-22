package pluginruntime

import (
	"modbus_simulator/internal/domain/protocol"

	"google.golang.org/grpc"
)

type plugin interface {
	Register(*grpc.Server)
}

type ChangeHookDataStore interface {
	protocol.DataStore
	SetChangeHook(hook DataChangeHook)
}

type Manifest struct {
	Name         string   `json:"name"`
	EntryPoint   string   `json:"entrypoint"`
	Version      string   `json:"version"`
	ProtocolType string   `json:"protocol_type"`
	DisplayName  string   `json:"display_name"`
	Variants     []string `json:"variants"`
	Capabilities protocol.ProtocolCapabilities
}
