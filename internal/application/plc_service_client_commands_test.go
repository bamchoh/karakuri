package application

import (
	"strings"
	"testing"
)

func TestSavedCommands_CRUD(t *testing.T) {
	svc, _ := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)

	cmds, err := svc.GetClientSavedCommands(c.ID)
	if err != nil || len(cmds) != 0 {
		t.Fatalf("初期状態: %+v %v", cmds, err)
	}
	if _, err := svc.GetClientSavedCommands("nope"); err == nil {
		t.Fatal("ない接続先はエラー")
	}

	// 組み立てたコマンドと、16 進で入れたコマンドを保存する
	a, err := svc.AddClientSavedCommand(c.ID, ClientSavedCommandInputDTO{Name: " 速度を読む ", Mode: "build", CommandID: "echo", Params: map[string]interface{}{"data": "AB"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "速度を読む" || a.Request != "41 42" || a.Summary != "echo AB" || a.CommandID != "echo" || a.Params["data"] != "AB" {
		t.Fatalf("組み立てたコマンド: %+v", a)
	}
	b, err := svc.AddClientSavedCommand(c.ID, ClientSavedCommandInputDTO{Mode: "hex", Request: "0x43,44"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Request != "43 44" || b.Name != "echo CD" || b.Params != nil {
		t.Fatalf("16 進のコマンド（名前が空なら要約）: %+v", b)
	}
	for _, bad := range []ClientSavedCommandInputDTO{
		{Mode: "build", CommandID: "echo", Params: map[string]interface{}{}},
		{Mode: "hex", Request: "ZZ"},
		{Mode: "hex", Request: " "},
		{Mode: "other"},
		{Name: strings.Repeat("あ", maxSavedCommandNameLen+1), Mode: "hex", Request: "01"},
	} {
		if _, err := svc.AddClientSavedCommand(c.ID, bad); err == nil {
			t.Errorf("保存できないはず: %+v", bad)
		}
	}

	// 中身を置き換えても、並び順と ID は変わらない
	if err := svc.UpdateClientSavedCommand(c.ID, a.ID, ClientSavedCommandInputDTO{Name: "速度を読む", Mode: "hex", Request: "45"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RenameClientSavedCommand(c.ID, b.ID, "停止"); err != nil {
		t.Fatal(err)
	}
	cmds, _ = svc.GetClientSavedCommands(c.ID)
	if len(cmds) != 2 || cmds[0].ID != a.ID || cmds[0].Request != "45" || cmds[0].Mode != "hex" || cmds[1].Name != "停止" {
		t.Fatalf("変更後: %+v", cmds)
	}
	// 名前を空にすると、内容の要約に戻る
	if err := svc.RenameClientSavedCommand(c.ID, b.ID, " "); err != nil {
		t.Fatal(err)
	}
	cmds, _ = svc.GetClientSavedCommands(c.ID)
	if cmds[1].Name != "echo CD" {
		t.Fatalf("空の名前: %q", cmds[1].Name)
	}

	if err := svc.RemoveClientSavedCommand(c.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveClientSavedCommand(c.ID, a.ID); err == nil {
		t.Fatal("ないコマンドの削除はエラー")
	}
	cmds, _ = svc.GetClientSavedCommands(c.ID)
	if len(cmds) != 1 || cmds[0].ID != b.ID {
		t.Fatalf("削除後: %+v", cmds)
	}
}

func TestSavedCommands_SendRecordsLabel(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	cmd, _ := svc.AddClientSavedCommand(c.ID, ClientSavedCommandInputDTO{Name: "速度", Mode: "hex", Request: "41"})

	log, err := svc.SendClientSavedCommand(c.ID, cmd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if log.Label != "速度" || log.Sent != "41" || log.Status != "ok" {
		t.Fatalf("log = %+v", log)
	}
	if got := f.CreatedClients()[0].SendCount(); got != 1 {
		t.Fatalf("送った回数 = %d", got)
	}
	// 直接送ったものには付けない
	direct, _ := svc.SendClientRequest(c.ID, "42")
	if direct.Label != "" {
		t.Fatalf("direct label = %q", direct.Label)
	}
	if _, err := svc.SendClientSavedCommand(c.ID, "nope"); err == nil {
		t.Fatal("ないコマンドはエラー")
	}
}

func TestSavedCommands_ProjectExportImport(t *testing.T) {
	svc, _ := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	_, _ = svc.AddClientSavedCommand(c.ID, ClientSavedCommandInputDTO{Name: "読む", Mode: "build", CommandID: "echo", Params: map[string]interface{}{"data": "AB"}})
	_, _ = svc.AddClientSavedCommand(c.ID, ClientSavedCommandInputDTO{Name: "書く", Mode: "hex", Request: "57"})

	data := svc.ExportProject()
	saved := data.Clients[0].SavedCommands
	if len(saved) != 2 {
		t.Fatalf("export: %+v", saved)
	}
	// 使えないコマンドは読み飛ばす
	data.Clients[0].SavedCommands = append(saved, ClientSavedCommandDTO{Name: "壊れた", Mode: "hex", Request: "ZZ"})
	if err := svc.ImportProject(data); err != nil {
		t.Fatal(err)
	}
	cmds, _ := svc.GetClientSavedCommands(svc.GetClients()[0].ID)
	if len(cmds) != 2 || cmds[0].Name != "読む" || cmds[0].Mode != "build" || cmds[0].Request != "41 42" || cmds[1].Request != "57" {
		t.Fatalf("import: %+v", cmds)
	}
}
