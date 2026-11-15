package rtu

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// RTUServer はModbus RTUサーバーを表す
type RTUServer struct {
	mu        sync.Mutex
	serial    *SerialManager
	processor *Processor
	running   bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	reopenInterval time.Duration // ポートが使えなくなったときに、開き直す間隔
}

// NewRTUServer は新しいRTUServerを作成する
func NewRTUServer(config SerialConfig, handler RequestHandler) *RTUServer {
	return newRTUServer(config, handler, OpenSerialPort)
}

// newRTUServer は、ポートを open で開く RTUServer を作る（テストではフェイクのポートを開く）
func newRTUServer(config SerialConfig, handler RequestHandler, open PortOpener) *RTUServer {
	return &RTUServer{
		serial:         newSerialManager(config, open),
		processor:      NewProcessor(handler),
		reopenInterval: defaultReopenInterval,
	}
}

// Start はサーバーを起動する
func (s *RTUServer) Start() error {
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
func (s *RTUServer) Stop() error {
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
func (s *RTUServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *RTUServer) mainLoop() {
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
func (s *RTUServer) reconnect(cause error) {
	name := s.serial.PortName()
	log.Printf("[Modbus RTU] %s が使えなくなりました。%v おきに開き直します（error: %v）", name, s.reopenInterval, cause)
	if waitReopen(s.ctx, s.serial, s.reopenInterval) {
		log.Printf("[Modbus RTU] %s を開き直しました", name)
	}
}

func (s *RTUServer) processNextRequest() {
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
	req, err := ParseRequest(frame)
	if err != nil {
		log.Printf("RTU: failed to parse request: %v", err)
		return
	}

	// リクエストを処理
	response := s.processor.Process(req)
	if response == nil {
		// UnitIDが無効な場合は応答しない
		return
	}

	// 応答前に3.5文字時間待機
	time.Sleep(s.serial.SilenceTime())

	// レスポンスを送信
	if err := s.serial.Write(response); err != nil {
		log.Printf("RTU: failed to write response: %v", err)
	}
}
