package modbus

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

	"karakuri/internal/protocols/modbus/rtu"
)

// TCPTransport は Modbus TCP の待ち受け。
//
// リクエストの処理は RTU / ASCII と同じ rtu.Processor（→ RTUDataStoreAdapter → DataStore）で行う。
// TCP で違うのは、フレームの形（MBAP ヘッダー + PDU。CRC はない）だけ。
// そのため、オフにした UnitID に応答しないこと・個数の誤りに例外 03 を返すことなどは、3 つの通信方式で同じになる。
//
// 以前は simonvetter/modbus のサーバーを使い、オフにした UnitID 宛てのフレームを捨てる中継を前に置いていた。
// このライブラリは、受け取ったリクエストに必ず応答し、個数の誤りには例外を返さずに接続を閉じるため、やめた。
type TCPTransport struct {
	config    *ModbusConfig
	processor *rtu.Processor

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{} // 開いているクライアントの接続
	closed   bool
	wg       sync.WaitGroup
}

func newTCPTransport(config *ModbusConfig, handler *DataStoreHandler) *TCPTransport {
	return &TCPTransport{
		config:    config,
		processor: rtu.NewProcessor(NewRTUDataStoreAdapter(handler)),
	}
}

// Start は待ち受けを始める（ポートが使用中ならエラー）
func (t *TCPTransport) Start() error {
	listener, err := net.Listen("tcp", net.JoinHostPort(t.config.TCPAddress, fmt.Sprint(t.config.TCPPort)))
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
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

func (t *TCPTransport) acceptLoop(listener net.Listener) {
	defer t.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return // Stop で閉じられた
		}
		t.wg.Add(1)
		go t.handleConn(conn)
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

// handleConn は 1 つのクライアント接続のリクエストを、届いた順に 1 つずつ処理する
func (t *TCPTransport) handleConn(conn net.Conn) {
	defer t.wg.Done()
	if !t.track(conn) {
		_ = conn.Close()
		return
	}
	defer t.untrack(conn)
	t.serve(conn, conn)
}

// mbapHeaderSize は Modbus TCP のヘッダーの長さ:
// トランザクション ID(2) + プロトコル ID(2) + 長さ(2) + UnitID(1)
const mbapHeaderSize = 7

// maxMBAPLength は、ヘッダーの「長さ」（UnitID 以降のバイト数）の最大。
// PDU は最大 253 バイト（Modbus Application Protocol Specification V1.1b3 の 4.1）なので、UnitID を足して 254
const maxMBAPLength = 1 + 253

// serve は r からリクエストのフレームを読み、応答を w に書く。r が終わるか、フレームの区切りがわからなくなったら戻る（呼び出し側が接続を閉じる）。
//
//   - 長さが 2 より小さい（機能コードがない）か、254 を超えるフレームは、区切りを信用できないので接続を閉じる
//   - プロトコル ID が 0（Modbus）でないフレームは、応答せずに読み捨てる
//   - オフにした UnitID 宛てには応答しない（例外応答も返さない）
//   - 機能コードに対してデータが短すぎるリクエストは、例外 03（Illegal Data Value）
func (t *TCPTransport) serve(r io.Reader, w io.Writer) {
	header := make([]byte, mbapHeaderSize)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			return
		}
		length := int(binary.BigEndian.Uint16(header[4:6]))
		if length < 2 || length > maxMBAPLength {
			return
		}
		// 本体: UnitID（ヘッダーの最後のバイト）+ PDU
		body := make([]byte, length)
		body[0] = header[6]
		if _, err := io.ReadFull(r, body[1:]); err != nil {
			return
		}
		if binary.BigEndian.Uint16(header[2:4]) != 0 {
			continue
		}

		res := t.respond(body)
		if res == nil {
			continue
		}
		frame := make([]byte, mbapHeaderSize-1, mbapHeaderSize-1+len(res))
		copy(frame, header[:4]) // トランザクション ID とプロトコル ID はリクエストと同じ
		binary.BigEndian.PutUint16(frame[4:6], uint16(len(res)))
		frame = append(frame, res...)
		if _, err := w.Write(frame); err != nil {
			return
		}
	}
}

// respond は UnitID + PDU のリクエストを処理し、UnitID + PDU の応答を返す（応答しないなら nil）
func (t *TCPTransport) respond(body []byte) []byte {
	req, err := rtu.ParseRequestBody(body)
	if err != nil {
		// データが短すぎる。UnitID と機能コードはあるので、例外応答を返せる
		req = &rtu.Request{UnitID: body[0], FunctionCode: body[1]}
		if !t.processor.IsUnitIDEnabled(req.UnitID) {
			return nil
		}
		return rtu.BuildExceptionResponse(req.UnitID, req.FunctionCode, rtu.ExceptionIllegalDataValue)[:3]
	}
	return t.processor.ProcessBody(req)
}

// Stop は待ち受けをやめ、開いている接続をすべて閉じる（処理中のリクエストが終わるまで待つ）
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
	return nil
}
