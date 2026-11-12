package modbus

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

	"karakuri/internal/domain/protocol"

	"github.com/simonvetter/modbus"
)

// TCPTransport は Modbus TCP の待ち受け。
//
// リクエストの処理は simonvetter/modbus のサーバーに任せるが、このライブラリは
// 受け取ったリクエストに必ず応答する（ハンドラーから「応答しない」を選べない）。
// オフにした UnitID には応答しないようにするため、クライアントとの間に中継を入れる:
//
//	クライアント ──▶ 中継（設定のアドレス・ポートで待ち受け） ──▶ ライブラリのサーバー（127.0.0.1 の空きポート）
//
// 中継はリクエストのフレームを 1 つずつ読み、オフにした UnitID 宛てのものを捨てる。
// それ以外のリクエストと、すべての応答はそのまま通す。
type TCPTransport struct {
	server *modbus.ModbusServer

	config         *ModbusConfig
	handler        *DataStoreHandler
	eventEmitter   protocol.CommunicationEventEmitter
	sessionManager *SessionManager

	// 中継
	listener    net.Listener
	backendAddr string
	mu          sync.Mutex
	conns       map[net.Conn]struct{} // 開いている接続（クライアント側・ライブラリ側の両方）
	closed      bool
	wg          sync.WaitGroup
}

func newTCPTransport(config *ModbusConfig, handler *DataStoreHandler, eventEmitter protocol.CommunicationEventEmitter, sessionManager *SessionManager) *TCPTransport {
	return &TCPTransport{
		config:         config,
		handler:        handler,
		eventEmitter:   eventEmitter,
		sessionManager: sessionManager,
	}
}

// Start は待ち受けを始める
func (t *TCPTransport) Start() error {
	// 先に設定のポートを開く（使用中ならここでエラーにする）
	listener, err := net.Listen("tcp", net.JoinHostPort(t.config.TCPAddress, fmt.Sprint(t.config.TCPPort)))
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	if err := t.startBackend(); err != nil {
		_ = listener.Close()
		return err
	}

	t.mu.Lock()
	t.listener = listener
	t.conns = make(map[net.Conn]struct{})
	t.closed = false
	t.mu.Unlock()

	t.wg.Add(1)
	go t.acceptLoop(listener)
	return nil
}

// startBackend はライブラリのサーバーを 127.0.0.1 の空きポートで起動する
func (t *TCPTransport) startBackend() error {
	reqHandler := NewDataStoreRequestHandler(t.handler)
	reqHandler.SetEventEmitter(t.eventEmitter)
	reqHandler.SetSessionManager(t.sessionManager)

	// ライブラリはポートを自分で選べないので、空きポートを調べてから渡す。
	// 調べてから開くまでの間に他のプロセスに取られた場合は、別のポートでやり直す
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		port, err := freeLoopbackPort()
		if err != nil {
			return fmt.Errorf("failed to start server: %w", err)
		}
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		srv, err := modbus.NewServer(&modbus.ServerConfiguration{URL: "tcp://" + addr}, reqHandler)
		if err != nil {
			return fmt.Errorf("failed to create server: %w", err)
		}
		if err := srv.Start(); err != nil {
			lastErr = err
			continue
		}
		t.server = srv
		t.backendAddr = addr
		return nil
	}
	return fmt.Errorf("failed to start server: %w", lastErr)
}

func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func (t *TCPTransport) acceptLoop(listener net.Listener) {
	defer t.wg.Done()
	for {
		client, err := listener.Accept()
		if err != nil {
			return // Stop で閉じられた
		}
		t.wg.Add(1)
		go t.relay(client)
	}
}

// track は開いている接続を覚える。停止済みなら false を返す（呼び出し側が閉じる）
func (t *TCPTransport) track(c net.Conn) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return false
	}
	t.conns[c] = struct{}{}
	return true
}

func (t *TCPTransport) untrack(c net.Conn) {
	t.mu.Lock()
	delete(t.conns, c)
	t.mu.Unlock()
	_ = c.Close()
}

// relay は 1 つのクライアント接続を中継する
func (t *TCPTransport) relay(client net.Conn) {
	defer t.wg.Done()
	if !t.track(client) {
		_ = client.Close()
		return
	}
	defer t.untrack(client)

	backend, err := net.Dial("tcp", t.backendAddr)
	if err != nil {
		return
	}
	if !t.track(backend) {
		_ = backend.Close()
		return
	}
	defer t.untrack(backend)

	// 応答（ライブラリ → クライアント）はそのまま通す
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		_, _ = io.Copy(client, backend)
		// ライブラリ側が閉じたら（アイドルのタイムアウトなど）、クライアント側も閉じる
		_ = client.Close()
	}()

	// リクエスト（クライアント → ライブラリ）は、フレームごとに UnitID を見て通すか捨てる
	t.forwardRequests(client, backend)
}

// mbapHeaderSize は Modbus TCP のヘッダーの長さ:
// トランザクション ID(2) + プロトコル ID(2) + 長さ(2) + UnitID(1)
const mbapHeaderSize = 7

// forwardRequests はクライアントのリクエストを読み、オフにした UnitID 宛てのもの以外をライブラリへ送る
func (t *TCPTransport) forwardRequests(client io.Reader, backend io.Writer) {
	header := make([]byte, mbapHeaderSize)
	for {
		if _, err := io.ReadFull(client, header); err != nil {
			return
		}
		// 長さは「UnitID 以降のバイト数」。UnitID はヘッダーに含めて読んだので、残りはその分少ない
		length := int(binary.BigEndian.Uint16(header[4:6]))
		frame := header
		if length > 1 {
			frame = make([]byte, mbapHeaderSize+length-1)
			copy(frame, header)
			if _, err := io.ReadFull(client, frame[mbapHeaderSize:]); err != nil {
				return
			}
		}

		unitID := header[6]
		if !t.handler.IsUnitIdEnabled(unitID) {
			// 応答しない。受信したことだけを通信インジケーターに伝える
			if t.eventEmitter != nil {
				t.eventEmitter.EmitRx()
			}
			continue
		}
		if _, err := backend.Write(frame); err != nil {
			return
		}
	}
}

// Stop は待ち受けをやめ、開いている接続をすべて閉じる
func (t *TCPTransport) Stop() error {
	t.mu.Lock()
	listener := t.listener
	t.listener = nil
	t.closed = true
	conns := make([]net.Conn, 0, len(t.conns))
	for c := range t.conns {
		conns = append(conns, c)
	}
	t.mu.Unlock()

	if listener != nil {
		_ = listener.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	t.wg.Wait()

	if t.server == nil {
		return nil
	}
	err := t.server.Stop()
	t.server = nil
	return err
}
