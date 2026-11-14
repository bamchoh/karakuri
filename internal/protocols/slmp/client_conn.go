package slmp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"karakuri/internal/domain/protocol"
)

var _ protocol.Client = (*Client)(nil)

// Client は SLMP のクライアント。接続は最初の Send でつなぎ、失敗・タイムアウトしたら閉じる（次の Send でつなぎ直す）。
// UDP も、相手を決めたソケットを「接続」として扱う。
//
// 3E フレームにはシリアル番号がないので、遅れて届いた応答を見分けられない。
// タイムアウトしたら接続を閉じるのは、そのため（UDP も、つなぎ直すと自分のポートが変わるので、前の応答は届かない）
type Client struct {
	address   string
	transport string
	kind      byte // サブヘッダーの 1 バイト目（3E か 4E）
	route     [routeLength]byte
	timer     uint16
	timeout   time.Duration

	mu        sync.Mutex // Send を 1 つずつにする。以下を守る
	conn      net.Conn
	serial    uint16
	connected atomic.Bool
}

// Connected は接続中かどうかを返す
func (c *Client) Connected() bool {
	return c.connected.Load()
}

// Close は接続を閉じる
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
	return nil
}

func (c *Client) closeLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.connected.Store(false)
}

func isTimeout(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// maxRequestLength は、リクエスト（コマンド + サブコマンド + データ）の最大のバイト数（データ長の欄に、監視タイマの 2 バイトも入る）
const maxRequestLength = maxDataLength - 2

// Send は、リクエスト（コマンド + サブコマンド + データ）を送って応答を待つ
func (c *Client) Send(ctx context.Context, request []byte) protocol.CommandResult {
	result := protocol.CommandResult{Summary: describeRequest(request)}
	if len(request) < 4 || len(request) > maxRequestLength {
		result.Status = protocol.CommandError
		result.Error = fmt.Sprintf("リクエストは 4〜%d バイトにしてください（%d バイトあります）", maxRequestLength, len(request))
		return result
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	deadline := start.Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	fail := func(status protocol.CommandStatus, msg string) protocol.CommandResult {
		c.closeLocked()
		result.Status = status
		result.Error = msg
		result.Elapsed = time.Since(start)
		return result
	}

	if c.conn == nil {
		dialCtx, cancel := context.WithDeadline(ctx, deadline)
		var d net.Dialer
		conn, err := d.DialContext(dialCtx, c.transport, c.address)
		cancel()
		if err != nil {
			if isTimeout(err) {
				return fail(protocol.CommandTimeout, fmt.Sprintf("%s に接続できませんでした（タイムアウト）", c.address))
			}
			return fail(protocol.CommandError, fmt.Sprintf("%s に接続できませんでした: %v", c.address, err))
		}
		c.conn = conn
		c.connected.Store(true)
	}
	conn := c.conn

	// ctx が取り消されたら、待っている読み書きをすぐに終わらせる
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	_ = conn.SetDeadline(deadline)

	c.serial++
	serial := c.serial
	body := make([]byte, 0, 2+len(request))
	body = append(body, byte(c.timer), byte(c.timer>>8))
	body = append(body, request...)
	sent := appendFrame(make([]byte, 0, 16+len(body)), c.kind, serial, c.route, body)
	result.Sent = sent

	if _, err := conn.Write(sent); err != nil {
		if isTimeout(err) {
			return fail(protocol.CommandTimeout, "送信がタイムアウトしました")
		}
		return fail(protocol.CommandError, fmt.Sprintf("送信できませんでした: %v", err))
	}

	for {
		raw, err := c.receive(conn)
		if err != nil {
			if ctx.Err() != nil {
				return fail(protocol.CommandError, "取り消されました")
			}
			if isTimeout(err) {
				return fail(protocol.CommandTimeout, fmt.Sprintf("%d ms 以内に応答がありませんでした", c.timeout.Milliseconds()))
			}
			result.Received = raw
			return fail(protocol.CommandError, fmt.Sprintf("応答を受け取れませんでした: %v", err))
		}
		resp, err := parseFrame(raw)
		if err != nil {
			result.Received = raw
			return fail(protocol.CommandError, fmt.Sprintf("応答を読めません: %v", err))
		}
		if resp.is4E() && c.kind == sub4ERequest && resp.Serial != serial {
			// 前のリクエスト（タイムアウトしたもの）への遅れた応答。読み捨てて、待ち続ける
			continue
		}
		result.Received = raw
		result.Elapsed = time.Since(start)
		status, values, msg := decodeResponse(request, resp)
		if status != protocol.CommandError && resp.is4E() != (c.kind == sub4ERequest) {
			status, values, msg = protocol.CommandError, nil, "応答のフレームの種類（3E / 4E）がリクエストと違います"
		}
		result.Status = status
		result.Values = values
		result.Error = msg
		if status == protocol.CommandError {
			// 応答の中身が壊れているときは、続きのバイトが残っているかもしれないので閉じる
			c.closeLocked()
		}
		return result
	}
}

// receive は応答のフレームを 1 つ受け取り、そのバイト列を返す
func (c *Client) receive(conn net.Conn) ([]byte, error) {
	if c.transport != transportTCP {
		buf := make([]byte, 65535)
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		return buf[:n], nil
	}
	return readFrame(conn)
}
