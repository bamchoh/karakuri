package application

import (
	"fmt"
	"strings"
	"testing"

	"karakuri/internal/domain/protocol"
)

func TestScript_SendCommandAndRequest(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC-A", nil)
	g, _ := svc.AddClientCommandGroup(c.ID, "モーター")
	_, _ = svc.AddClientSavedCommand(c.ID, g.ID, ClientSavedCommandInputDTO{Name: "速度を読む", Mode: "hex", Request: "41"})

	w := uint16(1234)
	on := true
	f.SetRespond(func(req []byte) protocol.CommandResult {
		return protocol.CommandResult{
			Sent: req, Received: []byte{0x99}, Status: protocol.CommandOK, Summary: "echo",
			Values: []protocol.ResultValue{
				{Label: "アドレス 0", Value: "1234 (0x04D2)", Word: &w},
				{Label: "アドレス 1", Value: "ON", Bit: &on},
			},
		}
	})

	res, err := svc.RunScriptOnce(`
		const r = plc.sendCommand("PLC-A", "モーター", "速度を読む");
		const r2 = plc.sendRequest("PLC-A", "42 43");
		return [r.status, r.values[0].word, r.values[1].bit, r.values[0].label, r.sent, r.received, typeof r.elapsedMs, r2.status, r2.sent].join("|");
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(res); got != "ok|1234|true|アドレス 0|41|99|number|ok|42 43" {
		t.Fatalf("script = %s", got)
	}

	// 履歴に、スクリプトから送ったと分かる名前で残る
	logs := svc.GetClientCommandLogs()
	if len(logs) != 2 || logs[1].Label != "スクリプト「1 回実行」: モーター / 速度を読む" || logs[0].Label != "スクリプト「1 回実行」: 16 進で送信" {
		t.Fatalf("logs = %+v / %+v", logs[0].Label, logs[1].Label)
	}
}

// 見つからない・読めないときは、例外ではなく status が error の結果を返す
func TestScript_SendErrorsDoNotThrow(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC-A", nil)
	g, _ := svc.AddClientCommandGroup(c.ID, "モーター")
	_, _ = svc.AddClientSavedCommand(c.ID, g.ID, ClientSavedCommandInputDTO{Name: "読む", Mode: "hex", Request: "41"})

	cases := map[string]string{
		`plc.sendCommand("nope", "モーター", "読む")`:    "接続先「nope」がありません",
		`plc.sendCommand("PLC-A", "nope", "読む")`:   "コマンドグループ「nope」がありません",
		`plc.sendCommand("PLC-A", "モーター", "nope")`: "グループ「モーター」にコマンド「nope」がありません",
		`plc.sendRequest("PLC-A", "ZZ")`:           "16 進",
		`plc.sendRequest("PLC-A", "")`:             "リクエストを入力してください",
	}
	for code, want := range cases {
		res, err := svc.RunScriptOnce(`const r = ` + code + `; return r.status + "|" + r.error + "|" + r.values.length;`)
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		got := fmt.Sprint(res)
		if !strings.HasPrefix(got, "error|") || !strings.Contains(got, want) || !strings.HasSuffix(got, "|0") {
			t.Errorf("%s = %s", code, got)
		}
	}

	// 通信の失敗（タイムアウト）も、結果の status で返す
	f.SetRespond(func(req []byte) protocol.CommandResult {
		return protocol.CommandResult{Sent: req, Status: protocol.CommandTimeout, Error: "応答がありません"}
	})
	res, err := svc.RunScriptOnce(`const r = plc.sendCommand("PLC-A", "モーター", "読む"); return r.status + "|" + r.error;`)
	if err != nil || fmt.Sprint(res) != "timeout|応答がありません" {
		t.Fatalf("timeout: %v %v", res, err)
	}
}
