package rtu

import (
	"context"
	"errors"
	"time"
)

// errPortLost は、開いていたポートが読めなくなった（USB シリアル変換器を抜いたなど）ことを表す。
// ReadFrame はポートを閉じてからこれを返す。サーバーは reopenInterval おきに Reopen で開き直す。
//
// ライブラリ（go.bug.st/serial）は、通信エラー（フレーミング・パリティ）では読み取りを失敗にしないので、
// Read のエラーは「ポートが使えなくなった」として扱う。閉じたポートをそのまま読み直すと、すぐエラーが返り続けて
// CPU を使い切り、変換器を挿し直しても同じハンドルでは読めない。
var errPortLost = errors.New("serial port lost")

// defaultReopenInterval は、読めなくなったポートを開き直す間隔
const defaultReopenInterval = time.Second

type reopener interface {
	Reopen() error
}

// waitReopen は、ctx が取り消されるまで、interval おきにポートを開き直す。開けたら true、取り消されたら false。
func waitReopen(ctx context.Context, sm reopener, interval time.Duration) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(interval):
		}
		if err := sm.Reopen(); err == nil {
			return true
		}
	}
}
