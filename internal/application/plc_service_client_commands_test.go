package application

import (
	"strings"
	"testing"
)

func TestCommandGroups_CRUD(t *testing.T) {
	svc, _ := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)

	groups, err := svc.GetClientCommandGroups(c.ID)
	if err != nil || len(groups) != 0 {
		t.Fatalf("初期状態: %+v %v", groups, err)
	}
	if _, err := svc.GetClientCommandGroups("nope"); err == nil {
		t.Fatal("ない接続先はエラー")
	}

	g1, err := svc.AddClientCommandGroup(c.ID, "")
	if err != nil || g1.Name != "グループ 1" {
		t.Fatalf("既定の名前: %+v %v", g1, err)
	}
	g2, _ := svc.AddClientCommandGroup(c.ID, " モーター ")
	if g2.Name != "モーター" {
		t.Fatalf("前後の空白を除く: %q", g2.Name)
	}
	if err := svc.RenameClientCommandGroup(c.ID, g1.ID, "初期化"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RenameClientCommandGroup(c.ID, g1.ID, "  "); err == nil {
		t.Fatal("空の名前にはできない")
	}
	if _, err := svc.AddClientCommandGroup(c.ID, strings.Repeat("あ", maxCommandGroupNameLn+1)); err == nil {
		t.Fatal("長すぎる名前はエラー")
	}

	// 組み立てたコマンドと、16 進で入れたコマンドを保存する
	a, err := svc.AddClientSavedCommand(c.ID, g2.ID, ClientSavedCommandInputDTO{Name: "速度を読む", Mode: "build", CommandID: "echo", Params: map[string]interface{}{"data": "AB"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Request != "41 42" || a.Summary != "echo AB" || a.CommandID != "echo" || a.Params["data"] != "AB" {
		t.Fatalf("組み立てたコマンド: %+v", a)
	}
	b, err := svc.AddClientSavedCommand(c.ID, g2.ID, ClientSavedCommandInputDTO{Mode: "hex", Request: "0x43,44"})
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
	} {
		if _, err := svc.AddClientSavedCommand(c.ID, g2.ID, bad); err == nil {
			t.Errorf("保存できないはず: %+v", bad)
		}
	}
	if _, err := svc.AddClientSavedCommand(c.ID, "nope", ClientSavedCommandInputDTO{Mode: "hex", Request: "01"}); err == nil {
		t.Fatal("ないグループはエラー")
	}

	// 中身を置き換えても、並び順と ID は変わらない
	if err := svc.UpdateClientSavedCommand(c.ID, g2.ID, a.ID, ClientSavedCommandInputDTO{Name: "速度を読む", Mode: "hex", Request: "45"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RenameClientSavedCommand(c.ID, g2.ID, b.ID, "停止"); err != nil {
		t.Fatal(err)
	}
	groups, _ = svc.GetClientCommandGroups(c.ID)
	if len(groups) != 2 || groups[0].Name != "初期化" || groups[1].Name != "モーター" {
		t.Fatalf("グループ: %+v", groups)
	}
	cmds := groups[1].Commands
	if len(cmds) != 2 || cmds[0].ID != a.ID || cmds[0].Request != "45" || cmds[0].Mode != "hex" || cmds[1].Name != "停止" {
		t.Fatalf("コマンド: %+v", cmds)
	}

	if err := svc.RemoveClientSavedCommand(c.ID, g2.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveClientSavedCommand(c.ID, g2.ID, a.ID); err == nil {
		t.Fatal("ないコマンドの削除はエラー")
	}
	if err := svc.RemoveClientCommandGroup(c.ID, g1.ID); err != nil {
		t.Fatal(err)
	}
	groups, _ = svc.GetClientCommandGroups(c.ID)
	if len(groups) != 1 || groups[0].ID != g2.ID || len(groups[0].Commands) != 1 {
		t.Fatalf("削除後: %+v", groups)
	}
}

func TestCommandGroups_SendRecordsLabel(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	g, _ := svc.AddClientCommandGroup(c.ID, "モーター")
	cmd, _ := svc.AddClientSavedCommand(c.ID, g.ID, ClientSavedCommandInputDTO{Name: "速度", Mode: "hex", Request: "41"})

	log, err := svc.SendClientSavedCommand(c.ID, g.ID, cmd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if log.Label != "モーター / 速度" || log.Sent != "41" || log.Status != "ok" {
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
	if _, err := svc.SendClientSavedCommand(c.ID, g.ID, "nope"); err == nil {
		t.Fatal("ないコマンドはエラー")
	}
}

func TestCommandGroups_ProjectExportImport(t *testing.T) {
	svc, _ := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	g, _ := svc.AddClientCommandGroup(c.ID, "モーター")
	_, _ = svc.AddClientSavedCommand(c.ID, g.ID, ClientSavedCommandInputDTO{Name: "読む", Mode: "build", CommandID: "echo", Params: map[string]interface{}{"data": "AB"}})
	_, _ = svc.AddClientSavedCommand(c.ID, g.ID, ClientSavedCommandInputDTO{Name: "書く", Mode: "hex", Request: "57"})

	data := svc.ExportProject()
	saved := data.Clients[0].CommandGroups
	if len(saved) != 1 || len(saved[0].Commands) != 2 {
		t.Fatalf("export: %+v", saved)
	}
	// 使えないコマンドは読み飛ばす
	saved[0].Commands = append(saved[0].Commands, ClientSavedCommandDTO{Name: "壊れた", Mode: "hex", Request: "ZZ"})
	if err := svc.ImportProject(data); err != nil {
		t.Fatal(err)
	}
	groups, _ := svc.GetClientCommandGroups(svc.GetClients()[0].ID)
	if len(groups) != 1 || groups[0].Name != "モーター" || len(groups[0].Commands) != 2 {
		t.Fatalf("import: %+v", groups)
	}
	if groups[0].Commands[0].Mode != "build" || groups[0].Commands[0].Request != "41 42" || groups[0].Commands[1].Request != "57" {
		t.Fatalf("import の中身: %+v", groups[0].Commands)
	}
}
