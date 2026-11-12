package plugin

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/protocoltest"
	"karakuri/internal/pluginruntime"
)

// このファイルは、プラグイン側の実装（pluginruntime.Server）をテストのプロセス内の gRPC サーバーとして立て、
// ホスト側の Remote* / LazyRemoteServerFactory から実際の gRPC 越しに呼ぶための準備をまとめる。
// プラグインの exe は起動しない（plugin.json の debug_port と同じ「起動済みのプロセスに接続する」経路を使う）。

const testProtocol = "fake"

// hookStore は書き込みのたびに変更フックを呼ぶ DataStore（Modbus の DataStore と同じ動き）。
// テストから直接書き込むと「プロトコルのクライアントが書いた」ことになる。
type hookStore struct {
	*protocoltest.DataStore
	mu   sync.Mutex
	hook pluginruntime.DataChangeHook
}

func (d *hookStore) SetChangeHook(hook pluginruntime.DataChangeHook) {
	d.mu.Lock()
	d.hook = hook
	d.mu.Unlock()
}

func (d *hookStore) notify(area string, address uint32, words []uint16, bits []bool) {
	d.mu.Lock()
	hook := d.hook
	d.mu.Unlock()
	if hook != nil {
		hook(area, address, words, bits != nil, bits)
	}
}

func (d *hookStore) WriteWord(area string, address uint32, value uint16) error {
	return d.WriteWords(area, address, []uint16{value})
}

func (d *hookStore) WriteWords(area string, address uint32, values []uint16) error {
	if err := d.DataStore.WriteWords(area, address, values); err != nil {
		return err
	}
	d.notify(area, address, values, nil)
	return nil
}

func (d *hookStore) WriteBit(area string, address uint32, value bool) error {
	return d.WriteBits(area, address, []bool{value})
}

func (d *hookStore) WriteBits(area string, address uint32, values []bool) error {
	if err := d.DataStore.WriteBits(area, address, values); err != nil {
		return err
	}
	d.notify(area, address, nil, values)
	return nil
}

// testServer は通信イベントの送信先とノードアドレスを持つフェイクのサーバー
type testServer struct {
	*protocoltest.Server
	mu      sync.Mutex
	emitter protocol.CommunicationEventEmitter
}

func (s *testServer) SetEventEmitter(e protocol.CommunicationEventEmitter) {
	s.mu.Lock()
	s.emitter = e
	s.mu.Unlock()
}

func (s *testServer) getEmitter() protocol.CommunicationEventEmitter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emitter
}

// NodeAddresses は "port<設定のポート>:<変数名>" を返す。"!" を含む名前は公開できない
func (s *testServer) NodeAddresses(vars []protocol.NodeVariable) map[string]protocol.NodeAddress {
	port := s.Cfg.(*protocoltest.Config).Settings["port"]
	m := map[string]protocol.NodeAddress{}
	for _, v := range vars {
		a := protocol.NodeAddress{Address: fmt.Sprintf("port%v:%s", port, v.Name)}
		for _, r := range v.Name {
			if r == '!' {
				a = protocol.NodeAddress{Error: "! は使えません"}
			}
		}
		m[v.ID] = a
	}
	return m
}

// testFactory はプラグイン側で動くフェイクのファクトリー。最後に作った DataStore とサーバーを覚えておく
type testFactory struct {
	*protocoltest.Factory
	mu     sync.Mutex
	store  *hookStore
	server *testServer
}

func (f *testFactory) GetProtocolCapabilities() protocol.ProtocolCapabilities {
	return protocol.ProtocolCapabilities{SupportsUnitID: true, UnitIDMin: 1, UnitIDMax: 247, SupportsNodePublishing: true}
}

func (f *testFactory) CreateDataStore() protocol.DataStore {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store = &hookStore{DataStore: protocoltest.NewDataStore()}
	return f.store
}

func (f *testFactory) CreateServer(cfg protocol.ProtocolConfig, _ protocol.DataStore) (protocol.ProtocolServer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.server = &testServer{Server: protocoltest.NewServer(cfg)}
	return f.server, nil
}

// pluginStore / pluginServer はプラグイン側で今使われている DataStore / サーバーを返す
func (f *testFactory) pluginStore() *hookStore {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.store
}

func (f *testFactory) pluginServer() *testServer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.server
}

// testManifest はテスト用プラグインのマニフェスト
func testManifest(debugPort int) *PluginManifest {
	return &PluginManifest{
		Name:         "Fake Plugin",
		Entrypoint:   "fake-plugin.exe",
		Version:      "1.0.0",
		ProtocolType: testProtocol,
		DisplayName:  "Fake",
		Variants:     []ManifestVariant{{ID: "v1", DisplayName: "Variant 1"}},
		Capabilities: ManifestCapabilities{SupportsUnitID: true, UnitIDMin: 1, UnitIDMax: 247, SupportsNodePublishing: true},
		DebugPort:    debugPort,
	}
}

// startPlugin はプラグイン側の gRPC サーバーをプロセス内に立て、それに接続するホスト側のファクトリーを返す
func startPlugin(t *testing.T) (*testFactory, *LazyRemoteServerFactory) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	pluginSide := &testFactory{Factory: protocoltest.NewFactory(testProtocol, "v1", "Fake")}
	grpcServer := grpc.NewServer()
	pluginruntime.NewServer(pluginSide).Register(grpcServer)
	go grpcServer.Serve(lis) //nolint:errcheck
	t.Cleanup(grpcServer.Stop)

	entry := &PluginManifestEntry{Manifest: testManifest(lis.Addr().(*net.TCPAddr).Port), Dir: t.TempDir()}
	hostSide := NewLazyRemoteServerFactory(entry, NewPluginProcessManager(""))
	t.Cleanup(hostSide.StopProcess)
	return pluginSide, hostSide
}

// startedServer はプラグインに接続し、既定の設定でサーバーを作って起動する
func startedServer(t *testing.T) (*testFactory, *LazyRemoteServerFactory, *RemoteDataStore, *RemoteProtocolServer) {
	t.Helper()
	pluginSide, hostSide := startPlugin(t)
	if err := hostSide.EnsureStarted(); err != nil {
		t.Fatalf("EnsureStarted: %v", err)
	}
	ds := hostSide.CreateDataStore().(*RemoteDataStore)
	srv, err := hostSide.CreateServer(hostSide.CreateConfigFromVariant("v1"), ds)
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	rs := srv.(*RemoteProtocolServer)
	if err := rs.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rs.Stop() })
	return pluginSide, hostSide, ds, rs
}

// eventually は cond が true になるまで待つ（2 秒で失敗）
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("待っても成り立たなかった: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
