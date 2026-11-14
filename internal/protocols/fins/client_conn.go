package fins

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

// Client は FINS のクライアント。接続は最初の Send でつなぎ、失敗・タイムアウトしたら閉じる（次の Send でつなぎ直す）。
// FINS/UDP も、相手を決めたソケットを「接続」として扱う
type Client struct {
	address   string
	transport string
	destNode  byte // 0 なら自動
	srcNode   byte // 0 なら自動
	timeout   time.Duration

	mu        sync.Mutex // Send を 1 つずつにする。以下を守る
	conn      net.Conn
	da1, sa1  byte // つないだときに決めたノードアドレス
	sid       byte
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

// lastOctet は、アドレスの IPv4 の最後の数字を返す（IPv4 でなければ 0）
func lastOctet(addr net.Addr) byte {
	var ip net.IP
	switch a := addr.(type) {
	case *net.UDPAddr:
		ip = a.IP
	case *net.TCPAddr:
		ip = a.IP
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[3]
	}
	return 0
}

// connectLocked はつなぎ、ノードアドレスを決める。FINS/TCP は、ノードアドレスを決めるやりとりもここで行う
func (c *Client) connectLocked(ctx context.Context, deadline time.Time) error {
	dialCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(dialCtx, c.transport, c.address)
	if err != nil {
		return err
	}
	da1, sa1 := c.destNode, c.srcNode

	if c.transport == transportTCP {
		_ = conn.SetDeadline(deadline)
		req := appendTCPHeader(nil, tcpCmdNodeRequest, 0, 4)
		req = append(req, 0, 0, 0, c.srcNode)
		if _, err := conn.Write(req); err != nil {
			_ = conn.Close()
			return err
		}
		msg, _, err := readTCPMessage(conn)
		if err != nil {
			_ = conn.Close()
			return err
		}
		if msg.Cmd != tcpCmdNodeResponse || len(msg.Body) != 8 {
			_ = conn.Close()
			return fmt.Errorf("ノードアドレスのやりとりに失敗しました（FINS/TCP コマンド %d、エラーコード %d）", msg.Cmd, msg.ErrCode)
		}
		sa1 = msg.Body[3]
		if da1 == 0 {
			da1 = msg.Body[7]
		}
	} else {
		if da1 == 0 {
			da1 = lastOctet(conn.RemoteAddr())
		}
		if sa1 == 0 {
			sa1 = lastOctet(conn.LocalAddr())
		}
	}

	c.conn, c.da1, c.sa1 = conn, da1, sa1
	c.connected.Store(true)
	return nil
}

// Send は、リクエスト（コマンドコード + パラメーター）を送って応答を待つ
func (c *Client) Send(ctx context.Context, request []byte) protocol.CommandResult {
	result := protocol.CommandResult{Summary: describeRequest(request)}
	if len(request) < 2 || len(request) > maxFrameLength-headerLength {
		result.Status = protocol.CommandError
		result.Error = fmt.Sprintf("リクエストは 2〜%d バイトにしてください（%d バイトあります）", maxFrameLength-headerLength, len(request))
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
		if err := c.connectLocked(ctx, deadline); err != nil {
			if isTimeout(err) {
				return fail(protocol.CommandTimeout, fmt.Sprintf("%s に接続できませんでした（タイムアウト）", c.address))
			}
			return fail(protocol.CommandError, fmt.Sprintf("%s に接続できませんでした: %v", c.address, err))
		}
	}
	conn := c.conn

	// ctx が取り消されたら、待っている読み書きをすぐに終わらせる
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	_ = conn.SetDeadline(deadline)

	c.sid++
	sid := c.sid
	frame := make([]byte, 0, tcpHeaderLength+headerLength+len(request))
	if c.transport == transportTCP {
		frame = appendTCPHeader(frame, tcpCmdFrame, 0, headerLength+len(request))
	}
	frame = header{ICF: 0x80, GCT: 0x02, DA1: c.da1, SA1: c.sa1, SID: sid}.append(frame)
	frame = append(frame, request...)
	result.Sent = frame

	if _, err := conn.Write(frame); err != nil {
		if isTimeout(err) {
			return fail(protocol.CommandTimeout, "送信がタイムアウトしました")
		}
		return fail(protocol.CommandError, fmt.Sprintf("送信できませんでした: %v", err))
	}

	for {
		raw, fins, err := c.receive(conn)
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
		if len(fins) >= headerLength && fins[9] != sid {
			// 前のリクエスト（タイムアウトしたもの）への遅れた応答。読み捨てて、待ち続ける
			continue
		}
		result.Received = raw
		result.Elapsed = time.Since(start)
		status, values, msg := decodeResponse(request, fins)
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

// receive は応答を 1 つ受け取る。raw は受け取ったバイト列そのまま、fins はその中の FINS フレーム
func (c *Client) receive(conn net.Conn) (raw, fins []byte, err error) {
	if c.transport != transportTCP {
		buf := make([]byte, 65535)
		n, err := conn.Read(buf)
		if err != nil {
			return nil, nil, err
		}
		return buf[:n], buf[:n], nil
	}
	msg, _, err := readTCPMessage(conn)
	if err != nil {
		return nil, nil, err
	}
	switch msg.Cmd {
	case tcpCmdFrame:
		return msg.Raw, msg.Body, nil
	case tcpCmdError:
		return msg.Raw, nil, fmt.Errorf("PLC が FINS/TCP のエラー（エラーコード %d）を返しました", msg.ErrCode)
	}
	return msg.Raw, nil, fmt.Errorf("FINS/TCP の不明なコマンド（%d）を受け取りました", msg.Cmd)
}
