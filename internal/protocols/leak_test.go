package protocols_test

import (
	"bytes"
	"fmt"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"

	"karakuri/internal/application"
)

// サーバーの起動・停止、クライアントの接続・送信・切断、ポーリングとサブスクリプションの開始・停止、
// 接続先の追加・削除、スクリプトの開始・停止をくり返しても、ゴルーチンが残らない（増え続けない）。
// アプリは長く動かし続けるので、後片付けの漏れは時間がたってから効いてくる。
// シリアルのサーバー・クライアントは、COM ポートがないので対象外（Stop → Start は各パッケージの試験で確かめている）
func TestNoGoroutineLeak_RepeatedLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("-short では飛ばす")
	}
	svc := newService(t)
	ports := map[string]int{modbusTCP: addModbus(t, svc), finsType: addFINS(t, svc), slmpType: addSLMP(t, svc)}
	must(t, svc.AddServer(opcUA, ""))
	ports[opcUA] = freePort(t)
	cfg := svc.GetServerConfig(opcUA)
	cfg.Settings["host"] = "127.0.0.1"
	cfg.Settings["port"] = ports[opcUA]
	must(t, svc.UpdateServerConfig(cfg))

	v, err := svc.CreateVariable("Counter", "INT", 0)
	must(t, err)
	must(t, svc.UpdateVariableNodePublishing(v.ID, opcUA, &application.NodePublishingDTO{ProtocolType: opcUA, Enabled: true, AccessMode: "readwrite"}))
	script, err := svc.CreateScript("カウンター", "vars.Counter = vars.Counter + 1;", 50)
	must(t, err)

	// 自分のサーバーにつなぐ接続先と、その読み取りのリクエスト
	clientSettings := func(pt string) map[string]interface{} {
		switch pt {
		case modbusTCP:
			return map[string]interface{}{"host": "127.0.0.1", "port": ports[pt], "timeoutMs": 500}
		case opcUA:
			return map[string]interface{}{"endpointUrl": fmt.Sprintf("opc.tcp://127.0.0.1:%d", ports[pt]), "timeoutMs": 2000}
		default: // FINS / SLMP
			return map[string]interface{}{"host": "127.0.0.1", "port": ports[pt], "transport": "tcp", "timeoutMs": 500}
		}
	}
	type target struct {
		pt, id, request string
		pollCmd         string
		pollParams      map[string]interface{}
	}
	var targets []target
	for _, cp := range svc.GetClientProtocols() {
		if _, ok := ports[cp.ProtocolType]; !ok {
			continue // シリアル
		}
		c, err := svc.AddClient(cp.ProtocolType, "自分 "+cp.ProtocolType, clientSettings(cp.ProtocolType))
		must(t, err)
		tg := target{pt: cp.ProtocolType, id: c.ID}
		for _, cmd := range cp.Commands {
			if cmd.Pollable {
				tg.pollCmd, tg.pollParams = cmd.ID, defaultParams(cmd.Fields)
				if cp.ProtocolType == opcUA {
					tg.pollParams["nodeId"] = "ns=2;s=Counter"
				}
				tg.request, err = svc.BuildClientRequest(cp.ProtocolType, cmd.ID, tg.pollParams)
				must(t, err)
				break
			}
		}
		_, err = svc.AddClientPollItem(c.ID, tg.pollCmd, tg.pollParams, "")
		must(t, err)
		must(t, svc.SetClientPollInterval(c.ID, 50))
		if cp.SupportsSubscription {
			_, err := svc.AddClientSubscriptionItems(c.ID, map[string]interface{}{"nodeId": "ns=2;s=Counter"})
			must(t, err)
			must(t, svc.SetClientSubscriptionInterval(c.ID, 50))
		}
		targets = append(targets, tg)
	}
	if len(targets) != 4 {
		t.Fatalf("自分のサーバーにつなぐ接続先が %d 個（want 4）", len(targets))
	}

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("5 秒待っても、%s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	cycle := func() {
		for pt := range ports {
			must(t, svc.StartServer(pt))
		}
		must(t, svc.StartScript(script.ID))
		for _, tg := range targets {
			// 送信（応答が返る）
			log, err := svc.SendClientRequest(tg.id, tg.request)
			must(t, err)
			if log.Status != "ok" {
				t.Fatalf("%s: 送信の結果 %s（%s）", tg.pt, log.Status, log.Error)
			}
			// ポーリングを 1 周以上回して止める
			must(t, svc.StartClientPolling(tg.id))
			waitFor(tg.pt+" のポーリングが回らない", func() bool {
				p, err := svc.GetClientPolling(tg.id)
				return err == nil && p.Cycles > 0
			})
			must(t, svc.StopClientPolling(tg.id))
			if tg.pt == opcUA {
				must(t, svc.StartClientSubscription(tg.id))
				waitFor("サブスクリプションが監視中にならない", func() bool {
					s, err := svc.GetClientSubscription(tg.id)
					return err == nil && s.State == "ok"
				})
				must(t, svc.StopClientSubscription(tg.id))
			}
			must(t, svc.DisconnectClient(tg.id))
		}
		// 接続先の追加・送信・削除（削除で接続が閉じる）
		tmp, err := svc.AddClient(modbusTCP, "一時", clientSettings(modbusTCP))
		must(t, err)
		_, err = svc.SendClientRequest(tmp.ID, targets[0].request)
		must(t, err)
		must(t, svc.RemoveClient(tmp.ID))

		must(t, svc.StopScript(script.ID))
		for pt := range ports {
			must(t, svc.StopServer(pt))
		}
	}

	// 最初の数回は、一度だけ作られて残るもの（ライブラリの中のゴルーチンなど）ができるので、数えない
	for range 3 {
		cycle()
	}
	before := settledGoroutines()
	const rounds = 20
	for range rounds {
		cycle()
	}
	// 終わったゴルーチンが消えるまで、少し待つ
	var after int
	deadline := time.Now().Add(5 * time.Second)
	for {
		after = settledGoroutines()
		if after <= before+2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if after > before+2 {
		var buf bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&buf, 1)
		t.Fatalf("%d 回くり返したら、ゴルーチンが %d → %d に増えた\n%s", rounds, before, after, buf.String())
	}
	t.Logf("ゴルーチン: %d → %d（%d 回くり返したあと）", before, after, rounds)
}

// settledGoroutines は、短い間をおいて数えたゴルーチンの数の、少ないほう（終わりかけのものを数えにくくする）
func settledGoroutines() int {
	n := runtime.NumGoroutine()
	for range 5 {
		time.Sleep(20 * time.Millisecond)
		n = min(n, runtime.NumGoroutine())
	}
	return n
}
