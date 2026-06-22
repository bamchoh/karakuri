package modbus

import (
	"modbus_simulator/cmd/modbus-plugin/internal/modbus/rtu"
	"modbus_simulator/internal/domain/protocol"

	"github.com/simonvetter/modbus"
)

// DataStoreRequestHandler はDataStoreHandlerをsimonvetter/modbusのRequestHandlerに適合させるアダプター
type DataStoreRequestHandler struct {
	handler        *DataStoreHandler
	sessionManager *protocol.SessionManager
	eventEmitter   protocol.CommunicationEventEmitter
}

// NewDataStoreRequestHandler は新しいDataStoreRequestHandlerを作成する
func NewDataStoreRequestHandler(handler *DataStoreHandler) *DataStoreRequestHandler {
	return &DataStoreRequestHandler{handler: handler}
}

// SetSessionManager はセッションマネージャーを設定する
func (h *DataStoreRequestHandler) SetSessionManager(manager *protocol.SessionManager) {
	h.sessionManager = manager
}

// SetEventEmitter はイベントエミッターを設定する
func (h *DataStoreRequestHandler) SetEventEmitter(emitter protocol.CommunicationEventEmitter) {
	h.eventEmitter = emitter
}

// emitRxTx は受信・送信イベントを発行する
func (h *DataStoreRequestHandler) emitRxTx(unitID uint8) {
	if h.sessionManager != nil {
		h.sessionManager.RecordActivityWithUnitID(unitID)
	}
	if h.eventEmitter != nil {
		h.eventEmitter.EmitRx()
		h.eventEmitter.EmitTx()
	}
}

// HandleCoils はコイル読み取りを処理する (Function Code 1)
func (h *DataStoreRequestHandler) HandleCoils(req *modbus.CoilsRequest) ([]bool, error) {
	h.emitRxTx(req.UnitId)
	if !h.handler.IsUnitIdEnabled(req.UnitId) {
		return nil, modbus.ErrIllegalFunction
	}

	if req.IsWrite {
		// 書き込みリクエスト (Function Code 6, 16)
		if err := h.handler.store.WriteBits(AreaHoldingRegs, uint32(req.Addr), req.Args); err != nil {
			return nil, modbus.ErrIllegalDataAddress
		}
		return req.Args, nil
	}

	return h.handler.store.ReadBits(AreaCoils, uint32(req.Addr), req.Quantity)
}

// HandleDiscreteInputs はディスクリート入力読み取りを処理する (Function Code 2)
func (h *DataStoreRequestHandler) HandleDiscreteInputs(req *modbus.DiscreteInputsRequest) ([]bool, error) {
	h.emitRxTx(req.UnitId)
	if !h.handler.IsUnitIdEnabled(req.UnitId) {
		return nil, modbus.ErrIllegalFunction
	}
	return h.handler.store.ReadBits(AreaDiscreteInputs, uint32(req.Addr), req.Quantity)
}

// HandleHoldingRegisters は保持レジスタ読み取りを処理する (Function Code 3)
func (h *DataStoreRequestHandler) HandleHoldingRegisters(req *modbus.HoldingRegistersRequest) ([]uint16, error) {
	h.emitRxTx(req.UnitId)
	if !h.handler.IsUnitIdEnabled(req.UnitId) {
		return nil, modbus.ErrIllegalFunction
	}

	if req.IsWrite {
		// 書き込みリクエスト (Function Code 6, 16)
		if err := h.handler.store.WriteWords(AreaHoldingRegs, uint32(req.Addr), req.Args); err != nil {
			return nil, modbus.ErrIllegalDataAddress
		}
		return req.Args, nil
	}

	// 読み取りリクエスト
	return h.handler.store.ReadWords(AreaHoldingRegs, uint32(req.Addr), req.Quantity)
}

// HandleInputRegisters は入力レジスタ読み取りを処理する (Function Code 4)
func (h *DataStoreRequestHandler) HandleInputRegisters(req *modbus.InputRegistersRequest) ([]uint16, error) {
	h.emitRxTx(req.UnitId)
	if !h.handler.IsUnitIdEnabled(req.UnitId) {
		return nil, modbus.ErrIllegalFunction
	}
	return h.handler.store.ReadWords(AreaInputRegs, uint32(req.Addr), req.Quantity)
}

// HandleWriteSingleCoil は単一コイル書き込みを処理する (Function Code 5)
func (h *DataStoreRequestHandler) HandleWriteSingleCoil(req *modbus.CoilsRequest) error {
	h.emitRxTx(req.UnitId)
	if !h.handler.IsUnitIdEnabled(req.UnitId) {
		return modbus.ErrIllegalFunction
	}
	if len(req.Args) == 0 {
		return modbus.ErrIllegalDataValue
	}
	return h.handler.store.WriteBit(AreaCoils, uint32(req.Addr), req.Args[0])
}

// HandleWriteMultipleCoils は複数コイル書き込みを処理する (Function Code 15)
func (h *DataStoreRequestHandler) HandleWriteMultipleCoils(req *modbus.CoilsRequest) error {
	h.emitRxTx(req.UnitId)
	if !h.handler.IsUnitIdEnabled(req.UnitId) {
		return modbus.ErrIllegalFunction
	}
	return h.handler.store.WriteBits(AreaCoils, uint32(req.Addr), req.Args)
}

// RTUDataStoreAdapter はDataStoreHandlerをrtu.RequestHandlerに適合させるアダプター
type RTUDataStoreAdapter struct {
	handler      *DataStoreHandler
	eventEmitter protocol.CommunicationEventEmitter
}

// NewRTUDataStoreAdapter は新しいRTUDataStoreAdapterを作成する
func NewRTUDataStoreAdapter(handler *DataStoreHandler) *RTUDataStoreAdapter {
	return &RTUDataStoreAdapter{handler: handler}
}

// SetEventEmitter はイベントエミッターを設定する
func (a *RTUDataStoreAdapter) SetEventEmitter(emitter protocol.CommunicationEventEmitter) {
	a.eventEmitter = emitter
}

// emitRxTx は受信・送信イベントを発行する
func (a *RTUDataStoreAdapter) emitRxTx() {
	if a.eventEmitter != nil {
		a.eventEmitter.EmitRx()
		a.eventEmitter.EmitTx()
	}
}

// HandleReadCoils はコイル読み取りを処理する (FC 01)
func (a *RTUDataStoreAdapter) HandleReadCoils(unitID byte, address, quantity uint16) ([]bool, error) {
	a.emitRxTx()
	if !a.handler.IsUnitIdEnabled(unitID) {
		return nil, rtu.ErrIllegalFunction
	}
	values, err := a.handler.store.ReadBits(AreaCoils, uint32(address), quantity)
	if err != nil {
		return nil, rtu.ErrIllegalDataAddress
	}
	return values, nil
}

// HandleReadDiscreteInputs はディスクリート入力読み取りを処理する (FC 02)
func (a *RTUDataStoreAdapter) HandleReadDiscreteInputs(unitID byte, address, quantity uint16) ([]bool, error) {
	a.emitRxTx()
	if !a.handler.IsUnitIdEnabled(unitID) {
		return nil, rtu.ErrIllegalFunction
	}
	values, err := a.handler.store.ReadBits(AreaDiscreteInputs, uint32(address), quantity)
	if err != nil {
		return nil, rtu.ErrIllegalDataAddress
	}
	return values, nil
}

// HandleReadHoldingRegisters は保持レジスタ読み取りを処理する (FC 03)
func (a *RTUDataStoreAdapter) HandleReadHoldingRegisters(unitID byte, address, quantity uint16) ([]uint16, error) {
	a.emitRxTx()
	if !a.handler.IsUnitIdEnabled(unitID) {
		return nil, rtu.ErrIllegalFunction
	}
	values, err := a.handler.store.ReadWords(AreaHoldingRegs, uint32(address), quantity)
	if err != nil {
		return nil, rtu.ErrIllegalDataAddress
	}
	return values, nil
}

// HandleReadInputRegisters は入力レジスタ読み取りを処理する (FC 04)
func (a *RTUDataStoreAdapter) HandleReadInputRegisters(unitID byte, address, quantity uint16) ([]uint16, error) {
	a.emitRxTx()
	if !a.handler.IsUnitIdEnabled(unitID) {
		return nil, rtu.ErrIllegalFunction
	}
	values, err := a.handler.store.ReadWords(AreaInputRegs, uint32(address), quantity)
	if err != nil {
		return nil, rtu.ErrIllegalDataAddress
	}
	return values, nil
}

// HandleWriteSingleCoil は単一コイル書き込みを処理する (FC 05)
func (a *RTUDataStoreAdapter) HandleWriteSingleCoil(unitID byte, address uint16, value bool) error {
	a.emitRxTx()
	if !a.handler.IsUnitIdEnabled(unitID) {
		return rtu.ErrIllegalFunction
	}
	if err := a.handler.store.WriteBit(AreaCoils, uint32(address), value); err != nil {
		return rtu.ErrIllegalDataAddress
	}
	return nil
}

// HandleWriteSingleRegister は単一レジスタ書き込みを処理する (FC 06)
func (a *RTUDataStoreAdapter) HandleWriteSingleRegister(unitID byte, address, value uint16) error {
	a.emitRxTx()
	if !a.handler.IsUnitIdEnabled(unitID) {
		return rtu.ErrIllegalFunction
	}
	if err := a.handler.store.WriteWord(AreaHoldingRegs, uint32(address), value); err != nil {
		return rtu.ErrIllegalDataAddress
	}
	return nil
}

// HandleWriteMultipleCoils は複数コイル書き込みを処理する (FC 15)
func (a *RTUDataStoreAdapter) HandleWriteMultipleCoils(unitID byte, address uint16, values []bool) error {
	a.emitRxTx()
	if !a.handler.IsUnitIdEnabled(unitID) {
		return rtu.ErrIllegalFunction
	}
	if err := a.handler.store.WriteBits(AreaCoils, uint32(address), values); err != nil {
		return rtu.ErrIllegalDataAddress
	}
	return nil
}

// HandleWriteMultipleRegisters は複数レジスタ書き込みを処理する (FC 16)
func (a *RTUDataStoreAdapter) HandleWriteMultipleRegisters(unitID byte, address uint16, values []uint16) error {
	a.emitRxTx()
	if !a.handler.IsUnitIdEnabled(unitID) {
		return rtu.ErrIllegalFunction
	}
	if err := a.handler.store.WriteWords(AreaHoldingRegs, uint32(address), values); err != nil {
		return rtu.ErrIllegalDataAddress
	}
	return nil
}

// IsUnitIDEnabled は指定したUnitIDが応答するかどうかを返す
func (a *RTUDataStoreAdapter) IsUnitIDEnabled(unitID byte) bool {
	return a.handler.IsUnitIdEnabled(unitID)
}
