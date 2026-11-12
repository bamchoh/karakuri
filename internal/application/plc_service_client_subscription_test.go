package application

import (
	"testing"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/protocoltest"
)

func subscription(t *testing.T, svc *PLCService, id string) *ClientSubscriptionDTO {
	t.Helper()
	s, err := svc.GetClientSubscription(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// lastSubscription は、フェイクのクライアントでいちばん新しく始まった監視を返す（n 個目が始まるまで待つ）
func lastSubscription(t *testing.T, f *protocoltest.ClientFactory, n int) *protocoltest.Subscription {
	t.Helper()
	var subs []*protocoltest.Subscription
	waitFor(t, "監視の開始", func() bool {
		subs = nil
		for _, c := range f.CreatedClients() {
			subs = append(subs, c.Subscriptions()...)
		}
		return len(subs) >= n
	})
	if len(subs) != n {
		t.Fatalf("始まった監視 = %d 個, want %d", len(subs), n)
	}
	return subs[n-1]
}

func TestSubscription_Items(t *testing.T) {
	svc, _ := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)

	protos := svc.GetClientProtocols()
	if !protos[0].SupportsSubscription || len(protos[0].MonitoredItemFields) != 1 {
		t.Fatalf("サブスクリプションの入力項目: %+v", protos[0])
	}
	s := subscription(t, svc, c.ID)
	if s.IntervalMs != defaultSubscriptionIntervalMs || s.Running || s.State != "" || len(s.Items) != 0 {
		t.Fatalf("初期状態: %+v", s)
	}
	if err := svc.StartClientSubscription(c.ID); err == nil {
		t.Fatal("監視項目がなければ始められない")
	}
	if _, err := svc.AddClientSubscriptionItems(c.ID, map[string]interface{}{"keys": " "}); err == nil {
		t.Fatal("使えない入力はエラー")
	}

	// 1 回の入力から、いくつも足される
	added, err := svc.AddClientSubscriptionItems(c.ID, map[string]interface{}{"keys": "a, b"})
	if err != nil || len(added) != 2 || added[0].Summary != "watch a" || added[1].Params["keys"] != "b" || added[0].ID == added[1].ID {
		t.Fatalf("追加: %+v, %v", added, err)
	}
	if err := svc.RemoveClientSubscriptionItem(c.ID, "nope"); err == nil {
		t.Fatal("ない項目の削除はエラー")
	}
	if err := svc.RemoveClientSubscriptionItem(c.ID, added[0].ID); err != nil {
		t.Fatal(err)
	}
	if s := subscription(t, svc, c.ID); len(s.Items) != 1 || s.Items[0].ID != added[1].ID {
		t.Fatalf("削除後: %+v", s.Items)
	}

	for _, ms := range []int{minSubscriptionIntervalMs - 1, maxSubscriptionIntervalMs + 1} {
		if err := svc.SetClientSubscriptionInterval(c.ID, ms); err == nil {
			t.Fatalf("間隔 %d ms はエラー", ms)
		}
	}
	if err := svc.SetClientSubscriptionInterval(c.ID, 250); err != nil {
		t.Fatal(err)
	}
	if s := subscription(t, svc, c.ID); s.IntervalMs != 250 {
		t.Fatalf("間隔 = %d", s.IntervalMs)
	}
	if _, err := svc.GetClientSubscription("nope"); err == nil {
		t.Fatal("ない接続先はエラー")
	}
}

func TestSubscription_ReceivesValuesAndStates(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	items, _ := svc.AddClientSubscriptionItems(c.ID, map[string]interface{}{"keys": "a,b"})
	if err := svc.SetClientSubscriptionInterval(c.ID, 100); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartClientSubscription(c.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartClientSubscription(c.ID); err != nil {
		t.Fatal("監視中に始めても何もしない:", err)
	}
	sub := lastSubscription(t, f, 1)
	if sub.Request.Interval.Milliseconds() != 100 || len(sub.Request.Items) != 2 || sub.Request.Items[0].ID != items[0].ID {
		t.Fatalf("監視の内容: %+v", sub.Request)
	}
	waitFor(t, "監視中", func() bool { s := subscription(t, svc, c.ID); return s.Running && s.State == "ok" })
	if !svc.GetClients()[0].Subscribing {
		t.Fatal("接続先の一覧に、監視中と出る")
	}

	// 値が届くと、項目に入る
	sub.EmitValue("a", "1")
	sub.EmitValue("a", "2")
	waitFor(t, "a = 2", func() bool {
		it := subscription(t, svc, c.ID).Items[0]
		return it.Status == "ok" && it.Count == 2 && len(it.Values) == 1 && it.Values[0].Value == "2"
	})
	if it := subscription(t, svc, c.ID).Items[1]; it.Status != "" || it.Count != 0 {
		t.Fatalf("届いていない項目: %+v", it)
	}

	// 機器が断った項目は、理由を出す。届いていた値は残す
	sub.Emit(protocol.SubscriptionEvent{ItemID: items[0].ID, Status: protocol.CommandException, Error: "bad"})
	waitFor(t, "a が例外", func() bool {
		it := subscription(t, svc, c.ID).Items[0]
		return it.Status == "exception" && it.Error == "bad" && len(it.Values) == 1 && it.Values[0].Value == "2" && it.Count == 2
	})

	// 切れた・つながったは、全体の状態に出る
	sub.Emit(protocol.SubscriptionEvent{Status: protocol.CommandError, Error: "切れた"})
	waitFor(t, "切れた", func() bool {
		s := subscription(t, svc, c.ID)
		return s.State == "error" && s.Error == "切れた" && s.Running
	})
	sub.Emit(protocol.SubscriptionEvent{Status: protocol.CommandOK})
	waitFor(t, "戻った", func() bool { s := subscription(t, svc, c.ID); return s.State == "ok" && s.Error == "" })

	// 止めると、監視のゴルーチンが終わるまで待つ。値は残る
	if err := svc.StopClientSubscription(c.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sub.Done():
	default:
		t.Fatal("止めたら、監視が終わっている")
	}
	s := subscription(t, svc, c.ID)
	if s.Running || s.State != "" || s.Items[0].Values[0].Value != "2" || svc.GetClients()[0].Subscribing {
		t.Fatalf("停止後: %+v", s)
	}
}

// 監視中に項目・間隔・接続先の設定を変えると、監視を作り直す
func TestSubscription_RestartsOnChange(t *testing.T) {
	svc, f := newClientTestService(t)
	c, _ := svc.AddClient("fake-client", "PLC", nil)
	items, _ := svc.AddClientSubscriptionItems(c.ID, map[string]interface{}{"keys": "a"})
	if err := svc.StartClientSubscription(c.ID); err != nil {
		t.Fatal(err)
	}
	first := lastSubscription(t, f, 1)
	first.EmitValue("a", "1")
	waitFor(t, "a = 1", func() bool { return subscription(t, svc, c.ID).Items[0].Count == 1 })

	// 項目を足す
	if _, err := svc.AddClientSubscriptionItems(c.ID, map[string]interface{}{"keys": "b"}); err != nil {
		t.Fatal(err)
	}
	second := lastSubscription(t, f, 2)
	select {
	case <-first.Done():
	default:
		t.Fatal("作り直す前の監視は終わっている")
	}
	if len(second.Request.Items) != 2 {
		t.Fatalf("作り直した監視の項目: %+v", second.Request.Items)
	}
	// 作り直すと、項目の状態は「まだ届いていない」に戻る。届いていた値は残る
	waitFor(t, "監視中", func() bool { return subscription(t, svc, c.ID).State == "ok" })
	if it := subscription(t, svc, c.ID).Items[0]; it.Status != "" || len(it.Values) != 1 || it.Count != 1 {
		t.Fatalf("作り直したあとの項目: %+v", it)
	}
	// 古い監視からの知らせは届かない
	if first.EmitValue("a", "古い") {
		t.Fatal("終わった監視には送れない")
	}

	// 間隔を変える（同じ値なら作り直さない）
	if err := svc.SetClientSubscriptionInterval(c.ID, defaultSubscriptionIntervalMs); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetClientSubscriptionInterval(c.ID, 300); err != nil {
		t.Fatal(err)
	}
	third := lastSubscription(t, f, 3)
	if third.Request.Interval.Milliseconds() != 300 {
		t.Fatalf("間隔: %v", third.Request.Interval)
	}

	// 接続先の設定を変えると、新しいクライアントで始め直す
	if err := svc.UpdateClient(c.ID, "PLC", map[string]interface{}{"target": "other"}); err != nil {
		t.Fatal(err)
	}
	fourth := lastSubscription(t, f, 4)
	clients := f.CreatedClients()
	if got := clients[len(clients)-1].Subscriptions(); len(got) != 1 || got[0] != fourth {
		t.Fatalf("新しいクライアントで監視していない: %d 個", len(got))
	}
	waitFor(t, "監視中", func() bool { s := subscription(t, svc, c.ID); return s.Running && s.State == "ok" })

	// 項目を消す。最後の 1 つを消したら、監視は止まる
	if err := svc.RemoveClientSubscriptionItem(c.ID, items[0].ID); err != nil {
		t.Fatal(err)
	}
	fifth := lastSubscription(t, f, 5)
	if len(fifth.Request.Items) != 1 {
		t.Fatalf("項目: %+v", fifth.Request.Items)
	}
	if err := svc.RemoveClientSubscriptionItem(c.ID, fifth.Request.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	<-fifth.Done()
	if s := subscription(t, svc, c.ID); s.Running || len(s.Items) != 0 {
		t.Fatalf("項目がなくなったあと: %+v", s)
	}
}

// 接続先の削除・プロジェクトの読み込み・終了で、監視は止まる。監視項目と間隔はプロジェクトに残る
func TestSubscription_StopsAndPersists(t *testing.T) {
	svc, f := newClientTestService(t)
	a, _ := svc.AddClient("fake-client", "A", nil)
	b, _ := svc.AddClient("fake-client", "B", nil)
	for _, id := range []string{a.ID, b.ID} {
		if _, err := svc.AddClientSubscriptionItems(id, map[string]interface{}{"keys": "x,y"}); err != nil {
			t.Fatal(err)
		}
		if err := svc.StartClientSubscription(id); err != nil {
			t.Fatal(err)
		}
	}
	lastSubscription(t, f, 2)
	if err := svc.SetClientSubscriptionInterval(b.ID, 400); err != nil {
		t.Fatal(err)
	}
	lastSubscription(t, f, 3)

	if err := svc.RemoveClient(a.ID); err != nil {
		t.Fatal(err)
	}
	project := svc.ExportProject()
	if len(project.Clients) != 1 || project.Clients[0].Subscription == nil ||
		project.Clients[0].Subscription.IntervalMs != 400 || len(project.Clients[0].Subscription.Items) != 2 {
		t.Fatalf("保存した内容: %+v", project.Clients)
	}
	if err := svc.ImportProject(project); err != nil {
		t.Fatal(err)
	}
	// 削除・読み込みで、それまでの監視はすべて終わっている
	for _, c := range f.CreatedClients() {
		for _, sub := range c.Subscriptions() {
			select {
			case <-sub.Done():
			default:
				t.Fatal("終わっていない監視がある")
			}
		}
	}
	// 読み込んだら停止中。項目と間隔は戻る
	clients := svc.GetClients()
	s := subscription(t, svc, clients[0].ID)
	if s.Running || s.IntervalMs != 400 || len(s.Items) != 2 || s.Items[1].Summary != "watch y" {
		t.Fatalf("読み込んだ内容: %+v", s)
	}

	if err := svc.StartClientSubscription(clients[0].ID); err != nil {
		t.Fatal(err)
	}
	last := lastSubscription(t, f, 4)
	svc.Shutdown()
	<-last.Done()
}
