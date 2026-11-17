package application

import (
	"testing"
)

// monitoringValue は、モニタリング項目 id のいまの値を返す
func monitoringValue(t *testing.T, svc *PLCService, id string) MonitoringValueDTO {
	t.Helper()
	for _, v := range svc.GetMonitoringValues() {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("モニタリング項目 %s の値がない", id)
	return MonitoringValueDTO{}
}

func TestMonitoringValues_Variables(t *testing.T) {
	svc, _ := newClientTestService(t)
	counter, err := svc.CreateVariable("Counter", "INT", 5)
	if err != nil {
		t.Fatal(err)
	}
	big, _ := svc.CreateVariable("Big", "LINT", 1)
	arr, _ := svc.CreateVariable("Values", "ARRAY[1..3] OF INT", nil)
	flag, _ := svc.CreateVariable("Flag", "BOOL", true)

	add := func(item *MonitoringItemDTO) string {
		t.Helper()
		added, err := svc.AddMonitoringItem(item)
		if err != nil {
			t.Fatal(err)
		}
		return added.ID
	}
	// レジスタの項目は、ここには出ない（画面がメモリを読む）
	add(&MonitoringItemDTO{ProtocolType: "fake", MemoryArea: "words", Address: 1, BitWidth: 16})
	idCounter := add(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: counter.ID})
	idBig := add(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: big.ID})
	idArr := add(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: arr.ID})
	idElem := add(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: arr.ID, FieldPath: "[2]"})
	idFlag := add(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: flag.ID})
	idBadPath := add(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: counter.ID, FieldPath: "nope"})

	values := svc.GetMonitoringValues()
	if len(values) != 6 || values[0].ID != idCounter || values[5].ID != idBadPath {
		t.Fatalf("値の並び: %+v", values)
	}
	if v := values[0]; v.Source != "変数" || v.Label != "Counter" || v.Text != "5" || v.Status != "ok" {
		t.Fatalf("Counter: %+v", v)
	}
	// LINT は、桁を落とさないよう文字列
	if err := svc.UpdateVariableValue(big.ID, "9223372036854775807"); err != nil {
		t.Fatal(err)
	}
	if v := monitoringValue(t, svc, idBig); v.Text != "9223372036854775807" || v.Value != "9223372036854775807" {
		t.Fatalf("Big: %+v", v)
	}
	// 配列はまるごと 1 行、要素は名前にパスが付く
	if err := svc.UpdateVariableFieldValue(arr.ID, "[2]", 7); err != nil {
		t.Fatal(err)
	}
	// 配列は、要素を 1 つずつの行にする（添字は宣言どおり 1 から。パスは書き込みに使える）
	v := monitoringValue(t, svc, idArr)
	if v.Text != "" || len(v.Rows) != 3 || v.Rows[1].Label != "[2]" || v.Rows[1].Path != "[2]" || v.Rows[1].Text != "7" || v.Rows[1].Depth != 1 || v.Rows[1].Composite {
		t.Fatalf("Values: %+v", v)
	}
	if err := svc.UpdateVariableFieldValue(arr.ID, v.Rows[2].Path, 8); err != nil {
		t.Fatal(err)
	}
	if v := monitoringValue(t, svc, idArr); v.Rows[2].Text != "8" {
		t.Fatalf("行のパスで書いたあと: %+v", v.Rows)
	}
	if v := monitoringValue(t, svc, idElem); v.Label != "Values[2]" || v.Text != "7" {
		t.Fatalf("Values[2]: %+v", v)
	}
	if v := monitoringValue(t, svc, idFlag); v.Text != "TRUE" || v.Value != true {
		t.Fatalf("Flag: %+v", v)
	}
	if v := monitoringValue(t, svc, idBadPath); v.Status != "error" || v.Error == "" || v.Label != "Counter.nope" {
		t.Fatalf("ないパス: %+v", v)
	}

	// 変数を消すと、その変数の項目も消える（ほかは残る）
	if err := svc.DeleteVariable(arr.ID); err != nil {
		t.Fatal(err)
	}
	if got := len(svc.GetMonitoringValues()); got != 4 {
		t.Fatalf("変数を消したあとの値 = %d 個, want 4", got)
	}
	if got := len(svc.GetMonitoringItems()); got != 5 {
		t.Fatalf("変数を消したあとの項目 = %d 個, want 5（レジスタ 1 + 変数 4）", got)
	}
}

func TestMonitoringValues_ClientSubscriptionAndPolling(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	if _, err := svc.AddClientSubscriptionItems(c.ID, map[string]interface{}{"keys": "a, b"}); err != nil {
		t.Fatal(err)
	}
	poll, err := svc.AddClientPollItem(c.ID, "echo", map[string]interface{}{"data": "A"}, "")
	if err != nil {
		t.Fatal(err)
	}

	subItem, _ := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindSubscription, ClientID: c.ID, Summary: "watch a"})
	gone, _ := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindSubscription, ClientID: c.ID, Summary: "watch zzz"})
	pollItem, _ := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindPoll, ClientID: c.ID, Summary: poll.Summary})
	noClient, _ := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindPoll, ClientID: "nope", Summary: "x"})

	// 止まっている間は、値がなく「監視していません」
	if v := monitoringValue(t, svc, subItem.ID); v.Source != "接続先: PLC" || v.Label != "watch a" || v.Status != "stale" || v.Text != "" || v.Error != "監視していません" {
		t.Fatalf("停止中の監視項目: %+v", v)
	}
	if v := monitoringValue(t, svc, gone.ID); v.Status != "error" || v.Error != "監視項目がありません" {
		t.Fatalf("ない監視項目: %+v", v)
	}
	if v := monitoringValue(t, svc, pollItem.ID); v.Status != "stale" || v.Error != "ポーリングしていません" {
		t.Fatalf("停止中の読み取り項目: %+v", v)
	}
	if v := monitoringValue(t, svc, noClient.ID); v.Status != "error" || v.Error != "接続先がありません" {
		t.Fatalf("ない接続先: %+v", v)
	}

	// 監視を始めて値が届くと、出る
	if err := svc.StartClientSubscription(c.ID); err != nil {
		t.Fatal(err)
	}
	sub := lastSubscription(t, f, 1)
	waitFor(t, "監視中", func() bool { s := subscription(t, svc, c.ID); return s.Running && s.State == "ok" })
	sub.EmitValue("a", "42")
	waitFor(t, "a = 42", func() bool {
		v := monitoringValue(t, svc, subItem.ID)
		return v.Status == "ok" && v.Text == "42"
	})
	// 止めても、最後の値は残る
	if err := svc.StopClientSubscription(c.ID); err != nil {
		t.Fatal(err)
	}
	if v := monitoringValue(t, svc, subItem.ID); v.Status != "stale" || v.Text != "42" {
		t.Fatalf("止めたあとの監視項目: %+v", v)
	}

	// ポーリングで読めた値も出る
	if err := svc.StartClientPolling(c.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "読み取り項目の値", func() bool {
		v := monitoringValue(t, svc, pollItem.ID)
		return v.Status == "ok" && v.Text != ""
	})
	if err := svc.StopClientPolling(c.ID); err != nil {
		t.Fatal(err)
	}

	// 接続先を消すと、その接続先の項目も消える（ない接続先を指していた項目は残る）
	if err := svc.RemoveClient(c.ID); err != nil {
		t.Fatal(err)
	}
	items := svc.GetMonitoringItems()
	if len(items) != 1 || items[0].ID != noClient.ID {
		t.Fatalf("接続先を消したあとの項目: %+v", items)
	}
}

func TestMonitoringValues_StructAndNestedArrayRows(t *testing.T) {
	svc, _ := newClientTestService(t)
	// フィールドは、名前の順ではなく、定義の順に並ぶ
	if _, err := svc.RegisterStructType(StructTypeDTO{Name: "Motor", Fields: []StructFieldDTO{
		{Name: "speed", DataType: "INT"},
		{Name: "on", DataType: "BOOL"},
		{Name: "limits", DataType: "ARRAY[1..2] OF INT"},
	}}); err != nil {
		t.Fatal(err)
	}
	line, err := svc.CreateVariable("Line", "ARRAY[5..6] OF Motor", nil)
	if err != nil {
		t.Fatal(err)
	}
	grid, _ := svc.CreateVariable("Grid", "ARRAY[0..1, 1..2] OF INT", nil)
	whole, _ := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: line.ID})
	part, _ := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: line.ID, FieldPath: "[6]"})
	gridItem, _ := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: grid.ID})

	if err := svc.UpdateVariableFieldValue(line.ID, "[6].limits[2]", 9); err != nil {
		t.Fatal(err)
	}
	type want struct {
		path, label string
		depth       int
		text        string
		composite   bool
	}
	check := func(name string, rows []MonitoringValueRowDTO, wants []want) {
		t.Helper()
		if len(rows) != len(wants) {
			t.Fatalf("%s: 行 = %d 個, want %d: %+v", name, len(rows), len(wants), rows)
		}
		for i, w := range wants {
			r := rows[i]
			if r.Path != w.path || r.Label != w.label || r.Depth != w.depth || r.Text != w.text || r.Composite != w.composite {
				t.Fatalf("%s: 行 %d = %+v, want %+v", name, i, r, w)
			}
		}
	}
	motor := func(prefix string, depth int, last string) []want {
		return []want{
			{prefix + ".speed", "speed", depth, "0", false},
			{prefix + ".on", "on", depth, "FALSE", false},
			{prefix + ".limits", "limits", depth, "", true},
			{prefix + ".limits[1]", "[1]", depth + 1, "0", false},
			{prefix + ".limits[2]", "[2]", depth + 1, last, false},
		}
	}
	wants := append([]want{{"[5]", "[5]", 1, "", true}}, motor("[5]", 2, "0")...)
	wants = append(wants, want{"[6]", "[6]", 1, "", true})
	wants = append(wants, motor("[6]", 2, "9")...)
	check("まるごと", monitoringValue(t, svc, whole.ID).Rows, wants)

	// 要素を指した項目は、その中身だけ。パスは変数の先頭から
	v := monitoringValue(t, svc, part.ID)
	if v.Label != "Line[6]" {
		t.Fatalf("要素の項目の名前 = %q", v.Label)
	}
	check("要素", v.Rows, motor("[6]", 1, "9"))

	// 多次元配列は、次元ごとに 1 段ずつ（添字は、それぞれの次元の宣言どおり）
	check("多次元", monitoringValue(t, svc, gridItem.ID).Rows, []want{
		{"[0]", "[0]", 1, "", true},
		{"[0][1]", "[1]", 2, "0", false},
		{"[0][2]", "[2]", 2, "0", false},
		{"[1]", "[1]", 1, "", true},
		{"[1][1]", "[1]", 2, "0", false},
		{"[1][2]", "[2]", 2, "0", false},
	})
}

func TestMonitoringValues_RowLimit(t *testing.T) {
	svc, _ := newClientTestService(t)
	big, err := svc.CreateVariable("Big", "ARRAY[0..499] OF INT", nil)
	if err != nil {
		t.Fatal(err)
	}
	item, _ := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: big.ID})
	rows := monitoringValue(t, svc, item.ID).Rows
	if len(rows) != monitoringMaxRows+1 {
		t.Fatalf("行 = %d 個, want %d", len(rows), monitoringMaxRows+1)
	}
	if last := rows[len(rows)-1]; !last.More || last.Label != "…あと 300 行" {
		t.Fatalf("最後の行: %+v", last)
	}

	// 接続先の値も、いくつもあれば 1 つずつの行にする（1 つなら、項目の行に出す）
	var v MonitoringValueDTO
	fillResultValues(&v, []ClientResultValueDTO{{Label: "[0]", Value: "1"}, {Label: "[1]", Value: "2"}})
	if v.Text != "" || len(v.Rows) != 2 || v.Rows[1].Label != "[1]" || v.Rows[1].Text != "2" || v.Rows[1].Depth != 1 {
		t.Fatalf("値が 2 つ: %+v", v)
	}
	v = MonitoringValueDTO{}
	fillResultValues(&v, []ClientResultValueDTO{{Label: "Value", Value: "5"}})
	if v.Text != "5" || len(v.Rows) != 0 {
		t.Fatalf("値が 1 つ: %+v", v)
	}
}

// 起動した直後は変数も接続先もないので、前に使っていたときの変数・接続先の項目は消す（レジスタの項目は残す）
func TestMonitoring_StartupDropsItemsOfMissingVariablesAndClients(t *testing.T) {
	svc, _ := newClientTestService(t)
	v, err := svc.CreateVariable("Counter", "INT", 1)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	reg, _ := svc.AddMonitoringItem(&MonitoringItemDTO{ProtocolType: "modbus-tcp", MemoryArea: "holdingRegisters", Address: 3, BitWidth: 16})
	if _, err := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: v.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindSubscription, ClientID: c.ID, Summary: "watch a"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveMonitoringConfig(); err != nil {
		t.Fatal(err)
	}
	if got := len(readSavedMonitoringItems(t)); got != 3 {
		t.Fatalf("保存した項目 = %d 個, want 3", got)
	}

	// アプリを起動し直す（設定ファイルは残り、変数と接続先はない）
	restarted := NewPLCService()
	t.Cleanup(restarted.Shutdown)
	items := restarted.GetMonitoringItems()
	if len(items) != 1 || items[0].ID != reg.ID {
		t.Fatalf("起動した直後の項目: %+v", items)
	}
	if values := restarted.GetMonitoringValues(); len(values) != 0 {
		t.Fatalf("起動した直後の値（「変数がありません」が出ない）: %+v", values)
	}
}

// プロジェクトを読み込んだあとも、もうない変数・接続先を指す項目は残さない
func TestMonitoring_ImportDropsItemsOfMissingVariablesAndClients(t *testing.T) {
	svc, _ := newClientTestService(t)
	v, _ := svc.CreateVariable("Counter", "INT", 1)
	kept, _ := svc.AddMonitoringItem(&MonitoringItemDTO{Kind: MonitoringKindVariable, VariableID: v.ID})
	reg, _ := svc.AddMonitoringItem(&MonitoringItemDTO{ProtocolType: "modbus-tcp", MemoryArea: "holdingRegisters", Address: 3, BitWidth: 16})
	project := svc.ExportProject()
	// 手で書き足した・古いプロジェクトに残っていた、指す先のない項目
	project.MonitoringItems = append(project.MonitoringItems,
		&MonitoringItemDTO{ID: "orphan-var", Order: 10, Kind: MonitoringKindVariable, VariableID: "nope"},
		&MonitoringItemDTO{ID: "orphan-client", Order: 11, Kind: MonitoringKindPoll, ClientID: "nope", Summary: "x"},
	)
	if err := svc.ImportProject(project); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, item := range svc.GetMonitoringItems() {
		ids[item.ID] = true
	}
	if len(ids) != 2 || !ids[kept.ID] || !ids[reg.ID] {
		t.Fatalf("読み込んだあとの項目: %v", ids)
	}
	// 変数の ID は読み込むと変わるが、項目は同じ変数を指し続ける
	if vars := svc.GetVariables(); len(vars) != 1 || vars[0].ID == v.ID {
		t.Fatalf("読み込んだあとの変数（ID は作り直す）: %+v", vars)
	}
	if val := monitoringValue(t, svc, kept.ID); val.Status != "ok" || val.Text != "1" {
		t.Fatalf("残った変数の項目: %+v", val)
	}
}
