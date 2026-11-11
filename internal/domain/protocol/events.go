package protocol

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CommunicationEventEmitter は通信イベントを発行するインターフェース
type CommunicationEventEmitter interface {
	EmitRx()
	EmitTx()
	EmitConnection(count int)
}

// WailsEventEmitter はWailsランタイムを使用したイベントエミッター
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
