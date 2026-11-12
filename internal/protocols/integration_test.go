package protocols_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	uaclient "github.com/bamchoh/uastack/client"
	"github.com/bamchoh/uastack/ua"
	"github.com/simonvetter/modbus"

	"karakuri/internal/application"
	"karakuri/internal/infrastructure/appconfig"
	"karakuri/internal/protocols"
)

// このファイルは、アプリと同じ組み立て（PLCService に組み込みのプロトコルを登録したもの）で、
// 実際のプロトコルのクライアントからつないで確認する。
// ホスト側だけのテスト（フェイクのプロトコルを使う）では確認できない、
// 「サーバーの起動・停止をまたいだ状態」と「変数との同期」を対象にする。

// TestMain は設定フォルダを一時フォルダに差し替える（開発 PC の実際の設定を書き換えないため）
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "karakuri-protocols-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv(appconfig.EnvDir, dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

const (
	modbusTCP = "modbus-tcp"
	opcUA     = "opcua"
	hr        = "holdingRegisters"
)

// newService はアプリと同じように、組み込みのプロトコルを登録した PLCService を作る
func newService(t *testing.T) *application.PLCService {
	t.Helper()
	svc := application.NewPLCService()
	for _, f := range protocols.Factories() {
		svc.RegisterFactory(f)
	}
	for _, f := range protocols.ClientFactories() {
		svc.RegisterClientFactory(f)
	}
	svc.ClearMonitoringItems()
	t.Cleanup(svc.Shutdown)
	return svc
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// addModbus は Modbus TCP サーバーを追加し、127.0.0.1 の空きポートを設定する（起動はしない）
func addModbus(t *testing.T, svc *application.PLCService) int {
	t.Helper()
	must(t, svc.AddServer(modbusTCP, ""))
	port := freePort(t)
	cfg := svc.GetServerConfig(modbusTCP)
	cfg.Settings["tcpAddress"] = "127.0.0.1"
	cfg.Settings["tcpPort"] = port
	must(t, svc.UpdateServerConfig(cfg))
	return port
}

func modbusClient(t *testing.T, port int) *modbus.ModbusClient {
	t.Helper()
	c, err := modbus.NewClient(&modbus.ClientConfiguration{
		URL:     fmt.Sprintf("tcp://127.0.0.1:%d", port),
		Timeout: 2 * time.Second,
	})
	must(t, err)
	must(t, c.Open())
	t.Cleanup(func() { _ = c.Close() })
	must(t, c.SetUnitId(1))
	return c
}

// variableValue は変数の現在の値を文字列で返す
func variableValue(t *testing.T, svc *application.PLCService, id string) string {
	t.Helper()
	for _, v := range svc.GetVariables() {
		if v.ID == id {
			return fmt.Sprint(v.Value)
		}
	}
	t.Fatalf("変数 %s がない", id)
	return ""
}

// ===== 組み込みのプロトコルの一覧 =====

func TestFactories(t *testing.T) {
	svc := newService(t)

	var types []string
	for _, p := range svc.GetAvailableProtocols() {
		types = append(types, p.Type)
		// どのプロトコルも、設定項目を持つバリアントが 1 つ以上ある
		schema, err := svc.GetProtocolSchema(p.Type)
		if err != nil {
			t.Errorf("%s: GetProtocolSchema: %v", p.Type, err)
			continue
		}
		if len(schema.Variants) == 0 || len(schema.Variants[0].Fields) == 0 {
			t.Errorf("%s: 設定項目がない: %+v", p.Type, schema)
		}
	}
	sort.Strings(types)
	if want := []string{"modbus-ascii", "modbus-rtu", "modbus-tcp", "opcua"}; !reflect.DeepEqual(types, want) {
		t.Errorf("プロトコル = %v, want %v", types, want)
	}

	// Factories は呼ぶたびに別のファクトリーを返す（PLCService 同士で状態を共有しない）
	a, b := protocols.Factories(), protocols.Factories()
	for i := range a {
		if a[i] == b[i] {
			t.Errorf("%s: 同じファクトリーが返った", a[i].ProtocolType())
		}
	}
}

// バリアントを指定しなければ、そのプロトコルの最初のバリアントになる
func TestAddServer_DefaultVariant(t *testing.T) {
	svc := newService(t)
	must(t, svc.AddServer(modbusTCP, ""))
	want := svc.GetAvailableProtocols()
	var wantVariant string
	for _, p := range want {
		if p.Type == modbusTCP {
			wantVariant = p.Variants[0].ID
		}
	}
	if got := svc.GetServerInstances()[0].Variant; got == "" || got != wantVariant {
		t.Errorf("バリアント = %q, want %q", got, wantVariant)
	}
	// 設定画面は、このバリアントの設定項目を出す
	schema, _ := svc.GetProtocolSchema(modbusTCP)
	found := false
	for _, v := range schema.Variants {
		found = found || v.ID == wantVariant
	}
	if !found {
		t.Errorf("スキーマにバリアント %q がない: %+v", wantVariant, schema.Variants)
	}
}

// ===== Modbus: レジスタの寿命（#44） =====

func TestModbus_RegistersBeforeStartAndAcrossRestart(t *testing.T) {
	svc := newService(t)
	port := addModbus(t, svc)

	// 一度も起動していなくても、エリアの一覧が取れて読み書きできる
	if areas := svc.GetMemoryAreas(modbusTCP); len(areas) != 4 {
		t.Fatalf("起動前のエリア数 = %d, want 4", len(areas))
	}
	must(t, svc.WriteWord(modbusTCP, hr, 5, 77))
	must(t, svc.WriteBit(modbusTCP, "coils", 5, true))

	// 起動すると、クライアントから同じ値が読める
	must(t, svc.StartServer(modbusTCP))
	c := modbusClient(t, port)
	if got, err := c.ReadRegister(5, modbus.HOLDING_REGISTER); err != nil || got != 77 {
		t.Errorf("起動後にクライアントが読んだ値 = %d, %v, want 77", got, err)
	}
	if got, err := c.ReadCoil(5); err != nil || !got {
		t.Errorf("起動後にクライアントが読んだコイル = %v, %v, want true", got, err)
	}

	// クライアントが書いた値は、停止中も、起動し直したあとも残る
	must(t, c.WriteRegister(6, 88))
	for i := 0; i < 2; i++ {
		must(t, svc.StopServer(modbusTCP))
		if got, err := svc.ReadWords(modbusTCP, hr, 5, 2); err != nil || !reflect.DeepEqual(got, []int{77, 88}) {
			t.Errorf("停止中（%d 回目）= %v, %v, want [77 88]", i+1, got, err)
		}
		must(t, svc.StartServer(modbusTCP))
		c2 := modbusClient(t, port)
		if got, err := c2.ReadRegisters(5, 2, modbus.HOLDING_REGISTER); err != nil || !reflect.DeepEqual(got, []uint16{77, 88}) {
			t.Errorf("起動し直したあと（%d 回目）= %v, %v, want [77 88]", i+1, got, err)
		}
	}
}

// ===== Modbus: 変数との同期 =====

func TestModbus_VariableSync(t *testing.T) {
	svc := newService(t)
	port := addModbus(t, svc)
	speed, err := svc.CreateVariable("Speed", "INT", 12)
	must(t, err)
	run, err := svc.CreateVariable("Run", "BOOL", true)
	must(t, err)
	big, err := svc.CreateVariable("Big", "DINT", 0x12345678)
	must(t, err)
	mapTo := func(id, area string, addr int) {
		must(t, svc.UpdateVariableMappings(id, []application.ProtocolMappingDTO{
			{ProtocolType: modbusTCP, MemoryArea: area, Address: addr, Endianness: "big"},
		}))
	}

	// マッピングを設定した時点（起動前）で、変数の値がレジスタに入る
	mapTo(speed.ID, hr, 10)
	mapTo(run.ID, "coils", 3)
	mapTo(big.ID, hr, 20)
	if got, _ := svc.ReadWords(modbusTCP, hr, 10, 1); !reflect.DeepEqual(got, []int{12}) {
		t.Errorf("起動前: レジスタ = %v, want [12]", got)
	}
	if got, _ := svc.ReadWords(modbusTCP, hr, 20, 2); !reflect.DeepEqual(got, []int{0x1234, 0x5678}) {
		t.Errorf("起動前: 2 ワードの変数 = %v, want [0x1234 0x5678]", got)
	}

	must(t, svc.StartServer(modbusTCP))
	c := modbusClient(t, port)

	// 変数 → レジスタ: クライアントから読める
	must(t, svc.UpdateVariableValue(speed.ID, 345))
	if got, err := c.ReadRegister(10, modbus.HOLDING_REGISTER); err != nil || got != 345 {
		t.Errorf("変数を変えたあとクライアントが読んだ値 = %d, %v, want 345", got, err)
	}
	if got, err := c.ReadCoil(3); err != nil || !got {
		t.Errorf("BOOL の変数のコイル = %v, %v, want true", got, err)
	}

	// クライアントの書き込み → 変数（応答が返った時点で反映されている）
	must(t, c.WriteRegister(10, 56))
	if got := variableValue(t, svc, speed.ID); got != "56" {
		t.Errorf("クライアントが書いたあとの変数 = %s, want 56", got)
	}
	must(t, c.WriteCoil(3, false))
	if got := variableValue(t, svc, run.ID); got != "false" {
		t.Errorf("クライアントがコイルを書いたあとの変数 = %s, want false", got)
	}
	// 2 ワードの変数の片方だけを書いても、変数全体が正しく変わる
	must(t, c.WriteRegister(21, 0x9ABC))
	if got := variableValue(t, svc, big.ID); got != fmt.Sprint(0x12349ABC) {
		t.Errorf("下位ワードを書いたあとの DINT = %s, want %d", got, 0x12349ABC)
	}
	must(t, c.WriteRegisters(20, []uint16{0x0001, 0x0002}))
	if got := variableValue(t, svc, big.ID); got != fmt.Sprint(0x00010002) {
		t.Errorf("2 ワードを書いたあとの DINT = %s, want %d", got, 0x00010002)
	}

	// 画面・スクリプトからのレジスタ書き込み → 変数
	must(t, svc.WriteWord(modbusTCP, hr, 10, 78))
	if got := variableValue(t, svc, speed.ID); got != "78" {
		t.Errorf("ホストがレジスタを書いたあとの変数 = %s, want 78", got)
	}

	// 起動し直しても同期は続く
	must(t, svc.StopServer(modbusTCP))
	must(t, svc.UpdateVariableValue(speed.ID, 90))
	must(t, svc.StartServer(modbusTCP))
	c2 := modbusClient(t, port)
	if got, err := c2.ReadRegister(10, modbus.HOLDING_REGISTER); err != nil || got != 90 {
		t.Errorf("停止中に変えた変数を、起動し直したあとクライアントが読んだ値 = %d, %v, want 90", got, err)
	}
	must(t, c2.WriteRegister(10, 91))
	if got := variableValue(t, svc, speed.ID); got != "91" {
		t.Errorf("起動し直したあとクライアントが書いた変数 = %s, want 91", got)
	}

	// サーバーを削除すると、変数を変えてもエラーにならず、値は変数に残る
	must(t, svc.StopServer(modbusTCP))
	must(t, svc.RemoveServer(modbusTCP))
	must(t, svc.UpdateVariableValue(speed.ID, 100))
	if got := variableValue(t, svc, speed.ID); got != "100" {
		t.Errorf("サーバー削除後の変数 = %s, want 100", got)
	}
}

// ===== Modbus: UnitID の応答設定（#49） =====

func TestModbus_UnitIDSettings(t *testing.T) {
	svc := newService(t)
	port := addModbus(t, svc)

	// 一度も起動していなくても設定できる
	must(t, svc.SetUnitIDEnabled(modbusTCP, 2, false))
	if got := svc.GetDisabledUnitIDs(modbusTCP); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("起動前の設定 = %v, want [2]", got)
	}

	// オフにした UnitID には応答しない（クライアントから見るとタイムアウト）。起動し直しても同じ
	for i := 0; i < 2; i++ {
		must(t, svc.StartServer(modbusTCP))
		c := modbusClient(t, port)
		if _, err := c.ReadRegister(0, modbus.HOLDING_REGISTER); err != nil {
			t.Errorf("起動 %d 回目: UnitID 1 が応答しない: %v", i+1, err)
		}
		must(t, c.SetUnitId(2))
		if _, err := c.ReadRegister(0, modbus.HOLDING_REGISTER); !errors.Is(err, modbus.ErrRequestTimedOut) {
			t.Errorf("起動 %d 回目: オフにした UnitID 2 の結果 = %v, want タイムアウト", i+1, err)
		}
		_ = c.Close()
		must(t, svc.StopServer(modbusTCP))
		if got := svc.GetDisabledUnitIDs(modbusTCP); !reflect.DeepEqual(got, []int{2}) {
			t.Errorf("停止後の設定（%d 回目）= %v, want [2]", i+1, got)
		}
	}

	// 設定（ポート）を変えても残る
	cfg := svc.GetServerConfig(modbusTCP)
	cfg.Settings["tcpPort"] = freePort(t)
	must(t, svc.UpdateServerConfig(cfg))
	if got := svc.GetDisabledUnitIDs(modbusTCP); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("設定変更後 = %v, want [2]", got)
	}
}

// ===== クライアント（外部の機器へつなぐ側） =====

// Modbus TCP のクライアントから、Karakuri 自身の Modbus TCP サーバーにつないで読み書きする
func TestModbusClient_AgainstOwnServer(t *testing.T) {
	svc := newService(t)
	port := addModbus(t, svc)
	must(t, svc.SetUnitIDEnabled(modbusTCP, 2, false))
	must(t, svc.StartServer(modbusTCP))

	c, err := svc.AddClient(modbusTCP, "自分", map[string]interface{}{"host": "127.0.0.1", "port": port, "unitId": 1, "timeoutMs": 300})
	must(t, err)

	send := func(cmd string, params map[string]interface{}) *application.ClientCommandLogDTO {
		t.Helper()
		req, err := svc.BuildClientRequest(modbusTCP, cmd, params)
		must(t, err)
		log, err := svc.SendClientRequest(c.ID, req)
		must(t, err)
		return log
	}

	// FC16 で書くと、サーバーのレジスタに入る
	if log := send("16", map[string]interface{}{"address": float64(10), "values": "1234, 0xBEEF"}); log.Status != "ok" {
		t.Fatalf("FC16: %+v", log)
	}
	words, err := svc.ReadWords(modbusTCP, hr, 10, 2)
	must(t, err)
	if !reflect.DeepEqual(words, []int{1234, 0xBEEF}) {
		t.Fatalf("サーバーのレジスタ = %v", words)
	}

	// 画面から書いた値を FC03 で読める
	must(t, svc.WriteWord(modbusTCP, hr, 12, 42))
	log := send("3", map[string]interface{}{"address": float64(10), "count": float64(3)})
	if log.Status != "ok" || len(log.Values) != 3 || log.Values[2].Value != "42 (0x002A)" {
		t.Fatalf("FC03: %+v", log)
	}
	// 2 回目のリクエストなので、トランザクション ID は 2
	if log.Sent != "00 02 00 00 00 06 01 03 00 0A 00 03" {
		t.Fatalf("送ったフレーム = %s", log.Sent)
	}

	// 16 進で直接入れたリクエスト（サーバーが対応しないファンクションコード）は例外応答になる
	if log, err := svc.SendClientRequest(c.ID, "2B 0E 01 00"); err != nil || log.Status != "exception" || log.Received == "" {
		t.Fatalf("FC43: %+v, %v", log, err)
	}

	// オフにした UnitID 宛てはタイムアウトし、次の送信でつなぎ直す
	must(t, svc.UpdateClient(c.ID, "自分", map[string]interface{}{"host": "127.0.0.1", "port": port, "unitId": 2, "timeoutMs": 200}))
	if log := send("3", map[string]interface{}{"address": float64(0), "count": float64(1)}); log.Status != "timeout" {
		t.Fatalf("オフにした UnitID: %+v", log)
	}
	if svc.GetClients()[0].Connected {
		t.Fatal("タイムアウトしたら接続を閉じる")
	}
	must(t, svc.UpdateClient(c.ID, "自分", map[string]interface{}{"host": "127.0.0.1", "port": port, "unitId": 1, "timeoutMs": 300}))
	if log := send("3", map[string]interface{}{"address": float64(10), "count": float64(1)}); log.Status != "ok" {
		t.Fatalf("つなぎ直し: %+v", log)
	}

	// サーバーを止めると、接続できないエラーになる
	must(t, svc.StopServer(modbusTCP))
	if log := send("3", map[string]interface{}{"address": float64(10), "count": float64(1)}); log.Status == "ok" {
		t.Fatalf("停止後: %+v", log)
	}
}

// ポーリングで、Karakuri 自身の Modbus TCP サーバーのレジスタを周期的に読む
func TestModbusClient_Polling(t *testing.T) {
	svc := newService(t)
	port := addModbus(t, svc)
	must(t, svc.StartServer(modbusTCP))
	c, err := svc.AddClient(modbusTCP, "自分", map[string]interface{}{"host": "127.0.0.1", "port": port, "unitId": 1, "timeoutMs": 200})
	must(t, err)
	must(t, svc.SetClientPollInterval(c.ID, 50))
	_, err = svc.AddClientPollItem(c.ID, "3", map[string]interface{}{"address": float64(20), "count": float64(2)}, "")
	must(t, err)
	if _, err := svc.AddClientPollItem(c.ID, "16", map[string]interface{}{"address": float64(0), "values": "1"}, ""); err == nil {
		t.Fatal("書き込みのコマンドはポーリングに使えない")
	}
	must(t, svc.StartClientPolling(c.ID))
	t.Cleanup(func() { _ = svc.StopClientPolling(c.ID) })

	words := func() []int {
		p, err := svc.GetClientPolling(c.ID)
		must(t, err)
		it := p.Items[0]
		if it.Status != "ok" || len(it.Values) != 2 {
			return nil
		}
		return []int{*it.Values[0].Word, *it.Values[1].Word}
	}
	waitUntil := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("待っても %s にならない", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	must(t, svc.WriteWord(modbusTCP, hr, 20, 111))
	must(t, svc.WriteWord(modbusTCP, hr, 21, 222))
	waitUntil("[111 222]", func() bool { return reflect.DeepEqual(words(), []int{111, 222}) })
	must(t, svc.WriteWord(modbusTCP, hr, 21, 333))
	waitUntil("[111 333]", func() bool { return reflect.DeepEqual(words(), []int{111, 333}) })

	// 応答しない UnitID にすると、タイムアウトになり、最後に読めた値は残る
	must(t, svc.SetUnitIDEnabled(modbusTCP, 1, false))
	waitUntil("タイムアウト", func() bool {
		p, _ := svc.GetClientPolling(c.ID)
		return p.Items[0].Status == "timeout"
	})
	p, _ := svc.GetClientPolling(c.ID)
	if v := p.Items[0].Values; len(v) != 2 || *v[1].Word != 333 {
		t.Fatalf("最後に読めた値が残っていない: %+v", v)
	}
	// 応答するようにすると、また読める
	must(t, svc.SetUnitIDEnabled(modbusTCP, 1, true))
	must(t, svc.WriteWord(modbusTCP, hr, 20, 444))
	waitUntil("[444 333]", func() bool { return reflect.DeepEqual(words(), []int{444, 333}) })
}

// ===== プロジェクトの保存と読み込み =====

func TestProject_ImportReplacesServersAndState(t *testing.T) {
	svc := newService(t)
	addModbus(t, svc)
	must(t, svc.SetUnitIDEnabled(modbusTCP, 3, false))
	must(t, svc.WriteWord(modbusTCP, hr, 5, 77))
	exported := svc.ExportProject()

	// 取り出したあとに状態を変え、別のプロトコルのサーバーも追加する
	must(t, svc.SetUnitIDEnabled(modbusTCP, 4, false))
	must(t, svc.WriteWord(modbusTCP, hr, 6, 88))
	must(t, svc.AddServer(opcUA, ""))

	must(t, svc.ImportProject(exported))

	// サーバーは取り出したときの構成に戻る
	instances := svc.GetServerInstances()
	if len(instances) != 1 || instances[0].ProtocolType != modbusTCP || instances[0].Status != "Stopped" {
		t.Fatalf("読み込み後のサーバー = %+v", instances)
	}
	// UnitID の設定はプロジェクトのものになる（読み込む前の変更は残らない）
	if got := svc.GetDisabledUnitIDs(modbusTCP); !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("読み込み後の UnitID の設定 = %v, want [3]", got)
	}
	// レジスタの値はプロジェクトに含めないので、読み込むと消える（読み込む前の値は残らない）
	if got, err := svc.ReadWords(modbusTCP, hr, 5, 2); err != nil || !reflect.DeepEqual(got, []int{0, 0}) {
		t.Errorf("読み込み後のレジスタ = %v, %v, want [0 0]", got, err)
	}
}

// ===== OPC UA: 変数の公開 =====

func TestOPCUA_PublishedVariable(t *testing.T) {
	svc := newService(t)
	must(t, svc.AddServer(opcUA, ""))
	port := freePort(t)
	cfg := svc.GetServerConfig(opcUA)
	cfg.Settings["host"] = "127.0.0.1"
	cfg.Settings["port"] = port
	must(t, svc.UpdateServerConfig(cfg))

	counter, err := svc.CreateVariable("Counter", "UINT", 60000)
	must(t, err)
	label, err := svc.CreateVariable("Label", "STRING[10]", "abc")
	must(t, err)
	for _, id := range []string{counter.ID, label.ID} {
		must(t, svc.UpdateVariableNodePublishing(id, opcUA, &application.NodePublishingDTO{
			ProtocolType: opcUA, Enabled: true, AccessMode: "readwrite",
		}))
	}

	// 停止中は名前空間の番号が決まらない
	addr := func() string {
		for _, a := range svc.GetNodeAddresses(opcUA, []string{counter.ID}) {
			return a.Address
		}
		return ""
	}
	if got := addr(); got != "ns=???;s=Counter" {
		t.Errorf("停止中の NodeID = %q, want ns=???;s=Counter", got)
	}

	must(t, svc.StartServer(opcUA))
	nodeID := addr()
	if !strings.HasPrefix(nodeID, "ns=") || strings.Contains(nodeID, "?") || !strings.HasSuffix(nodeID, ";s=Counter") {
		t.Fatalf("起動中の NodeID = %q", nodeID)
	}
	var ns uint16
	if _, err := fmt.Sscanf(nodeID, "ns=%d;", &ns); err != nil {
		t.Fatalf("NodeID %q から名前空間を読めない: %v", nodeID, err)
	}

	ctx := context.Background()
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := uaclient.Dial(dialCtx, fmt.Sprintf("opc.tcp://127.0.0.1:%d", port), uaclient.Config{})
	must(t, err)
	defer c.Close(ctx) //nolint:errcheck
	read := func(name string) interface{} {
		t.Helper()
		dv, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, name))
		if err != nil {
			t.Fatalf("%s を読めない: %v", name, err)
		}
		return dv.Value.Value()
	}

	// 変数の値が、型に合った値で読める
	if got := read("Counter"); got != uint16(60000) {
		t.Errorf("Counter = %v (%T), want uint16(60000)", got, got)
	}
	if got := read("Label"); got != "abc" {
		t.Errorf("Label = %v (%T), want abc", got, got)
	}

	// クライアントの書き込み → 変数（128 以上の符号なし整数と文字列。#43）
	must(t, c.WriteValue(ctx, ua.NewStringNodeId(ns, "Counter"), ua.NewVariant(uint16(65535))))
	must(t, c.WriteValue(ctx, ua.NewStringNodeId(ns, "Label"), ua.NewVariant("hello")))
	if got := variableValue(t, svc, counter.ID); got != "65535" {
		t.Errorf("書き込み後の Counter = %s, want 65535", got)
	}
	if got := variableValue(t, svc, label.ID); got != "hello" {
		t.Errorf("書き込み後の Label = %s, want hello", got)
	}

	// 変数 → OPC UA: 変更はすぐ届く（1 秒ごとの読み直しを待たない）
	must(t, svc.UpdateVariableValue(counter.ID, 123))
	deadline := time.Now().Add(500 * time.Millisecond)
	for read("Counter") != uint16(123) {
		if time.Now().After(deadline) {
			t.Fatalf("変数の変更が 500ms 以内に届かない: Counter = %v", read("Counter"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
