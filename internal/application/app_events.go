package application

import (
	"context"
	"sync"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// AppStateEmitter はアプリケーション状態変化イベントを発行するインターフェース
type AppStateEmitter interface {
	EmitServerChanged(instances []ServerInstanceDTO, protocols []ProtocolInfoDTO)
	EmitVariablesChanged(variables []*VariableDTO)
	EmitScriptsChanged(scripts []*ScriptDTO)
	EmitConsoleLogAdded(entry ConsoleLogDTO)
}

// WailsAppStateEmitter はWailsランタイムを使用したAppStateEmitter実装
type WailsAppStateEmitter struct {
	ctx context.Context
}

// NewWailsAppStateEmitter は新しいWailsAppStateEmitterを作成する
func NewWailsAppStateEmitter(ctx context.Context) *WailsAppStateEmitter {
	return &WailsAppStateEmitter{ctx: ctx}
}

// EmitServerChanged はサーバー状態変化イベントを発行する
func (e *WailsAppStateEmitter) EmitServerChanged(instances []ServerInstanceDTO, protocols []ProtocolInfoDTO) {
	if e.ctx == nil {
		return
	}
	runtime.EventsEmit(e.ctx, "plc:server-changed", instances)
	runtime.EventsEmit(e.ctx, "plc:protocols-changed", protocols)
}

// EmitVariablesChanged は変数一覧変化イベントを発行する
func (e *WailsAppStateEmitter) EmitVariablesChanged(variables []*VariableDTO) {
	if e.ctx == nil {
		return
	}
	runtime.EventsEmit(e.ctx, "plc:variables-changed", variables)
}

// EmitScriptsChanged はスクリプト一覧変化イベントを発行する
func (e *WailsAppStateEmitter) EmitScriptsChanged(scripts []*ScriptDTO) {
	if e.ctx == nil {
		return
	}
	runtime.EventsEmit(e.ctx, "plc:scripts-changed", scripts)
}

// EmitConsoleLogAdded はコンソールログ追加イベントを発行する
func (e *WailsAppStateEmitter) EmitConsoleLogAdded(entry ConsoleLogDTO) {
	if e.ctx == nil {
		return
	}
	runtime.EventsEmit(e.ctx, "plc:console-log-added", entry)
}

// variableChangeListener は VariableStore の変更を受け取りスロットルしてイベント発行するリスナー。
//
// 動作: leading fire + 定間隔 trailing fire
//   - 最初の変更で即座に発火（leading edge）し、interval タイマーを開始。
//   - タイマー動作中に変更があれば pending フラグのみ更新。
//   - タイマー満了時に pending があれば発火してタイマーを再起動（定間隔継続）。
//   - pending がなければタイマーを停止（次の変更で再び leading fire）。
//
// 例: 100ms 間隔スクリプト / interval=300ms → T=0,300,600,... で発火（二重発火なし）
type variableChangeListener struct {
	emitFn   func()
	mu       sync.Mutex
	timer    *time.Timer
	pending  bool // interval 中に変更があったか
	interval time.Duration
}

// newVariableChangeListener は新しい variableChangeListener を作成する
func newVariableChangeListener(emitFn func(), interval time.Duration) *variableChangeListener {
	return &variableChangeListener{
		emitFn:   emitFn,
		interval: interval,
	}
}

// OnVariableChanged は変数変更通知を受け取り、スロットルして発行する。
func (l *variableChangeListener) OnVariableChanged(_ *variable.Variable, _ []variable.ProtocolMapping, _ string, _ interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.timer != nil {
		// interval 中: trailing 発火フラグをセット
		l.pending = true
		return
	}
	// Leading edge: 即座に発火
	go l.emitFn()
	l.pending = false
	l.timer = time.AfterFunc(l.interval, l.onTimer)
}

// onTimer はタイマー満了時に呼ばれる。pending があれば発火してタイマーを再起動する。
func (l *variableChangeListener) onTimer() {
	l.mu.Lock()
	needsFire := l.pending
	l.pending = false
	if needsFire {
		// 変更が続いているのでタイマーを再起動して定間隔を維持
		l.timer = time.AfterFunc(l.interval, l.onTimer)
	} else {
		// 変更が止まったのでタイマーを終了
		l.timer = nil
	}
	l.mu.Unlock()
	if needsFire {
		l.emitFn()
	}
}

var _ protocol.CommunicationEventEmitter = (*WailsEventEmitter)(nil)

// WailsEventEmitter は通信イベント（RX / TX ランプ・接続数）を Wails ランタイムで画面へ送る。
// protocol.CommunicationEventEmitter の実装
type WailsEventEmitter struct {
	ctx context.Context
}

// NewWailsEventEmitter は新しいWailsEventEmitterを作成する
func NewWailsEventEmitter(ctx context.Context) *WailsEventEmitter {
	return &WailsEventEmitter{ctx: ctx}
}

// EmitRx は受信イベントを発行する
func (e *WailsEventEmitter) EmitRx() {
	if e.ctx != nil {
		runtime.EventsEmit(e.ctx, "comm:rx", nil)
	}
}

// EmitTx は送信イベントを発行する
func (e *WailsEventEmitter) EmitTx() {
	if e.ctx != nil {
		runtime.EventsEmit(e.ctx, "comm:tx", nil)
	}
}

// EmitConnection は接続数変更イベントを発行する
func (e *WailsEventEmitter) EmitConnection(count int) {
	if e.ctx != nil {
		runtime.EventsEmit(e.ctx, "comm:connection", map[string]int{"count": count})
	}
}
