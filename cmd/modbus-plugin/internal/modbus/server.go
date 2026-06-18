package modbus

import (
	"fmt"
	"sync"

	"modbus_simulator/internal/domain/protocol"
	"modbus_simulator/internal/domain/server"
)

type Transport interface {
	Start() error
	Stop() error
}

// ModbusHandler はModbusリクエストを処理するインターフェース
type ModbusHandler interface {
	SetUnitIdEnabled(unitId uint8, enabled bool)
	IsUnitIdEnabled(unitId uint8) bool
	GetDisabledUnitIDs() []uint8
	SetDisabledUnitIDs(ids []uint8)
}

// Server はModbusサーバーを管理する
type Server struct {
	mu        sync.Mutex
	transport Transport
	status    server.ServerStatus
}

// NewServerWithHandler はDataStoreHandlerを使用するサーバーを作成する
func NewServerWithHandler(config *ModbusConfig, handler *DataStoreHandler, eventEmitter protocol.CommunicationEventEmitter, sessionManager *protocol.SessionManager) *Server {
	// ModbusConfigからserver.ServerConfigへ変換
	var transport Transport
	switch config.GetVariant() {
	case VariantRTU:
		transport = newRTUTransport(config, handler, eventEmitter, sessionManager)
	case VariantASCII:
		transport = newASCIITransport(config, handler, eventEmitter, sessionManager)
	default:
		transport = newTCPTransport(config, handler, eventEmitter, sessionManager)
	}

	return &Server{
		transport: transport,
		status:    server.StatusStopped,
	}
}

// Start はサーバーを起動する
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status == server.StatusRunning {
		return fmt.Errorf("server is already running")
	}

	if err := s.transport.Start(); err != nil {
		s.status = server.StatusError
		return err
	}

	s.status = server.StatusRunning
	return nil
}

// Stop はサーバーを停止する
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.transport.Stop(); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	s.status = server.StatusStopped
	return nil
}

// Status はサーバーの状態を返す
func (s *Server) Status() server.ServerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}
