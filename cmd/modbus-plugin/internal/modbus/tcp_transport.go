package modbus

import (
	"fmt"
	"modbus_simulator/internal/domain/protocol"

	"github.com/simonvetter/modbus"
)

type TCPTransport struct {
	server *modbus.ModbusServer

	config         *ModbusConfig
	handler        *DataStoreHandler
	eventEmitter   protocol.CommunicationEventEmitter
	sessionManager *protocol.SessionManager
}

func newTCPTransport(config *ModbusConfig, handler *DataStoreHandler, eventEmitter protocol.CommunicationEventEmitter, sessionManager *protocol.SessionManager) *TCPTransport {
	return &TCPTransport{
		config:         config,
		handler:        handler,
		eventEmitter:   eventEmitter,
		sessionManager: sessionManager,
	}
}

// startTCPServer はTCPサーバーを起動する（simonvetter/modbusを使用）
func (t *TCPTransport) Start() error {
	url := fmt.Sprintf("tcp://%s:%d", t.config.TCPAddress, t.config.TCPPort)

	// 使用するハンドラーを決定
	reqHandler := NewDataStoreRequestHandler(t.handler)
	reqHandler.SetEventEmitter(t.eventEmitter)
	reqHandler.SetSessionManager(t.sessionManager)

	srv, err := modbus.NewServer(&modbus.ServerConfiguration{
		URL: url,
	}, reqHandler)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	if err := srv.Start(); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	t.server = srv
	return nil
}

func (t *TCPTransport) Stop() error {
	if t.server == nil {
		return nil
	}

	err := t.server.Stop()
	t.server = nil

	return err
}
