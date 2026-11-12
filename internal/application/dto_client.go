package application

// === クライアント（外部の機器へつなぐ側）の DTO ===

// ClientProtocolDTO はクライアントとして使えるプロトコル
type ClientProtocolDTO struct {
	ProtocolType     string             `json:"protocolType"`
	DisplayName      string             `json:"displayName"`
	ConnectionFields []FieldDTO         `json:"connectionFields"`
	Commands         []ClientCommandDTO `json:"commands"`
}

// ClientCommandDTO は、項目を入れて組み立てられるコマンド（Modbus ならファンクションコード）
type ClientCommandDTO struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"displayName"`
	Fields      []FieldDTO `json:"fields"`
	Pollable    bool       `json:"pollable"` // ポーリング（周期的な読み取り）に使えるか
}

// ClientDTO は接続先
type ClientDTO struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	ProtocolType string                 `json:"protocolType"`
	DisplayName  string                 `json:"displayName"`
	Settings     map[string]interface{} `json:"settings"`
	Connected    bool                   `json:"connected"`
	Polling      bool                   `json:"polling"` // ポーリング中か
}

// ClientCommandLogDTO は送ったコマンドの履歴の 1 件。バイト列は "03 00 0A" の形の 16 進
type ClientCommandLogDTO struct {
	Seq        int64                  `json:"seq"`
	ClientID   string                 `json:"clientId"`
	ClientName string                 `json:"clientName"`
	At         string                 `json:"at"`        // RFC 3339
	Request    string                 `json:"request"`   // 送るよう指定したリクエスト（Modbus なら PDU）
	Sent       string                 `json:"sent"`      // 実際に送ったバイト列（Modbus TCP なら MBAP ヘッダーを含む）
	Received   string                 `json:"received"`  // 受け取ったバイト列
	ElapsedMs  float64                `json:"elapsedMs"` // 送ってから応答まで
	Status     string                 `json:"status"`    // "ok" | "exception" | "timeout" | "error"
	Error      string                 `json:"error,omitempty"`
	Summary    string                 `json:"summary"`
	Values     []ClientResultValueDTO `json:"values"`
}

// ClientResultValueDTO は応答の内容の 1 項目。読み取った値は bit / word にも元の値を入れる（画面で表示形式を変えるため）
type ClientResultValueDTO struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Bit   *bool  `json:"bit,omitempty"`
	Word  *int   `json:"word,omitempty"`
}

// ClientPollingDTO は接続先のポーリング（周期的な読み取り）の状態
type ClientPollingDTO struct {
	IntervalMs  int                 `json:"intervalMs"`
	Running     bool                `json:"running"`
	Cycles      int64               `json:"cycles"`      // 開始してから読み取りを回った回数
	LastCycleMs float64             `json:"lastCycleMs"` // 最後の 1 周にかかった時間（周期より長ければ、周期どおりに回れていない）
	Items       []ClientPollItemDTO `json:"items"`
}

// ClientPollItemDTO はポーリングの読み取り項目
type ClientPollItemDTO struct {
	ID        string                 `json:"id"`
	CommandID string                 `json:"commandId"`
	Params    map[string]interface{} `json:"params"`
	Format    string                 `json:"format"`  // 表示形式（画面が決める値。ホストは意味を持たない）
	Request   string                 `json:"request"` // 送るリクエスト（16 進）
	Summary   string                 `json:"summary"`
	// 最後に送った結果。status は "" (まだ送っていない) / "ok" / "exception" / "timeout" / "error" / "skipped"（前の項目が失敗したので、その周期は送らなかった）
	Status    string  `json:"status"`
	Error     string  `json:"error,omitempty"`
	At        string  `json:"at,omitempty"` // 最後に送った時刻（RFC 3339）
	ElapsedMs float64 `json:"elapsedMs"`
	// 最後に読めた値。読めなかったときも残す（status が ok 以外なら古い値）
	Values     []ClientResultValueDTO `json:"values"`
	ValuesAt   string                 `json:"valuesAt,omitempty"`
	OKCount    int64                  `json:"okCount"`
	ErrorCount int64                  `json:"errorCount"` // ok 以外（skipped を含む）
}

// ClientSnapshotDTO はプロジェクトに保存する接続先
type ClientSnapshotDTO struct {
	Name         string                    `json:"name"`
	ProtocolType string                    `json:"protocolType"`
	Settings     map[string]interface{}    `json:"settings"`
	Polling      *ClientPollingSnapshotDTO `json:"polling,omitempty"`
}

// ClientPollingSnapshotDTO はプロジェクトに保存するポーリングの設定（ポーリング中かどうかは保存しない）
type ClientPollingSnapshotDTO struct {
	IntervalMs int                         `json:"intervalMs"`
	Items      []ClientPollItemSnapshotDTO `json:"items"`
}

// ClientPollItemSnapshotDTO はプロジェクトに保存する読み取り項目
type ClientPollItemSnapshotDTO struct {
	CommandID string                 `json:"commandId"`
	Params    map[string]interface{} `json:"params"`
	Format    string                 `json:"format,omitempty"`
}
