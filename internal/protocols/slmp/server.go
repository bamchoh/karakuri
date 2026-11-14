package slmp

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"

	"karakuri/internal/domain/protocol"
)

var _ protocol.ProtocolServer = (*Server)(nil)

// Server は SLMP サーバー。Start / Stop は何度呼ばれてもよい（止めたあとに、また起動できる）
type Server struct {
	store protocol.DataStore

	mu       sync.Mutex
	config   *Config
	status   protocol.ServerStatus
	udp      *net.UDPConn
	listener net.Listener
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

// NewServer は SLMP サーバーを作る
func NewServer(config *Config, store protocol.DataStore) *Server {
	return &Server{config: config, store: store, status: protocol.StatusStopped}
}

func (s *Server) ProtocolType() protocol.ProtocolType { return ProtocolSLMP }

func (s *Server) Status() protocol.ServerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Server) Config() protocol.ProtocolConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func (s *Server) UpdateConfig(config protocol.ProtocolConfig) error {
	c, ok := config.(*Config)
	if !ok {
		return fmt.Errorf("invalid config type: expected slmp.Config")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == protocol.StatusRunning {
		return fmt.Errorf("cannot update config while server is running")
	}
	s.config = c
	return nil
}

// Start は待ち受けを始める
func (s *Server) Start(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == protocol.StatusRunning {
		return fmt.Errorf("server is already running")
	}
	if err := s.config.Validate(); err != nil {
		s.status = protocol.StatusError
		return err
	}
	address := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	h := newHandler(s.store)

	var udp *net.UDPConn
	if s.config.Transport != transportTCP {
		addr, err := net.ResolveUDPAddr("udp", address)
		if err == nil {
			udp, err = net.ListenUDP("udp", addr)
		}
		if err != nil {
			s.status = protocol.StatusError
			return fmt.Errorf("failed to start server (SLMP/UDP): %w", err)
		}
	}
	var listener net.Listener
	if s.config.Transport != transportUDP {
		ln, err := net.Listen("tcp", address)
		if err != nil {
			if udp != nil {
				_ = udp.Close()
			}
			s.status = protocol.StatusError
			return fmt.Errorf("failed to start server (SLMP/TCP): %w", err)
		}
		listener = ln
	}

	s.udp, s.listener = udp, listener
	s.conns = make(map[net.Conn]struct{})
	if udp != nil {
		s.wg.Add(1)
		go s.serveUDP(udp, h)
	}
	if listener != nil {
		s.wg.Add(1)
		go s.acceptLoop(listener, h)
	}
	s.status = protocol.StatusRunning
	return nil
}

// Stop は待ち受けをやめ、開いている接続をすべて閉じる
func (s *Server) Stop() error {
	s.mu.Lock()
	udp, listener := s.udp, s.listener
	s.udp, s.listener = nil, nil
	conns := make([]net.Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.conns = nil
	s.status = protocol.StatusStopped
	s.mu.Unlock()

	if udp != nil {
		_ = udp.Close()
	}
	if listener != nil {
		_ = listener.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	s.wg.Wait()
	return nil
}

func (s *Server) serveUDP(conn *net.UDPConn, h *handler) {
	defer s.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return // Stop で閉じられた
		}
		if resp := h.handle(buf[:n]); resp != nil {
			_, _ = conn.WriteToUDP(resp, from)
		}
	}
}

func (s *Server) acceptLoop(listener net.Listener, h *handler) {
	defer s.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return // Stop で閉じられた
		}
		s.mu.Lock()
		if s.conns == nil { // 停止済み
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		s.conns[conn] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serveTCP(conn, h)
	}
}

// serveTCP は、1 つの接続の要求を順に処理する。フレームとして読めないものが来たら、接続を閉じる
// （どこからが次のフレームかが分からなくなるため）
func (s *Server) serveTCP(conn net.Conn, h *handler) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()
	for {
		raw, err := readFrame(conn)
		if err != nil {
			return
		}
		resp := h.handle(raw)
		if resp == nil {
			return
		}
		if _, err := conn.Write(resp); err != nil {
			return
		}
	}
}
