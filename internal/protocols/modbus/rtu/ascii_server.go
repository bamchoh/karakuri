package rtu

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// ASCIIServer はModbus ASCIIサーバーを表す
type ASCIIServer struct {
	mu        sync.Mutex
	serial    *ASCIISerialManager
	processor *Processor
	running   bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	reopenInterval time.Duration // ポートが使えなくなったときに、開き直す間隔
}

// NewASCIIServer は新しいASCIIServerを作成する
func NewASCIIServer(config SerialConfig, handler RequestHandler) *ASCIIServer {
	return newASCIIServer(config, handler, OpenSerialPort)
}

// newASCIIServer は、ポートを open で開く ASCIIServer を作る（テストではフェイクのポートを開く）
func newASCIIServer(config SerialConfig, handler RequestHandler, open PortOpener) *ASCIIServer {
	return &ASCIIServer{
		serial:         newASCIISerialManager(config, open),
		processor:      NewProcessor(handler),
		reopenInterval: defaultReopenInterval,
	}
}

// Start はサーバーを起動する
func (s *ASCIIServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server is already running")
	}

	if err := s.serial.Open(); err != nil {
		return err
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.running = true

	s.wg.Add(1)
	go s.mainLoop()

	return nil
}

// Stop はサーバーを停止する
func (s *ASCIIServer) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.cancel()
	s.running = false
	s.mu.Unlock()

	// シリアルポートを閉じてReadFrameをアンブロックする
	s.serial.Close()

	// ゴルーチンの終了を待つ
	s.wg.Wait()

	return nil
}

// IsRunning はサーバーが実行中かどうかを返す
func (s *ASCIIServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *ASCIIServer) mainLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			s.processNextRequest()
		}
	}
}

// reconnect は、使えなくなったポートを、開けるか Stop されるまで reopenInterval おきに開き直す
func (s *ASCIIServer) reconnect(cause error) {
	name := s.serial.PortName()
	log.Printf("[Modbus ASCII] %s が使えなくなりました。%v おきに開き直します（error: %v）", name, s.reopenInterval, cause)
	if waitReopen(s.ctx, s.serial, s.reopenInterval) {
		log.Printf("[Modbus ASCII] %s を開き直しました", name)
	}
}

func (s *ASCIIServer) processNextRequest() {
	// フレームを読み取る
	frame, err := s.serial.ReadFrame()
	if err != nil {
		if errors.Is(err, errPortLost) {
			s.reconnect(err)
		}
		return
	}

	if len(frame) == 0 {
		return
	}

	// リクエストを解析
	req, err := ParseASCIIRequest(frame)
	if err != nil {
		log.Printf("ASCII: failed to parse request: %v", err)
		return
	}

	// リクエストを処理（UnitID が無効な場合は応答しない）
	response := s.processor.ProcessASCII(req)
	if response == nil {
		return
	}

	// レスポンスを送信
	if err := s.serial.Write(response); err != nil {
		log.Printf("ASCII: failed to write response: %v", err)
	}
}
