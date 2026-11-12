package pluginruntime

import (
	"sync"
	"time"

	"karakuri/internal/domain/protocol"
	pb "karakuri/pb/pluginpb"
)

// commEventInterval は通信イベントをまとめて送る間隔。
// ホストの通信インジケーターは 300ms 点灯するので、これより十分短ければ見た目は変わらない。
const commEventInterval = 50 * time.Millisecond

var _ protocol.CommunicationEventEmitter = (*commEventHub)(nil)

// commEventHub はプラグイン内のサーバーが発行した通信イベントを、
// SubscribeCommunicationEvents の購読者（ホスト）へまとめて届ける。
// 高頻度のポーリングでもイベントを 1 件ずつ送らず、購読者ごとに「前回の送信以降に起きたこと」だけを持つ。
type commEventHub struct {
	mu    sync.Mutex
	subs  map[*commSubscriber]struct{}
	count int // 現在の接続数
}

// commSubscriber は 1 つの購読者がまだ受け取っていないイベント
type commSubscriber struct {
	rx, tx, conn bool
	notify       chan struct{} // イベントがあることを知らせる（容量 1）
}

func newCommEventHub() *commEventHub {
	return &commEventHub{subs: map[*commSubscriber]struct{}{}}
}

func (h *commEventHub) EmitRx() { h.mark(func(s *commSubscriber) { s.rx = true }) }
func (h *commEventHub) EmitTx() { h.mark(func(s *commSubscriber) { s.tx = true }) }

func (h *commEventHub) EmitConnection(count int) {
	h.mu.Lock()
	h.count = count
	h.mu.Unlock()
	h.mark(func(s *commSubscriber) { s.conn = true })
}

func (h *commEventHub) mark(set func(*commSubscriber)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		set(s)
		select {
		case s.notify <- struct{}{}:
		default: // すでに知らせてある
		}
	}
}

// subscribe は購読者を登録する。現在の接続数を最初に届ける。
func (h *commEventHub) subscribe() *commSubscriber {
	s := &commSubscriber{conn: true, notify: make(chan struct{}, 1)}
	s.notify <- struct{}{}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *commEventHub) unsubscribe(s *commSubscriber) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

// take は購読者がまだ受け取っていないイベントを取り出す。何もなければ nil。
func (h *commEventHub) take(s *commSubscriber) *pb.CommunicationEvents {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !s.rx && !s.tx && !s.conn {
		return nil
	}
	ev := &pb.CommunicationEvents{Rx: s.rx, Tx: s.tx, ConnectionChanged: s.conn, ConnectionCount: int32(h.count)}
	s.rx, s.tx, s.conn = false, false, false
	return ev
}

// SubscribeCommunicationEvents は通信イベントをホストへストリームで送る
func (s *Server) SubscribeCommunicationEvents(_ *pb.Empty, stream pb.PluginService_SubscribeCommunicationEventsServer) error {
	sub := s.commEvents.subscribe()
	defer s.commEvents.unsubscribe(sub)

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sub.notify:
		}
		if ev := s.commEvents.take(sub); ev != nil {
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
		// 次の送信まで少し待ち、その間のイベントを 1 回にまとめる
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(commEventInterval):
		}
	}
}
