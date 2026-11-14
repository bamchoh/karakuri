package application

import (
	"karakuri/internal/domain/protocol"
	"karakuri/internal/infrastructure/applog"
)

// アプリ全体のログ（サーバーの起動・停止、クライアントの通信エラーなど）。
// 画面のどのページからも見られるログの欄に出す。スクリプトのコンソールとは別。

// ログの出どころ（サーバーとクライアント以外）
const (
	logSourceProject = "プロジェクト"
)

// AppLogDTO はアプリ全体のログの 1 件
type AppLogDTO struct {
	ID      int64  `json:"id"`      // 追加した順の番号
	At      int64  `json:"at"`      // Unix ミリ秒
	Level   string `json:"level"`   // "info" | "warn" | "error"
	Source  string `json:"source"`  // 出どころ（"Modbus TCP"、"接続先: PLC-A"、"プロジェクト" など）
	Target  string `json:"target"`  // 関係するものの場所（"server:<プロトコルタイプ>" / "client:<ID>" / "script:<ID>"）。なければ空
	Message string `json:"message"` // 内容
}

func appLogToDTO(e applog.Entry) AppLogDTO {
	return AppLogDTO{ID: e.ID, At: e.At.UnixMilli(), Level: string(e.Level), Source: e.Source, Target: e.Target, Message: e.Message}
}

// AppLog は、アプリ全体のログを返す（ホストのほかの部分（app.go の HTTP API など）がログを足すのに使う）
func (s *PLCService) AppLog() *applog.Log {
	return s.appLog
}

// GetAppLogs は、持っているログを古い順に返す
func (s *PLCService) GetAppLogs() []AppLogDTO {
	entries := s.appLog.Entries()
	result := make([]AppLogDTO, len(entries))
	for i, e := range entries {
		result[i] = appLogToDTO(e)
	}
	return result
}

// ClearAppLogs は、持っているログをすべて消す
func (s *PLCService) ClearAppLogs() {
	s.appLog.Clear()
	s.appLogMu.Lock()
	onCleared := s.onAppLogCleared
	s.appLogMu.Unlock()
	if onCleared != nil {
		onCleared()
	}
}

// SetAppLogCallbacks は、ログを追加するたびに呼ぶ関数と、ログをすべて消したときに呼ぶ関数を設定する
// （画面への通知に使う。REST API など、画面の外から消したときにも画面の表示を空にするため、消したことも知らせる）
func (s *PLCService) SetAppLogCallbacks(onAdded func(AppLogDTO), onCleared func()) {
	s.appLogMu.Lock()
	s.onAppLogCleared = onCleared
	s.appLogMu.Unlock()
	if onAdded == nil {
		s.appLog.SetOnAdded(nil)
		return
	}
	s.appLog.SetOnAdded(func(e applog.Entry) { onAdded(appLogToDTO(e)) })
}

// serverLogSource は、サーバーのログの出どころ（プロトコルの表示名。分からなければプロトコルタイプ）を返す。
// s.mu を取るので、s.mu を持ったまま呼ばない（defer で、ロックを外したあとに呼ぶ）
func (s *PLCService) serverLogSource(protocolType string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if f, ok := s.factories[protocol.ProtocolType(protocolType)]; ok {
		return f.DisplayName()
	}
	return protocolType
}

// serverLog は、サーバーのログを出すロガーを返す（行き先は、通信画面のそのサーバー）。
// serverLogSource と同じく、s.mu を持ったまま呼ばない
func (s *PLCService) serverLog(protocolType string) applog.Scope {
	return s.appLog.Scope(s.serverLogSource(protocolType), "server:"+protocolType)
}

// clientLogSource は、クライアント（接続先）のログの出どころを返す
func clientLogSource(name string) string {
	return "接続先: " + name
}

// clientLog は、接続先のログを出すロガーを返す（行き先は、通信画面のその接続先）
func (m *clientManager) clientLog(id, name string) applog.Scope {
	return m.log.Scope(clientLogSource(name), "client:"+id)
}

// scriptLog は、スクリプトのログを出すロガーを返す（行き先は、スクリプトのページのそのスクリプト）
func (s *PLCService) scriptLog(id, name string) applog.Scope {
	return s.appLog.Scope("スクリプト: "+name, "script:"+id)
}
