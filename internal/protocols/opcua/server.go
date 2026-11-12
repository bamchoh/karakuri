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
var _ protocol.NodeAddressResolver = (*Server)(nil)

// Server は uastack を使った OPC UA サーバー。
// ホストの VariableStore で OPC UA 公開設定された変数を、設定のネームスペースのノードとして公開する。
type Server struct {
	mu       sync.Mutex
	config   *Config
	accessor protocol.VariableStoreAccessor
	logger   *log.Logger

	srv    *server.Server
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
// ctx は呼び出しの間だけ有効なものなので、サーバーの寿命には使わない。
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
		ApplicationName:  s.config.ApplicationName,
		ApplicationURI:   "urn:" + host + ":karakuri",
		ProductURI:       s.config.ProductURI,
		ManufacturerName: s.config.ManufacturerName,
		ProductName:      s.config.ApplicationName,
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

	nodes, err := newPLCNodes(srv.AddressSpace(), s.config.NamespaceURI, s.accessor, s.logger.Printf)
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
	s.nodes = nodes
	s.cancel = cancel
	s.done = done
	s.status = protocol.StatusRunning
	return nil
}

// Stop は OPC UA サーバーを停止する
func (s *Server) Stop() error {
	s.mu.Lock()
	srv, cancel, done := s.srv, s.cancel, s.done
	s.srv, s.nodes, s.cancel, s.done = nil, nil, nil, nil
	s.status = protocol.StatusStopped
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if srv != nil {
		// Close は Serve に渡したリスナーも閉じる（Serve の開始前に呼んでも Serve は終了する）
		_ = srv.Close()
		<-done
	}
	return nil
}

func (s *Server) Status() protocol.ServerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Server) ProtocolType() protocol.ProtocolType { return ProtocolOPCUA }

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

// unknownNamespace は停止中の NodeID に表示するネームスペース番号（番号は起動時に決まる）
const unknownNamespace = "???"

// NodeAddresses は変数 ID ごとのルートノードの NodeID（ns=N;s=変数名）を返す（NodeAddressResolver）。
// 起動中はアドレス空間に登録したネームスペースの番号を使う。停止中は番号が決まらないため ns=??? とする。
// 公開できない変数名（NodeID が他のノードと重なる）には Error を付ける。
func (s *Server) NodeAddresses(variables []protocol.NodeVariable) map[string]protocol.NodeAddress {
	s.mu.Lock()
	nodes := s.nodes
	s.mu.Unlock()

	ns := unknownNamespace
	if nodes != nil {
		ns = fmt.Sprint(nodes.ns)
	}
	result := make(map[string]protocol.NodeAddress, len(variables))
	for _, v := range variables {
		result[v.ID] = protocol.NodeAddress{Address: "ns=" + ns + ";s=" + v.Name, Error: publishNameError(v.Name)}
	}
	return result
}

// pkiDir は証明書ストアの場所（アプリの exe と同じフォルダの pki）。
// 現在はセキュリティなし・匿名認証のみのため証明書は作られない。
func pkiDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "pki"
	}
	return filepath.Join(filepath.Dir(exe), "pki")
}
