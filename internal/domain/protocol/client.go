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
	// WordValues は、読み取った値を ResultValue.Bit / Word でも返すコマンドかどうか
	// （true なら、画面で値の表示形式（10 進・16 進など）を選べる）
	WordValues bool
}

// RequestHelp は、リクエストを 16 進で直接入れる欄に出す説明
type RequestHelp struct {
	Description string // 何を入れるか（Modbus なら「PDU（ファンクションコード + データ）」）
	Example     string // 入力例（16 進）
}

// RequestHelpProvider は、16 進で入れるリクエストの説明を返すファクトリー（任意で実装する）
type RequestHelpProvider interface {
	RequestHelp() RequestHelp
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
	Sent     []byte        // 実際に送ったバイト列（Modbus TCP なら MBAP ヘッダーを含む。OPC UA はサービスのリクエスト本体）
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

// MappableClientFactory は、変数を外部の機器のアドレスにマッピングできるクライアントのファクトリー（任意で実装する）。
//
// ホストは、マッピングされた範囲を読む・書くリクエストをこのインターフェースで作り、
// 読み取りの結果は CommandResult.Values の Bit / Word（アドレス順）から取り出す。
type MappableClientFactory interface {
	// MappingAreas はマッピングできるメモリエリアを返す。機器の側で書き込めないエリアは ReadOnly にする（読むだけにする）
	MappingAreas() []MemoryArea
	// MaxReadCount / MaxWriteCount は、1 回のリクエストで読める・書ける個数（ワードかビット）
	MaxReadCount(area string) int
	MaxWriteCount(area string) int
	// BuildReadRequest は area の address から count 個を読むリクエストを作る
	BuildReadRequest(area string, address uint32, count int) ([]byte, error)
	// BuildWriteRequest は area の address からワード（ワードのエリア）かビット（ビットのエリア）を書くリクエストを作る
	BuildWriteRequest(area string, address uint32, words []uint16, bits []bool) ([]byte, error)
}

// --- サブスクリプション（値の変化の通知） ---
//
// ポーリングはホストが周期ごとに読むが、サブスクリプションは機器の側が値の変化を知らせる（OPC UA など）。
// 対応するプロトコルは、ファクトリーで SubscriptionClientFactory を、クライアントで SubscribingClient を実装する。

// SubscriptionClientFactory は、サブスクリプションに対応するクライアントのファクトリー（任意で実装する）
type SubscriptionClientFactory interface {
	// MonitoredItemFields は、監視項目を追加するときの入力項目を返す
	MonitoredItemFields() []ConfigField
	// ExpandMonitoredItems は入力を検証し、監視項目にする。1 回の入力から、いくつも作ってよい
	// （OPC UA は NodeId を 1 行に 1 つ入れると、行の数だけ作る）
	ExpandMonitoredItems(params map[string]interface{}) ([]MonitoredItemSpec, error)
}

// MonitoredItemSpec は監視項目 1 つの内容（ExpandMonitoredItems が返す）
type MonitoredItemSpec struct {
	Params  map[string]interface{} // この項目だけを表す入力（保存し、SubscriptionRequest で渡す）
	Summary string                 // 一覧に出す 1 行の要約
}

// SubscribingClient は、サブスクリプションに対応するクライアント（SubscriptionClientFactory の NewClient が返す Client が実装する）
type SubscribingClient interface {
	// RunSubscription は監視を始め、ctx が取り消されるまで戻らない。
	// 値の通知と状態の変化は onEvent で知らせる（1 つずつ呼ぶ。同時には呼ばない）。
	// つながらない・切れたときは、状態を知らせてから、自分でつなぎ直す。
	// Send の接続とは別に動く（コマンドのタイムアウトで監視が切れないようにする）
	RunSubscription(ctx context.Context, req SubscriptionRequest, onEvent func(SubscriptionEvent))
}

// SubscriptionRequest は、監視する内容
type SubscriptionRequest struct {
	Interval time.Duration // 機器が通知をまとめて送る間隔
	Items    []MonitoredItemRequest
}

// MonitoredItemRequest は監視項目 1 つ。ID はホストが付けた ID（SubscriptionEvent.ItemID で返す）
type MonitoredItemRequest struct {
	ID     string
	Params map[string]interface{}
}

// SubscriptionEvent は、サブスクリプションからの知らせ。
//
// ItemID が空なら、監視全体の状態: Status が ok なら監視中、それ以外ならつながっていない（Error に理由。つなぎ直している）。
// ItemID があれば、その項目の知らせ: ok なら Values が新しい値。exception なら、機器がその項目を断った（Error に理由）
type SubscriptionEvent struct {
	ItemID string
	Status CommandStatus
	Error  string
	Values []ResultValue
}

// --- アドレスの参照（機器が持つアドレスの一覧から選ぶ） ---

// BrowsableClient は、機器が持つアドレス（OPC UA ならノード）を一覧できるクライアント（任意で実装する）。
// 画面は、ConfigField.Browse を指定した項目に「参照」ボタンを出し、ツリーから選ばせる
type BrowsableClient interface {
	// Browse は、parent の子を返す。parent が空なら、いちばん上の一覧を返す。
	// 通信の失敗は error で返す（Send と同じく、タイムアウトしたら接続を閉じる）
	Browse(ctx context.Context, parent string) ([]BrowseEntry, error)
}

// BrowseEntry は、アドレスの一覧の 1 項目
type BrowseEntry struct {
	Address     string // このアドレスを表す文字列（Browse の parent に渡せる。OPC UA なら NodeId）
	Name        string // 表示する名前
	Kind        string // 種類の表示（OPC UA なら "Variable"、"Object" など）
	Detail      string // 補足の表示（OPC UA の変数ならデータ型）
	HasChildren bool   // 子がある（ツリーで開ける）
	Selectable  bool   // 入力欄に入れられる（値を持つアドレス）
	// Fields は、選んだときに入力欄に入れる内容。キーは項目の名前（ConfigField.Name）。
	// 選ぶのに使った項目のほかに、同じフォームにある項目（書き込みのデータ型など）にも入れる。
	// 項目の名前がなければ Address を入れる
	Fields map[string]string
}
