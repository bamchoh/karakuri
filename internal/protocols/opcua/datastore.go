package opcua

import "karakuri/internal/domain/protocol"

// emptyDataStore は OPC UA 用の空 DataStore。
// OPC UA はレジスタ（メモリエリア）を持たず、変数をノードとして直接公開する。
type emptyDataStore struct{}

var _ protocol.DataStore = (*emptyDataStore)(nil)

func (d *emptyDataStore) GetAreas() []protocol.MemoryArea                    { return nil }
func (d *emptyDataStore) ReadBit(string, uint32) (bool, error)               { return false, nil }
func (d *emptyDataStore) WriteBit(string, uint32, bool) error                { return nil }
func (d *emptyDataStore) ReadBits(string, uint32, uint16) ([]bool, error)    { return nil, nil }
func (d *emptyDataStore) WriteBits(string, uint32, []bool) error             { return nil }
func (d *emptyDataStore) ReadWord(string, uint32) (uint16, error)            { return 0, nil }
func (d *emptyDataStore) WriteWord(string, uint32, uint16) error             { return nil }
func (d *emptyDataStore) ReadWords(string, uint32, uint16) ([]uint16, error) { return nil, nil }
func (d *emptyDataStore) WriteWords(string, uint32, []uint16) error          { return nil }
