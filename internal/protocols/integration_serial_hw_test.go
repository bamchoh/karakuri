package protocols_test

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"karakuri/internal/application"
)

// 実際の COM ポートで、Karakuri の Modbus RTU サーバーに、Karakuri の Modbus RTU クライアントからつなぐ。
// 2 つのポートをクロスでつなぎ、環境変数で指定したときだけ動く（CI では飛ばす）。
//
//	$env:KARAKURI_SERIAL_PAIR = "COM4,COM6"   # サーバー側, クライアント側
//	go test -run HW -v ./internal/protocols/

const modbusRTU = "modbus-rtu"

func serialPair(t *testing.T) (server, client string) {
	t.Helper()
	parts := strings.Split(os.Getenv("KARAKURI_SERIAL_PAIR"), ",")
	if len(parts) != 2 {
		t.Skip("KARAKURI_SERIAL_PAIR（例: COM4,COM6）が設定されていない")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

// startRTUPair は、サーバーを server のポートで起動し、client のポートの接続先を追加する
func startRTUPair(t *testing.T, svc *application.PLCService, baud int) application.ClientDTO {
	t.Helper()
	serverPort, clientPort := serialPair(t)
	must(t, svc.AddServer(modbusRTU, ""))
	cfg := svc.GetServerConfig(modbusRTU)
	cfg.Settings["serialPort"] = serverPort
	cfg.Settings["baudRate"] = baud
	must(t, svc.UpdateServerConfig(cfg))
	must(t, svc.StartServer(modbusRTU))
	t.Cleanup(func() { _ = svc.StopServer(modbusRTU) })

	c, err := svc.AddClient(modbusRTU, "RTU", map[string]interface{}{
		"serialPort": clientPort, "baudRate": baud, "unitId": 1, "timeoutMs": 500,
	})
	must(t, err)
	t.Cleanup(func() { _ = svc.DisconnectClient(c.ID) })
	return *c
}

func TestHW_RTUClient_AgainstOwnServer(t *testing.T) {
	for _, baud := range []int{9600, 115200} {
		svc := newService(t)
		c := startRTUPair(t, svc, baud)

		send := func(pdu string) application.ClientCommandLogDTO {
			t.Helper()
			r, err := svc.SendClientRequest(c.ID, pdu)
			must(t, err)
			return *r
		}

		// 書いて、読む（サーバーのレジスタにも入る）
		if r := send("10 00 00 00 03 06 00 0B 00 16 00 21"); r.Status != "ok" {
			t.Fatalf("%dbps FC16: %s %s", baud, r.Status, r.Error)
		}
		if w, _ := svc.ReadWords(modbusRTU, hr, 0, 3); !reflect.DeepEqual(w, []int{11, 22, 33}) {
			t.Errorf("%dbps: サーバーのレジスタ = %v", baud, w)
		}
		must(t, svc.WriteWord(modbusRTU, hr, 100, 4321))
		r := send("03 00 64 00 01")
		if r.Status != "ok" || len(r.Values) != 1 || r.Values[0].Word == nil || *r.Values[0].Word != 4321 {
			t.Fatalf("%dbps FC03: %+v", baud, r)
		}

		// 最大の読み出し（125 ワード。255 バイトの応答）
		r = send("03 00 00 00 7D")
		if r.Status != "ok" || len(r.Values) != 125 {
			t.Fatalf("%dbps 125 ワード: %s %s（%d 個）", baud, r.Status, r.Error, len(r.Values))
		}

		// 例外応答（範囲外ではなく、対応していないファンクション）
		if r := send("08 00 00 12 34"); r.Status != "exception" {
			t.Errorf("%dbps FC08: %s %s", baud, r.Status, r.Error)
		}

		// 応答しない UnitID はタイムアウト。戻せば、また応答する
		must(t, svc.SetUnitIDEnabled(modbusRTU, 1, false))
		if r := send("03 00 00 00 01"); r.Status != "timeout" {
			t.Errorf("%dbps 応答しない UnitID: %s %s", baud, r.Status, r.Error)
		}
		must(t, svc.SetUnitIDEnabled(modbusRTU, 1, true))
		if r := send("03 00 00 00 01"); r.Status != "ok" {
			t.Errorf("%dbps UnitID を戻したあと: %s %s", baud, r.Status, r.Error)
		}

		// 速さ: 続けて 50 回
		start := time.Now()
		for range 50 {
			if r := send("03 00 00 00 01"); r.Status != "ok" {
				t.Fatalf("%dbps 連続: %s %s", baud, r.Status, r.Error)
			}
		}
		t.Logf("%dbps: FC03（1 ワード）50 回 平均 %v", baud, time.Since(start)/50)

		svc.Shutdown()
	}
}

// ポーリングで、変数を実機の先のサーバーのレジスタと同期する
func TestHW_RTUClient_VariableMapping(t *testing.T) {
	svc := newService(t)
	c := startRTUPair(t, svc, 115200)
	must(t, svc.SetClientPollInterval(c.ID, 100))
	key := "client:" + c.ID

	must(t, svc.WriteWord(modbusRTU, hr, 50, 0))
	must(t, svc.WriteWord(modbusRTU, hr, 51, 321))
	v, err := svc.CreateVariable("remote", "DINT", 0)
	must(t, err)
	must(t, svc.UpdateVariableMappings(v.ID, []application.ProtocolMappingDTO{{ProtocolType: key, MemoryArea: hr, Address: 50, Endianness: "big"}}))
	must(t, svc.StartClientPolling(c.ID))
	t.Cleanup(func() { _ = svc.StopClientPolling(c.ID) })

	waitUntil := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("待っても %s にならない（値 %s、状態 %+v）", what, variableValue(t, svc, v.ID), svc.VariableDeviceStatus(v.ID))
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitUntil("機器の値を読む", func() bool { return variableValue(t, svc, v.ID) == "321" })

	must(t, svc.UpdateVariableValue(v.ID, 70000))
	waitUntil("書く", func() bool {
		w, _ := svc.ReadWords(modbusRTU, hr, 50, 2)
		return reflect.DeepEqual(w, []int{1, 70000 - 65536})
	})
}
