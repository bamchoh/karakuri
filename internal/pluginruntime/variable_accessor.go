package pluginruntime

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/valuecodec"
	pb "karakuri/pb/pluginpb"
)

// rpcTimeout はホストへの単発 RPC のタイムアウト
const rpcTimeout = 5 * time.Second

// RemoteVariableStoreAccessor はホストの VariableAccessorService を gRPC で呼び出す
// protocol.VariableStoreAccessor 実装。SupportsNodePublishing=true のプラグインに注入する。
type RemoteVariableStoreAccessor struct {
	conn   *grpc.ClientConn
	client pb.VariableAccessorServiceClient
}

var _ protocol.VariableStoreAccessor = (*RemoteVariableStoreAccessor)(nil)

// DialVariableStoreAccessor はホストの gRPC アドレスに接続して accessor を作成する
func DialVariableStoreAccessor(addr string) (*RemoteVariableStoreAccessor, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("HostGrpcServer への接続失敗: %w", err)
	}
	return &RemoteVariableStoreAccessor{conn: conn, client: pb.NewVariableAccessorServiceClient(conn)}, nil
}

// Close は gRPC 接続を閉じる
func (a *RemoteVariableStoreAccessor) Close() error {
	return a.conn.Close()
}

func rpcCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), rpcTimeout)
}

func (a *RemoteVariableStoreAccessor) GetEnabledNodePublishings(protocolType string) []protocol.NodePublishingInfo {
	ctx, cancel := rpcCtx()
	defer cancel()
	resp, err := a.client.GetEnabledNodePublishings(ctx, &pb.GetNodePublishingsRequest{ProtocolType: protocolType})
	if err != nil {
		return nil
	}
	result := make([]protocol.NodePublishingInfo, len(resp.Publishings))
	for i, p := range resp.Publishings {
		result[i] = protocol.NodePublishingInfo{
			VariableID:   p.VariableId,
			VariableName: p.VariableName,
			DataType:     p.DataType,
			AccessMode:   p.AccessMode,
		}
	}
	return result
}

func (a *RemoteVariableStoreAccessor) ReadVariableValue(variableID string) (interface{}, error) {
	ctx, cancel := rpcCtx()
	defer cancel()
	resp, err := a.client.ReadVariableValue(ctx, &pb.ReadVariableValueRequest{VariableId: variableID})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var value interface{}
	if err := valuecodec.Unmarshal(resp.ValueMsgpack, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (a *RemoteVariableStoreAccessor) WriteVariableValue(variableID string, value interface{}) error {
	b, err := valuecodec.Marshal(value)
	if err != nil {
		return err
	}
	ctx, cancel := rpcCtx()
	defer cancel()
	_, err = a.client.WriteVariableValue(ctx, &pb.WriteVariableValueRequest{VariableId: variableID, ValueMsgpack: b})
	return err
}

func (a *RemoteVariableStoreAccessor) WriteVariableFieldValue(variableID, fieldPath string, value interface{}) error {
	b, err := valuecodec.Marshal(value)
	if err != nil {
		return err
	}
	ctx, cancel := rpcCtx()
	defer cancel()
	_, err = a.client.WriteVariableField(ctx, &pb.WriteVariableFieldRequest{
		VariableId:   variableID,
		FieldPath:    fieldPath,
		ValueMsgpack: b,
	})
	return err
}

func (a *RemoteVariableStoreAccessor) GetStructFields(typeName string) []protocol.StructFieldInfo {
	ctx, cancel := rpcCtx()
	defer cancel()
	resp, err := a.client.GetStructFields(ctx, &pb.GetStructFieldsRequest{TypeName: typeName})
	if err != nil {
		return nil
	}
	result := make([]protocol.StructFieldInfo, len(resp.Fields))
	for i, f := range resp.Fields {
		result[i] = protocol.StructFieldInfo{Name: f.Name, DataType: f.DataType}
	}
	return result
}

// SubscribeVariableChanges はホストの変数変更ストリームを購読し、変更された変数 ID を onChange に渡す。
// ctx がキャンセルされるかストリームが切れるまでブロックする。
func (a *RemoteVariableStoreAccessor) SubscribeVariableChanges(ctx context.Context, onChange func(variableID string)) error {
	stream, err := a.client.SubscribeVariableChanges(ctx, &pb.Empty{})
	if err != nil {
		return err
	}
	for {
		change, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		onChange(change.VariableId)
	}
}
