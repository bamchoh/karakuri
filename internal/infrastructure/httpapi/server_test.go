package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"karakuri/internal/application"
	"karakuri/internal/domain/protocol/protocoltest"
	"karakuri/internal/infrastructure/appconfig"
)

// TestMain は設定フォルダを一時フォルダに差し替える（開発 PC の実際の設定を書き換えないため）
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "karakuri-httpapi-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv(appconfig.EnvDir, dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// testAPI はフェイクのプロトコル（fake-a / fake-b）を登録した PLCService と、その REST API
type testAPI struct {
	t       *testing.T
	svc     *application.PLCService
	handler http.Handler
}

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	svc := application.NewPLCService()
	svc.RegisterPluginFactory(protocoltest.NewFactory("fake-a", "va", "Fake A"))
	svc.RegisterPluginFactory(protocoltest.NewFactory("fake-b", "vb", "Fake B"))
	svc.ClearMonitoringItems()
	t.Cleanup(func() {
		for _, inst := range svc.GetServerInstances() {
			_ = svc.StopServer(inst.ProtocolType)
		}
	})
	return &testAPI{t: t, svc: svc, handler: NewServer(svc, 0).server.Handler}
}

// withServer は fake-a のサーバーを追加した API を返す
func newTestAPIWithServer(t *testing.T) *testAPI {
	api := newTestAPI(t)
	api.do("POST", "/api/servers", `{"protocolType":"fake-a","variantId":"va"}`).status(http.StatusCreated)
	return api
}

type response struct {
	t    *testing.T
	what string
	rec  *httptest.ResponseRecorder
}

// do はリクエストを送る。body が空文字列ならボディなし
func (a *testAPI) do(method, path, body string) *response {
	a.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return &response{t: a.t, what: method + " " + path, rec: rec}
}

// status はステータスコードを確かめる
func (r *response) status(want int) *response {
	r.t.Helper()
	if r.rec.Code != want {
		r.t.Fatalf("%s: status = %d, want %d (body: %s)", r.what, r.rec.Code, want, r.rec.Body.String())
	}
	return r
}

// json はレスポンスの JSON を v に読み込む
func (r *response) json(v interface{}) {
	r.t.Helper()
	if ct := r.rec.Header().Get("Content-Type"); ct != "application/json" {
		r.t.Errorf("%s: Content-Type = %q, want application/json", r.what, ct)
	}
	if err := json.Unmarshal(r.rec.Body.Bytes(), v); err != nil {
		r.t.Fatalf("%s: JSON を読めない: %v (body: %s)", r.what, err, r.rec.Body.String())
	}
}

// isError はエラー応答（{"error": "..."}）であることを確かめる
func (r *response) isError(wantStatus int) {
	r.t.Helper()
	r.status(wantStatus)
	var body map[string]string
	r.json(&body)
	if body["error"] == "" {
		r.t.Errorf("%s: error のメッセージがない (body: %s)", r.what, r.rec.Body.String())
	}
}

// ===== サーバー管理 =====

func TestServers_Lifecycle(t *testing.T) {
	api := newTestAPI(t)

	var servers []application.ServerInstanceDTO
	api.do("GET", "/api/servers", "").status(http.StatusOK).json(&servers)
	if len(servers) != 0 {
		t.Fatalf("最初はサーバーなしのはず: %+v", servers)
	}

	api.do("POST", "/api/servers", `{"protocolType":"fake-a","variantId":"va"}`).status(http.StatusCreated)
	api.do("GET", "/api/servers", "").status(http.StatusOK).json(&servers)
	if len(servers) != 1 || servers[0].ProtocolType != "fake-a" || servers[0].DisplayName != "Fake A" || servers[0].Variant != "va" {
		t.Fatalf("追加後の一覧が違う: %+v", servers)
	}

	status := func() string {
		var body map[string]string
		api.do("GET", "/api/servers/fake-a/status", "").status(http.StatusOK).json(&body)
		return body["status"]
	}
	stopped := status()
	api.do("POST", "/api/servers/fake-a/start", "").status(http.StatusNoContent)
	running := status()
	if running == stopped {
		t.Errorf("起動してもステータスが変わらない: %q", running)
	}
	api.do("POST", "/api/servers/fake-a/stop", "").status(http.StatusNoContent)
	if got := status(); got != stopped {
		t.Errorf("停止後のステータス = %q, want %q", got, stopped)
	}

	api.do("DELETE", "/api/servers/fake-a", "").status(http.StatusNoContent)
	api.do("GET", "/api/servers", "").status(http.StatusOK).json(&servers)
	if len(servers) != 0 {
		t.Errorf("削除後もサーバーが残っている: %+v", servers)
	}
}

func TestServers_Errors(t *testing.T) {
	api := newTestAPIWithServer(t)

	api.do("POST", "/api/servers", `{"protocolType":`).isError(http.StatusBadRequest)                           // 不正な JSON
	api.do("POST", "/api/servers", `{"protocolType":"nope","variantId":"x"}`).isError(http.StatusBadRequest)    // 知らないプロトコル
	api.do("POST", "/api/servers", `{"protocolType":"fake-a","variantId":"va"}`).isError(http.StatusBadRequest) // 追加済み
	api.do("DELETE", "/api/servers/fake-b", "").isError(http.StatusBadRequest)                                  // 追加していない
	api.do("POST", "/api/servers/fake-b/start", "").isError(http.StatusBadRequest)
	api.do("POST", "/api/servers/fake-b/stop", "").isError(http.StatusBadRequest)
	api.do("GET", "/api/servers/fake-b/config", "").isError(http.StatusNotFound)
	api.do("PUT", "/api/servers/fake-b/config", `{"variant":"vb","settings":{}}`).isError(http.StatusBadRequest)
}

func TestServerConfig_GetAndUpdate(t *testing.T) {
	api := newTestAPIWithServer(t)

	var cfg application.ServerConfigDTO
	api.do("GET", "/api/servers/fake-a/config", "").status(http.StatusOK).json(&cfg)
	if cfg.ProtocolType != "fake-a" || cfg.Variant != "va" || cfg.Settings["port"] != float64(protocoltest.DefaultPort) {
		t.Fatalf("既定の設定が違う: %+v", cfg)
	}

	// 対象はパスで決まる（ボディの protocolType は使わない）
	api.do("PUT", "/api/servers/fake-a/config", `{"protocolType":"fake-b","variant":"va","settings":{"port":1502}}`).status(http.StatusNoContent)
	api.do("GET", "/api/servers/fake-a/config", "").status(http.StatusOK).json(&cfg)
	if cfg.Settings["port"] != float64(1502) {
		t.Errorf("更新後の port = %v, want 1502", cfg.Settings["port"])
	}

	// 検証に通らない設定・不正な JSON は拒否し、設定は変わらない
	api.do("PUT", "/api/servers/fake-a/config", `{"variant":"va","settings":{"port":0}}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/servers/fake-a/config", `not json`).isError(http.StatusBadRequest)
	api.do("GET", "/api/servers/fake-a/config", "").status(http.StatusOK).json(&cfg)
	if cfg.Settings["port"] != float64(1502) {
		t.Errorf("拒否された更新のあと port = %v, want 1502", cfg.Settings["port"])
	}

	// 起動中は変更できない
	api.do("POST", "/api/servers/fake-a/start", "").status(http.StatusNoContent)
	api.do("PUT", "/api/servers/fake-a/config", `{"variant":"va","settings":{"port":1503}}`).isError(http.StatusBadRequest)
}

// ===== メモリ操作 =====

func TestMemory_Areas(t *testing.T) {
	api := newTestAPIWithServer(t)

	var areas []application.MemoryAreaDTO
	api.do("GET", "/api/memory/fake-a/areas", "").status(http.StatusOK).json(&areas)
	if len(areas) != len(protocoltest.Areas) {
		t.Fatalf("エリア数 = %d, want %d", len(areas), len(protocoltest.Areas))
	}
	if a := areas[2]; a.ID != "holdingRegisters" || a.IsBit || !a.OneOrigin || a.ShortName != "4" {
		t.Errorf("holdingRegisters の属性が違う: %+v", a)
	}
}

func TestMemory_Words(t *testing.T) {
	api := newTestAPIWithServer(t)
	read := func(query string) []int {
		t.Helper()
		var body struct{ Values []int }
		api.do("GET", "/api/memory/fake-a/holdingRegisters/words"+query, "").status(http.StatusOK).json(&body)
		return body.Values
	}

	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words/10", `{"value":1234}`).status(http.StatusNoContent)
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words/12", `{"value":65535}`).status(http.StatusNoContent)

	if got := read("?address=10&count=3"); !reflect.DeepEqual(got, []int{1234, 0, 65535}) {
		t.Errorf("読み取り = %v, want [1234 0 65535]", got)
	}
	// count を省略すると 1 件、address を省略すると 0 番地
	if got := read("?address=10"); !reflect.DeepEqual(got, []int{1234}) {
		t.Errorf("count 省略 = %v, want [1234]", got)
	}
	if got := read(""); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("address 省略 = %v, want [0]", got)
	}

	// エラー
	api.do("GET", "/api/memory/fake-b/holdingRegisters/words?address=0", "").isError(http.StatusBadRequest)    // 追加していないサーバー
	api.do("GET", "/api/memory/fake-a/nope/words?address=0", "").isError(http.StatusBadRequest)                // 存在しないエリア
	api.do("GET", "/api/memory/fake-a/holdingRegisters/words?address=9999", "").isError(http.StatusBadRequest) // 範囲外
	api.do("GET", "/api/memory/fake-a/holdingRegisters/words?address=9990&count=20", "").isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words/abc", `{"value":1}`).isError(http.StatusBadRequest) // 数値でないアドレス
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words/9999", `{"value":1}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words/10", `{"value":`).isError(http.StatusBadRequest) // 不正な JSON
	api.do("PUT", "/api/memory/fake-b/holdingRegisters/words/10", `{"value":1}`).isError(http.StatusBadRequest)

	// エラーになった書き込みで値は変わらない
	if got := read("?address=10"); !reflect.DeepEqual(got, []int{1234}) {
		t.Errorf("エラーのあと = %v, want [1234]", got)
	}
}

func TestMemory_Bits(t *testing.T) {
	api := newTestAPIWithServer(t)
	read := func(query string) []bool {
		t.Helper()
		var body struct{ Values []bool }
		api.do("GET", "/api/memory/fake-a/coils/bits"+query, "").status(http.StatusOK).json(&body)
		return body.Values
	}

	api.do("PUT", "/api/memory/fake-a/coils/bits/5", `{"value":true}`).status(http.StatusNoContent)
	if got := read("?address=4&count=3"); !reflect.DeepEqual(got, []bool{false, true, false}) {
		t.Errorf("読み取り = %v, want [false true false]", got)
	}
	api.do("PUT", "/api/memory/fake-a/coils/bits/5", `{"value":false}`).status(http.StatusNoContent)
	if got := read("?address=5"); !reflect.DeepEqual(got, []bool{false}) {
		t.Errorf("OFF に戻したあと = %v, want [false]", got)
	}

	api.do("GET", "/api/memory/fake-b/coils/bits?address=0", "").isError(http.StatusBadRequest)
	api.do("GET", "/api/memory/fake-a/coils/bits?address=9999", "").isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/coils/bits/abc", `{"value":true}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/coils/bits/5", `{"value":"yes"}`).isError(http.StatusBadRequest) // 型が違う
	api.do("PUT", "/api/memory/fake-b/coils/bits/5", `{"value":true}`).isError(http.StatusBadRequest)
}

// ===== 変数管理 =====

func TestVariables_CRUD(t *testing.T) {
	api := newTestAPI(t)

	var created application.VariableDTO
	api.do("POST", "/api/variables", `{"name":"Speed","dataType":"INT","value":12}`).status(http.StatusCreated).json(&created)
	if created.ID == "" || created.Name != "Speed" || created.DataType != "INT" || created.Value != float64(12) {
		t.Fatalf("作成した変数が違う: %+v", created)
	}

	list := func() []application.VariableDTO {
		t.Helper()
		var vars []application.VariableDTO
		api.do("GET", "/api/variables", "").status(http.StatusOK).json(&vars)
		return vars
	}
	if vars := list(); len(vars) != 1 || vars[0].ID != created.ID {
		t.Fatalf("一覧が違う: %+v", vars)
	}

	api.do("PUT", "/api/variables/"+created.ID+"/value", `{"value":345}`).status(http.StatusNoContent)
	if vars := list(); vars[0].Value != float64(345) {
		t.Errorf("更新後の値 = %v, want 345", vars[0].Value)
	}

	api.do("DELETE", "/api/variables/"+created.ID, "").status(http.StatusNoContent)
	if vars := list(); len(vars) != 0 {
		t.Errorf("削除後も変数が残っている: %+v", vars)
	}
}

func TestVariables_Errors(t *testing.T) {
	api := newTestAPI(t)
	api.do("POST", "/api/variables", `{"name":"A","dataType":"INT","value":0}`).status(http.StatusCreated)

	api.do("POST", "/api/variables", `{"name":`).isError(http.StatusBadRequest)                                 // 不正な JSON
	api.do("POST", "/api/variables", `{"name":"A","dataType":"INT","value":0}`).isError(http.StatusBadRequest)  // 名前の重複
	api.do("POST", "/api/variables", `{"name":"B","dataType":"NOPE","value":0}`).isError(http.StatusBadRequest) // 知らない型
	api.do("PUT", "/api/variables/no-such-id/value", `{"value":1}`).isError(http.StatusBadRequest)              // 存在しない変数
	api.do("PUT", "/api/variables/no-such-id/value", `{"value":`).isError(http.StatusBadRequest)                // 不正な JSON
	api.do("DELETE", "/api/variables/no-such-id", "").isError(http.StatusBadRequest)
}

// ===== プロジェクト =====

func TestProject_ExportImportRoundTrip(t *testing.T) {
	src := newTestAPIWithServer(t)
	src.do("PUT", "/api/servers/fake-a/config", `{"variant":"va","settings":{"port":1502}}`).status(http.StatusNoContent)
	src.do("POST", "/api/variables", `{"name":"Speed","dataType":"INT","value":12}`).status(http.StatusCreated)
	src.do("POST", "/api/variables", `{"name":"Flags","dataType":"ARRAY[0..2] OF BOOL","value":[true,false,true]}`).status(http.StatusCreated)

	exported := src.do("GET", "/api/project/export", "").status(http.StatusOK).rec.Body.String()

	// 別のサービス（サーバーが違い、変数も違う）に読み込むと、エクスポートした内容に置き換わる
	dst := newTestAPI(t)
	dst.do("POST", "/api/servers", `{"protocolType":"fake-b","variantId":"vb"}`).status(http.StatusCreated)
	dst.do("POST", "/api/variables", `{"name":"Old","dataType":"INT","value":1}`).status(http.StatusCreated)
	dst.do("POST", "/api/project/import", exported).status(http.StatusNoContent)

	var servers []application.ServerInstanceDTO
	dst.do("GET", "/api/servers", "").status(http.StatusOK).json(&servers)
	if len(servers) != 1 || servers[0].ProtocolType != "fake-a" {
		t.Errorf("読み込み後のサーバーが違う: %+v", servers)
	}
	var cfg application.ServerConfigDTO
	dst.do("GET", "/api/servers/fake-a/config", "").status(http.StatusOK).json(&cfg)
	if cfg.Settings["port"] != float64(1502) {
		t.Errorf("読み込み後の port = %v, want 1502", cfg.Settings["port"])
	}

	var vars []application.VariableDTO
	dst.do("GET", "/api/variables", "").status(http.StatusOK).json(&vars)
	got := map[string]string{}
	for _, v := range vars {
		got[v.Name] = fmt.Sprintf("%s=%v", v.DataType, v.Value)
	}
	want := map[string]string{"Speed": "INT=12", "Flags": "ARRAY[0..2] OF BOOL=[true false true]"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("読み込み後の変数 = %v, want %v", got, want)
	}

	dst.do("POST", "/api/project/import", `{"servers":`).isError(http.StatusBadRequest) // 不正な JSON
}

// ===== CORS・ルーティング =====

func TestCORS(t *testing.T) {
	api := newTestAPI(t)

	// プリフライトは 204
	pre := api.do("OPTIONS", "/api/variables", "")
	pre.status(http.StatusNoContent)
	if got := pre.rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("プリフライトの Allow-Origin = %q, want *", got)
	}
	for _, m := range []string{"GET", "POST", "PUT", "DELETE"} {
		if !strings.Contains(pre.rec.Header().Get("Access-Control-Allow-Methods"), m) {
			t.Errorf("Allow-Methods に %s がない: %q", m, pre.rec.Header().Get("Access-Control-Allow-Methods"))
		}
	}

	// 通常の応答・エラー応答にも付く
	if got := api.do("GET", "/api/servers", "").rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("GET の Allow-Origin = %q, want *", got)
	}
	if got := api.do("DELETE", "/api/variables/nope", "").rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("エラー応答の Allow-Origin = %q, want *", got)
	}
}

func TestRouting_UnknownPathAndMethod(t *testing.T) {
	api := newTestAPI(t)
	api.do("GET", "/api/nope", "").status(http.StatusNotFound)
	api.do("PATCH", "/api/servers", "").status(http.StatusMethodNotAllowed)
}

// ===== 実際のポートでの起動・再起動 =====

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestServer_StartRestartShutdown(t *testing.T) {
	svc := application.NewPLCService()
	get := func(port int) error {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/servers", port))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body) //nolint:errcheck
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status %d: %s", resp.StatusCode, buf.String())
		}
		return nil
	}

	p1, p2 := freePort(t), freePort(t)
	srv := NewServer(svc, p1)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := get(p1); err != nil {
		t.Fatalf("起動後に応答しない: %v", err)
	}

	// 使用中のポートでは起動できない
	if err := NewServer(svc, p1).Start(); err == nil {
		t.Error("使用中のポートで Start がエラーにならなかった")
	}

	// 再起動すると新しいポートで応答し、古いポートは閉じる
	if err := srv.Restart(p2); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if err := get(p2); err != nil {
		t.Errorf("再起動後に新しいポートで応答しない: %v", err)
	}
	if err := get(p1); err == nil {
		t.Error("再起動後も古いポートが応答している")
	}

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := get(p2); err == nil {
		t.Error("停止後も応答している")
	}
}
