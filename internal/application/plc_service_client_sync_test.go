package application

import (
	"fmt"
	"testing"
	"time"

	"karakuri/internal/domain/protocol/protocoltest"
)

type mappingFixture struct {
	t      *testing.T
	svc    *PLCService
	f      *protocoltest.DeviceClientFactory
	client *ClientDTO
	key    string
}

func newMappingFixture(t *testing.T) *mappingFixture {
	t.Helper()
	svc := newTestService(t)
	f := protocoltest.NewDeviceClientFactory("fake-device")
	svc.RegisterClientFactory(f)
	t.Cleanup(svc.Shutdown)
	c, err := svc.AddClient("fake-device", "機器", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetClientPollInterval(c.ID, minPollIntervalMs); err != nil {
		t.Fatal(err)
	}
	return &mappingFixture{t: t, svc: svc, f: f, client: c, key: clientMappingKey(c.ID)}
}

func (x *mappingFixture) variable(name, dataType string, value interface{}, area string, addr int) string {
	x.t.Helper()
	v, err := x.svc.CreateVariable(name, dataType, value)
	if err != nil {
		x.t.Fatal(err)
	}
	if err := x.svc.UpdateVariableMappings(v.ID, []ProtocolMappingDTO{{ProtocolType: x.key, MemoryArea: area, Address: addr, Endianness: "big"}}); err != nil {
		x.t.Fatal(err)
	}
	return v.ID
}

func (x *mappingFixture) value(id string) string {
	for _, v := range x.svc.GetVariables() {
		if v.ID == id {
			return fmt.Sprint(v.Value)
		}
	}
	x.t.Fatalf("変数 %s がない", id)
	return ""
}

func (x *mappingFixture) status(id string) VariableDeviceStatusDTO {
	for _, st := range x.svc.GetVariableDeviceStatuses() {
		if st.VariableID == id {
			return st
		}
	}
	return VariableDeviceStatusDTO{}
}

func (x *mappingFixture) start() {
	x.t.Helper()
	if err := x.svc.StartClientPolling(x.client.ID); err != nil {
		x.t.Fatal(err)
	}
}

func TestMapping_TargetsAndAreas(t *testing.T) {
	x := newMappingFixture(t)
	// マッピングできないプロトコルの接続先は、マッピング先に出さない
	x.svc.RegisterClientFactory(protocoltest.NewClientFactory("fake-client"))
	_, _ = x.svc.AddClient("fake-client", "コマンドだけ", nil)

	targets := x.svc.GetMappingTargets()
	var found *MappingTargetDTO
	for i, tg := range targets {
		if tg.IsClient {
			if found != nil {
				t.Fatalf("マッピングできる接続先は 1 つ: %+v", targets)
			}
			found = &targets[i]
		}
	}
	if found == nil || found.ProtocolType != x.key || found.Label != "機器" {
		t.Fatalf("targets = %+v", targets)
	}
	if targets[0].IsClient || targets[0].ProtocolType != "modbus-tcp" {
		t.Fatalf("サーバーが先に並ぶ: %+v", targets)
	}
	areas := x.svc.GetMemoryAreas(x.key)
	if len(areas) != len(protocoltest.DeviceAreas) || areas[1].ID != "inputs" || !areas[1].ReadOnly {
		t.Fatalf("areas = %+v", areas)
	}
	if got := x.svc.GetMemoryAreas("client:nope"); got != nil {
		t.Fatalf("ない接続先: %+v", got)
	}
}

// 機器が先に読まれ、変数を変えると機器に書く。機器の側の変化も変数に反映する
func TestMapping_ReadThenWrite(t *testing.T) {
	x := newMappingFixture(t)
	x.f.Device.SetWord("words", 10, 1234)
	x.f.Device.SetBit("bits", 3, true)
	a := x.variable("a", "INT", 5, "words", 10)
	b := x.variable("b", "BOOL", false, "bits", 3)
	// ポーリングの前は、変数を変えても書かない（機器が先）
	_ = x.svc.UpdateVariableValue(a, 7)
	if st := x.status(a); st.State != syncStateStopped || st.WritePending {
		t.Fatalf("開始前: %+v", st)
	}

	x.start()
	waitFor(t, "機器の値を読む", func() bool { return x.value(a) == "1234" && x.value(b) == "true" })
	if st := x.status(a); st.State != "ok" || st.ClientName != "機器" || st.At == "" {
		t.Fatalf("読んだあと: %+v", st)
	}
	if x.f.Device.Writes.Load() != 0 {
		t.Fatal("読んだ値を機器に書き戻している")
	}

	// 変数を変えると、周期を待たずに機器へ書く
	_ = x.svc.SetClientPollInterval(x.client.ID, 60_000)
	time.Sleep(3 * minPollIntervalMs * time.Millisecond) // 長い周期の待ちに入るまで
	_ = x.svc.UpdateVariableValue(a, -3)
	_ = x.svc.UpdateVariableValue(b, false)
	waitFor(t, "機器へ書く", func() bool {
		return x.f.Device.Word("words", 10) == 0xFFFD && !x.f.Device.Bit("bits", 3)
	})
	waitFor(t, "書き込み未反映が消える", func() bool { return !x.status(a).WritePending })
	_ = x.svc.SetClientPollInterval(x.client.ID, minPollIntervalMs)

	// 機器の側で変わると、変数に反映する
	x.f.Device.SetWord("words", 10, 42)
	waitFor(t, "機器の変化", func() bool { return x.value(a) == "42" })
}

// 読むだけのエリアには書かない。大きな変数は分けて読み書きする
func TestMapping_ReadOnlyAndChunks(t *testing.T) {
	x := newMappingFixture(t)
	for i := 0; i < 10; i++ {
		x.f.Device.SetWord("inputWords", i, uint16(i+1))
	}
	arr := x.variable("arr", "ARRAY[0..9] OF INT", []interface{}{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, "inputWords", 0)
	big := x.variable("big", "ARRAY[0..5] OF INT", []interface{}{0, 0, 0, 0, 0, 0}, "words", 100)
	x.start()
	waitFor(t, "10 ワードを分けて読む", func() bool { return x.value(arr) == "[1 2 3 4 5 6 7 8 9 10]" })
	waitFor(t, "big を読む", func() bool { return x.status(big).State == "ok" })

	_ = x.svc.UpdateVariableValue(arr, []interface{}{9, 9, 9, 9, 9, 9, 9, 9, 9, 9})
	_ = x.svc.UpdateVariableValue(big, []interface{}{1, 2, 3, 4, 5, 6})
	waitFor(t, "6 ワードを分けて書く", func() bool { return x.f.Device.Word("words", 105) == 6 && x.f.Device.Word("words", 100) == 1 })
	if x.status(arr).WritePending {
		t.Fatal("読むだけのエリアは書き込み未反映にしない")
	}
	// 読むだけのエリアの変数は、次の読み取りで機器の値に戻る
	waitFor(t, "機器の値に戻る", func() bool { return x.value(arr) == "[1 2 3 4 5 6 7 8 9 10]" })
}

// 読めなくなったら、値を残して決めた回数で通信エラーにする。書き込みは未反映のまま残し、つながったら送る
func TestMapping_TimeoutKeepsValueAndRetriesWrite(t *testing.T) {
	x := newMappingFixture(t)
	if err := x.svc.SetClientFailThreshold(x.client.ID, 0); err == nil {
		t.Fatal("0 回はエラー")
	}
	if err := x.svc.SetClientFailThreshold(x.client.ID, 2); err != nil {
		t.Fatal(err)
	}
	x.f.Device.SetWord("words", 0, 11)
	a := x.variable("a", "INT", 0, "words", 0)
	x.start()
	waitFor(t, "読む", func() bool { return x.value(a) == "11" })

	x.f.Device.Down.Store(true)
	waitFor(t, "通信エラー", func() bool { return x.status(a).State == "comm_error" })
	if x.value(a) != "11" || x.status(a).Error == "" {
		t.Fatalf("値を残し、理由を出す: %s %+v", x.value(a), x.status(a))
	}

	_ = x.svc.UpdateVariableValue(a, 99)
	waitFor(t, "書き込み未反映", func() bool { return x.status(a).WritePending })
	time.Sleep(5 * minPollIntervalMs * time.Millisecond)
	if st := x.status(a); !st.WritePending || x.f.Device.Word("words", 0) != 11 {
		t.Fatalf("つながらない間は書けない: %+v", st)
	}
	p, _ := x.svc.GetClientPolling(x.client.ID)
	if p.Mapping == nil || p.Mapping.Variables != 1 || p.Mapping.CommError != 1 || p.Mapping.WritePending != 1 {
		t.Fatalf("まとめ: %+v", p.Mapping)
	}

	x.f.Device.Down.Store(false)
	waitFor(t, "つながったら送る", func() bool { return x.f.Device.Word("words", 0) == 99 })
	waitFor(t, "状態が戻る", func() bool {
		st := x.status(a)
		return st.State == "ok" && !st.WritePending && st.Error == ""
	})
	if x.value(a) != "99" {
		t.Fatalf("書いた値のまま: %s", x.value(a))
	}
}

// 止めている間に変えた値は、書き込み未反映として残り、開始したら機器へ書く
func TestMapping_PendingSurvivesStop(t *testing.T) {
	x := newMappingFixture(t)
	a := x.variable("a", "INT", 0, "words", 0)
	x.start()
	waitFor(t, "読む", func() bool { return x.status(a).State == "ok" })
	_ = x.svc.StopClientPolling(x.client.ID)

	_ = x.svc.UpdateVariableValue(a, 5)
	if st := x.status(a); st.State != syncStateStopped || !st.WritePending {
		t.Fatalf("停止中: %+v", st)
	}
	x.f.Device.SetWord("words", 0, 77) // 止めている間に機器の側も変わった
	x.start()
	waitFor(t, "書く", func() bool { return x.f.Device.Word("words", 0) == 5 })
	time.Sleep(3 * minPollIntervalMs * time.Millisecond)
	if x.value(a) != "5" {
		t.Fatalf("最後に書いた側（変数）が残る: %s", x.value(a))
	}
}

func TestMapping_InvalidAndRemove(t *testing.T) {
	x := newMappingFixture(t)
	bad := x.variable("bad", "INT", 0, "bits", 0) // ビットのエリアに INT
	keep, _ := x.svc.CreateVariable("keep", "INT", 0)
	_ = x.svc.UpdateVariableMappings(keep.ID, []ProtocolMappingDTO{
		{ProtocolType: "modbus-tcp", MemoryArea: "holdingRegisters", Address: 0, Endianness: "big"},
		{ProtocolType: x.key, MemoryArea: "words", Address: 5, Endianness: "big"},
	})
	if st := x.status(bad); st.State != "invalid" || st.Error == "" {
		t.Fatalf("使えないマッピング: %+v", st)
	}

	x.start()
	if err := x.svc.RemoveClient(x.client.ID); err != nil {
		t.Fatal(err)
	}
	// 接続先を削除すると、その接続先へのマッピングだけ外れる
	ms, _ := x.svc.GetVariableMappings(keep.ID)
	if len(ms) != 1 || ms[0].ProtocolType != "modbus-tcp" {
		t.Fatalf("残ったマッピング: %+v", ms)
	}
	if ms, _ := x.svc.GetVariableMappings(bad); len(ms) != 0 {
		t.Fatalf("外れていない: %+v", ms)
	}
	if len(x.svc.GetVariableDeviceStatuses()) != 0 {
		t.Fatal("状態が残っている")
	}
}

// プロジェクトを読み込んでも、接続先の ID が変わらず、マッピングが同じ接続先を指す
func TestMapping_ProjectKeepsClientID(t *testing.T) {
	x := newMappingFixture(t)
	_ = x.svc.SetClientFailThreshold(x.client.ID, 7)
	x.f.Device.SetWord("words", 3, 55)
	x.variable("a", "INT", 0, "words", 3)

	data := x.svc.ExportProject()
	if data.Clients[0].ID != x.client.ID || data.Clients[0].Polling.FailThreshold != 7 {
		t.Fatalf("export: %+v %+v", data.Clients[0], data.Clients[0].Polling)
	}
	if err := x.svc.ImportProject(data); err != nil {
		t.Fatal(err)
	}
	list := x.svc.GetClients()
	if len(list) != 1 || list[0].ID != x.client.ID {
		t.Fatalf("ID が変わった: %+v", list)
	}
	p, _ := x.svc.GetClientPolling(x.client.ID)
	if p.FailThreshold != 7 {
		t.Fatalf("failThreshold = %d", p.FailThreshold)
	}
	var id string
	for _, v := range x.svc.GetVariables() {
		if v.Name == "a" {
			id = v.ID
		}
	}
	x.start()
	waitFor(t, "読み込み後も同期する", func() bool { return x.value(id) == "55" })
}

// スクリプトから同期の状態を読める
func TestMapping_ScriptStatus(t *testing.T) {
	x := newMappingFixture(t)
	x.variable("a", "INT", 0, "words", 0)
	x.start()
	waitFor(t, "読む", func() bool {
		st := x.svc.VariableDeviceStatus(x.svc.GetVariables()[0].ID)
		return st != nil && st.State == "ok"
	})
	res, err := x.svc.RunScriptOnce(`const s = plc.getVariableStatus("a"); return s.state + "/" + s.writePending + "/" + s.client + "/" + plc.getVariableStatus("nope");`)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(res) != "ok/false/機器/null" {
		t.Fatalf("script = %v", res)
	}
}
