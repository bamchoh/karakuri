package fins

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
)

// クライアントの、相手がふつうでない動きをしたときの確認。
// 相手は、テストが応答を決められるフェイク（UDP のソケット・TCP の待ち受け）にする。

// fakeUDPDevice は、受け取った FINS フレームごとに、respond が返すデータグラムを順に返す
func fakeUDPDevice(t *testing.T, respond func(request []byte) [][]byte) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			for _, resp := range respond(append([]byte(nil), buf[:n]...)) {
				_, _ = pc.WriteTo(resp, from)
			}
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

// replyTo は、リクエストに対する正常な応答（終了コード 0000 + data）を作る
func replyTo(request []byte, data ...byte) []byte {
	resp := parseHeader(request).reply().append(nil)
	resp = append(resp, request[headerLength], request[headerLength+1], 0x00, 0x00)
	return append(resp, data...)
}

// 前のリクエスト（タイムアウトしたもの）への遅れた応答は、SID が違うので読み捨てる
func TestClient_DiscardsLateResponse(t *testing.T) {
	port := fakeUDPDevice(t, func(req []byte) [][]byte {
		// 先に、SID が違う応答（別の値）を返し、そのあとで正しい応答を返す
		stale := replyTo(req, 0xDE, 0xAD)
		stale[9] = req[9] - 1
		return [][]byte{stale, replyTo(req, 0x12, 0x34)}
	})
	c := newClient(t, port, transportUDP)
	r := c.Send(context.Background(), unhex(t, "01 01 82 00 64 00 00 01"))
	if r.Status != protocol.CommandOK || len(r.Values) != 1 || *r.Values[0].Word != 0x1234 {
		t.Fatalf("遅れた応答を、今の応答として読んだ: %+v", r)
	}
	// 受け取ったバイト列は、正しい応答のもの
	if got := hexOf(r.Received); !strings.HasSuffix(got, "010100001234") {
		t.Errorf("受け取ったバイト列 = %s", got)
	}
	// 次のリクエストは SID が 1 つ進み、同じように読める
	r2 := c.Send(context.Background(), unhex(t, "01 01 82 00 64 00 00 01"))
	if r2.Status != protocol.CommandOK || r2.Sent[9] != r.Sent[9]+1 {
		t.Fatalf("次のリクエスト: %+v", r2)
	}
}

// 応答しなかった相手が応答するようになったら、つなぎ直して読める
func TestClient_ReconnectsAfterTimeout(t *testing.T) {
	var down atomic.Bool
	down.Store(true)
	port := fakeUDPDevice(t, func(req []byte) [][]byte {
		if down.Load() {
			return nil
		}
		return [][]byte{replyTo(req, 0x00, 0x2A)}
	})
	c := newClient(t, port, transportUDP)
	c.timeout = 100 * time.Millisecond
	req := unhex(t, "01 01 82 00 00 00 00 01")

	r := c.Send(context.Background(), req)
	if r.Status != protocol.CommandTimeout || c.Connected() {
		t.Fatalf("応答しない相手: %+v（接続中 = %v）", r, c.Connected())
	}
	if r.Elapsed < 90*time.Millisecond || !strings.Contains(r.Error, "100 ms") {
		t.Errorf("タイムアウトまでの時間 = %v、理由 = %q", r.Elapsed, r.Error)
	}

	down.Store(false)
	r = c.Send(context.Background(), req)
	if r.Status != protocol.CommandOK || *r.Values[0].Word != 42 || !c.Connected() {
		t.Fatalf("応答するようになったあと: %+v", r)
	}
}

// 応答の中身がおかしいとき: 終了コードが 0 以外なら異常終了、形が壊れていればエラー（接続を閉じる）
func TestClient_BadResponses(t *testing.T) {
	var mode atomic.Value
	mode.Store("")
	port := fakeUDPDevice(t, func(req []byte) [][]byte {
		switch mode.Load().(string) {
		case "endcode":
			resp := replyTo(req)
			resp[12], resp[13] = 0x11, 0x03 // 先頭アドレスが範囲外
			return [][]byte{resp}
		case "relay":
			// 中継のエラーを表すビット（最上位）と、CPU の異常を表すビット（下位バイトの上位 2 ビット）が立っているだけなら、正常
			resp := replyTo(req, 0x00, 0x07)
			resp[12], resp[13] = 0x80, 0x40
			return [][]byte{resp}
		case "short":
			return [][]byte{replyTo(req)[:12]}
		case "wrongcommand":
			resp := replyTo(req, 0x00, 0x01)
			resp[11] = 0x02 // 0102 の応答
			return [][]byte{resp}
		case "wrongsize":
			return [][]byte{replyTo(req, 0x00, 0x01, 0x00)} // 1 ワードを頼んだのに 3 バイト
		case "request":
			return [][]byte{req} // 応答ではなく、リクエストをそのまま返す
		}
		return [][]byte{replyTo(req, 0x00, 0x01)}
	})
	c := newClient(t, port, transportUDP)
	req := unhex(t, "01 01 82 00 00 00 00 01")
	send := func(m string) protocol.CommandResult {
		mode.Store(m)
		return c.Send(context.Background(), req)
	}

	if r := send("endcode"); r.Status != protocol.CommandException || !strings.Contains(r.Error, "0x1103") || !c.Connected() {
		t.Errorf("終了コードが 0 以外: %+v（接続中 = %v）", r, c.Connected())
	}
	if r := send("relay"); r.Status != protocol.CommandOK || *r.Values[0].Word != 7 {
		t.Errorf("中継・CPU の異常のビットだけが立っている応答: %+v", r)
	}
	for _, m := range []string{"short", "wrongcommand", "wrongsize", "request"} {
		r := send(m)
		if r.Status != protocol.CommandError || r.Error == "" {
			t.Errorf("%s: %+v", m, r)
		}
		if c.Connected() {
			t.Errorf("%s: 壊れた応答のあとも、接続を閉じていない", m)
		}
		// 次の送信で、つなぎ直して読める
		if r := send(""); r.Status != protocol.CommandOK {
			t.Errorf("%s のあとの送信: %+v", m, r)
		}
	}
}

// FINS/TCP: 相手がエラーを知らせてきたら、そのことを伝える。ノードアドレスのやりとりができなければ、つながらない
func TestClient_TCPErrors(t *testing.T) {
	// behave は、つながってきた相手ごとのふるまい
	serve := func(behave func(conn net.Conn)) int {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func() {
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
					behave(conn)
				}()
			}
		}()
		return ln.Addr().(*net.TCPAddr).Port
	}
	// handshake は、ノードアドレスのやりとりに正しく答える（クライアント 0x0B、サーバー 0x0A）
	handshake := func(conn net.Conn) bool {
		if _, _, err := readTCPMessage(conn); err != nil {
			return false
		}
		resp := appendTCPHeader(nil, tcpCmdNodeResponse, 0, 8)
		_, err := conn.Write(append(resp, 0, 0, 0, 0x0B, 0, 0, 0, 0x0A))
		return err == nil
	}
	req := unhex(t, "01 01 82 00 00 00 00 01")

	// やりとりのあと、FINS フレームに対してエラーを知らせて閉じる
	port := serve(func(conn net.Conn) {
		if !handshake(conn) {
			return
		}
		if _, _, err := readTCPMessage(conn); err != nil {
			return
		}
		_, _ = conn.Write(appendTCPHeader(nil, tcpCmdError, tcpErrUnsupported, 0))
	})
	c := newClient(t, port, transportTCP)
	r := c.Send(context.Background(), req)
	if r.Status != protocol.CommandError || !strings.Contains(r.Error, "FINS/TCP のエラー（エラーコード 3）") || c.Connected() {
		t.Errorf("エラーの通知: %+v（接続中 = %v）", r, c.Connected())
	}
	// 送ったフレームには、やりとりで決まったノードアドレスが入っている（宛先 0x0A、送信元 0x0B）
	if got := hexOf(r.Sent); !strings.HasPrefix(got, "46494E530000001A0000000200000000"+"800002000A00000B00") {
		t.Errorf("送ったバイト列 = %s", got)
	}
	// 受け取ったバイト列には、エラーの通知がそのまま入っている
	if got := hexOf(r.Received); got != "46494E53000000080000000300000003" {
		t.Errorf("受け取ったバイト列 = %s", got)
	}

	// ノードアドレスのやりとりに、エラーで答える相手にはつながらない
	port = serve(func(conn net.Conn) {
		if _, _, err := readTCPMessage(conn); err != nil {
			return
		}
		_, _ = conn.Write(appendTCPHeader(nil, tcpCmdError, tcpErrNotFINS, 0))
	})
	c = newClient(t, port, transportTCP)
	r = c.Send(context.Background(), req)
	if r.Status != protocol.CommandError || !strings.Contains(r.Error, "接続できませんでした") || c.Connected() {
		t.Errorf("やりとりの失敗: %+v", r)
	}

	// FINS/TCP ではないもの（"FINS" で始まらない）を返す相手
	port = serve(func(conn net.Conn) {
		buf := make([]byte, 64)
		_, _ = conn.Read(buf)
		_, _ = conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
	})
	c = newClient(t, port, transportTCP)
	r = c.Send(context.Background(), req)
	if r.Status != protocol.CommandError || c.Connected() {
		t.Errorf("FINS/TCP ではない相手: %+v", r)
	}

	// つながるが、何も返さない相手はタイムアウト
	port = serve(func(conn net.Conn) {
		buf := make([]byte, 64)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	})
	c = newClient(t, port, transportTCP)
	c.timeout = 100 * time.Millisecond
	r = c.Send(context.Background(), req)
	if r.Status != protocol.CommandTimeout || c.Connected() {
		t.Errorf("何も返さない相手: %+v", r)
	}
}

// 送れないリクエスト・取り消し
func TestClient_InvalidRequestAndCancel(t *testing.T) {
	port := fakeUDPDevice(t, func(req []byte) [][]byte { return nil })
	c := newClient(t, port, transportUDP)

	// コマンドコードに足りないリクエストは、送らない
	if r := c.Send(context.Background(), []byte{0x01}); r.Status != protocol.CommandError || len(r.Sent) != 0 {
		t.Errorf("短すぎるリクエスト: %+v", r)
	}
	if r := c.Send(context.Background(), make([]byte, maxFrameLength)); r.Status != protocol.CommandError || len(r.Sent) != 0 {
		t.Errorf("長すぎるリクエスト: %+v", r)
	}

	// 待っている間に取り消されたら、タイムアウトを待たずに戻る
	c.timeout = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	r := c.Send(ctx, unhex(t, "01 01 82 00 00 00 00 01"))
	if r.Status != protocol.CommandError || !strings.Contains(r.Error, "取り消されました") {
		t.Errorf("取り消し: %+v", r)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("取り消してから戻るまで %v かかった", elapsed)
	}
}
