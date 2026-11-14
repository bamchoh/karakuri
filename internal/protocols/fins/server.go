package fins

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"karakuri/internal/domain/protocol"
)

// FINS/TCP のヘッダー: "FINS"(4) 長さ(4) コマンド(4) エラーコード(4)。長さは、コマンド以降のバイト数
const (
	tcpHeaderLength = 16

	tcpCmdNodeRequest  uint32 = 0 // クライアント → サーバー: 自分のノードアドレス（0 なら、割り当ててもらう）
	tcpCmdNodeResponse uint32 = 1 // サーバー → クライアント: クライアントとサーバーのノードアドレス
	tcpCmdFrame        uint32 = 2 // FINS フレーム
	tcpCmdError        uint32 = 3 // エラーの通知（このあと接続を閉じる）

	tcpErrNotFINS     uint32 = 1
	tcpErrTooLong     uint32 = 2
	tcpErrUnsupported uint32 = 3
)

var tcpMagic = []byte("FINS")

// appendTCPHeader は FINS/TCP のヘッダーを足す。bodyLength は、ヘッダーのあとに続くバイト数
func appendTCPHeader(dst []byte, cmd, errCode uint32, bodyLength int) []byte {
	dst = append(dst, tcpMagic...)
	dst = binary.BigEndian.AppendUint32(dst, uint32(8+bodyLength))
	dst = binary.BigEndian.AppendUint32(dst, cmd)
	return binary.BigEndian.AppendUint32(dst, errCode)
}

// tcpMessage は FINS/TCP のメッセージ 1 つ
type tcpMessage struct {
	Cmd     uint32
	ErrCode uint32 // ヘッダーのエラーコード（エラーの通知のときに入る）
	Body    []byte
	Raw     []byte // 受け取ったバイト列そのまま（ヘッダー + 本体）
}

// readTCPMessage は FINS/TCP のメッセージを 1 つ読む。reject は、相手に知らせる FINS/TCP のエラー（0 なら読めた）
func readTCPMessage(r io.Reader) (msg tcpMessage, reject uint32, err error) {
	head := make([]byte, tcpHeaderLength)
	if _, err := io.ReadFull(r, head); err != nil {
		return msg, 0, err
	}
	if string(head[:4]) != string(tcpMagic) {
		return msg, tcpErrNotFINS, fmt.Errorf("FINS/TCP のヘッダーではありません")
	}
	length := binary.BigEndian.Uint32(head[4:8])
	if length < 8 || length > 8+maxFrameLength {
		return msg, tcpErrTooLong, fmt.Errorf("FINS/TCP の長さ（%d）が不正です", length)
	}
	raw := make([]byte, tcpHeaderLength+int(length)-8)
	copy(raw, head)
	if _, err := io.ReadFull(r, raw[tcpHeaderLength:]); err != nil {
		return msg, 0, err
	}
	return tcpMessage{
		Cmd:     binary.BigEndian.Uint32(head[8:12]),
		ErrCode: binary.BigEndian.Uint32(head[12:16]),
		Body:    raw[tcpHeaderLength:],
		Raw:     raw,
	}, 0, nil
}

var _ protocol.ProtocolServer = (*Server)(nil)

// Server は FINS サーバー。Start / Stop は何度呼ばれてもよい（止めたあとに、また起動できる）
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

// NewServer は FINS サーバーを作る
func NewServer(config *Config, store protocol.DataStore) *Server {
	return &Server{config: config, store: store, status: protocol.StatusStopped}
}

func (s *Server) ProtocolType() protocol.ProtocolType { return ProtocolFINS }

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
		return fmt.Errorf("invalid config type: expected fins.Config")
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
			return fmt.Errorf("failed to start server (FINS/UDP): %w", err)
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
			return fmt.Errorf("failed to start server (FINS/TCP): %w", err)
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
		go s.acceptLoop(listener, h, byte(s.config.Node))
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

func (s *Server) acceptLoop(listener net.Listener, h *handler, node byte) {
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
		go s.serveTCP(conn, h, node)
	}
}

func (s *Server) serveTCP(conn net.Conn, h *handler, node byte) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()

	fail := func(errCode uint32) {
		_, _ = conn.Write(appendTCPHeader(nil, tcpCmdError, errCode, 0))
	}

	// 最初に、ノードアドレスを決めるやりとりをする
	msg, reject, err := readTCPMessage(conn)
	if err != nil {
		if reject != 0 {
			fail(reject)
		}
		return
	}
	if msg.Cmd != tcpCmdNodeRequest || len(msg.Body) != 4 {
		fail(tcpErrUnsupported)
		return
	}
	clientNode := msg.Body[3]
	if clientNode == 0 {
		clientNode = assignClientNode(conn.RemoteAddr(), node)
	}
	resp := appendTCPHeader(nil, tcpCmdNodeResponse, 0, 8)
	resp = append(resp, 0, 0, 0, clientNode, 0, 0, 0, node)
	if _, err := conn.Write(resp); err != nil {
		return
	}

	for {
		msg, reject, err := readTCPMessage(conn)
		if err != nil {
			if reject != 0 {
				fail(reject)
			}
			return
		}
		if msg.Cmd != tcpCmdFrame {
			fail(tcpErrUnsupported)
			return
		}
		frame := h.handle(msg.Body)
		if frame == nil {
			continue
		}
		out := appendTCPHeader(make([]byte, 0, tcpHeaderLength+len(frame)), tcpCmdFrame, 0, len(frame))
		if _, err := conn.Write(append(out, frame...)); err != nil {
			return
		}
	}
}

// assignClientNode は、ノードアドレスを指定しなかったクライアントに割り当てるアドレスを決める。
// クライアントの IP アドレスの最後の数字を使い、使えない値（0・255・サーバーと同じ）なら別の値にする
func assignClientNode(addr net.Addr, serverNode byte) byte {
	var node byte
	if tcp, ok := addr.(*net.TCPAddr); ok {
		if ip4 := tcp.IP.To4(); ip4 != nil {
			node = ip4[3]
		}
	}
	if node == 0 || node == 255 || node == serverNode {
		node = 254
		if serverNode == 254 {
			node = 253
		}
	}
	return node
}
