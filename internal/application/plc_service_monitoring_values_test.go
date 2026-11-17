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
	if v := monitoringValue(t, svc, idArr); v.Text != "[0,7,0]" {
		t.Fatalf("Values: %+v", v)
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

func TestMonitoringValues_ManyValuesInOneItem(t *testing.T) {
	values := make([]ClientResultValueDTO, 20)
	for i := range values {
		values[i] = ClientResultValueDTO{Label: "v", Value: "1"}
	}
	if got := resultValuesText(values[:2]); got != "v: 1, v: 1" {
		t.Fatalf("2 個 = %q", got)
	}
	got := resultValuesText(values)
	if want := "…（全 20 個）"; len(got) < len(want) || got[len(got)-len(want):] != want {
		t.Fatalf("20 個は途中で切る: %q", got)
	}
}
