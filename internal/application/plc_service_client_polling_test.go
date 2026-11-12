package application

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
)

// waitFor は cond が true になるまで待つ（最大 3 秒）
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("待っても %s にならない", what)
}

func polling(t *testing.T, svc *PLCService, id string) *ClientPollingDTO {
	t.Helper()
	p, err := svc.GetClientPolling(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPolling_Items(t *testing.T) {
	svc, _ := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)

	p := polling(t, svc, c.ID)
	if p.IntervalMs != defaultPollIntervalMs || p.Running || len(p.Items) != 0 {
		t.Fatalf("初期状態: %+v", p)
	}

	if _, err := svc.AddClientPollItem(c.ID, "write", map[string]interface{}{"data": "W"}, ""); err == nil {
		t.Fatal("ポーリングに使えないコマンドはエラー")
	}
	if _, err := svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{}, ""); err == nil {
		t.Fatal("組み立てられない項目はエラー")
	}
	a, err := svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "A"}, "dec")
	if err != nil {
		t.Fatal(err)
	}
	if a.Request != "41" || a.Summary != "echo A" || a.Status != "" || a.Format != "dec" {
		t.Fatalf("追加した項目: %+v", a)
	}
	b, _ := svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "B"}, "")

	// 表示形式だけの変更
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(svc.UpdateClientPollItem(c.ID, a.ID, "echo", map[string]interface{}{"data": "A"}, "hex"))
	must(svc.UpdateClientPollItem(c.ID, b.ID, "echo", map[string]interface{}{"data": "C"}, ""))
	p = polling(t, svc, c.ID)
	if p.Items[0].Format != "hex" || p.Items[1].Request != "43" || p.Items[1].ID != b.ID {
		t.Fatalf("変更後: %+v", p.Items)
	}
	if err := svc.UpdateClientPollItem(c.ID, "nope", "echo", map[string]interface{}{"data": "A"}, ""); err == nil {
		t.Fatal("ない項目の変更はエラー")
	}

	must(svc.RemoveClientPollItem(c.ID, a.ID))
	p = polling(t, svc, c.ID)
	if len(p.Items) != 1 || p.Items[0].ID != b.ID {
		t.Fatalf("削除後: %+v", p.Items)
	}

	for _, ms := range []int{minPollIntervalMs - 1, maxPollIntervalMs + 1} {
		if err := svc.SetClientPollInterval(c.ID, ms); err == nil {
			t.Fatalf("周期 %d はエラー", ms)
		}
	}
	must(svc.SetClientPollInterval(c.ID, 250))
	if polling(t, svc, c.ID).IntervalMs != 250 {
		t.Fatal("周期が変わっていない")
	}

	for i := len(p.Items); i < maxPollItems; i++ {
		if _, err := svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "X"}, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "X"}, ""); err == nil {
		t.Fatalf("%d 個を超えたらエラー", maxPollItems)
	}
}

func TestPolling_RunsAndRecords(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	_ = svc.SetClientPollInterval(c.ID, minPollIntervalMs)
	_, _ = svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "A"}, "")
	_, _ = svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "B"}, "")

	word := uint16(7)
	f.SetRespond(func(req []byte) protocol.CommandResult {
		w := word
		return protocol.CommandResult{Sent: req, Status: protocol.CommandOK, Values: []protocol.ResultValue{{Label: "v", Value: string(req), Word: &w}}}
	})

	if err := svc.StartClientPolling(c.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartClientPolling(c.ID); err != nil {
		t.Fatal("ポーリング中にもう一度始めても何もしない")
	}
	if !svc.GetClients()[0].Polling {
		t.Fatal("一覧にポーリング中と出る")
	}
	waitFor(t, "3 周", func() bool { return polling(t, svc, c.ID).Cycles >= 3 })

	p := polling(t, svc, c.ID)
	if !p.Running {
		t.Fatal("ポーリング中のはず")
	}
	for _, it := range p.Items {
		if it.Status != "ok" || it.OKCount < 3 || len(it.Values) != 1 || it.Values[0].Word == nil || *it.Values[0].Word != 7 || it.At == "" {
			t.Fatalf("項目の結果: %+v", it)
		}
	}
	if p.Items[1].Values[0].Value != "B" {
		t.Fatalf("項目ごとに自分のリクエストの結果が入る: %+v", p.Items[1].Values)
	}
	// ポーリングは履歴に入れない
	if n := len(svc.GetClientCommandLogs()); n != 0 {
		t.Fatalf("履歴 %d 件", n)
	}

	if err := svc.StopClientPolling(c.ID); err != nil {
		t.Fatal(err)
	}
	stopped := polling(t, svc, c.ID)
	if stopped.Running || svc.GetClients()[0].Polling {
		t.Fatal("止まっていない")
	}
	time.Sleep(3 * minPollIntervalMs * time.Millisecond)
	if after := polling(t, svc, c.ID).Cycles; after != stopped.Cycles {
		t.Fatalf("止めたあとも回っている: %d → %d", stopped.Cycles, after)
	}
}

// 読めなかった項目は最後に読めた値を残す。失敗した項目のあとの項目は、その周は送らない
func TestPolling_FailureKeepsValuesAndSkipsRest(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	_ = svc.SetClientPollInterval(c.ID, minPollIntervalMs)
	_, _ = svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "A"}, "")
	_, _ = svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "B"}, "")
	_, _ = svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "C"}, "")

	var failA atomic.Bool
	var sentC atomic.Int32
	f.SetRespond(func(req []byte) protocol.CommandResult {
		switch string(req) {
		case "A":
			if failA.Load() {
				return protocol.CommandResult{Status: protocol.CommandException, Error: "例外応答: 0x02"}
			}
		case "B":
			if failA.Load() {
				return protocol.CommandResult{Status: protocol.CommandTimeout, Error: "応答がありません"}
			}
		case "C":
			sentC.Add(1)
		}
		return protocol.CommandResult{Status: protocol.CommandOK, Values: []protocol.ResultValue{{Label: "v", Value: "ok-" + string(req)}}}
	})
	_ = svc.StartClientPolling(c.ID)
	waitFor(t, "最初の 1 周", func() bool { return polling(t, svc, c.ID).Cycles >= 1 })

	failA.Store(true)
	start := polling(t, svc, c.ID).Cycles
	waitFor(t, "失敗してから 2 周", func() bool { return polling(t, svc, c.ID).Cycles >= start+2 })
	sentBefore := sentC.Load()
	time.Sleep(3 * minPollIntervalMs * time.Millisecond)
	if sentC.Load() != sentBefore {
		t.Fatal("タイムアウトした項目のあとの項目は送らない")
	}
	_ = svc.StopClientPolling(c.ID)

	items := polling(t, svc, c.ID).Items
	// 例外応答は失敗として数えるが、次の項目は送る
	if items[0].Status != "exception" || items[0].Values[0].Value != "ok-A" || items[0].ErrorCount == 0 {
		t.Fatalf("A: %+v", items[0])
	}
	if items[1].Status != "timeout" || items[1].Values[0].Value != "ok-B" {
		t.Fatalf("B（最後に読めた値を残す）: %+v", items[1])
	}
	if items[2].Status != "skipped" || !strings.Contains(items[2].Error, "応答がありません") || items[2].Values[0].Value != "ok-C" {
		t.Fatalf("C: %+v", items[2])
	}
}

// 送っている間に項目の削除・周期の変更・接続先の設定変更が重なっても競合しない（-race で確認）。
// 接続先を削除するとポーリングは止まる
func TestPolling_ConcurrentChangesAndRemove(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	_ = svc.SetClientPollInterval(c.ID, minPollIntervalMs)
	f.SetRespond(func(req []byte) protocol.CommandResult {
		time.Sleep(time.Millisecond)
		return protocol.CommandResult{Status: protocol.CommandOK}
	})
	_ = svc.StartClientPolling(c.ID)

	var wg sync.WaitGroup
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				it, err := svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "A"}, "")
				if err == nil && i%2 == 0 {
					_ = svc.RemoveClientPollItem(c.ID, it.ID)
				}
				_ = svc.SetClientPollInterval(c.ID, minPollIntervalMs+i)
				_, _ = svc.GetClientPolling(c.ID)
				_ = svc.GetClients()
			}
		}()
	}
	for i := 0; i < 10; i++ {
		_ = svc.UpdateClient(c.ID, "PLC", map[string]interface{}{"target": "x"})
	}
	wg.Wait()

	if err := svc.RemoveClient(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetClientPolling(c.ID); err == nil {
		t.Fatal("削除した接続先")
	}
	// 削除したあと、作られたクライアントに送られ続けていない
	clients := f.CreatedClients()
	last := clients[len(clients)-1]
	n := last.SendCount()
	time.Sleep(3 * minPollIntervalMs * time.Millisecond)
	if last.SendCount() != n {
		t.Fatal("削除したあとも送っている")
	}
}

func TestPolling_ProjectExportImport(t *testing.T) {
	svc, _ := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	_ = svc.SetClientPollInterval(c.ID, 300)
	_, _ = svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "A"}, "hex")
	_ = svc.StartClientPolling(c.ID)

	data := svc.ExportProject()
	snap := data.Clients[0].Polling
	if snap == nil || snap.IntervalMs != 300 || len(snap.Items) != 1 || snap.Items[0].Format != "hex" || snap.Items[0].Params["data"] != "A" {
		t.Fatalf("export: %+v", snap)
	}
	// 使えない項目は読み飛ばす
	snap.Items = append(snap.Items, ClientPollItemSnapshotDTO{CommandID: "write", Params: map[string]interface{}{"data": "W"}})
	if err := svc.ImportProject(data); err != nil {
		t.Fatal(err)
	}
	list := svc.GetClients()
	if len(list) != 1 || list[0].Polling {
		t.Fatalf("読み込んだ接続先はポーリングを止めた状態で始める: %+v", list)
	}
	p := polling(t, svc, list[0].ID)
	if p.IntervalMs != 300 || len(p.Items) != 1 || p.Items[0].Request != "41" || p.Items[0].Format != "hex" {
		t.Fatalf("import: %+v", p)
	}

	// 既定のままの接続先は polling を保存しない
	_ = svc.ImportProject(&ProjectDataDTO{Clients: []ClientSnapshotDTO{{Name: "x", ProtocolType: "fake-client"}}})
	if svc.ExportProject().Clients[0].Polling != nil {
		t.Fatal("既定のままなら保存しない")
	}
}
