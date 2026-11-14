package application

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/protocoltest"
)

// appLogLines は、ログを「種類 出どころ: メッセージ」の形の行にして返す
func appLogLines(svc *PLCService) []string {
	var lines []string
	for _, e := range svc.GetAppLogs() {
		lines = append(lines, fmt.Sprintf("%s %s: %s", e.Level, e.Source, e.Message))
	}
	return lines
}

func hasAppLog(svc *PLCService, level, source, messagePart string) bool {
	for _, e := range svc.GetAppLogs() {
		if e.Level == level && e.Source == source && strings.Contains(e.Message, messagePart) {
			return true
		}
	}
	return false
}

func requireAppLog(t *testing.T, svc *PLCService, level, source, messagePart string) {
	t.Helper()
	if !hasAppLog(svc, level, source, messagePart) {
		t.Errorf("ログがない: %s %s: …%s…\n持っているログ:\n  %s", level, source, messagePart, strings.Join(appLogLines(svc), "\n  "))
	}
}

// サーバーの追加・起動・停止・設定の変更・削除と、その失敗がログに出る
func TestAppLog_Servers(t *testing.T) {
	svc := newTestService(t) // modbus-tcp（表示名 "Modbus TCP"）が追加済み
	svc.ClearAppLogs()

	var notified atomic.Int64
	var cleared atomic.Int64
	svc.SetAppLogCallbacks(func(e AppLogDTO) { notified.Add(1) }, func() { cleared.Add(1) })

	if err := svc.AddServer("modbus-rtu", ""); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "info", "Modbus RTU", "サーバーを追加しました")
	if err := svc.AddServer("modbus-rtu", ""); err == nil {
		t.Fatal("同じプロトコルを 2 回追加できた")
	}
	requireAppLog(t, svc, "error", "Modbus RTU", "サーバーを追加できませんでした")

	if err := svc.StartServer("modbus-tcp"); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "info", "Modbus TCP", "サーバーを起動しました")
	// 起動に失敗したとき（フェイクのサーバーに、起動のエラーを返させる）
	fake := svc.servers["modbus-rtu"].server.(*protocoltest.Server)
	fake.StartErr = errors.New("ポートを開けません")
	if err := svc.StartServer("modbus-rtu"); err == nil {
		t.Fatal("起動に失敗するはず")
	}
	requireAppLog(t, svc, "error", "Modbus RTU", "サーバーの起動に失敗しました: ポートを開けません")

	// 起動中は設定を変えられない
	cfg := svc.GetServerConfig("modbus-tcp")
	if err := svc.UpdateServerConfig(cfg); err == nil {
		t.Fatal("起動中に設定を変えられた")
	}
	requireAppLog(t, svc, "error", "Modbus TCP", "設定を変更できませんでした")

	if err := svc.StopServer("modbus-tcp"); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "info", "Modbus TCP", "サーバーを停止しました")
	// 止まっているサーバーを止めても、ログは増えない
	before := len(svc.GetAppLogs())
	if err := svc.StopServer("modbus-tcp"); err != nil {
		t.Fatal(err)
	}
	if after := len(svc.GetAppLogs()); after != before {
		t.Errorf("止まっているサーバーを止めたらログが増えた: %v", appLogLines(svc)[before:])
	}

	if err := svc.UpdateServerConfig(cfg); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "info", "Modbus TCP", "設定を変更しました")
	if err := svc.RemoveServer("modbus-rtu"); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "info", "Modbus RTU", "サーバーを削除しました")
	// 登録していないプロトコルは、プロトコルタイプを出どころにする
	if err := svc.StartServer("nope"); err == nil {
		t.Fatal("ないサーバーを起動できた")
	}
	if !hasAppLog(svc, "error", "nope", "") {
		t.Errorf("ないサーバーの失敗がログにない: %v", appLogLines(svc))
	}

	// 追加のたびに通知され、番号は追加した順
	logs := svc.GetAppLogs()
	if int(notified.Load()) != len(logs) {
		t.Errorf("通知 %d 回、ログ %d 件", notified.Load(), len(logs))
	}
	for i := 1; i < len(logs); i++ {
		if logs[i].ID <= logs[i-1].ID || logs[i].At < logs[i-1].At {
			t.Fatalf("ログの順番がおかしい: %+v, %+v", logs[i-1], logs[i])
		}
	}

	svc.ClearAppLogs()
	if len(svc.GetAppLogs()) != 0 {
		t.Error("消したあともログが残っている")
	}
	if cleared.Load() != 1 {
		t.Errorf("消したことの通知 = %d 回, want 1", cleared.Load())
	}
}

// プロジェクトの読み込みがログに出る
func TestAppLog_Project(t *testing.T) {
	svc := newTestService(t)
	data := svc.ExportProject()
	svc.ClearAppLogs()
	if err := svc.ImportProject(data); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "info", "プロジェクト", "プロジェクトを読み込みました")
}

// クライアント: 接続先の追加・削除、コマンドの失敗、ポーリングの開始・停止と通信エラーがログに出る
func TestAppLog_Clients(t *testing.T) {
	svc, f := newClientTestService(t)
	svc.ClearAppLogs()

	if _, err := svc.AddClient("nope", "X", nil); err == nil {
		t.Fatal("ないプロトコルの接続先を追加できた")
	}
	requireAppLog(t, svc, "error", "接続先", "接続先を追加できませんでした")
	c, err := svc.AddClient("fake-client", "PLC", nil)
	if err != nil {
		t.Fatal(err)
	}
	const source = "接続先: PLC"
	requireAppLog(t, svc, "info", source, "接続先を追加しました")

	// 成功したコマンドはログに出さない。機器が断ったものは警告、通信の失敗はエラー
	var status atomic.Value
	status.Store(protocol.CommandOK)
	f.SetRespond(func(request []byte) protocol.CommandResult {
		st := status.Load().(protocol.CommandStatus)
		res := protocol.CommandResult{Status: st, Summary: "要約"}
		if st != protocol.CommandOK {
			res.Error = "理由"
		}
		return res
	})
	before := len(svc.GetAppLogs())
	if _, err := svc.SendClientRequest(c.ID, "41"); err != nil {
		t.Fatal(err)
	}
	if after := len(svc.GetAppLogs()); after != before {
		t.Errorf("成功したコマンドでログが増えた: %v", appLogLines(svc)[before:])
	}
	status.Store(protocol.CommandException)
	if _, err := svc.SendClientRequest(c.ID, "41"); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "warn", source, "コマンドが異常終了しました: 要約（理由）")
	status.Store(protocol.CommandTimeout)
	if _, err := svc.SendClientRequest(c.ID, "41"); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "error", source, "コマンドを送れませんでした: 要約（理由）")

	// ポーリング: 開始・通信エラー（失敗し始めたときに 1 回だけ）・戻ったとき・停止
	status.Store(protocol.CommandOK)
	if _, err := svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "A"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetClientPollInterval(c.ID, minPollIntervalMs); err != nil {
		t.Fatal(err)
	}
	svc.ClearAppLogs()
	if err := svc.StartClientPolling(c.ID); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "info", source, "ポーリングを開始しました")
	waitFor(t, "1 周する", func() bool { return polling(t, svc, c.ID).Cycles >= 1 })

	status.Store(protocol.CommandTimeout)
	waitFor(t, "通信エラーのログ", func() bool {
		return hasAppLog(svc, "error", source, "ポーリングで通信エラーが起きました: 理由")
	})
	cycles := polling(t, svc, c.ID).Cycles
	waitFor(t, "失敗したまま何周かする", func() bool { return polling(t, svc, c.ID).Cycles >= cycles+3 })
	status.Store(protocol.CommandOK)
	waitFor(t, "戻ったログ", func() bool { return hasAppLog(svc, "info", source, "ポーリングの通信が戻りました") })

	if err := svc.StopClientPolling(c.ID); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "info", source, "ポーリングを停止しました")
	// 止まっているポーリングを止めても、ログは増えない。通信エラーのログは 1 回だけ
	before = len(svc.GetAppLogs())
	_ = svc.StopClientPolling(c.ID)
	if after := len(svc.GetAppLogs()); after != before {
		t.Errorf("止まっているポーリングを止めたらログが増えた: %v", appLogLines(svc)[before:])
	}
	count := 0
	for _, line := range appLogLines(svc) {
		if strings.Contains(line, "ポーリングで通信エラーが起きました") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("通信エラーのログが %d 回出た（1 回だけのはず）: %v", count, appLogLines(svc))
	}

	if err := svc.RemoveClient(c.ID); err != nil {
		t.Fatal(err)
	}
	requireAppLog(t, svc, "info", source, "接続先を削除しました")
}
