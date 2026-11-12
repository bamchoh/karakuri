package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"google.golang.org/grpc"

	pb "karakuri/pb/pluginpb"

	"karakuri/internal/domain/protocol"
)

var _ protocol.UnitIDConfigurable = (*RemoteProtocolServer)(nil)
var _ protocol.CommunicationEventAware = (*RemoteProtocolServer)(nil)

// RemoteProtocolServer は gRPC クライアントを通じてプラグインプロセスの ProtocolServer を実装する
type RemoteProtocolServer struct {
	pluginClient pb.PluginServiceClient
	conn         *grpc.ClientConn
	config       protocol.ProtocolConfig
	// hostGrpcAddr は HostGrpcServer のアドレス（SupportsNodePublishing=true の場合に使用）
	hostGrpcAddr string

	// 通信イベント（プラグイン → ホスト）の転送
	commMu      sync.Mutex
	emitter     protocol.CommunicationEventEmitter
	cancelComm  context.CancelFunc // 起動中の購読を止める
	commStopped chan struct{}      // 購読のゴルーチンが終わると閉じる
}

func NewRemoteProtocolServer(client pb.PluginServiceClient, conn *grpc.ClientConn, config protocol.ProtocolConfig) *RemoteProtocolServer {
	return &RemoteProtocolServer{
		pluginClient: client,
		conn:         conn,
		config:       config,
	}
}

// SetHostGrpcAddr はホストの VariableAccessorService gRPC アドレスを設定する
func (s *RemoteProtocolServer) SetHostGrpcAddr(addr string) {
	s.hostGrpcAddr = addr
}

func (s *RemoteProtocolServer) Start(ctx context.Context) error {
	rc, ok := s.config.(*remoteProtocolConfig)
	if !ok {
		return fmt.Errorf("設定の型が不正: %T", s.config)
	}
	_, err := s.pluginClient.CreateAndStart(ctx, &pb.CreateAndStartRequest{
		VariantId:    rc.variantID,
		SettingsJson: rc.settingsJSON,
		HostGrpcAddr: s.hostGrpcAddr,
	})
	if err != nil {
		return err
	}
	s.startCommunicationEvents()
	return nil
}

func (s *RemoteProtocolServer) Stop() error {
	s.stopCommunicationEvents()
	_, err := s.pluginClient.Stop(backgroundCtx(), &pb.Empty{})
	return err
}

// SetEventEmitter は通信イベントの送信先を設定する（CommunicationEventAware）。
// 次に Start したときから、プラグインの通信イベントをこの送信先へ転送する。
func (s *RemoteProtocolServer) SetEventEmitter(emitter protocol.CommunicationEventEmitter) {
	s.commMu.Lock()
	s.emitter = emitter
	s.commMu.Unlock()
}

// startCommunicationEvents はプラグインの通信イベントのストリームを購読し、送信先へ転送する
func (s *RemoteProtocolServer) startCommunicationEvents() {
	s.stopCommunicationEvents()

	s.commMu.Lock()
	defer s.commMu.Unlock()
	emitter := s.emitter
	if emitter == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	s.cancelComm, s.commStopped = cancel, stopped

	go func() {
		defer close(stopped)
		// 止めたとき・プラグインが終了したときは、接続数を 0 に戻す
		defer emitter.EmitConnection(0)
		stream, err := s.pluginClient.SubscribeCommunicationEvents(ctx, &pb.Empty{})
		if err != nil {
			return
		}
		for {
			ev, err := stream.Recv()
			if err != nil {
				return
			}
			if ev.Rx {
				emitter.EmitRx()
			}
			if ev.Tx {
				emitter.EmitTx()
			}
			if ev.ConnectionChanged {
				emitter.EmitConnection(int(ev.ConnectionCount))
			}
		}
	}()
}

// stopCommunicationEvents は通信イベントの購読を止め、ゴルーチンの終了を待つ
func (s *RemoteProtocolServer) stopCommunicationEvents() {
	s.commMu.Lock()
	cancel, stopped := s.cancelComm, s.commStopped
	s.cancelComm, s.commStopped = nil, nil
	s.commMu.Unlock()
	if cancel != nil {
		cancel()
		<-stopped
	}
}

func (s *RemoteProtocolServer) Status() protocol.ServerStatus {
	resp, err := s.pluginClient.GetStatus(backgroundCtx(), &pb.Empty{})
	if err != nil {
		return protocol.StatusError
	}
	switch resp.Status {
	case "Running":
		return protocol.StatusRunning
	case "Stopped":
		return protocol.StatusStopped
	default:
		return protocol.StatusError
	}
}

func (s *RemoteProtocolServer) ProtocolType() protocol.ProtocolType {
	return s.config.ProtocolType()
}

func (s *RemoteProtocolServer) Config() protocol.ProtocolConfig {
	return s.config
}

func (s *RemoteProtocolServer) UpdateConfig(config protocol.ProtocolConfig) error {
	// 呼び出し元（PLCService）がサーバー起動中の更新を禁止しているため、
	// ここに到達する時点でサーバーは常に停止中。ローカルの config を更新するのみ。
	s.config = config
	return nil
}

// OnNodePublishingUpdated は NodePublishingAware を duck-typing で満たすためのメソッド
func (s *RemoteProtocolServer) OnNodePublishingUpdated() {
	_, _ = s.pluginClient.OnNodePublishingUpdated(backgroundCtx(), &pb.Empty{})
}

// NodeAddresses はプラグインに公開変数のノードアドレスを問い合わせる（NodeAddressResolver）。
// 停止中のサーバーでも計算できるよう、ホスト側の現在の設定を渡す。
func (s *RemoteProtocolServer) NodeAddresses(variables []protocol.NodeVariable) map[string]protocol.NodeAddress {
	req := &pb.GetNodeAddressesRequest{Variables: make([]*pb.NodeVariable, len(variables))}
	for i, v := range variables {
		req.Variables[i] = &pb.NodeVariable{Id: v.ID, Name: v.Name}
	}
	if rc, ok := s.config.(*remoteProtocolConfig); ok {
		req.VariantId = rc.variantID
		req.SettingsJson = rc.settingsJSON
	}
	resp, err := s.pluginClient.GetNodeAddresses(backgroundCtx(), req)
	if err != nil {
		return nil
	}
	result := make(map[string]protocol.NodeAddress, len(resp.Addresses))
	for id, a := range resp.Addresses {
		result[id] = protocol.NodeAddress{Address: a.Address, Error: a.Error}
	}
	return result
}

// GetDisabledUnitIDs は無効化された UnitID を返す（UnitIDConfigurable）
func (s *RemoteProtocolServer) GetDisabledUnitIDs() []uint8 {
	resp, err := s.pluginClient.GetUnitIDSettings(backgroundCtx(), &pb.Empty{})
	if err != nil {
		return nil
	}
	ids := make([]uint8, len(resp.DisabledIds))
	for i, id := range resp.DisabledIds {
		ids[i] = uint8(id)
	}
	return ids
}

// SetUnitIdEnabled は UnitID duck-typing インターフェースを満たすためのメソッド
func (s *RemoteProtocolServer) SetUnitIdEnabled(unitId uint8, enabled bool) {
	_, _ = s.pluginClient.SetUnitIDEnabled(backgroundCtx(), &pb.SetUnitIDEnabledRequest{
		UnitId:  int32(unitId),
		Enabled: enabled,
	})
}

// SetDisabledUnitIDs は UnitID duck-typing インターフェースを満たすためのメソッド
func (s *RemoteProtocolServer) SetDisabledUnitIDs(ids []uint8) {
	pbIDs := make([]int32, len(ids))
	for i, id := range ids {
		pbIDs[i] = int32(id)
	}
	_, _ = s.pluginClient.SetDisabledUnitIDs(backgroundCtx(), &pb.SetDisabledUnitIDsRequest{Ids: pbIDs})
}

// ConfigSettingsToMap は設定を JSON から map に変換するユーティリティ
func configSettingsFromJSON(settingsJSON string) map[string]interface{} {
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(settingsJSON), &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}
