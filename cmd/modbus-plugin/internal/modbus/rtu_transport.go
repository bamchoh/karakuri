package modbus

import (
	"fmt"
	"karakuri/cmd/modbus-plugin/internal/modbus/rtu"
)

type RTUTransport struct {
	server *rtu.RTUServer

	config  *ModbusConfig
	handler *DataStoreHandler
}

func newRTUTransport(config *ModbusConfig, handler *DataStoreHandler) *RTUTransport {
	return &RTUTransport{
		config:  config,
		handler: handler,
	}
}

// startRTUServer はRTUサーバーを起動する（自作実装）
func (t *RTUTransport) Start() error {
	config := rtu.SerialConfig{
		Port:     t.config.SerialPort,
		BaudRate: t.config.BaudRate,
		DataBits: t.config.DataBits,
		StopBits: t.config.StopBits,
		Parity:   t.config.Parity,
	}

	adapter := NewRTUDataStoreAdapter(t.handler)
	rtuSrv := rtu.NewRTUServer(config, adapter)

	if err := rtuSrv.Start(); err != nil {
		return fmt.Errorf("failed to start RTU server: %w", err)
	}

	t.server = rtuSrv
	return nil
}

func (t *RTUTransport) Stop() error {
	if t.server == nil {
		return nil
	}

	err := t.server.Stop()
	t.server = nil

	return err
}
