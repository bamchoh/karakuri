package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	pb "karakuri/pb/pluginpb"

	"karakuri/internal/domain/protocol"
)

// RemoteDataStore は gRPC クライアントを通じてプラグインプロセスの DataStore を実装する
type RemoteDataStore struct {
	client pb.DataStoreServiceClient
}

func NewRemoteDataStore(client pb.DataStoreServiceClient) *RemoteDataStore {
	return &RemoteDataStore{client: client}
}

func (d *RemoteDataStore) GetAreas() []protocol.MemoryArea {
	resp, err := d.client.GetAreas(backgroundCtx(), &pb.Empty{})
	if err != nil {
		return nil
	}
	areas := make([]protocol.MemoryArea, len(resp.Areas))
	for i, a := range resp.Areas {
		areas[i] = protocol.MemoryArea{
			ID:             a.Id,
			DisplayName:    a.DisplayName,
			IsBit:          a.IsBit,
			Size:           a.Size,
			ReadOnly:       a.ReadOnly,
			ByteAddressing: a.ByteAddressing,
			OneOrigin:      a.OneOrigin,
			ShortName:      a.ShortName,
		}
	}
	return areas
}

func (d *RemoteDataStore) ReadBit(area string, address uint32) (bool, error) {
	resp, err := d.client.ReadBit(backgroundCtx(), &pb.ReadBitRequest{Area: area, Address: address})
	if err != nil {
		return false, err
	}
	return resp.Value, nil
}

func (d *RemoteDataStore) WriteBit(area string, address uint32, value bool) error {
	_, err := d.client.WriteBit(backgroundCtx(), &pb.WriteBitRequest{Area: area, Address: address, Value: value})
	return err
}

func (d *RemoteDataStore) ReadBits(area string, address uint32, count uint16) ([]bool, error) {
	resp, err := d.client.ReadBits(backgroundCtx(), &pb.ReadBitsRequest{
		Area:    area,
		Address: address,
		Count:   uint32(count),
	})
	if err != nil {
		return nil, err
	}
	return resp.Values, nil
}

func (d *RemoteDataStore) WriteBits(area string, address uint32, values []bool) error {
	_, err := d.client.WriteBits(backgroundCtx(), &pb.WriteBitsRequest{
		Area:    area,
		Address: address,
		Values:  values,
	})
	return err
}

func (d *RemoteDataStore) ReadWord(area string, address uint32) (uint16, error) {
	resp, err := d.client.ReadWord(backgroundCtx(), &pb.ReadWordRequest{Area: area, Address: address})
	if err != nil {
		return 0, err
	}
	return uint16(resp.Value), nil
}

func (d *RemoteDataStore) WriteWord(area string, address uint32, value uint16) error {
	_, err := d.client.WriteWord(backgroundCtx(), &pb.WriteWordRequest{
		Area:    area,
		Address: address,
		Value:   uint32(value),
	})
	return err
}

func (d *RemoteDataStore) ReadWords(area string, address uint32, count uint16) ([]uint16, error) {
	resp, err := d.client.ReadWords(backgroundCtx(), &pb.ReadWordsRequest{
		Area:    area,
		Address: address,
		Count:   uint32(count),
	})
	if err != nil {
		return nil, err
	}
	words := make([]uint16, len(resp.Values))
	for i, v := range resp.Values {
		words[i] = uint16(v)
	}
	return words, nil
}

func (d *RemoteDataStore) WriteWords(area string, address uint32, values []uint16) error {
	pbValues := make([]uint32, len(values))
	for i, v := range values {
		pbValues[i] = uint32(v)
	}
	_, err := d.client.WriteWords(backgroundCtx(), &pb.WriteWordsRequest{
		Area:    area,
		Address: address,
		Values:  pbValues,
	})
	return err
}

func (d *RemoteDataStore) Snapshot() map[string]interface{} {
	resp, err := d.client.Snapshot(backgroundCtx(), &pb.Empty{})
	if err != nil {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if err := json.Unmarshal(resp.SnapshotJson, &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

func (d *RemoteDataStore) Restore(data map[string]interface{}) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("スナップショットの JSON 変換に失敗: %w", err)
	}
	_, err = d.client.Restore(backgroundCtx(), &pb.RestoreRequest{SnapshotJson: jsonBytes})
	return err
}

func (d *RemoteDataStore) ClearAll() {
	_, _ = d.client.ClearAll(backgroundCtx(), &pb.Empty{})
}

// SubscribeChanges はプラグインからの DataChange ストリームを受信するクライアントを返す
func (d *RemoteDataStore) SubscribeChanges(ctx context.Context) (pb.DataStoreService_SubscribeChangesClient, error) {
	return d.client.SubscribeChanges(ctx, &pb.Empty{})
}
