package modbus

import (
	"karakuri/internal/protocols/modbus/rtu"
)

// RTUDataStoreAdapter はDataStoreHandlerをrtu.RequestHandlerに適合させるアダプター。
// Modbus TCP / RTU / ASCII のサーバーはどれも、rtu.Processor からこれを通して DataStore を読み書きする
type RTUDataStoreAdapter struct {
	handler *DataStoreHandler
}

// NewRTUDataStoreAdapter は新しいRTUDataStoreAdapterを作成する
func NewRTUDataStoreAdapter(handler *DataStoreHandler) *RTUDataStoreAdapter {
	return &RTUDataStoreAdapter{handler: handler}
}

// HandleReadCoils はコイル読み取りを処理する (FC 01)
func (a *RTUDataStoreAdapter) HandleReadCoils(unitID byte, address, quantity uint16) ([]bool, error) {
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
