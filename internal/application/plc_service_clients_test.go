package application

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/protocoltest"
)

func newClientTestService(t *testing.T) (*PLCService, *protocoltest.ClientFactory) {
	t.Helper()
	svc := newTestService(t)
	f := protocoltest.NewClientFactory("fake-client")
	svc.RegisterClientFactory(f)
	t.Cleanup(svc.Shutdown)
	return svc, f
}

func TestClients_Browse(t *testing.T) {
	svc, _ := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)

	top, err := svc.BrowseClient(c.ID, "")
	if err != nil || len(top) != 1 || top[0].Address != "folder" || !top[0].HasChildren || top[0].Selectable {
		t.Fatalf("いちばん上: %+v, %v", top, err)
	}
	children, err := svc.BrowseClient(c.ID, "folder")
	if err != nil || len(children) != 2 || !children[0].Selectable || children[0].Detail != "INT" || children[0].Fields["keys"] != "a" {
		t.Fatalf("子: %+v, %v", children, err)
	}
	// 入れる内容がない項目でも、画面で扱いやすいよう空のマップにする
	if children[1].Fields == nil {
		t.Fatalf("Fields が nil: %+v", children[1])
	}
	if _, err := svc.BrowseClient(c.ID, "missing"); err == nil {
		t.Fatal("プロトコルのエラーは、そのまま返す")
	}
	if _, err := svc.BrowseClient("nope", ""); err == nil {
		t.Fatal("ない接続先はエラー")
	}
}

func TestParseHex(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
		err  bool
	}{
		{"03 00 00 00 0A", []byte{0x03, 0x00, 0x00, 0x00, 0x0a}, false},
		{"0300 000a", []byte{0x03, 0x00, 0x00, 0x0a}, false},
		{"0x03,0x0,0xff", []byte{0x03, 0x00, 0xff}, false},
		{"03:00-10", []byte{0x03, 0x00, 0x10}, false},
		{"", []byte{}, false},
		{"030", nil, true},
		{"0G", nil, true},
	}
	for _, c := range cases {
		got, err := parseHex(c.in)
		if c.err {
			if err == nil {
				t.Errorf("%q: エラーになるはず", c.in)
			}
			continue
		}
		if err != nil || !bytes.Equal(got, c.want) {
			t.Errorf("%q: got % X err=%v", c.in, got, err)
		}
	}
	if s := formatHex([]byte{0x03, 0xAB}); s != "03 AB" {
		t.Errorf("formatHex = %q", s)
	}
}

func TestClients_AddUpdateRemove(t *testing.T) {
	svc, f := newClientTestService(t)

	protos := svc.GetClientProtocols()
	if len(protos) != 1 || protos[0].ProtocolType != "fake-client" || len(protos[0].Commands) != 2 || !protos[0].Commands[0].Pollable || protos[0].Commands[1].Pollable || len(protos[0].ConnectionFields) != 2 {
		t.Fatalf("protocols = %+v", protos)
	}
	if protos[0].RequestDescription == "" || protos[0].RequestExample != "41 42" {
		t.Fatalf("16 進で入れるリクエストの説明がない: %+v", protos[0])
	}

	if _, err := svc.AddClient("unknown", "x", nil); err == nil {
		t.Fatal("知らないプロトコルはエラー")
	}
	a, err := svc.AddClient("fake-client", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "接続先 1" || a.Settings["target"] != "fake" {
		t.Fatalf("既定の名前と設定: %+v", a)
	}
	b, err := svc.AddClient("fake-client", "PLC-2", map[string]interface{}{"target": "b"})
	if err != nil {
		t.Fatal(err)
	}
	list := svc.GetClients()
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("追加した順に並ぶ: %+v", list)
	}

	// 設定を変えると、古いクライアントを閉じて作り直す
	old := f.CreatedClients()[0]
	if err := svc.UpdateClient(a.ID, "PLC-1", map[string]interface{}{"target": "a", "timeoutMs": float64(500)}); err != nil {
		t.Fatal(err)
	}
	if old.CloseCount() != 1 {
		t.Fatalf("古いクライアントを閉じる: %d", old.CloseCount())
	}
	got := svc.GetClients()[0]
	if got.Name != "PLC-1" || got.Settings["target"] != "a" || got.Settings["timeoutMs"] != 500 {
		t.Fatalf("更新後: %+v", got)
	}
	if err := svc.UpdateClient(a.ID, " ", nil); err == nil {
		t.Fatal("空の名前はエラー")
	}
	if err := svc.UpdateClient(a.ID, "PLC-1", map[string]interface{}{"target": ""}); err == nil {
		t.Fatal("検証に通らない設定はエラー")
	}

	if err := svc.RemoveClient(b.ID); err != nil {
		t.Fatal(err)
	}
	if len(svc.GetClients()) != 1 {
		t.Fatal("削除されていない")
	}
	if err := svc.RemoveClient(b.ID); err == nil {
		t.Fatal("ない接続先の削除はエラー")
	}

	// 既定の名前は、使われていない最も小さい番号にする
	c1, _ := svc.AddClient("fake-client", "", nil)
	if c1.Name != "接続先 1" {
		t.Fatalf("PLC-1 に改名したので 接続先 1 が空いている: %s", c1.Name)
	}
	c2, _ := svc.AddClient("fake-client", "", nil)
	if c2.Name != "接続先 2" {
		t.Fatalf("次は 接続先 2: %s", c2.Name)
	}
}

func TestClients_BuildAndSend(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)

	hexReq, err := svc.BuildClientRequest("fake-client", "echo", map[string]interface{}{"data": "AB"})
	if err != nil || hexReq != "41 42" {
		t.Fatalf("build = %q, %v", hexReq, err)
	}
	if _, err := svc.BuildClientRequest("fake-client", "echo", map[string]interface{}{}); err == nil {
		t.Fatal("項目の検証エラーを返す")
	}

	log, err := svc.SendClientRequest(c.ID, hexReq)
	if err != nil {
		t.Fatal(err)
	}
	if log.Status != "ok" || log.Sent != "41 42" || log.Received != "41 42" || log.ClientName != "PLC" || len(log.Values) != 1 {
		t.Fatalf("log = %+v", log)
	}
	if !svc.GetClients()[0].Connected {
		t.Fatal("送信後は接続中")
	}

	// 通信の失敗は error ではなく結果の status で返し、履歴にも残す
	f.SetRespond(func(req []byte) protocol.CommandResult {
		return protocol.CommandResult{Sent: req, Status: protocol.CommandTimeout, Error: "100 ms 以内に応答がありませんでした"}
	})
	log2, err := svc.SendClientRequest(c.ID, "01")
	if err != nil {
		t.Fatal(err)
	}
	if log2.Status != "timeout" || !strings.Contains(log2.Error, "応答") || log2.Received != "" {
		t.Fatalf("log2 = %+v", log2)
	}

	// 送る前に分かる誤りは error
	if _, err := svc.SendClientRequest(c.ID, "ZZ"); err == nil {
		t.Fatal("読めない 16 進はエラー")
	}
	if _, err := svc.SendClientRequest(c.ID, "  "); err == nil {
		t.Fatal("空のリクエストはエラー")
	}
	if _, err := svc.SendClientRequest("nope", "01"); err == nil {
		t.Fatal("ない接続先はエラー")
	}

	logs := svc.GetClientCommandLogs()
	if len(logs) != 2 || logs[0].Seq != log2.Seq || logs[1].Seq != log.Seq {
		t.Fatalf("履歴は新しい順: %+v", logs)
	}
	svc.ClearClientCommandLogs()
	if len(svc.GetClientCommandLogs()) != 0 {
		t.Fatal("履歴が消えていない")
	}
}

func TestClients_LogLimit(t *testing.T) {
	svc, _ := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	for i := 0; i < maxClientCommandLogs+5; i++ {
		if _, err := svc.SendClientRequest(c.ID, "01"); err != nil {
			t.Fatal(err)
		}
	}
	logs := svc.GetClientCommandLogs()
	if len(logs) != maxClientCommandLogs {
		t.Fatalf("len = %d", len(logs))
	}
	if logs[0].Seq != int64(maxClientCommandLogs+5) || logs[len(logs)-1].Seq != 6 {
		t.Fatalf("古いものから消す: first=%d last=%d", logs[0].Seq, logs[len(logs)-1].Seq)
	}
}

// 送信中に設定変更・削除・一覧の取得が重なっても競合しない（-race で確認）
func TestClients_ConcurrentSendAndUpdate(t *testing.T) {
	svc, _ := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = svc.SendClientRequest(c.ID, "01 02")
				_ = svc.GetClients()
				_ = svc.GetClientCommandLogs()
			}
		}()
	}
	for j := 0; j < 50; j++ {
		_ = svc.UpdateClient(c.ID, "PLC", map[string]interface{}{"target": "x"})
	}
	wg.Wait()
}

func TestClients_ProjectExportImport(t *testing.T) {
	svc, f := newClientTestService(t)
	_, _ = svc.AddClient("fake-client", "PLC-1", map[string]interface{}{"target": "a"})
	_, _ = svc.AddClient("fake-client", "PLC-2", map[string]interface{}{"target": "b"})

	data := svc.ExportProject()
	if len(data.Clients) != 2 || data.Clients[0].Name != "PLC-1" || data.Clients[1].Settings["target"] != "b" {
		t.Fatalf("export = %+v", data.Clients)
	}

	before := f.CreatedClients()
	data.Clients = data.Clients[1:]
	data.Clients = append(data.Clients, ClientSnapshotDTO{Name: "unknown", ProtocolType: "nope"})
	if err := svc.ImportProject(data); err != nil {
		t.Fatal(err)
	}
	for _, c := range before {
		if c.CloseCount() == 0 {
			t.Fatal("読み込む前の接続を閉じる")
		}
	}
	list := svc.GetClients()
	if len(list) != 1 || list[0].Name != "PLC-2" || list[0].Settings["target"] != "b" {
		t.Fatalf("読み込み後（使えないプロトコルは読み飛ばす）: %+v", list)
	}

	// 接続先を持たないプロジェクトを読み込むと、接続先は空になる
	if err := svc.ImportProject(&ProjectDataDTO{}); err != nil {
		t.Fatal(err)
	}
	if len(svc.GetClients()) != 0 {
		t.Fatal("接続先が残っている")
	}
}
