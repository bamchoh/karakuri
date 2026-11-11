package modbus

import (
	"sync"
	"time"

	"karakuri/internal/domain/protocol"
)

// SessionManager はアクティブセッション方式で接続数を管理する
// Modbus TCPなど、正確な接続追跡ができないプロトコル向け
// UnitIDごとにセッションを追跡し、複数クライアントを識別する
type SessionManager struct {
	mu       sync.Mutex
	sessions map[uint8]time.Time // UnitID -> 最終アクティビティ時刻
	timeout  time.Duration
	emitter  protocol.CommunicationEventEmitter
	stopCh   chan struct{}
	running  bool
}

// NewSessionManager は新しいSessionManagerを作成する
func NewSessionManager(timeout time.Duration, emitter protocol.CommunicationEventEmitter) *SessionManager {
	return &SessionManager{
		sessions: make(map[uint8]time.Time),
		timeout:  timeout,
		emitter:  emitter,
		stopCh:   make(chan struct{}),
	}
}

// SetEmitter はイベントエミッターを設定する
func (m *SessionManager) SetEmitter(emitter protocol.CommunicationEventEmitter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitter = emitter
}

// RecordActivity は通信アクティビティを記録する（UnitID指定なし、デフォルトUnitID 1）
func (m *SessionManager) RecordActivity() {
	m.RecordActivityWithUnitID(1)
}

// RecordActivityWithUnitID は指定されたUnitIDの通信アクティビティを記録する
func (m *SessionManager) RecordActivityWithUnitID(unitID uint8) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prevCount := len(m.sessions)
	m.sessions[unitID] = time.Now()
	newCount := len(m.sessions)

	if newCount != prevCount && m.emitter != nil {
		m.emitter.EmitConnection(newCount)
	}
}

// Start はタイムアウト監視を開始する
func (m *SessionManager) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.stopCh = make(chan struct{})
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.checkTimeout()
			}
		}
	}()
}

// Stop はタイムアウト監視を停止する
func (m *SessionManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		close(m.stopCh)
		m.running = false
	}

	// 接続をリセット
	if len(m.sessions) > 0 {
		m.sessions = make(map[uint8]time.Time)
		if m.emitter != nil {
			m.emitter.EmitConnection(0)
		}
	}
}

// checkTimeout はタイムアウトをチェックし、期限切れのセッションを削除する
func (m *SessionManager) checkTimeout() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	prevCount := len(m.sessions)

	// 期限切れのセッションを削除
	for unitID, lastActivity := range m.sessions {
		if now.Sub(lastActivity) > m.timeout {
			delete(m.sessions, unitID)
		}
	}

	newCount := len(m.sessions)
	if newCount != prevCount && m.emitter != nil {
		m.emitter.EmitConnection(newCount)
	}
}

// GetActiveCount は現在のアクティブ接続数を返す
func (m *SessionManager) GetActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}
