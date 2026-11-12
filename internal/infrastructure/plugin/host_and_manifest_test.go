package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/protocol/protocoltest"
	"karakuri/internal/domain/variable"
	"karakuri/internal/infrastructure/adapter"
	"karakuri/internal/pluginruntime"
)

// ===== HostGrpcServer ↔ RemoteVariableStoreAccessor（プラグインからホストの変数へのアクセス） =====

// startHost はホスト側の gRPC サーバーを立て、プラグイン側のクライアントをつなぐ
func startHost(t *testing.T) (*variable.VariableStore, *pluginruntime.RemoteVariableStoreAccessor) {
	t.Helper()
	vars := variable.NewVariableStore()
	host := NewHostGrpcServer(adapter.NewVariableStoreAccessor(vars), vars)
	if host.Addr() != "" {
		t.Errorf("起動前の Addr = %q, want 空", host.Addr())
	}
	port, err := host.Start()
	if err != nil {
		t.Fatalf("HostGrpcServer.Start: %v", err)
	}
	if want := fmt.Sprintf("127.0.0.1:%d", port); host.Addr() != want {
		t.Errorf("Addr = %q, want %q", host.Addr(), want)
	}
	acc, err := pluginruntime.DialVariableStoreAccessor(host.Addr())
	if err != nil {
		t.Fatalf("DialVariableStoreAccessor: %v", err)
	}
	t.Cleanup(func() {
		_ = acc.Close()
		host.Stop()
	})
	return vars, acc
}

func TestHost_NodePublishings(t *testing.T) {
	vars, acc := startHost(t)
	a, _ := vars.CreateVariable("A", "INT", 1)
	b, _ := vars.CreateVariable("B", "REAL", 1.5)
	c, _ := vars.CreateVariable("C", "BOOL", true)
	vars.SetNodePublishing(a.ID, "opcua", &variable.NodePublishing{Enabled: true, AccessMode: "readwrite"})
	vars.SetNodePublishing(b.ID, "opcua", &variable.NodePublishing{Enabled: false, AccessMode: "read"}) // 公開していない
	vars.SetNodePublishing(c.ID, "other", &variable.NodePublishing{Enabled: true, AccessMode: "read"})  // 別のプロトコル

	got := acc.GetEnabledNodePublishings("opcua")
	want := []protocol.NodePublishingInfo{{VariableID: a.ID, VariableName: "A", DataType: "INT", AccessMode: "readwrite"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetEnabledNodePublishings = %+v, want %+v", got, want)
	}
	if got := acc.GetEnabledNodePublishings("nope"); len(got) != 0 {
		t.Errorf("公開のないプロトコル = %+v, want 空", got)
	}
}

// registerPoint は構造体型 Point{x INT, y REAL} を登録する
func registerPoint(t *testing.T, vars *variable.VariableStore) {
	t.Helper()
	point, err := variable.NewStructTypeDef("Point", []variable.StructField{{Name: "x", DataType: "INT"}, {Name: "y", DataType: "REAL"}}, vars)
	if err != nil {
		t.Fatal(err)
	}
	if err := vars.RegisterStructType(point); err != nil {
		t.Fatal(err)
	}
}

func TestHost_ReadValues(t *testing.T) {
	vars, acc := startHost(t)
	registerPoint(t, vars)

	// 型ごとに、ホストの変数の値がプラグインから読める（値は MessagePack で運ばれる）
	cases := []struct {
		dataType string
		value    interface{}
		want     string
	}{
		{"BOOL", true, "true"},
		{"SINT", -5, "-5"},
		{"INT", -123, "-123"},
		{"DINT", 100000, "100000"},
		{"LINT", "-9000000000000000000", "-9000000000000000000"},
		{"USINT", 200, "200"},
		{"UINT", 60000, "60000"},
		{"UDINT", 4000000000, "4000000000"},
		{"ULINT", "18000000000000000000", "18000000000000000000"},
		{"REAL", 1.5, "1.5"},
		{"LREAL", 2.25, "2.25"},
		{"STRING[10]", "hello", "hello"},
		{"TIME", "T#1s", "T#1s"},
		{"ARRAY[0..2] OF INT", []interface{}{1, 2, 3}, "[1 2 3]"},
		{"ARRAY[0..1, 0..1] OF BOOL", []interface{}{[]interface{}{true, false}, []interface{}{false, true}}, "[[true false] [false true]]"},
		{"ARRAY[0..1] OF STRING[5]", []interface{}{"a", "b"}, "[a b]"},
		{"Point", map[string]interface{}{"x": 7, "y": 0.5}, "map[x:7 y:0.5]"},
	}
	for i, c := range cases {
		v, err := vars.CreateVariable(fmt.Sprintf("V%d", i), variable.DataType(c.dataType), c.value)
		if err != nil {
			t.Errorf("%s: 変数を作れない: %v", c.dataType, err)
			continue
		}
		if stored := fmt.Sprint(v.Value); stored != c.want {
			t.Errorf("%s: ホストの値 = %s, want %s（テストの前提が違う）", c.dataType, stored, c.want)
			continue
		}
		got, err := acc.ReadVariableValue(v.ID)
		if err != nil {
			t.Errorf("%s: ReadVariableValue: %v", c.dataType, err)
			continue
		}
		if s := fmt.Sprint(got); s != c.want {
			t.Errorf("%s: 読んだ値 = %s (%T), want %s", c.dataType, s, got, c.want)
		}
	}
}

// writeCase はプラグインからホストの変数へ書く値（OPC UA プラグインが渡すのと同じ Go の型）と、書いたあとの値
type writeCase struct {
	dataType string
	write    interface{}
	want     string
}

// checkWrites は、プラグインから書いた値がホストの変数に入ることを確かめる
func checkWrites(t *testing.T, vars *variable.VariableStore, acc *pluginruntime.RemoteVariableStoreAccessor, cases []writeCase) {
	t.Helper()
	for i, c := range cases {
		v, err := vars.CreateVariable(fmt.Sprintf("W%d_%s", i, t.Name()), variable.DataType(c.dataType), nil)
		if err != nil {
			t.Errorf("%s: 変数を作れない: %v", c.dataType, err)
			continue
		}
		if err := acc.WriteVariableValue(v.ID, c.write); err != nil {
			t.Errorf("%s: %v (%T) の WriteVariableValue: %v", c.dataType, c.write, c.write, err)
			continue
		}
		stored, _ := vars.GetVariable(v.ID)
		if got := fmt.Sprint(stored.Value); got != c.want {
			t.Errorf("%s: 書いたあとのホストの値 = %s, want %s", c.dataType, got, c.want)
		}
	}
}

func TestHost_WriteValues(t *testing.T) {
	vars, acc := startHost(t)
	checkWrites(t, vars, acc, []writeCase{
		{"BOOL", true, "true"},
		{"SINT", int8(-5), "-5"},
		{"INT", int16(-123), "-123"},
		{"INT", int16(30000), "30000"},
		{"DINT", int32(100000), "100000"},
		{"LINT", int64(-9000000000000000000), "-9000000000000000000"},
		{"USINT", uint8(5), "5"},
		{"ULINT", uint64(18000000000000000000), "18000000000000000000"},
		{"REAL", float32(1.5), "1.5"},
		{"LREAL", 2.25, "2.25"},
		{"ARRAY[0..2] OF INT", []interface{}{int16(1), int16(2), int16(3)}, "[1 2 3]"},
		{"ARRAY[0..1, 0..1] OF BOOL", []interface{}{[]interface{}{true, false}, []interface{}{false, true}}, "[[true false] [false true]]"},
	})
}

func TestHost_WriteFieldAndStructFields(t *testing.T) {
	vars, acc := startHost(t)
	registerPoint(t, vars)
	p, _ := vars.CreateVariable("P", "Point", nil)
	arr, _ := vars.CreateVariable("Arr", "ARRAY[1..3] OF INT", nil)

	// 構造体のフィールド
	if err := acc.WriteVariableFieldValue(p.ID, "x", int16(42)); err != nil {
		t.Fatalf("WriteVariableFieldValue(x): %v", err)
	}
	if got, _ := vars.GetVariable(p.ID); fmt.Sprint(got.Value) != "map[x:42 y:0]" {
		t.Errorf("フィールド書き込み後 = %v", got.Value)
	}
	// 配列の要素（添字は外部インデックス。ARRAY[1..3] の [3] は最後の要素）
	if err := acc.WriteVariableFieldValue(arr.ID, "[3]", int16(9)); err != nil {
		t.Fatalf("WriteVariableFieldValue([3]): %v", err)
	}
	if got, _ := vars.GetVariable(arr.ID); fmt.Sprint(got.Value) != "[0 0 9]" {
		t.Errorf("要素書き込み後 = %v", got.Value)
	}

	// 構造体型のフィールド一覧
	want := []protocol.StructFieldInfo{{Name: "x", DataType: "INT"}, {Name: "y", DataType: "REAL"}}
	if got := acc.GetStructFields("Point"); !reflect.DeepEqual(got, want) {
		t.Errorf("GetStructFields = %+v, want %+v", got, want)
	}
	if got := acc.GetStructFields("Nope"); len(got) != 0 {
		t.Errorf("存在しない型の GetStructFields = %+v, want 空", got)
	}
}

func TestHost_Errors(t *testing.T) {
	vars, acc := startHost(t)
	v, _ := vars.CreateVariable("A", "INT", 1)

	if _, err := acc.ReadVariableValue("no-such-id"); err == nil {
		t.Error("存在しない変数の ReadVariableValue がエラーにならなかった")
	}
	if err := acc.WriteVariableValue("no-such-id", int16(1)); err == nil {
		t.Error("存在しない変数の WriteVariableValue がエラーにならなかった")
	}
	if err := acc.WriteVariableValue(v.ID, "数値ではない"); err == nil {
		t.Error("型の合わない WriteVariableValue がエラーにならなかった")
	}
	if err := acc.WriteVariableFieldValue(v.ID, "nope", int16(1)); err == nil {
		t.Error("存在しないフィールドの WriteVariableFieldValue がエラーにならなかった")
	}
	// エラーになった書き込みで値は変わらない
	if got, _ := vars.GetVariable(v.ID); fmt.Sprint(got.Value) != "1" {
		t.Errorf("エラーのあとの値 = %v, want 1", got.Value)
	}
}

func TestHost_SubscribeVariableChanges(t *testing.T) {
	vars, acc := startHost(t)
	a, _ := vars.CreateVariable("A", "INT", 1)
	b, _ := vars.CreateVariable("B", "ARRAY[0..1] OF INT", nil)

	var mu sync.Mutex
	changed := map[string]int{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- acc.SubscribeVariableChanges(ctx, func(id string) {
			mu.Lock()
			changed[id]++
			mu.Unlock()
		})
	}()
	count := func(id string) int {
		mu.Lock()
		defer mu.Unlock()
		return changed[id]
	}

	// 変数全体の変更も、要素だけの変更も、変数 ID で通知される（購読の開始を待つため、届くまで繰り返す）
	eventually(t, "変数の変更がプラグインに届く", func() bool {
		_ = vars.UpdateValue(a.ID, 2)
		return count(a.ID) > 0
	})
	eventually(t, "要素の変更がプラグインに届く", func() bool {
		_ = vars.UpdateFieldValue(b.ID, "[1]", 5)
		return count(b.ID) > 0
	})

	// 購読をやめると正常に終わる
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("購読の終了がエラーになった: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("購読が終わらない")
	}
}

// ===== プラグインの検出（plugin.json） =====

func writeManifest(t *testing.T, pluginsDir, name, content string) {
	t.Helper()
	dir := filepath.Join(pluginsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if content == "" {
		return // plugin.json のないフォルダ
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverManifests(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "good-plugin", `{
		"name": "Good Plugin", "entrypoint": "good.exe", "version": "1.2.3",
		"protocol_type": "good", "display_name": "Good",
		"variants": [{"id": "tcp", "display_name": "TCP"}],
		"capabilities": {"supports_unit_id": true, "unit_id_min": 1, "unit_id_max": 247, "supports_node_publishing": true}
	}`)
	writeManifest(t, dir, "minimal-plugin", `{"entrypoint": "min.exe", "protocol_type": "minimal", "display_name": "Minimal", "variants": []}`)
	writeManifest(t, dir, "no-manifest", "")
	writeManifest(t, dir, "broken-json", `{"entrypoint": `)
	writeManifest(t, dir, "no-entrypoint", `{"protocol_type": "x", "display_name": "X"}`)
	writeManifest(t, dir, "no-protocol-type", `{"entrypoint": "old.exe", "display_name": "Old"}`)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"entrypoint":"root.exe","protocol_type":"root"}`), 0o644); err != nil {
		t.Fatal(err) // プラグインのフォルダの外にあるファイルは見ない
	}

	entries, err := NewPluginProcessManager("").DiscoverManifests(dir)
	if err != nil {
		t.Fatalf("DiscoverManifests: %v", err)
	}
	var found []string
	byType := map[string]*PluginManifestEntry{}
	for _, e := range entries {
		found = append(found, e.Manifest.ProtocolType)
		byType[e.Manifest.ProtocolType] = e
	}
	sort.Strings(found)
	if want := []string{"good", "minimal"}; !reflect.DeepEqual(found, want) {
		t.Fatalf("見つかったプラグイン = %v, want %v", found, want)
	}

	good := byType["good"]
	if good.Dir != filepath.Join(dir, "good-plugin") {
		t.Errorf("Dir = %q", good.Dir)
	}
	want := PluginManifest{
		Name: "Good Plugin", Entrypoint: "good.exe", Version: "1.2.3",
		ProtocolType: "good", DisplayName: "Good",
		Variants:     []ManifestVariant{{ID: "tcp", DisplayName: "TCP"}},
		Capabilities: ManifestCapabilities{SupportsUnitID: true, UnitIDMin: 1, UnitIDMax: 247, SupportsNodePublishing: true},
	}
	if !reflect.DeepEqual(*good.Manifest, want) {
		t.Errorf("マニフェスト = %+v, want %+v", *good.Manifest, want)
	}
	// capabilities を省略すると、どの機能も持たない
	if got := byType["minimal"].Manifest.Capabilities; got != (ManifestCapabilities{}) {
		t.Errorf("省略した capabilities = %+v", got)
	}

	// フォルダがなければエラー
	if _, err := NewPluginProcessManager("").DiscoverManifests(filepath.Join(dir, "nope")); err == nil {
		t.Error("存在しないフォルダがエラーにならなかった")
	}
}

// プラグイン側が書き出すマニフェスト（pluginruntime.NewManifest）を、ホスト側がそのまま読める
func TestDiscoverManifests_ReadsPluginRuntimeManifest(t *testing.T) {
	dir := t.TempDir()
	pluginSide := &testFactory{Factory: protocoltest.NewFactory(testProtocol, "v1", "Fake")}
	data, err := json.MarshalIndent(pluginruntime.NewManifest(pluginSide, "fake-plugin.exe"), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeManifest(t, dir, "fake-plugin", string(data))

	entries, err := NewPluginProcessManager("").DiscoverManifests(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("DiscoverManifests = %d 件, %v", len(entries), err)
	}
	if got := entries[0].Manifest.Entrypoint; got != "fake-plugin.exe" {
		t.Errorf("Entrypoint = %q", got)
	}
	f := NewLazyRemoteServerFactory(entries[0], NewPluginProcessManager(""))
	if f.ProtocolType() != pluginSide.ProtocolType() || f.DisplayName() != pluginSide.DisplayName() {
		t.Errorf("ProtocolType / DisplayName = %q / %q", f.ProtocolType(), f.DisplayName())
	}
	if got := f.GetProtocolCapabilities(); got != pluginSide.GetProtocolCapabilities() {
		t.Errorf("capabilities = %+v, want %+v", got, pluginSide.GetProtocolCapabilities())
	}
}

// ===== プラグインの起動時の出力（GRPC_PORT=N）の読み取り =====

func TestReadGrpcPort(t *testing.T) {
	read := func(output string) (int, error) {
		return readGrpcPort(strings.NewReader(output), time.Second)
	}

	if port, err := read("GRPC_PORT=50123\n"); err != nil || port != 50123 {
		t.Errorf("ポートだけの出力: %d, %v", port, err)
	}
	// ポートの行より前にログが出ていてもよい。行末の空白や CR は無視する
	if port, err := read("starting...\nloading config\nGRPC_PORT=50124 \r\nready\n"); err != nil || port != 50124 {
		t.Errorf("前後にログのある出力: %d, %v", port, err)
	}
	if _, err := read("GRPC_PORT=abc\n"); err == nil {
		t.Error("数値でないポートがエラーにならなかった")
	}
	// ポートを出さずに終了した
	if _, err := read("starting...\nfatal error\n"); err == nil {
		t.Error("ポートのない出力がエラーにならなかった")
	}
}

func TestReadGrpcPort_Timeout(t *testing.T) {
	// 何も出力しないまま動き続けるプラグイン
	r, w := io.Pipe()
	defer w.Close()
	start := time.Now()
	if _, err := readGrpcPort(r, 50*time.Millisecond); err == nil {
		t.Error("タイムアウトがエラーにならなかった")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("タイムアウトまでが長すぎる: %v", elapsed)
	}
}
