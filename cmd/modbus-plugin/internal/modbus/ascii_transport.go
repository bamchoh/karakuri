package modbus

import (
	"fmt"
	"modbus_simulator/cmd/modbus-plugin/internal/modbus/rtu"
	"modbus_simulator/internal/domain/protocol"
)

type ASCIITransport struct {
	server *rtu.ASCIIServer

	config         *ModbusConfig
	handler        *DataStoreHandler
	eventEmitter   protocol.CommunicationEventEmitter
	sessionManager *protocol.SessionManager
}

func newASCIITransport(config *ModbusConfig, handler *DataStoreHandler, eventEmitter protocol.CommunicationEventEmitter, sessionManager *protocol.SessionManager) *ASCIITransport {
	return &ASCIITransport{
		config:         config,
		handler:        handler,
		eventEmitter:   eventEmitter,
		sessionManager: sessionManager,
	}
}

// startASCIIServer はRTU ASCIIサーバーを起動する（自作実装）
func (t *ASCIITransport) Start() error {
	config := rtu.SerialConfig{
		Port:     t.config.SerialPort,
		BaudRate: t.config.BaudRate,
		DataBits: t.config.DataBits,
		StopBits: t.config.StopBits,
		Parity:   t.config.Parity,
	}

	var adapter rtu.RequestHandler
	rtuAdapter := NewRTUDataStoreAdapter(t.handler)
	rtuAdapter.SetEventEmitter(t.eventEmitter)
	adapter = rtuAdapter
	asciiSrv := rtu.NewASCIIServer(config, adapter)

	if err := asciiSrv.Start(); err != nil {
		return fmt.Errorf("failed to start ASCII server: %w", err)
	}

	t.server = asciiSrv
	return nil
}

func (t *ASCIITransport) Stop() error {
	if t.server == nil {
		return nil
	}

	err := t.server.Stop()
	t.server = nil
	return err
}
