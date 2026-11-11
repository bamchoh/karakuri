package opcua

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/bamchoh/uastack/server"

	"karakuri/internal/domain/protocol"
)

var _ protocol.ProtocolServer = (*Server)(nil)
var _ protocol.NodePublishingAware = (*Server)(nil)

// Server は uastack を使った OPC UA サーバー。
// ホストの VariableStore で OPC UA 公開設定された変数を ns=<NamespaceURI> のノードとして公開する。
type Server struct {
	mu       sync.Mutex
	config   *Config
	accessor protocol.VariableStoreAccessor
	logger   *log.Logger

	srv    *server.Server
	ln     net.Listener
	nodes  *plcNodes
	cancel context.CancelFunc
	done   chan struct{}
	status protocol.ServerStatus
}

// NewServer は OPC UA サーバーを作成する（起動はしない）
func NewServer(config *Config, accessor protocol.VariableStoreAccessor) *Server {
	return &Server{
		config:   config,
		accessor: accessor,
		logger:   log.New(os.Stderr, "[opcua] ", log.LstdFlags),
		status:   protocol.StatusStopped,
	}
}

// Start は OPC UA サーバーを起動する。
// ctx は gRPC リクエストのコンテキストでありすぐにキャンセルされるため、サーバーの寿命には使わない。
func (s *Server) Start(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status == protocol.StatusRunning {
		return fmt.Errorf("OPC UA サーバーは既に起動しています")
	}
	if err := s.config.Validate(); err != nil {
		return err
	}
	if s.accessor == nil {
		s.logger.Printf("[WARN] ホストの変数ストアに接続されていないため、変数は公開されません")
	}

	host, _ := os.Hostname()
	if host == "" {
		host = "localhost"
	}
	srv, err := server.New(server.Config{
		ApplicationName:  "Karakuri PLC Simulator",
		ApplicationURI:   "urn:" + host + ":karakuri",
		ProductURI:       "urn:karakuri",
		ManufacturerName: "karakuri",
		ProductName:      "Karakuri PLC Simulator",
		SoftwareVersion:  VERSION,
		ListenAddr:       s.config.listenAddr(),
		EndpointURL:      s.config.endpointURL(),
		PKIDir:           pkiDir(),
		Logger:           s.logger,
	})
	if err != nil {
		s.status = protocol.StatusError
		return fmt.Errorf("OPC UA サーバーの作成に失敗: %w", err)
	}

	nodes, err := newPLCNodes(srv.AddressSpace(), s.accessor, s.logger.Printf)
	if err != nil {
		s.status = protocol.StatusError
		return fmt.Errorf("アドレス空間の作成に失敗: %w", err)
	}
	nodes.rebuild()

	// ポートの使用中などのエラーを Start の戻り値で返すため、Listen はここで行う
	ln, err := net.Listen("tcp", s.config.listenAddr())
	if err != nil {
		s.status = protocol.StatusError
		return fmt.Errorf("OPC UA サーバーの待ち受けに失敗 (%s): %w", s.config.listenAddr(), err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go nodes.run(ctx)
	go func() {
		defer close(done)
		if err := srv.Serve(ln); err != nil {
			s.logger.Printf("[ERROR] OPC UA サーバーが停止しました: %v", err)
			s.mu.Lock()
			if s.srv == srv {
				s.status = protocol.StatusError
			}
			s.mu.Unlock()
		}
	}()

	s.srv = srv
	s.ln = ln
	s.nodes = nodes
	s.cancel = cancel
	s.done = done
	s.status = protocol.StatusRunning
	return nil
}

// Stop は OPC UA サーバーを停止する
func (s *Server) Stop() error {
	s.mu.Lock()
	srv, ln, cancel, done := s.srv, s.ln, s.cancel, s.done
	s.srv, s.ln, s.nodes, s.cancel, s.done = nil, nil, nil, nil, nil
	s.status = protocol.StatusStopped
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if srv != nil {
		// uastack v0.1.0 は Serve がリスナーを登録する前に Close されると Accept で待ち続けるため、
		// 自前で Listen したリスナーをここで閉じて Serve を確実に終わらせる
		_ = srv.Close()
		_ = ln.Close()
		<-done
	}
	return nil
}

func (s *Server) Status() protocol.ServerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Server) ProtocolType() protocol.ProtocolType { return protocol.ProtocolOPCUA }

func (s *Server) Config() protocol.ProtocolConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func (s *Server) UpdateConfig(config protocol.ProtocolConfig) error {
	cfg, ok := config.(*Config)
	if !ok {
		return fmt.Errorf("invalid config type: %T", config)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == protocol.StatusRunning {
		return fmt.Errorf("サーバー起動中は設定を変更できません")
	}
	s.config = cfg
	return nil
}

// OnNodePublishingUpdated は公開設定の変更を受けてノードを作り直す（NodePublishingAware）
func (s *Server) OnNodePublishingUpdated() {
	s.mu.Lock()
	nodes := s.nodes
	s.mu.Unlock()
	if nodes != nil {
		nodes.rebuild()
	}
}

// pkiDir は証明書ストアの場所（プラグイン exe と同じフォルダの pki）。
// 現在はセキュリティなし・匿名認証のみのため証明書は作られない。
func pkiDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "pki"
	}
	return filepath.Join(filepath.Dir(exe), "pki")
}
