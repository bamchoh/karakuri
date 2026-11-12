package modbus

import (
	"fmt"
	"karakuri/internal/protocols/modbus/rtu"
)

type ASCIITransport struct {
	server *rtu.ASCIIServer

	config  *ModbusConfig
	handler *DataStoreHandler
}

func newASCIITransport(config *ModbusConfig, handler *DataStoreHandler) *ASCIITransport {
	return &ASCIITransport{
		config:  config,
		handler: handler,
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

	adapter := NewRTUDataStoreAdapter(t.handler)
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
