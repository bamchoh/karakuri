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
}

// ClientDTO は接続先
type ClientDTO struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	ProtocolType string                 `json:"protocolType"`
	DisplayName  string                 `json:"displayName"`
	Settings     map[string]interface{} `json:"settings"`
	Connected    bool                   `json:"connected"`
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

// ClientResultValueDTO は応答の内容の 1 項目
type ClientResultValueDTO struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ClientSnapshotDTO はプロジェクトに保存する接続先
type ClientSnapshotDTO struct {
	Name         string                 `json:"name"`
	ProtocolType string                 `json:"protocolType"`
	Settings     map[string]interface{} `json:"settings"`
}
