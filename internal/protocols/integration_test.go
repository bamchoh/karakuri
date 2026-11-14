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
	if want := []string{"fins", "modbus-ascii", "modbus-rtu", "modbus-tcp", "opcua"}; !reflect.DeepEqual(types, want) {
		t.Errorf("プロトコル = %v, want %v", types, want)
	}

	// クライアントとして使えるプロトコルは、コマンドと、16 進で入れるリクエストの説明を持つ
	var clientTypes []string
	for _, p := range svc.GetClientProtocols() {
		clientTypes = append(clientTypes, p.ProtocolType)
		if len(p.ConnectionFields) == 0 || len(p.Commands) == 0 || p.RequestDescription == "" {
			t.Errorf("%s: クライアントの定義が足りない: %+v", p.ProtocolType, p)
		}
	}
	// 並びは、登録した順（最初のものが、接続先を追加するときの既定になる）
	if want := []string{"modbus-tcp", "opcua", "fins"}; !reflect.DeepEqual(clientTypes, want) {
		t.Errorf("クライアントのプロトコル = %v, want %v", clientTypes, want)
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

// ===== FINS =====

const finsType = "fins"

// addFINS は FINS サーバーを追加し、127.0.0.1 の空きポートを設定する（起動はしない）
func addFINS(t *testing.T, svc *application.PLCService) int {
	t.Helper()
	must(t, svc.AddServer(finsType, ""))
	port := freePort(t)
	cfg := svc.GetServerConfig(finsType)
	cfg.Settings["host"] = "127.0.0.1"
	cfg.Settings["port"] = port
	must(t, svc.UpdateServerConfig(cfg))
	return port
}

type finsSend func(cmd string, params map[string]interface{}) *application.ClientCommandLogDTO

// finsSender は、Karakuri の FINS クライアントの接続先を作り、その ID と、コマンドを送る関数を返す
func finsSender(t *testing.T, svc *application.PLCService, port int, transport string) (string, finsSend) {
	t.Helper()
	c, err := svc.AddClient(finsType, "自分", map[string]interface{}{"host": "127.0.0.1", "port": port, "transport": transport, "timeoutMs": 300})
	must(t, err)
	return c.ID, func(cmd string, params map[string]interface{}) *application.ClientCommandLogDTO {
		t.Helper()
		req, err := svc.BuildClientRequest(finsType, cmd, params)
		must(t, err)
		log, err := svc.SendClientRequest(c.ID, req)
		must(t, err)
		return log
	}
}

// サーバーのメモリは起動前から読み書きでき、変数と同期する。FINS/UDP と FINS/TCP のどちらでも同じメモリを読む
func TestFINS_ServerAndVariableSync(t *testing.T) {
	svc := newService(t)
	port := addFINS(t, svc)
	if areas := svc.GetMemoryAreas(finsType); len(areas) != 10 {
		t.Fatalf("エリア数 = %d, want 10（ワード 6 + ビット 4）", len(areas))
	}

	speed, err := svc.CreateVariable("Speed", "INT", 12)
	must(t, err)
	run, err := svc.CreateVariable("Run", "BOOL", true)
	must(t, err)
	big, err := svc.CreateVariable("Big", "DINT", 0x12345678)
	must(t, err)
	mapTo := func(id, area string, addr int) {
		must(t, svc.UpdateVariableMappings(id, []application.ProtocolMappingDTO{
			{ProtocolType: finsType, MemoryArea: area, Address: addr, Endianness: "big"},
		}))
	}
	mapTo(speed.ID, "DM", 100)
	mapTo(run.ID, "CIO_BIT", 100*16+5) // CIO 100.05
	mapTo(big.ID, "DM", 200)

	// 起動前でも、変数の値がメモリに入っている。ビットのエリアとワードのエリアは同じメモリ
	if got, _ := svc.ReadWords(finsType, "CIO", 100, 1); !reflect.DeepEqual(got, []int{0x0020}) {
		t.Errorf("起動前: CIO 100 = %v, want [32]", got)
	}

	must(t, svc.StartServer(finsType))
	_, udp := finsSender(t, svc, port, "udp")
	_, tcp := finsSender(t, svc, port, "tcp")

	// 変数 → メモリ: クライアントから読める（UDP でも TCP でも）
	must(t, svc.UpdateVariableValue(speed.ID, 345))
	for name, send := range map[string]finsSend{"udp": udp, "tcp": tcp} {
		log := send("read", map[string]interface{}{"area": "DM", "unit": "word", "address": float64(100), "count": float64(1)})
		if log.Status != "ok" || len(log.Values) != 1 || log.Values[0].Value != "345 (0x0159)" {
			t.Errorf("%s: 変数を変えたあとクライアントが読んだ値: %+v", name, log)
		}
	}
	if log := udp("read", map[string]interface{}{"area": "DM", "unit": "word", "address": float64(200), "count": float64(2)}); log.Status != "ok" || log.Values[0].Value != "4660 (0x1234)" || log.Values[1].Value != "22136 (0x5678)" {
		t.Errorf("2 ワードの変数: %+v", log)
	}

	// クライアントの書き込み → 変数（応答が返った時点で反映されている）
	if log := tcp("write", map[string]interface{}{"area": "DM", "unit": "word", "address": float64(100), "values": "56"}); log.Status != "ok" {
		t.Fatalf("書き込み: %+v", log)
	}
	if got := variableValue(t, svc, speed.ID); got != "56" {
		t.Errorf("クライアントが書いたあとの変数 = %s, want 56", got)
	}
	if log := udp("write", map[string]interface{}{"area": "CIO", "unit": "bit", "address": float64(100), "bit": float64(5), "values": "0"}); log.Status != "ok" {
		t.Fatalf("ビットの書き込み: %+v", log)
	}
	if got := variableValue(t, svc, run.ID); got != "false" {
		t.Errorf("クライアントがビットを書いたあとの変数 = %s, want false", got)
	}
	if log := udp("write", map[string]interface{}{"area": "DM", "unit": "word", "address": float64(201), "values": "0x9ABC"}); log.Status != "ok" {
		t.Fatalf("書き込み: %+v", log)
	}
	if got := variableValue(t, svc, big.ID); got != fmt.Sprint(0x12349ABC) {
		t.Errorf("下位ワードを書いたあとの DINT = %s, want %d", got, 0x12349ABC)
	}

	// 起動し直しても、メモリと同期は残る
	must(t, svc.StopServer(finsType))
	must(t, svc.UpdateVariableValue(speed.ID, 90))
	must(t, svc.StartServer(finsType))
	if log := udp("read", map[string]interface{}{"area": "DM", "unit": "word", "address": float64(100), "count": float64(1)}); log.Status != "ok" || log.Values[0].Value != "90 (0x005A)" {
		t.Errorf("起動し直したあと: %+v", log)
	}
}

// ビットのエリアとワードのエリアは同じメモリを指す（MemoryArea.BitsOf）。
// 片方に書くと、同じ場所のもう片方にマッピングした変数にも反映される
func TestFINS_BitAndWordAreasShareVariables(t *testing.T) {
	svc := newService(t)
	addFINS(t, svc)
	run, err := svc.CreateVariable("Run", "BOOL", false)
	must(t, err)
	word, err := svc.CreateVariable("Word", "INT", 0)
	must(t, err)
	must(t, svc.UpdateVariableMappings(run.ID, []application.ProtocolMappingDTO{{ProtocolType: finsType, MemoryArea: "CIO_BIT", Address: 100*16 + 5, Endianness: "big"}}))
	must(t, svc.UpdateVariableMappings(word.ID, []application.ProtocolMappingDTO{{ProtocolType: finsType, MemoryArea: "CIO", Address: 200, Endianness: "big"}}))

	must(t, svc.WriteWord(finsType, "CIO", 100, 0x0020))       // CIO 100.05 を、ワードで ON にする
	must(t, svc.WriteBit(finsType, "CIO_BIT", 200*16+3, true)) // CIO 200.03 を、ビットで ON にする
	gotRun, gotWord := variableValue(t, svc, run.ID), variableValue(t, svc, word.ID)
	if gotRun != "true" || gotWord != "8" {
		t.Errorf("ワードで書いたあとの BOOL = %s（want true）、ビットで書いたあとの INT = %s（want 8）", gotRun, gotWord)
	}

	// 変数を変えたときも、同じ場所のもう片方の変数に反映される
	both, err := svc.CreateVariable("Both", "INT", 0)
	must(t, err)
	must(t, svc.UpdateVariableMappings(both.ID, []application.ProtocolMappingDTO{{ProtocolType: finsType, MemoryArea: "CIO", Address: 100, Endianness: "big"}}))
	if got := variableValue(t, svc, both.ID); got != "0" {
		// マッピングした時点では、変数の値（0）をメモリに書く。CIO 100.05 も OFF になる
		t.Errorf("マッピングした直後の INT = %s, want 0", got)
	}
	if got := variableValue(t, svc, run.ID); got != "false" {
		t.Errorf("INT をマッピングしたあとの BOOL = %s, want false", got)
	}
	must(t, svc.UpdateVariableValue(run.ID, true))
	if got := variableValue(t, svc, both.ID); got != "32" {
		t.Errorf("BOOL を ON にしたあとの INT = %s, want 32", got)
	}
	must(t, svc.UpdateVariableValue(both.ID, 0))
	if got := variableValue(t, svc, run.ID); got != "false" {
		t.Errorf("INT を 0 にしたあとの BOOL = %s, want false", got)
	}
	// 重ならない場所への書き込みでは変わらない
	must(t, svc.UpdateVariableValue(run.ID, true))
	must(t, svc.WriteWord(finsType, "CIO", 101, 0xFFFF))
	if got := variableValue(t, svc, both.ID); got != "32" {
		t.Errorf("隣のワードを書いたあとの INT = %s, want 32", got)
	}

	// FINS のクライアントからワードで書いても、BOOL の変数に反映される
	port := freePort(t)
	cfg := svc.GetServerConfig(finsType)
	cfg.Settings["host"] = "127.0.0.1"
	cfg.Settings["port"] = port
	must(t, svc.UpdateServerConfig(cfg))
	must(t, svc.StartServer(finsType))
	_, send := finsSender(t, svc, port, "tcp")
	if log := send("write", map[string]interface{}{"area": "CIO", "unit": "word", "address": float64(100), "values": "0"}); log.Status != "ok" {
		t.Fatalf("書き込み: %+v", log)
	}
	if got := variableValue(t, svc, run.ID); got != "false" {
		t.Errorf("クライアントがワードで書いたあとの BOOL = %s, want false", got)
	}
}

// 変数を FINS の接続先のアドレスにマッピングして、Karakuri 自身の FINS サーバーと同期する
func TestFINSClient_VariableMapping(t *testing.T) {
	svc := newService(t)
	port := addFINS(t, svc)
	must(t, svc.StartServer(finsType))
	id, _ := finsSender(t, svc, port, "udp")
	must(t, svc.SetClientPollInterval(id, 50))
	key := "client:" + id
	if n := len(svc.GetMemoryAreas(key)); n != 10 {
		t.Fatalf("接続先のエリア数 = %d, want 10", n)
	}

	must(t, svc.WriteWord(finsType, "DM", 51, 321)) // DINT（ビッグエンディアン）= 321
	must(t, svc.WriteBit(finsType, "WR_BIT", 10*16+15, true))
	num, err := svc.CreateVariable("remote", "DINT", 0)
	must(t, err)
	flag, err := svc.CreateVariable("remoteFlag", "BOOL", false)
	must(t, err)
	must(t, svc.UpdateVariableMappings(num.ID, []application.ProtocolMappingDTO{{ProtocolType: key, MemoryArea: "DM", Address: 50, Endianness: "big"}}))
	must(t, svc.UpdateVariableMappings(flag.ID, []application.ProtocolMappingDTO{{ProtocolType: key, MemoryArea: "WR_BIT", Address: 10*16 + 15, Endianness: "big"}}))
	must(t, svc.StartClientPolling(id))
	t.Cleanup(func() { _ = svc.StopClientPolling(id) })

	waitUntil := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("待っても %s にならない（remote = %s、remoteFlag = %s、状態 %+v）", what,
					variableValue(t, svc, num.ID), variableValue(t, svc, flag.ID), svc.VariableDeviceStatus(num.ID))
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitUntil("機器の値を読む", func() bool {
		return variableValue(t, svc, num.ID) == "321" && variableValue(t, svc, flag.ID) == "true"
	})

	// 変数を変えると、サーバーのメモリに書く
	must(t, svc.UpdateVariableValue(num.ID, 70000))
	must(t, svc.UpdateVariableValue(flag.ID, false))
	waitUntil("書く", func() bool {
		w, _ := svc.ReadWords(finsType, "DM", 50, 2)
		b, _ := svc.ReadBits(finsType, "WR_BIT", 10*16+15, 1)
		return reflect.DeepEqual(w, []int{1, 70000 - 65536}) && len(b) == 1 && !b[0]
	})
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

// 変数を接続先（Karakuri 自身の Modbus TCP サーバー）のアドレスにマッピングして同期する
func TestModbusClient_VariableMapping(t *testing.T) {
	svc := newService(t)
	port := addModbus(t, svc)
	must(t, svc.StartServer(modbusTCP))
	c, err := svc.AddClient(modbusTCP, "自分", map[string]interface{}{"host": "127.0.0.1", "port": port, "unitId": 1, "timeoutMs": 100})
	must(t, err)
	must(t, svc.SetClientPollInterval(c.ID, 50))
	must(t, svc.SetClientFailThreshold(c.ID, 2))
	key := "client:" + c.ID

	// マッピング先に、接続先が出る
	found := false
	for _, tg := range svc.GetMappingTargets() {
		if tg.ProtocolType == key && tg.IsClient {
			found = true
		}
	}
	if !found || len(svc.GetMemoryAreas(key)) != 4 {
		t.Fatal("マッピング先に接続先がない")
	}

	must(t, svc.WriteWord(modbusTCP, hr, 50, 0))
	must(t, svc.WriteWord(modbusTCP, hr, 51, 321)) // DINT（ビッグエンディアン）= 321
	v, err := svc.CreateVariable("remote", "DINT", 0)
	must(t, err)
	must(t, svc.UpdateVariableMappings(v.ID, []application.ProtocolMappingDTO{{ProtocolType: key, MemoryArea: hr, Address: 50, Endianness: "big"}}))
	must(t, svc.StartClientPolling(c.ID))
	t.Cleanup(func() { _ = svc.StopClientPolling(c.ID) })

	status := func() application.VariableDeviceStatusDTO {
		st := svc.VariableDeviceStatus(v.ID)
		if st == nil {
			return application.VariableDeviceStatusDTO{}
		}
		return *st
	}
	waitUntil := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("待っても %s にならない（値 %s、状態 %+v）", what, variableValue(t, svc, v.ID), status())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	waitUntil("機器の値を読む", func() bool { return variableValue(t, svc, v.ID) == "321" && status().State == "ok" })

	// 変数を変えると、サーバーのレジスタに書く（2 ワード）
	must(t, svc.UpdateVariableValue(v.ID, 70000))
	waitUntil("書く", func() bool {
		w, _ := svc.ReadWords(modbusTCP, hr, 50, 2)
		return reflect.DeepEqual(w, []int{1, 70000 - 65536})
	})

	// 応答しない UnitID にすると、決めた回数で通信エラー。値は残る
	must(t, svc.SetUnitIDEnabled(modbusTCP, 1, false))
	waitUntil("通信エラー", func() bool { return status().State == "comm_error" })
	if variableValue(t, svc, v.ID) != "70000" {
		t.Fatalf("値が残っていない: %s", variableValue(t, svc, v.ID))
	}
	// その間に変えた値は書き込み未反映になり、応答するようになったら書く
	must(t, svc.UpdateVariableValue(v.ID, 5))
	waitUntil("書き込み未反映", func() bool { return status().WritePending })
	must(t, svc.SetUnitIDEnabled(modbusTCP, 1, true))
	waitUntil("つながったら書く", func() bool {
		w, _ := svc.ReadWords(modbusTCP, hr, 50, 2)
		return reflect.DeepEqual(w, []int{0, 5})
	})
	waitUntil("状態が戻る", func() bool { st := status(); return st.State == "ok" && !st.WritePending })
}

// スクリプトから、保存したコマンドと 16 進のリクエストを、Karakuri 自身の Modbus TCP サーバーへ送る
func TestModbusClient_FromScript(t *testing.T) {
	svc := newService(t)
	port := addModbus(t, svc)
	must(t, svc.StartServer(modbusTCP))
	c, err := svc.AddClient(modbusTCP, "自分", map[string]interface{}{"host": "127.0.0.1", "port": port, "unitId": 1, "timeoutMs": 300})
	must(t, err)
	_, err = svc.AddClientSavedCommand(c.ID, application.ClientSavedCommandInputDTO{
		Name: "先頭 2 個", Mode: "build", CommandID: "3", Params: map[string]interface{}{"address": float64(0), "count": float64(2)},
	})
	must(t, err)
	must(t, svc.WriteWord(modbusTCP, hr, 0, 11))
	must(t, svc.WriteWord(modbusTCP, hr, 1, 22))

	res, err := svc.RunScriptOnce(`
		const r = plc.sendCommand("自分", "先頭 2 個");
		const w = plc.sendRequest("自分", "06 00 05 00 2A"); // FC06: アドレス 5 に 42
		return r.status + ":" + (r.values[0].word + r.values[1].word) + "/" + w.status;
	`)
	must(t, err)
	if fmt.Sprint(res) != "ok:33/ok" {
		t.Fatalf("script = %v", res)
	}
	words, err := svc.ReadWords(modbusTCP, hr, 5, 1)
	must(t, err)
	if words[0] != 42 {
		t.Fatalf("スクリプトから書いた値 = %v", words)
	}
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

// OPC UA クライアントで、Karakuri 自身の OPC UA サーバーの公開変数を読み書きする（コマンドとポーリング）
func TestOPCUAClient_AgainstOwnServer(t *testing.T) {
	svc := newService(t)
	must(t, svc.AddServer(opcUA, ""))
	port := freePort(t)
	cfg := svc.GetServerConfig(opcUA)
	cfg.Settings["host"] = "127.0.0.1"
	cfg.Settings["port"] = port
	must(t, svc.UpdateServerConfig(cfg))

	counter, err := svc.CreateVariable("Counter", "INT", 5)
	must(t, err)
	must(t, svc.UpdateVariableNodePublishing(counter.ID, opcUA, &application.NodePublishingDTO{
		ProtocolType: opcUA, Enabled: true, AccessMode: "readwrite",
	}))
	must(t, svc.StartServer(opcUA))
	var nodeID string
	for _, a := range svc.GetNodeAddresses(opcUA, []string{counter.ID}) {
		nodeID = a.Address
	}

	settings := map[string]interface{}{"endpointUrl": fmt.Sprintf("opc.tcp://127.0.0.1:%d", port), "timeoutMs": 3000}
	c, err := svc.AddClient(opcUA, "自分", settings)
	must(t, err)
	send := func(cmd string, params map[string]interface{}) *application.ClientCommandLogDTO {
		t.Helper()
		req, err := svc.BuildClientRequest(opcUA, cmd, params)
		must(t, err)
		log, err := svc.SendClientRequest(c.ID, req)
		must(t, err)
		return log
	}
	value := func(log *application.ClientCommandLogDTO, label string) string {
		for _, v := range log.Values {
			if v.Label == label {
				return v.Value
			}
		}
		return ""
	}

	// 変数の値を読める。画面には、送ったリクエスト本体と応答本体が 16 進で出る
	log := send("read", map[string]interface{}{"nodeId": nodeID, "attribute": "Value"})
	if log.Status != "ok" || value(log, "Value（Int16）") != "5" || log.Sent == "" || log.Received == "" {
		t.Fatalf("Read: %+v", log)
	}
	if !svc.GetClients()[0].Connected {
		t.Fatal("送ったあとは接続中")
	}

	// 書くと、変数に入る
	if log := send("write", map[string]interface{}{"nodeId": nodeID, "dataType": "Int16", "rank": "scalar", "value": "-123"}); log.Status != "ok" {
		t.Fatalf("Write: %+v", log)
	}
	if got := variableValue(t, svc, counter.ID); got != "-123" {
		t.Fatalf("書き込み後の Counter = %s, want -123", got)
	}
	// 型が違う書き込みは例外応答（サーバーが断る）
	if log := send("write", map[string]interface{}{"nodeId": nodeID, "dataType": "Int32", "rank": "scalar", "value": "1"}); log.Status != "exception" {
		t.Fatalf("型が違う Write: %+v", log)
	}

	// 公開変数のフォルダをブラウズすると、変数が出る
	folder := strings.Replace(nodeID, ";s=Counter", ";s=PLCVariables", 1)
	if log := send("browse", map[string]interface{}{"nodeId": folder}); log.Status != "ok" || !strings.HasPrefix(value(log, "Counter"), nodeID) {
		t.Fatalf("Browse: %+v", log)
	}

	// 16 進として読めても、リクエストとして読めないものは、送らずにエラーにする
	if log, err := svc.SendClientRequest(c.ID, "01 02 03"); err != nil || log.Status != "error" || log.Sent != "" {
		t.Fatalf("読めないリクエスト: %+v, %v", log, err)
	}

	// ポーリングで、変数の変化を追う。書き込みのコマンドは使えない
	must(t, svc.SetClientPollInterval(c.ID, 50))
	_, err = svc.AddClientPollItem(c.ID, "read", map[string]interface{}{"nodeId": nodeID, "attribute": "Value"}, "")
	must(t, err)
	if _, err := svc.AddClientPollItem(c.ID, "write", map[string]interface{}{"nodeId": nodeID, "dataType": "Int16", "rank": "scalar", "value": "1"}, ""); err == nil {
		t.Fatal("書き込みのコマンドはポーリングに使えない")
	}
	must(t, svc.StartClientPolling(c.ID))
	t.Cleanup(func() { _ = svc.StopClientPolling(c.ID) })
	polled := func() (status, value string) {
		p, err := svc.GetClientPolling(c.ID)
		must(t, err)
		it := p.Items[0]
		if len(it.Values) == 0 {
			return it.Status, ""
		}
		return it.Status, it.Values[0].Value
	}
	waitUntil := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				status, value := polled()
				t.Fatalf("待っても %s にならない（%s, %q）", what, status, value)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitUntil("-123", func() bool { s, v := polled(); return s == "ok" && v == "-123" })
	must(t, svc.UpdateVariableValue(counter.ID, 77))
	waitUntil("77", func() bool { s, v := polled(); return s == "ok" && v == "77" })

	// サーバーを止めると読めなくなり、最後に読めた値は残る。起動し直すと、つなぎ直して読める
	must(t, svc.StopServer(opcUA))
	waitUntil("失敗", func() bool { s, _ := polled(); return s == "error" || s == "timeout" })
	if _, v := polled(); v != "77" {
		t.Fatalf("最後に読めた値が残っていない: %q", v)
	}
	must(t, svc.StartServer(opcUA))
	must(t, svc.UpdateVariableValue(counter.ID, 88))
	waitUntil("88", func() bool { s, v := polled(); return s == "ok" && v == "88" })

	// サブスクリプションでも、変数の変化が届く（配列は要素ごとに出る）
	values, err := svc.CreateVariable("Values", "ARRAY[0..2] OF INT", []interface{}{1, 2, 3})
	must(t, err)
	must(t, svc.UpdateVariableNodePublishing(values.ID, opcUA, &application.NodePublishingDTO{
		ProtocolType: opcUA, Enabled: true, AccessMode: "readwrite",
	}))
	valuesNode := strings.Replace(nodeID, ";s=Counter", ";s=Values", 1)
	must(t, svc.SetClientSubscriptionInterval(c.ID, 50))
	added, err := svc.AddClientSubscriptionItems(c.ID, map[string]interface{}{"nodeId": nodeID + "\n" + valuesNode})
	must(t, err)
	if len(added) != 2 || added[1].Summary != valuesNode {
		t.Fatalf("監視項目: %+v", added)
	}
	must(t, svc.StartClientSubscription(c.ID))
	t.Cleanup(func() { _ = svc.StopClientSubscription(c.ID) })
	subscribed := func(i int, label string) string {
		sub, err := svc.GetClientSubscription(c.ID)
		must(t, err)
		for _, v := range sub.Items[i].Values {
			if v.Label == label {
				return v.Value
			}
		}
		return ""
	}
	waitUntil("監視の最初の値", func() bool {
		return subscribed(0, "Value（Int16）") == "88" && subscribed(1, "[2]") == "3"
	})
	must(t, svc.UpdateVariableValue(counter.ID, 99))
	must(t, svc.UpdateVariableFieldValue(values.ID, "[2]", 30))
	waitUntil("監視の変化", func() bool {
		return subscribed(0, "Value（Int16）") == "99" && subscribed(1, "[2]") == "30"
	})
	if sub, _ := svc.GetClientSubscription(c.ID); sub.State != "ok" || !svc.GetClients()[0].Subscribing {
		t.Fatalf("監視の状態: %+v", sub)
	}
	must(t, svc.StopClientSubscription(c.ID))

	// プロジェクトに保存して読み込んでも、接続先と読み取り項目・監視項目が残る
	must(t, svc.StopClientPolling(c.ID))
	project := svc.ExportProject()
	must(t, svc.ImportProject(project))
	clients := svc.GetClients()
	if len(clients) != 1 || clients[0].ProtocolType != opcUA || clients[0].Settings["endpointUrl"] != settings["endpointUrl"] {
		t.Fatalf("読み込んだ接続先: %+v", clients)
	}
	if p, err := svc.GetClientPolling(clients[0].ID); err != nil || len(p.Items) != 1 || p.Items[0].Summary != "Read "+nodeID+" の Value" {
		t.Fatalf("読み込んだ読み取り項目: %+v, %v", p, err)
	}
	if sub, err := svc.GetClientSubscription(clients[0].ID); err != nil || sub.Running || sub.IntervalMs != 50 || len(sub.Items) != 2 || sub.Items[1].Summary != valuesNode {
		t.Fatalf("読み込んだ監視項目: %+v, %v", sub, err)
	}
}
