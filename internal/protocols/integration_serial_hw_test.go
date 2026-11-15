package protocols_test

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"karakuri/internal/application"
)

// 実際の COM ポートで、Karakuri の Modbus RTU / ASCII サーバーに、Karakuri の Modbus RTU / ASCII クライアントからつなぐ。
// 2 つのポートをクロスでつなぎ、環境変数で指定したときだけ動く（CI では飛ばす）。
//
//	$env:KARAKURI_SERIAL_PAIR = "COM4,COM6"   # サーバー側, クライアント側
//	go test -p 1 -run HW -v ./internal/protocols/

const (
	modbusRTU   = "modbus-rtu"
	modbusASCII = "modbus-ascii"
)

// serialProtocols は、試すシリアルのプロトコル（データビット・パリティなどは、サーバーとクライアントの既定のまま）
var serialProtocols = []string{modbusRTU, modbusASCII}

func serialPair(t *testing.T) (server, client string) {
	t.Helper()
	parts := strings.Split(os.Getenv("KARAKURI_SERIAL_PAIR"), ",")
	if len(parts) != 2 {
		t.Skip("KARAKURI_SERIAL_PAIR（例: COM4,COM6）が設定されていない")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

// startSerialPair は、プロトコル pt のサーバーを server のポートで起動し、client のポートの接続先を追加する
func startSerialPair(t *testing.T, svc *application.PLCService, pt string, baud int) application.ClientDTO {
	t.Helper()
	serverPort, clientPort := serialPair(t)
	must(t, svc.AddServer(pt, ""))
	cfg := svc.GetServerConfig(pt)
	cfg.Settings["serialPort"] = serverPort
	cfg.Settings["baudRate"] = baud
	must(t, svc.UpdateServerConfig(cfg))
	must(t, svc.StartServer(pt))
	t.Cleanup(func() { _ = svc.StopServer(pt) })

	// ASCII の 9600bps で 125 ワード（511 文字）を読むと 0.5 秒ほどかかる
	c, err := svc.AddClient(pt, "シリアル", map[string]interface{}{
		"serialPort": clientPort, "baudRate": baud, "unitId": 1, "timeoutMs": 2000,
	})
	must(t, err)
	t.Cleanup(func() { _ = svc.DisconnectClient(c.ID) })
	return *c
}

func TestHW_SerialClient_AgainstOwnServer(t *testing.T) {
	for _, pt := range serialProtocols {
		for _, baud := range []int{9600, 115200} {
			t.Run(fmt.Sprintf("%s/%dbps", pt, baud), func(t *testing.T) {
				svc := newService(t)
				c := startSerialPair(t, svc, pt, baud)

				send := func(pdu string) application.ClientCommandLogDTO {
					t.Helper()
					r, err := svc.SendClientRequest(c.ID, pdu)
					must(t, err)
					return *r
				}

				// 書いて、読む（サーバーのレジスタにも入る）
				if r := send("10 00 00 00 03 06 00 0B 00 16 00 21"); r.Status != "ok" {
					t.Fatalf("FC16: %s %s", r.Status, r.Error)
				}
				if w, _ := svc.ReadWords(pt, hr, 0, 3); !reflect.DeepEqual(w, []int{11, 22, 33}) {
					t.Errorf("サーバーのレジスタ = %v", w)
				}
				must(t, svc.WriteWord(pt, hr, 100, 4321))
				r := send("03 00 64 00 01")
				if r.Status != "ok" || len(r.Values) != 1 || r.Values[0].Word == nil || *r.Values[0].Word != 4321 {
					t.Fatalf("FC03: %+v", r)
				}

				// 最大の読み出し（125 ワード）
				r = send("03 00 00 00 7D")
				if r.Status != "ok" || len(r.Values) != 125 {
					t.Fatalf("125 ワード: %s %s（%d 個）", r.Status, r.Error, len(r.Values))
				}

				// 例外応答（対応していないファンクション）
				if r := send("08 00 00 12 34"); r.Status != "exception" {
					t.Errorf("FC08: %s %s", r.Status, r.Error)
				}

				// 応答しない UnitID はタイムアウト。戻せば、また応答する
				must(t, svc.SetUnitIDEnabled(pt, 1, false))
				if r := send("03 00 00 00 01"); r.Status != "timeout" {
					t.Errorf("応答しない UnitID: %s %s", r.Status, r.Error)
				}
				must(t, svc.SetUnitIDEnabled(pt, 1, true))
				if r := send("03 00 00 00 01"); r.Status != "ok" {
					t.Errorf("UnitID を戻したあと: %s %s", r.Status, r.Error)
				}

				// 速さ: 続けて 50 回
				start := time.Now()
				for range 50 {
					if r := send("03 00 00 00 01"); r.Status != "ok" {
						t.Fatalf("連続: %s %s", r.Status, r.Error)
					}
				}
				t.Logf("FC03（1 ワード）50 回 平均 %v", time.Since(start)/50)

				// 次のサブテストが同じポートを開けるよう、ここで閉じる
				svc.Shutdown()
			})
		}
	}
}

// ポーリングで、変数を実機の先のサーバーのレジスタと同期する
func TestHW_SerialClient_VariableMapping(t *testing.T) {
	for _, pt := range serialProtocols {
		t.Run(pt, func(t *testing.T) {
			svc := newService(t)
			c := startSerialPair(t, svc, pt, 115200)
			must(t, svc.SetClientPollInterval(c.ID, 100))
			key := "client:" + c.ID

			must(t, svc.WriteWord(pt, hr, 50, 0))
			must(t, svc.WriteWord(pt, hr, 51, 321))
			v, err := svc.CreateVariable("remote", "DINT", 0)
			must(t, err)
			must(t, svc.UpdateVariableMappings(v.ID, []application.ProtocolMappingDTO{{ProtocolType: key, MemoryArea: hr, Address: 50, Endianness: "big"}}))
			must(t, svc.StartClientPolling(c.ID))

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
				w, _ := svc.ReadWords(pt, hr, 50, 2)
				return reflect.DeepEqual(w, []int{1, 70000 - 65536})
			})

			_ = svc.StopClientPolling(c.ID)
			svc.Shutdown()
		})
	}
}

const finsHostLink = "fins-hostlink"

// FINS の Host Link: Karakuri のサーバー（COM の片側）に、Karakuri のクライアント（もう片側）からつなぐ
func TestHW_FINSHostLink_AgainstOwnServer(t *testing.T) {
	for _, baud := range []int{9600, 115200} {
		t.Run(fmt.Sprintf("%dbps", baud), func(t *testing.T) {
			svc := newService(t)
			c := startSerialPair(t, svc, finsHostLink, baud)

			send := func(hex string) application.ClientCommandLogDTO {
				t.Helper()
				r, err := svc.SendClientRequest(c.ID, hex)
				must(t, err)
				return *r
			}

			// DM 100 から 3 ワード書いて（0102）、サーバーのメモリに入る
			if r := send("01 02 82 00 64 00 00 03 00 0B 00 16 00 21"); r.Status != "ok" {
				t.Fatalf("書き込み: %s %s", r.Status, r.Error)
			}
			if w, _ := svc.ReadWords(finsHostLink, "DM", 100, 3); !reflect.DeepEqual(w, []int{11, 22, 33}) {
				t.Errorf("サーバーの DM = %v", w)
			}
			// 読み出し（0101）
			must(t, svc.WriteWord(finsHostLink, "DM", 200, 4321))
			r := send("01 01 82 00 C8 00 00 01")
			if r.Status != "ok" || len(r.Values) != 1 || r.Values[0].Word == nil || *r.Values[0].Word != 4321 {
				t.Fatalf("読み出し: %+v", r)
			}
			// 長い応答（1 フレームで返る）。9600bps・7E2 は 1 文字 11 ビットなので、500 ワード（約 2000 文字）は
			// 2 秒を超える。タイムアウト（2 秒）に収まるよう 100 ワードにする
			count := 500
			if baud == 9600 {
				count = 100
			}
			r = send(fmt.Sprintf("01 01 82 00 00 00 %02X %02X", count>>8, count&0xFF))
			if r.Status != "ok" || len(r.Values) != count {
				t.Fatalf("%d ワード: %s %s（%d 個）", count, r.Status, r.Error, len(r.Values))
			}
			// FINS の異常終了（未定義のコマンド）
			if r := send("29 99"); r.Status != "exception" {
				t.Errorf("未定義のコマンド: %s %s", r.Status, r.Error)
			}

			start := time.Now()
			for range 30 {
				if r := send("01 01 82 00 00 00 00 01"); r.Status != "ok" {
					t.Fatalf("連続: %s %s", r.Status, r.Error)
				}
			}
			t.Logf("0101（1 ワード）30 回 平均 %v", time.Since(start)/30)
			svc.Shutdown()
		})
	}
}

func TestHW_FINSHostLink_VariableMapping(t *testing.T) {
	svc := newService(t)
	c := startSerialPair(t, svc, finsHostLink, 115200)
	must(t, svc.SetClientPollInterval(c.ID, 100))
	key := "client:" + c.ID

	must(t, svc.WriteWord(finsHostLink, "DM", 50, 0))
	must(t, svc.WriteWord(finsHostLink, "DM", 51, 321))
	v, err := svc.CreateVariable("remote", "DINT", 0)
	must(t, err)
	must(t, svc.UpdateVariableMappings(v.ID, []application.ProtocolMappingDTO{{ProtocolType: key, MemoryArea: "DM", Address: 50, Endianness: "big"}}))
	must(t, svc.StartClientPolling(c.ID))

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
		w, _ := svc.ReadWords(finsHostLink, "DM", 50, 2)
		return reflect.DeepEqual(w, []int{1, 70000 - 65536})
	})
	_ = svc.StopClientPolling(c.ID)
	svc.Shutdown()
}
