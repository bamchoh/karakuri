package protocol

import (
	"context"
	"time"
)

// ClientFactory は、外部の機器へつなぐクライアントを作るファクトリー（プロトコルの実装者が実装する）。
//
// ホストはプロトコル固有の値を持たない。接続先の設定項目・送れるコマンドの定義・リクエストの組み立て・
// 応答の解釈は、すべてプロトコルの実装が受け持つ。
type ClientFactory interface {
	// ProtocolType はクライアントのプロトコルの種類を返す
	ProtocolType() ProtocolType
	// DisplayName はプロトコルの表示名を返す
	DisplayName() string
	// ConnectionFields は接続先の設定項目（ホスト・ポート・タイムアウトなど）を返す
	ConnectionFields() []ConfigField
	// NormalizeSettings は接続先の設定を検証し、ない項目を既定値で埋めたものを返す
	NormalizeSettings(settings map[string]interface{}) (map[string]interface{}, error)
	// Commands は、項目を入れて組み立てられるコマンドの一覧を返す
	Commands() []CommandDefinition
	// BuildRequest は、コマンドと項目の値から送るリクエスト（Modbus なら PDU）を作る
	BuildRequest(commandID string, params map[string]interface{}) ([]byte, error)
	// DescribeRequest はリクエストの内容を 1 行の要約にする（CommandResult.Summary と同じもの）
	DescribeRequest(request []byte) string
	// NewClient は接続先の設定（NormalizeSettings 済み）からクライアントを作る。つなぐのは最初の送信のとき
	NewClient(settings map[string]interface{}) (Client, error)
}

// CommandDefinition は、組み立てて送れるコマンド（Modbus ならファンクションコード）の定義
type CommandDefinition struct {
	ID          string
	DisplayName string
	Fields      []ConfigField
	// Pollable は、周期的な読み取り（ポーリング）に使えるコマンドかどうか（読み取りのコマンドだけ true にする）
	Pollable bool
}

// Client は外部の機器へつなぐクライアント。Send は複数のゴルーチンから呼ばれてもよい（実装が 1 つずつ送る）
type Client interface {
	// Send はリクエストを送って応答を待つ。タイムアウト・接続の失敗・例外応答は結果の Status と Error で返す
	Send(ctx context.Context, request []byte) CommandResult
	// Connected は接続中かどうかを返す
	Connected() bool
	// Close は接続を閉じる。閉じたあとに Send すると、つなぎ直す
	Close() error
}

// CommandStatus はコマンドの結果の種類
type CommandStatus string

const (
	CommandOK        CommandStatus = "ok"        // 正常な応答
	CommandException CommandStatus = "exception" // 機器が例外応答を返した
	CommandTimeout   CommandStatus = "timeout"   // 時間内に応答がなかった
	CommandError     CommandStatus = "error"     // 接続できない・応答が壊れているなど
)

// CommandResult はコマンドを 1 回送った結果
type CommandResult struct {
	Sent     []byte        // 実際に送ったバイト列（Modbus TCP なら MBAP ヘッダーを含む）
	Received []byte        // 受け取ったバイト列（なければ空）
	Elapsed  time.Duration // 送ってから応答を受け取るまで（失敗したときは失敗までの時間）
	Status   CommandStatus
	Error    string        // Status が ok 以外のときの理由
	Summary  string        // リクエストの内容の要約（例: "FC03 保持レジスタの読み取り アドレス 0 から 10 個"）
	Values   []ResultValue // 応答の内容（読み取った値など）
}

// ResultValue は応答の内容の 1 項目。
// 読み取った値は、画面で表示形式（10 進・16 進など）を切り替えられるよう、Bit / Word にも元の値を入れる
type ResultValue struct {
	Label string
	Value string  // 表示用の文字列
	Bit   *bool   // ビットの値（ビットでなければ nil）
	Word  *uint16 // ワードの値（ワードでなければ nil）
}
