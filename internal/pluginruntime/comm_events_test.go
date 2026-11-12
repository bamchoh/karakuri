package pluginruntime

import "testing"

func TestCommEventHub_CoalescesEventsPerSubscriber(t *testing.T) {
	h := newCommEventHub()
	h.EmitConnection(2) // 購読者がいなくても接続数は覚えている

	sub := h.subscribe()
	defer h.unsubscribe(sub)

	// 購読を始めると、現在の接続数が届く
	ev := h.take(sub)
	if ev == nil || !ev.ConnectionChanged || ev.ConnectionCount != 2 || ev.Rx || ev.Tx {
		t.Fatalf("initial event = %+v", ev)
	}
	if ev := h.take(sub); ev != nil {
		t.Fatalf("nothing pending, got %+v", ev)
	}

	// 何度発行しても、次に取り出すまでは 1 件にまとまる
	for i := 0; i < 1000; i++ {
		h.EmitRx()
		h.EmitTx()
	}
	h.EmitConnection(3)
	select {
	case <-sub.notify:
	default:
		t.Fatal("subscriber was not notified")
	}
	ev = h.take(sub)
	if ev == nil || !ev.Rx || !ev.Tx || !ev.ConnectionChanged || ev.ConnectionCount != 3 {
		t.Fatalf("coalesced event = %+v", ev)
	}
	if ev := h.take(sub); ev != nil {
		t.Fatalf("events were not cleared: %+v", ev)
	}

	// 受信だけなら、接続数の変化は付かない
	h.EmitRx()
	if ev := h.take(sub); ev == nil || !ev.Rx || ev.Tx || ev.ConnectionChanged {
		t.Fatalf("rx only = %+v", ev)
	}

	// 購読をやめたあとは溜めない
	h.unsubscribe(sub)
	h.EmitRx()
	if ev := h.take(sub); ev != nil {
		t.Fatalf("unsubscribed subscriber got %+v", ev)
	}
}

func TestCommEventHub_SubscribersAreIndependent(t *testing.T) {
	h := newCommEventHub()
	a, b := h.subscribe(), h.subscribe()
	h.take(a)
	h.take(b)

	h.EmitTx()
	if ev := h.take(a); ev == nil || !ev.Tx {
		t.Fatalf("a = %+v", ev)
	}
	// a が取り出しても、b の分は残っている
	if ev := h.take(b); ev == nil || !ev.Tx {
		t.Fatalf("b = %+v", ev)
	}
}
