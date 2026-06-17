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
