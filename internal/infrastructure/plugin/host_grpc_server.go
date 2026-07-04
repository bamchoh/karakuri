package plugin

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/ugorji/go/codec"
	"google.golang.org/grpc"

	pb "karakuri/pb/pluginpb"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
)

var msgpackHandle = func() *codec.MsgpackHandle {
	h := new(codec.MsgpackHandle)
	h.TypeInfos = codec.NewTypeInfos([]string{"msgpack"})
	return h
}()

func marshalMsgpack(v interface{}) ([]byte, error) {
	var b []byte
	enc := codec.NewEncoderBytes(&b, msgpackHandle)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b, nil
}

func unmarshalMsgpack(b []byte, v interface{}) error {
	dec := codec.NewDecoderBytes(b, msgpackHandle)
	return dec.Decode(v)
}

// HostGrpcServer はホスト側で動作する gRPC サーバー。
// OPC UA 等の SupportsNodePublishing=true プラグインが VariableAccessorService を呼び出すために使用する。
type HostGrpcServer struct {
	pb.UnimplementedVariableAccessorServiceServer

	accessor   protocol.VariableStoreAccessor
	varStore   *variable.VariableStore
	grpcServer *grpc.Server
	listener   net.Listener
	port       int

	mu          sync.RWMutex
	subscribers []chan *pb.VariableChange // SubscribeVariableChanges のストリーム送信用チャンネル
}

// NewHostGrpcServer は HostGrpcServer を作成する
func NewHostGrpcServer(accessor protocol.VariableStoreAccessor, varStore *variable.VariableStore) *HostGrpcServer {
	s := &HostGrpcServer{
		accessor: accessor,
		varStore: varStore,
	}
	// VariableStore の ChangeListener として登録（変数変更をプラグインにストリームで通知）
	varStore.AddListener(s)
	return s
}

// Start は空きポートで gRPC サーバーを起動し、ポート番号を返す
func (s *HostGrpcServer) Start() (int, error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("HostGrpcServer リスナー起動失敗: %w", err)
	}
	s.listener = lis
	s.port = lis.Addr().(*net.TCPAddr).Port

	s.grpcServer = grpc.NewServer()
	pb.RegisterVariableAccessorServiceServer(s.grpcServer, s)

	go func() {
		_ = s.grpcServer.Serve(lis)
	}()
	return s.port, nil
}

// Addr は gRPC サーバーのアドレスを返す（"127.0.0.1:port" 形式）
func (s *HostGrpcServer) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Stop は gRPC サーバーを停止する
func (s *HostGrpcServer) Stop() {
	s.varStore.RemoveListener(s)
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
}

// ---- VariableAccessorService 実装 ----

func (s *HostGrpcServer) GetEnabledNodePublishings(ctx context.Context, req *pb.GetNodePublishingsRequest) (*pb.GetNodePublishingsResponse, error) {
	publishings := s.accessor.GetEnabledNodePublishings(req.ProtocolType)
	pbPublishings := make([]*pb.NodePublishingInfo, len(publishings))
	for i, p := range publishings {
		pbPublishings[i] = &pb.NodePublishingInfo{
			VariableId:   p.VariableID,
			VariableName: p.VariableName,
			DataType:     p.DataType,
			AccessMode:   p.AccessMode,
		}
	}
	return &pb.GetNodePublishingsResponse{Publishings: pbPublishings}, nil
}

func (s *HostGrpcServer) ReadVariableValue(ctx context.Context, req *pb.ReadVariableValueRequest) (*pb.ReadVariableValueResponse, error) {
	val, err := s.accessor.ReadVariableValue(req.VariableId)
	if err != nil {
		return &pb.ReadVariableValueResponse{Error: err.Error()}, nil
	}
	b, err := marshalMsgpack(val)
	if err != nil {
		return &pb.ReadVariableValueResponse{Error: fmt.Sprintf("MessagePack 変換失敗: %v", err)}, nil
	}
	return &pb.ReadVariableValueResponse{ValueMsgpack: b}, nil
}

func (s *HostGrpcServer) WriteVariableValue(ctx context.Context, req *pb.WriteVariableValueRequest) (*pb.Empty, error) {
	var val interface{}
	if err := unmarshalMsgpack(req.ValueMsgpack, &val); err != nil {
		return nil, fmt.Errorf("MessagePack デコード失敗: %w", err)
	}
	if err := s.accessor.WriteVariableValue(req.VariableId, val); err != nil {
		return nil, err
	}
	return &pb.Empty{}, nil
}

func (s *HostGrpcServer) WriteVariableField(ctx context.Context, req *pb.WriteVariableFieldRequest) (*pb.Empty, error) {
	var val interface{}
	if err := unmarshalMsgpack(req.ValueMsgpack, &val); err != nil {
		return nil, fmt.Errorf("MessagePack デコード失敗: %w", err)
	}
	if err := s.accessor.WriteVariableFieldValue(req.VariableId, req.FieldPath, val); err != nil {
		return nil, err
	}
	return &pb.Empty{}, nil
}

func (s *HostGrpcServer) GetStructFields(ctx context.Context, req *pb.GetStructFieldsRequest) (*pb.GetStructFieldsResponse, error) {
	fields := s.accessor.GetStructFields(req.TypeName)
	pbFields := make([]*pb.StructFieldInfo, len(fields))
	for i, f := range fields {
		pbFields[i] = &pb.StructFieldInfo{
			Name:     f.Name,
			DataType: f.DataType,
		}
	}
	return &pb.GetStructFieldsResponse{Fields: pbFields}, nil
}

func (s *HostGrpcServer) SubscribeVariableChanges(_ *pb.Empty, stream pb.VariableAccessorService_SubscribeVariableChangesServer) error {
	ch := make(chan *pb.VariableChange, 64)
	s.mu.Lock()
	s.subscribers = append(s.subscribers, ch)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		for i, sub := range s.subscribers {
			if sub == ch {
				s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		close(ch)
	}()

	for {
		select {
		case change, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(change); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

// ---- variable.ChangeListener 実装 ----
// 変数が変更されたとき、購読中の全プラグインに通知する

func (s *HostGrpcServer) OnVariableChanged(v *variable.Variable, _ []variable.ProtocolMapping, changedPath string, changedValue interface{}) {
	// changedPath が空でない場合は部分値のみシリアライズ、空の場合は全体値
	target := v.Value
	if changedPath != "" && changedValue != nil {
		target = changedValue
	}
	b, err := marshalMsgpack(target)
	if err != nil {
		return
	}
	change := &pb.VariableChange{
		VariableId:   v.ID,
		ValueMsgpack: b,
		FieldPath:    changedPath,
	}
	s.mu.RLock()
	subs := make([]chan *pb.VariableChange, len(s.subscribers))
	copy(subs, s.subscribers)
	s.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- change:
		default:
			// バッファが溢れた場合はスキップ（プラグインが遅い場合）
		}
	}
}
