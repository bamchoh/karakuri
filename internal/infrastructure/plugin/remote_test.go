package plugin

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/protocoltest"
	"karakuri/internal/domain/variable"
)

// ===== LazyRemoteServerFactory =====

func TestFactory_ManifestInfoNeedsNoPlugin(t *testing.T) {
	// 接続先のないマニフェストでも、マニフェストだけで答えられる情報は返せる
	f := NewLazyRemoteServerFactory(&PluginManifestEntry{Manifest: testManifest(0), Dir: t.TempDir()}, NewPluginProcessManager(""))

	if f.ProtocolType() != testProtocol || f.DisplayName() != "Fake" {
		t.Errorf("ProtocolType / DisplayName = %q / %q", f.ProtocolType(), f.DisplayName())
	}
	if got, want := f.ConfigVariants(), []protocol.ConfigVariant{{ID: "v1", DisplayName: "Variant 1"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("ConfigVariants = %+v, want %+v", got, want)
	}
	want := protocol.ProtocolCapabilities{SupportsUnitID: true, UnitIDMin: 1, UnitIDMax: 247, SupportsNodePublishing: true}
	if got := f.GetProtocolCapabilities(); got != want {
		t.Errorf("GetProtocolCapabilities = %+v, want %+v", got, want)
	}
	if f.IsProcessCrashed() {
		t.Error("起動していないのに IsProcessCrashed = true")
	}
}

func TestFactory_EnsureStartedFailsWithoutPlugin(t *testing.T) {
	// 接続先がなく、exe も存在しない
	f := NewLazyRemoteServerFactory(&PluginManifestEntry{Manifest: testManifest(0), Dir: t.TempDir()}, NewPluginProcessManager(""))
	if err := f.EnsureStarted(); err == nil {
		t.Fatal("exe がないのに EnsureStarted がエラーにならなかった")
	}
	// 起動できないときも、設定まわりは落ちずに空の値を返す
	if cfg := f.CreateConfigFromVariant("v1"); cfg == nil || cfg.Variant() != "v1" || cfg.ProtocolType() != testProtocol {
		t.Errorf("CreateConfigFromVariant = %+v", cfg)
	}
	if fields := f.GetConfigFields("v1"); fields != nil {
		t.Errorf("GetConfigFields = %+v, want nil", fields)
	}
	if _, err := f.MapToConfig("v1", map[string]interface{}{}); err == nil {
		t.Error("MapToConfig がエラーにならなかった")
	}
}

func TestFactory_ConfigRoundTrip(t *testing.T) {
	_, f := startPlugin(t)

	// 既定の設定はプラグインが決める
	cfg := f.DefaultConfig()
	if cfg.ProtocolType() != testProtocol || cfg.Variant() != "v1" {
		t.Fatalf("DefaultConfig = %+v", cfg)
	}
	if got := f.ConfigToMap(cfg)["port"]; got != float64(protocoltest.DefaultPort) {
		t.Errorf("既定の port = %v, want %d", got, protocoltest.DefaultPort)
	}

	// 設定の更新と、プラグイン側の検証
	cfg, err := f.MapToConfig("v1", map[string]interface{}{"port": 1502})
	if err != nil {
		t.Fatalf("MapToConfig: %v", err)
	}
	if got := f.ConfigToMap(cfg)["port"]; got != float64(1502) {
		t.Errorf("更新後の port = %v, want 1502", got)
	}
	if got := f.ConfigToMap(cfg.Clone())["port"]; got != float64(1502) {
		t.Errorf("Clone した設定の port = %v, want 1502", got)
	}
	if _, err := f.MapToConfig("v1", map[string]interface{}{"port": 0}); err == nil {
		t.Error("プラグインが拒否する設定がエラーにならなかった")
	}

	// 他のファクトリーの設定は空のマップになる
	if got := f.ConfigToMap(&protocoltest.Config{}); len(got) != 0 {
		t.Errorf("別の型の設定 = %v, want 空", got)
	}
}

func TestFactory_GetConfigFields(t *testing.T) {
	_, f := startPlugin(t)

	fields := f.GetConfigFields("v1")
	if len(fields) != 1 {
		t.Fatalf("フィールド数 = %d, want 1", len(fields))
	}
	got := fields[0]
	if got.Name != "port" || got.Label != "ポート番号" || got.Type != "number" || !got.Required || got.Category != "ネットワーク" {
		t.Errorf("フィールドの属性が違う: %+v", got)
	}
	if got.Default != float64(protocoltest.DefaultPort) {
		t.Errorf("Default = %v (%T), want %d", got.Default, got.Default, protocoltest.DefaultPort)
	}
	if got.Min == nil || *got.Min != 1 || got.Max == nil || *got.Max != 65535 {
		t.Errorf("Min / Max が違う: %v / %v", got.Min, got.Max)
	}
	if got.Condition != nil || len(got.Options) != 0 {
		t.Errorf("Condition / Options は空のはず: %+v", got)
	}
}

func TestFactory_StopProcessThenReconnect(t *testing.T) {
	_, f := startPlugin(t)
	if err := f.EnsureStarted(); err != nil {
		t.Fatalf("EnsureStarted: %v", err)
	}
	f.StopProcess()
	// 止めたあとも、必要になればつなぎ直す
	if got := f.GetConfigFields("v1"); len(got) != 1 {
		t.Errorf("StopProcess のあと GetConfigFields = %+v", got)
	}
	if err := f.ForceReconnect(); err != nil {
		t.Fatalf("ForceReconnect: %v", err)
	}
	if got := f.GetConfigFields("v1"); len(got) != 1 {
		t.Errorf("ForceReconnect のあと GetConfigFields = %+v", got)
	}
}

// ===== RemoteProtocolServer =====

func TestServer_Lifecycle(t *testing.T) {
	pluginSide, f := startPlugin(t)
	cfg, err := f.MapToConfig("v1", map[string]interface{}{"port": 1502})
	if err != nil {
		t.Fatalf("MapToConfig: %v", err)
	}
	srv, err := f.CreateServer(cfg, f.CreateDataStore())
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if srv.ProtocolType() != testProtocol || srv.Config() != cfg {
		t.Errorf("ProtocolType / Config が違う: %q", srv.ProtocolType())
	}
	if got := srv.Status(); got != protocol.StatusStopped {
		t.Errorf("起動前の Status = %v, want Stopped", got)
	}

	if err := srv.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := srv.Status(); got != protocol.StatusRunning {
		t.Errorf("起動後の Status = %v, want Running", got)
	}
	// ホストの設定がプラグイン側のサーバーに渡っている
	if got := pluginSide.pluginServer().Cfg.(*protocoltest.Config).Settings["port"]; got != float64(1502) {
		t.Errorf("プラグイン側の port = %v, want 1502", got)
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := srv.Status(); got != protocol.StatusStopped {
		t.Errorf("停止後の Status = %v, want Stopped", got)
	}

	// 設定を変えて起動し直すと、新しい設定で動く
	cfg2, _ := f.MapToConfig("v1", map[string]interface{}{"port": 1503})
	if err := srv.UpdateConfig(cfg2); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if err := srv.Start(t.Context()); err != nil {
		t.Fatalf("2 回目の Start: %v", err)
	}
	if got := pluginSide.pluginServer().Cfg.(*protocoltest.Config).Settings["port"]; got != float64(1503) {
		t.Errorf("設定変更後のプラグイン側の port = %v, want 1503", got)
	}
	_ = srv.Stop()
}

func TestServer_StartRejectsForeignConfig(t *testing.T) {
	_, f := startPlugin(t)
	if err := f.EnsureStarted(); err != nil {
		t.Fatal(err)
	}
	srv, _ := f.CreateServer(&protocoltest.Config{}, f.CreateDataStore())
	if err := srv.Start(t.Context()); err == nil {
		t.Error("別の型の設定で Start がエラーにならなかった")
	}
}

func TestServer_StatusIsErrorWhenPluginIsGone(t *testing.T) {
	_, f, _, rs := startedServer(t)
	f.StopProcess() // 接続を閉じる
	if got := rs.Status(); got != protocol.StatusError {
		t.Errorf("接続が切れたときの Status = %v, want Error", got)
	}
}

func TestServer_UnitIDSettings(t *testing.T) {
	_, _, _, rs := startedServer(t)

	if got := rs.GetDisabledUnitIDs(); len(got) != 0 {
		t.Errorf("最初は無効な UnitID なしのはず: %v", got)
	}
	rs.SetUnitIdEnabled(5, false)
	rs.SetUnitIdEnabled(200, false)
	if got := rs.GetDisabledUnitIDs(); !reflect.DeepEqual(got, []uint8{5, 200}) {
		t.Errorf("無効にしたあと = %v, want [5 200]", got)
	}
	rs.SetUnitIdEnabled(5, true)
	if got := rs.GetDisabledUnitIDs(); !reflect.DeepEqual(got, []uint8{200}) {
		t.Errorf("有効に戻したあと = %v, want [200]", got)
	}
	rs.SetDisabledUnitIDs([]uint8{1, 2, 3})
	if got := rs.GetDisabledUnitIDs(); !reflect.DeepEqual(got, []uint8{1, 2, 3}) {
		t.Errorf("一括設定のあと = %v, want [1 2 3]", got)
	}
}

func TestServer_NodeAddresses(t *testing.T) {
	_, f := startPlugin(t)
	cfg, err := f.MapToConfig("v1", map[string]interface{}{"port": 1502})
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := f.CreateServer(cfg, f.CreateDataStore())
	rs := srv.(*RemoteProtocolServer)
	vars := []protocol.NodeVariable{{ID: "id1", Name: "Good"}, {ID: "id2", Name: "Bad!"}}
	want := map[string]protocol.NodeAddress{
		"id1": {Address: "port1502:Good"},
		"id2": {Error: "! は使えません"},
	}

	// 起動していなくても、ホストが持つ設定で答える
	if got := rs.NodeAddresses(vars); !reflect.DeepEqual(got, want) {
		t.Errorf("停止中 = %+v, want %+v", got, want)
	}
	if err := rs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer rs.Stop() //nolint:errcheck
	if got := rs.NodeAddresses(vars); !reflect.DeepEqual(got, want) {
		t.Errorf("起動中 = %+v, want %+v", got, want)
	}
}

// commRecorder は受け取った通信イベントを数える
type commRecorder struct {
	mu          sync.Mutex
	rx, tx      int
	connections []int
}

func (r *commRecorder) EmitRx() { r.mu.Lock(); r.rx++; r.mu.Unlock() }
func (r *commRecorder) EmitTx() { r.mu.Lock(); r.tx++; r.mu.Unlock() }
func (r *commRecorder) EmitConnection(n int) {
	r.mu.Lock()
	r.connections = append(r.connections, n)
	r.mu.Unlock()
}
func (r *commRecorder) snapshot() (rx, tx int, lastConn int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lastConn = -1
	if len(r.connections) > 0 {
		lastConn = r.connections[len(r.connections)-1]
	}
	return r.rx, r.tx, lastConn
}

func TestServer_CommunicationEventsReachHost(t *testing.T) {
	pluginSide, f := startPlugin(t)
	if err := f.EnsureStarted(); err != nil {
		t.Fatal(err)
	}
	srv, _ := f.CreateServer(f.CreateConfigFromVariant("v1"), f.CreateDataStore())
	rs := srv.(*RemoteProtocolServer)
	rec := &commRecorder{}
	rs.SetEventEmitter(rec)
	if err := rs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	// プラグイン側のサーバーには、起動前に送信先が渡っている
	emitter := pluginSide.pluginServer().getEmitter()
	if emitter == nil {
		t.Fatal("プラグイン側のサーバーに通信イベントの送信先が渡っていない")
	}

	// プラグインで起きたイベントがホストの送信先に届く（購読の開始を待つため、届くまで繰り返す）
	eventually(t, "RX / TX / 接続数がホストに届く", func() bool {
		emitter.EmitRx()
		emitter.EmitTx()
		emitter.EmitConnection(3)
		rx, tx, conn := rec.snapshot()
		return rx > 0 && tx > 0 && conn == 3
	})

	// 止めると接続数が 0 に戻り、それ以降は届かない
	if err := rs.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, _, conn := rec.snapshot(); conn != 0 {
		t.Errorf("停止後の接続数 = %d, want 0", conn)
	}
	rxBefore, _, _ := rec.snapshot()
	emitter.EmitRx()
	time.Sleep(150 * time.Millisecond)
	if rx, _, _ := rec.snapshot(); rx != rxBefore {
		t.Errorf("停止後もイベントが届いた: %d → %d", rxBefore, rx)
	}
}

// ===== RemoteDataStore =====

func TestDataStore_Areas(t *testing.T) {
	_, _, ds, _ := startedServer(t)
	// エリアの属性がすべて gRPC を越えて届く
	if got := ds.GetAreas(); !reflect.DeepEqual(got, protocoltest.Areas) {
		t.Errorf("GetAreas = %+v, want %+v", got, protocoltest.Areas)
	}
}

func TestDataStore_WordsAndBits(t *testing.T) {
	pluginSide, _, ds, _ := startedServer(t)
	const hr, coils = "holdingRegisters", "coils"

	// ワード
	if err := ds.WriteWord(hr, 10, 0xFFFF); err != nil {
		t.Fatalf("WriteWord: %v", err)
	}
	if err := ds.WriteWords(hr, 11, []uint16{1, 2, 3}); err != nil {
		t.Fatalf("WriteWords: %v", err)
	}
	if got, err := ds.ReadWord(hr, 10); err != nil || got != 0xFFFF {
		t.Errorf("ReadWord = %d, %v", got, err)
	}
	if got, err := ds.ReadWords(hr, 10, 5); err != nil || !reflect.DeepEqual(got, []uint16{0xFFFF, 1, 2, 3, 0}) {
		t.Errorf("ReadWords = %v, %v", got, err)
	}
	// プラグイン側の DataStore に実際に入っている
	if got, _ := pluginSide.pluginStore().ReadWords(hr, 10, 2); !reflect.DeepEqual(got, []uint16{0xFFFF, 1}) {
		t.Errorf("プラグイン側のワード = %v", got)
	}

	// ビット
	if err := ds.WriteBit(coils, 3, true); err != nil {
		t.Fatalf("WriteBit: %v", err)
	}
	if err := ds.WriteBits(coils, 4, []bool{false, true}); err != nil {
		t.Fatalf("WriteBits: %v", err)
	}
	if got, err := ds.ReadBit(coils, 3); err != nil || !got {
		t.Errorf("ReadBit = %v, %v", got, err)
	}
	if got, err := ds.ReadBits(coils, 2, 5); err != nil || !reflect.DeepEqual(got, []bool{false, true, false, true, false}) {
		t.Errorf("ReadBits = %v, %v", got, err)
	}

	// プラグイン側のエラーはエラーとして返る
	if _, err := ds.ReadWords(hr, 9999, 1); err == nil {
		t.Error("範囲外の ReadWords がエラーにならなかった")
	}
	if _, err := ds.ReadWord("nope", 0); err == nil {
		t.Error("存在しないエリアの ReadWord がエラーにならなかった")
	}
	if err := ds.WriteWord(hr, 9999, 1); err == nil {
		t.Error("範囲外の WriteWord がエラーにならなかった")
	}
	if err := ds.WriteWords(hr, 9998, []uint16{1, 2}); err == nil {
		t.Error("範囲外の WriteWords がエラーにならなかった")
	}
	if _, err := ds.ReadBits(coils, 9999, 1); err == nil {
		t.Error("範囲外の ReadBits がエラーにならなかった")
	}
	if _, err := ds.ReadBit(coils, 9999); err == nil {
		t.Error("範囲外の ReadBit がエラーにならなかった")
	}
	if err := ds.WriteBit(coils, 9999, true); err == nil {
		t.Error("範囲外の WriteBit がエラーにならなかった")
	}
	if err := ds.WriteBits(coils, 9998, []bool{true, true}); err == nil {
		t.Error("範囲外の WriteBits がエラーにならなかった")
	}
}

func TestDataStore_SnapshotRestoreClear(t *testing.T) {
	_, _, ds, _ := startedServer(t)
	const hr = "holdingRegisters"
	_ = ds.WriteWords(hr, 10, []uint16{11, 22})

	snap := ds.Snapshot()
	if got := fmt.Sprint(snap); got != "map[holdingRegisters:map[10:11 11:22]]" {
		t.Errorf("Snapshot = %s", got)
	}

	ds.ClearAll()
	if got, _ := ds.ReadWords(hr, 10, 2); !reflect.DeepEqual(got, []uint16{0, 0}) {
		t.Errorf("ClearAll のあと = %v, want [0 0]", got)
	}

	if err := ds.Restore(snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got, _ := ds.ReadWords(hr, 10, 2); !reflect.DeepEqual(got, []uint16{11, 22}) {
		t.Errorf("Restore のあと = %v, want [11 22]", got)
	}
}

// ===== RemoteVariableChangeListener（変数 ↔ プラグインの DataStore の同期） =====

// syncFixture は、INT の変数 Speed（holdingRegisters 10 番地）と BOOL の変数 Run（coils 3 番地）を
// 持つ変数ストアと、起動済みのプラグインにつないだリスナー
type syncFixture struct {
	t          *testing.T
	vars       *variable.VariableStore
	speed, run string // 変数 ID
	plugin     *hookStore
	ds         *RemoteDataStore
	listener   *RemoteVariableChangeListener
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	pluginSide, _, ds, _ := startedServer(t)
	vars := variable.NewVariableStore()
	speed, err := vars.CreateVariable("Speed", "INT", 12)
	if err != nil {
		t.Fatal(err)
	}
	run, err := vars.CreateVariable("Run", "BOOL", true)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := vars.CreateVariable("Other", "INT", 99)
	mustMap := func(id, protocolType, area string, addr uint32) {
		if err := vars.SetMappings(id, []variable.ProtocolMapping{{ProtocolType: protocolType, MemoryArea: area, Address: addr, Endianness: "big"}}); err != nil {
			t.Fatal(err)
		}
	}
	mustMap(speed.ID, testProtocol, "holdingRegisters", 10)
	mustMap(run.ID, testProtocol, "coils", 3)
	mustMap(other.ID, "another-protocol", "holdingRegisters", 20) // 別のプロトコルへのマッピングは対象外

	listener := NewRemoteVariableChangeListener(ds, vars, testProtocol)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = listener.StartChangeSubscription(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		listener.Detach()
	})
	return &syncFixture{t: t, vars: vars, speed: speed.ID, run: run.ID, plugin: pluginSide.pluginStore(), ds: ds, listener: listener}
}

func (f *syncFixture) value(id string) string {
	f.t.Helper()
	v, err := f.vars.GetVariable(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return fmt.Sprint(v.Value)
}

func (f *syncFixture) pluginWord(addr uint32) uint16 {
	v, _ := f.plugin.ReadWord("holdingRegisters", addr)
	return v
}

func (f *syncFixture) pluginBit(addr uint32) bool {
	v, _ := f.plugin.ReadBit("coils", addr)
	return v
}

func TestListener_VariableToPlugin(t *testing.T) {
	f := newSyncFixture(t)

	// つないだ時点の変数の値が、プラグインの DataStore に入る
	if got := f.pluginWord(10); got != 12 {
		t.Errorf("最初の同期: レジスタ = %d, want 12", got)
	}
	if !f.pluginBit(3) {
		t.Error("最初の同期: コイルが ON になっていない")
	}
	// 別のプロトコルにマッピングした変数は書かれない
	if got := f.pluginWord(20); got != 0 {
		t.Errorf("別のプロトコルの変数が書かれた: %d", got)
	}

	// 変数を変えると、プラグインの DataStore も変わる
	if err := f.vars.UpdateValue(f.speed, 345); err != nil {
		t.Fatal(err)
	}
	if got := f.pluginWord(10); got != 345 {
		t.Errorf("変数の変更後: レジスタ = %d, want 345", got)
	}
	if err := f.vars.UpdateValue(f.run, false); err != nil {
		t.Fatal(err)
	}
	if f.pluginBit(3) {
		t.Error("変数の変更後: コイルが OFF になっていない")
	}

	// 解除すると同期しなくなる
	f.listener.Detach()
	_ = f.vars.UpdateValue(f.speed, 678)
	if got := f.pluginWord(10); got != 345 {
		t.Errorf("解除後も同期された: レジスタ = %d, want 345", got)
	}
}

func TestListener_ClientWriteToVariable(t *testing.T) {
	f := newSyncFixture(t)

	// プロトコルのクライアントがレジスタに書くと、変数が変わる（購読の開始を待つため、変わるまで繰り返す）
	eventually(t, "クライアントのワード書き込みが変数に届く", func() bool {
		_ = f.plugin.WriteWords("holdingRegisters", 10, []uint16{56})
		return f.value(f.speed) == "56"
	})
	eventually(t, "クライアントのビット書き込みが変数に届く", func() bool {
		_ = f.plugin.WriteBits("coils", 3, []bool{false})
		return f.value(f.run) == "false"
	})

	// 変数が変わっても、プラグインへ書き戻して値を壊さない
	time.Sleep(50 * time.Millisecond)
	if got := f.pluginWord(10); got != 56 {
		t.Errorf("レジスタ = %d, want 56", got)
	}

	// マッピングのないアドレスへの書き込みは無視する
	_ = f.plugin.WriteWords("holdingRegisters", 500, []uint16{1})
	time.Sleep(50 * time.Millisecond)
	if got := f.value(f.speed); got != "56" {
		t.Errorf("関係ないアドレスへの書き込みで変数が変わった: %s", got)
	}
}

func TestListener_HostWriteToVariable(t *testing.T) {
	f := newSyncFixture(t)
	// 購読が始まったことを確かめておく
	eventually(t, "購読の開始", func() bool {
		_ = f.plugin.WriteWords("holdingRegisters", 10, []uint16{56})
		return f.value(f.speed) == "56"
	})

	// ホスト（画面・スクリプト）からの書き込みは、プラグインから変更として通知されない
	if err := f.ds.WriteWord("holdingRegisters", 10, 78); err != nil {
		t.Fatal(err)
	}
	if err := f.ds.WriteBit("coils", 3, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := f.value(f.speed); got != "56" {
		t.Errorf("ホストの書き込みが変更として通知された: Speed = %s, want 56", got)
	}
	if got := f.value(f.run); got != "true" {
		t.Errorf("ホストの書き込みが変更として通知された: Run = %s, want true", got)
	}

	// そのため、ホストが書いたあとは明示的に変数へ反映する
	f.listener.SyncHostWordWriteToVariable("holdingRegisters", 10)
	if got := f.value(f.speed); got != "78" {
		t.Errorf("SyncHostWordWriteToVariable のあと Speed = %s, want 78", got)
	}
	f.listener.SyncHostBitWriteToVariable("coils", 3)
	if got := f.value(f.run); got != "false" {
		t.Errorf("SyncHostBitWriteToVariable のあと Run = %s, want false", got)
	}
}

func TestListener_MultiWordVariable(t *testing.T) {
	pluginSide, _, ds, _ := startedServer(t)
	vars := variable.NewVariableStore()
	arr, err := vars.CreateVariable("Values", "ARRAY[0..2] OF INT", []interface{}{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := vars.SetMappings(arr.ID, []variable.ProtocolMapping{{ProtocolType: testProtocol, MemoryArea: "holdingRegisters", Address: 30, Endianness: "big"}}); err != nil {
		t.Fatal(err)
	}
	listener := NewRemoteVariableChangeListener(ds, vars, testProtocol)
	defer listener.Detach()

	// 配列は連続したレジスタに入る
	if got, _ := pluginSide.pluginStore().ReadWords("holdingRegisters", 30, 3); !reflect.DeepEqual(got, []uint16{1, 2, 3}) {
		t.Errorf("配列の同期: レジスタ = %v, want [1 2 3]", got)
	}

	// ホストが途中の 1 ワードだけ書いても、変数全体を読み直して反映する
	// （変更の通知は変数の先頭アドレスで行う）
	if err := ds.WriteWords("holdingRegisters", 30, []uint16{7, 8, 9}); err != nil {
		t.Fatal(err)
	}
	listener.SyncHostWordWriteToVariable("holdingRegisters", 30)
	v, _ := vars.GetVariable(arr.ID)
	if got := fmt.Sprint(v.Value); got != "[7 8 9]" {
		t.Errorf("配列の変数 = %s, want [7 8 9]", got)
	}
}

// ===== DataStore の寿命（#44: 起動前から使え、起動し直しても値が残る） =====

// stoppedServer はプラグインに接続し、サーバーを作るところまで行う（起動しない）。
// PLCService がサーバーを追加した直後と同じ状態
func stoppedServer(t *testing.T) (*testFactory, *RemoteDataStore, *RemoteProtocolServer) {
	t.Helper()
	pluginSide, hostSide := startPlugin(t)
	if err := hostSide.EnsureStarted(); err != nil {
		t.Fatalf("EnsureStarted: %v", err)
	}
	ds := hostSide.CreateDataStore().(*RemoteDataStore)
	srv, err := hostSide.CreateServer(hostSide.CreateConfigFromVariant("v1"), ds)
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	rs := srv.(*RemoteProtocolServer)
	t.Cleanup(func() { _ = rs.Stop() })
	return pluginSide, ds, rs
}

func TestDataStore_UsableBeforeServerStart(t *testing.T) {
	_, ds, rs := stoppedServer(t)
	const hr, coils = "holdingRegisters", "coils"

	// 一度も起動していなくても、エリアの一覧が取れて読み書きできる
	if got := ds.GetAreas(); !reflect.DeepEqual(got, protocoltest.Areas) {
		t.Errorf("起動前の GetAreas = %+v", got)
	}
	if err := ds.WriteWord(hr, 5, 77); err != nil {
		t.Fatalf("起動前の WriteWord: %v", err)
	}
	if err := ds.WriteBit(coils, 5, true); err != nil {
		t.Fatalf("起動前の WriteBit: %v", err)
	}
	if got, err := ds.ReadWord(hr, 5); err != nil || got != 77 {
		t.Errorf("起動前の ReadWord = %d, %v", got, err)
	}

	// 起動前に書いた値は、起動したあとも残っている
	if err := rs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, err := ds.ReadWord(hr, 5); err != nil || got != 77 {
		t.Errorf("起動後の ReadWord = %d, %v, want 77", got, err)
	}
	if got, err := ds.ReadBit(coils, 5); err != nil || !got {
		t.Errorf("起動後の ReadBit = %v, %v, want true", got, err)
	}
}

func TestDataStore_KeepsValuesAcrossRestart(t *testing.T) {
	pluginSide, _, ds, rs := startedServer(t)
	const hr = "holdingRegisters"
	if err := ds.WriteWords(hr, 5, []uint16{11, 22}); err != nil {
		t.Fatal(err)
	}
	store := pluginSide.pluginStore()

	for i := 0; i < 2; i++ {
		if err := rs.Stop(); err != nil {
			t.Fatal(err)
		}
		// 停止中も読める
		if got, err := ds.ReadWords(hr, 5, 2); err != nil || !reflect.DeepEqual(got, []uint16{11, 22}) {
			t.Errorf("停止中（%d 回目）= %v, %v, want [11 22]", i+1, got, err)
		}
		if err := rs.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got, err := ds.ReadWords(hr, 5, 2); err != nil || !reflect.DeepEqual(got, []uint16{11, 22}) {
			t.Errorf("起動し直したあと（%d 回目）= %v, %v, want [11 22]", i+1, got, err)
		}
	}
	// プラグイン側の DataStore は作り直されていない
	if pluginSide.pluginStore() != store {
		t.Error("起動し直したときに DataStore が作り直された")
	}
}

func TestListener_SyncSurvivesStartAndRestart(t *testing.T) {
	pluginSide, ds, rs := stoppedServer(t)
	vars := variable.NewVariableStore()
	speed, _ := vars.CreateVariable("Speed", "INT", 12)
	if err := vars.SetMappings(speed.ID, []variable.ProtocolMapping{{ProtocolType: testProtocol, MemoryArea: "holdingRegisters", Address: 10, Endianness: "big"}}); err != nil {
		t.Fatal(err)
	}
	value := func() string {
		v, _ := vars.GetVariable(speed.ID)
		return fmt.Sprint(v.Value)
	}
	register := func() uint16 {
		v, _ := pluginSide.pluginStore().ReadWord("holdingRegisters", 10)
		return v
	}

	// サーバーを追加した時点（起動前）で、変数の値がレジスタに入る
	listener := NewRemoteVariableChangeListener(ds, vars, testProtocol)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = listener.StartChangeSubscription(ctx)
	}()
	defer func() {
		cancel()
		<-done
		listener.Detach()
	}()
	if got := register(); got != 12 {
		t.Errorf("起動前: レジスタ = %d, want 12", got)
	}
	// 起動前に変数を変えても反映される
	_ = vars.UpdateValue(speed.ID, 34)
	if got := register(); got != 34 {
		t.Errorf("起動前の変数の変更: レジスタ = %d, want 34", got)
	}

	// 起動・起動し直しのあとも、レジスタは変数と同じ値のままで、双方向の同期が続く
	for i := 0; i < 2; i++ {
		if err := rs.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := register(); fmt.Sprint(got) != value() {
			t.Errorf("起動 %d 回目: レジスタ = %d, 変数 = %s", i+1, got, value())
		}
		want := uint16(100 + i)
		// 書き込みは 1 回ずつ行い、届くのを待つ（続けて書くと、遅れて届いた通知が後の値を上書きする）
		eventually(t, "起動後もクライアントの書き込みが変数に届く", func() bool {
			_ = pluginSide.pluginStore().WriteWords("holdingRegisters", 10, []uint16{want})
			for wait := 0; wait < 40 && value() != fmt.Sprint(want); wait++ {
				time.Sleep(5 * time.Millisecond)
			}
			return value() == fmt.Sprint(want)
		})
		time.Sleep(50 * time.Millisecond) // 残っている通知が処理されるのを待つ
		_ = vars.UpdateValue(speed.ID, 200+i)
		if got := register(); got != uint16(200+i) {
			t.Errorf("起動 %d 回目の変数の変更: レジスタ = %d, want %d", i+1, got, 200+i)
		}
		if err := rs.Stop(); err != nil {
			t.Fatal(err)
		}
	}
}

// ===== UnitID の応答設定の寿命（#49: 起動前から設定でき、起動し直しても残る） =====

func TestServer_UnitIDSettingsBeforeStartAndAcrossRestart(t *testing.T) {
	pluginSide, _, rs := stoppedServer(t)

	// 一度も起動していなくても設定でき、読み返せる
	rs.SetUnitIdEnabled(7, false)
	rs.SetUnitIdEnabled(200, false)
	rs.SetUnitIdEnabled(9, false)
	rs.SetUnitIdEnabled(9, true)
	if got := rs.GetDisabledUnitIDs(); !reflect.DeepEqual(got, []uint8{7, 200}) {
		t.Fatalf("起動前の設定 = %v, want [7 200]", got)
	}

	// 起動すると、プラグイン側のサーバーに渡っている
	if err := rs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := pluginSide.pluginServer().GetDisabledUnitIDs(); !reflect.DeepEqual(got, []uint8{7, 200}) {
		t.Errorf("起動後のプラグイン側のサーバー = %v, want [7 200]", got)
	}

	// 起動中の変更は、動いているサーバーにすぐ反映される
	rs.SetUnitIdEnabled(5, false)
	rs.SetUnitIdEnabled(200, true)
	if got := pluginSide.pluginServer().GetDisabledUnitIDs(); !reflect.DeepEqual(got, []uint8{5, 7}) {
		t.Errorf("起動中に変更したあとのプラグイン側のサーバー = %v, want [5 7]", got)
	}

	// 停止中も読め、起動し直しても残る（サーバーは作り直される）
	first := pluginSide.pluginServer()
	if err := rs.Stop(); err != nil {
		t.Fatal(err)
	}
	if got := rs.GetDisabledUnitIDs(); !reflect.DeepEqual(got, []uint8{5, 7}) {
		t.Errorf("停止中の設定 = %v, want [5 7]", got)
	}
	if err := rs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if pluginSide.pluginServer() == first {
		t.Fatal("テストの前提が違う: 起動し直してもサーバーが作り直されていない")
	}
	if got := pluginSide.pluginServer().GetDisabledUnitIDs(); !reflect.DeepEqual(got, []uint8{5, 7}) {
		t.Errorf("起動し直したあとのプラグイン側のサーバー = %v, want [5 7]", got)
	}
	if got := rs.GetDisabledUnitIDs(); !reflect.DeepEqual(got, []uint8{5, 7}) {
		t.Errorf("起動し直したあとの設定 = %v, want [5 7]", got)
	}

	// 一括設定は、それまでの設定を置き換える
	rs.SetDisabledUnitIDs([]uint8{3, 1, 2})
	if got := pluginSide.pluginServer().GetDisabledUnitIDs(); !reflect.DeepEqual(got, []uint8{1, 2, 3}) {
		t.Errorf("一括設定のあとのプラグイン側のサーバー = %v, want [1 2 3]", got)
	}
	rs.SetDisabledUnitIDs(nil)
	if got := rs.GetDisabledUnitIDs(); len(got) != 0 {
		t.Errorf("空の一括設定のあと = %v, want 空", got)
	}
}
