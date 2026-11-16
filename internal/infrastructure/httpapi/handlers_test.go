package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"karakuri/internal/application"
	"karakuri/internal/domain/protocol/protocoltest"
)

// ===== OpenAPI =====

// openapi.json のパスとメソッドが、routes() と一致すること（足し忘れを見つける）
func TestOpenAPI_MatchesRoutes(t *testing.T) {
	api := newTestAPI(t)
	var spec struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	api.do("GET", "/api/openapi.json", "").status(http.StatusOK).json(&spec)
	if spec.OpenAPI == "" {
		t.Fatal("openapi のバージョンがない")
	}

	var inSpec []string
	for path, ops := range spec.Paths {
		for method := range ops {
			if method == "parameters" {
				continue
			}
			inSpec = append(inSpec, strings.ToUpper(method)+" "+path)
		}
	}
	var inRoutes []string
	for _, rt := range NewServer(api.svc, 0).routes() {
		inRoutes = append(inRoutes, rt.method+" "+rt.path)
	}
	sort.Strings(inSpec)
	sort.Strings(inRoutes)
	if !reflect.DeepEqual(inSpec, inRoutes) {
		t.Errorf("openapi.json と routes() が違う\nopenapi: %v\nroutes:  %v", inSpec, inRoutes)
	}
}

// ===== メモリ =====

func TestMemory_BulkWrite(t *testing.T) {
	api := newTestAPIWithServer(t)
	readWords := func(query string) []int {
		t.Helper()
		var body struct{ Values []int }
		api.do("GET", "/api/memory/fake-a/holdingRegisters/words"+query, "").status(http.StatusOK).json(&body)
		return body.Values
	}

	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words", `{"address":5,"values":[1,2,-1]}`).status(http.StatusNoContent)
	if got := readWords("?address=5&count=3"); !reflect.DeepEqual(got, []int{1, 2, 65535}) {
		t.Errorf("まとめて書いたワード = %v, want [1 2 65535]（-1 は 65535）", got)
	}

	// 終わりを超えるなら 1 つも書かない
	var areas []application.MemoryAreaDTO
	api.do("GET", "/api/memory/fake-a/areas", "").status(http.StatusOK).json(&areas)
	last := int(areas[2].Size) - 1 // holdingRegisters の最後のアドレス
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words", fmt.Sprintf(`{"address":%d,"values":[7,7,7]}`, last-1)).isError(http.StatusBadRequest)
	if got := readWords(fmt.Sprintf("?address=%d&count=2", last-1)); !reflect.DeepEqual(got, []int{0, 0}) {
		t.Errorf("範囲外の書き込みで値が変わった: %v", got)
	}
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words", `{"address":0,"values":[65536]}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words", `{"address":0,"values":[]}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words", `{"values":[1]}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words/0", `{"value":-32769}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/holdingRegisters/words/0", `{}`).isError(http.StatusBadRequest)
	api.do("PUT", "/api/memory/fake-a/nope/words", `{"address":0,"values":[1]}`).isError(http.StatusBadRequest)

	api.do("PUT", "/api/memory/fake-a/coils/bits", `{"address":3,"values":[true,false,true]}`).status(http.StatusNoContent)
	var bits struct{ Values []bool }
	api.do("GET", "/api/memory/fake-a/coils/bits?address=3&count=3", "").status(http.StatusOK).json(&bits)
	if !reflect.DeepEqual(bits.Values, []bool{true, false, true}) {
		t.Errorf("まとめて書いたビット = %v", bits.Values)
	}
	api.do("PUT", "/api/memory/fake-a/coils/bits", fmt.Sprintf(`{"address":%d,"values":[true,true]}`, areas[0].Size-1)).isError(http.StatusBadRequest)
}

func TestMemory_QueryValidation(t *testing.T) {
	api := newTestAPIWithServer(t)
	for _, q := range []string{"?address=abc", "?count=x", "?address=-1", "?count=0", "?count=65536"} {
		api.do("GET", "/api/memory/fake-a/holdingRegisters/words"+q, "").isError(http.StatusBadRequest)
		api.do("GET", "/api/memory/fake-a/coils/bits"+q, "").isError(http.StatusBadRequest)
	}
}

// ===== 変数 =====

func TestVariables_ByNameAndMetadata(t *testing.T) {
	api := newTestAPI(t)
	var created application.VariableDTO
	api.do("POST", "/api/variables", `{"name":"Speed","dataType":"INT","value":12}`).status(http.StatusCreated).json(&created)

	// ID でも名前でも引ける
	for _, ref := range []string{created.ID, "Speed"} {
		var v application.VariableDTO
		api.do("GET", "/api/variables/"+ref, "").status(http.StatusOK).json(&v)
		if v.ID != created.ID {
			t.Errorf("%s で引いた変数が違う: %+v", ref, v)
		}
	}
	api.do("GET", "/api/variables/Nope", "").isError(http.StatusNotFound)

	// 名前だけ変える（型はそのまま）
	var renamed application.VariableDTO
	api.do("PUT", "/api/variables/Speed", `{"name":"Speed2"}`).status(http.StatusOK).json(&renamed)
	if renamed.Name != "Speed2" || renamed.DataType != "INT" || renamed.Value != float64(12) {
		t.Errorf("名前を変えたあと: %+v", renamed)
	}
	// 型を変えると値は既定値
	var retyped application.VariableDTO
	api.do("PUT", "/api/variables/Speed2", `{"dataType":"DINT"}`).status(http.StatusOK).json(&retyped)
	if retyped.Name != "Speed2" || retyped.DataType != "DINT" || retyped.Value != float64(0) {
		t.Errorf("型を変えたあと: %+v", retyped)
	}
	api.do("PUT", "/api/variables/Speed2", `{"dataType":"NOPE"}`).isError(http.StatusBadRequest)

	api.do("DELETE", "/api/variables/Speed2", "").status(http.StatusNoContent)
	api.do("GET", "/api/variables/Speed2", "").isError(http.StatusNotFound)
}

func TestVariables_ValueWithPath(t *testing.T) {
	api := newTestAPI(t)
	api.do("POST", "/api/struct-types", `{"name":"Motor","fields":[{"name":"speed","dataType":"INT"},{"name":"on","dataType":"BOOL"}]}`).status(http.StatusCreated)
	api.do("POST", "/api/variables", `{"name":"M1","dataType":"Motor"}`).status(http.StatusCreated)
	api.do("POST", "/api/variables", `{"name":"Values","dataType":"ARRAY[1..3] OF INT"}`).status(http.StatusCreated)

	readValue := func(path string) interface{} {
		t.Helper()
		var body struct{ Value interface{} }
		api.do("GET", path, "").status(http.StatusOK).json(&body)
		return body.Value
	}

	api.do("PUT", "/api/variables/M1/value?path=speed", `{"value":100}`).status(http.StatusNoContent)
	if got := readValue("/api/variables/M1/value?path=speed"); got != float64(100) {
		t.Errorf("M1.speed = %v, want 100", got)
	}
	api.do("PUT", "/api/variables/Values/value?path=%5B3%5D", `{"value":7}`).status(http.StatusNoContent) // [3]
	if got := readValue("/api/variables/Values/value"); !reflect.DeepEqual(got, []interface{}{float64(0), float64(0), float64(7)}) {
		t.Errorf("Values = %v, want [0 0 7]", got)
	}
	api.do("GET", "/api/variables/M1/value?path=nope", "").isError(http.StatusBadRequest)
	api.do("PUT", "/api/variables/M1/value?path=nope", `{"value":1}`).isError(http.StatusBadRequest)
}

// LINT / ULINT は、JSON の数値でも文字列でも桁を落とさずに書け、文字列で読める
func TestVariables_64bitPrecision(t *testing.T) {
	api := newTestAPI(t)
	api.do("POST", "/api/variables", `{"name":"L","dataType":"LINT","value":9007199254740993}`).status(http.StatusCreated)
	api.do("POST", "/api/variables", `{"name":"U","dataType":"ULINT"}`).status(http.StatusCreated)

	var body struct{ Value interface{} }
	api.do("GET", "/api/variables/L/value", "").status(http.StatusOK).json(&body)
	if body.Value != "9007199254740993" {
		t.Errorf("LINT = %#v, want \"9007199254740993\"", body.Value)
	}
	api.do("PUT", "/api/variables/U/value", `{"value":18446744073709551615}`).status(http.StatusNoContent)
	api.do("GET", "/api/variables/U/value", "").status(http.StatusOK).json(&body)
	if body.Value != "18446744073709551615" {
		t.Errorf("ULINT = %#v", body.Value)
	}
	api.do("PUT", "/api/variables/L/value", `{"value":"-9223372036854775808"}`).status(http.StatusNoContent)
	api.do("GET", "/api/variables/L/value", "").status(http.StatusOK).json(&body)
	if body.Value != "-9223372036854775808" {
		t.Errorf("LINT（文字列で書いた） = %#v", body.Value)
	}
}

func TestValues_ReadWriteMany(t *testing.T) {
	api := newTestAPI(t)
	api.do("POST", "/api/struct-types", `{"name":"Motor","fields":[{"name":"speed","dataType":"INT"}]}`).status(http.StatusCreated)
	api.do("POST", "/api/variables", `{"name":"Counter","dataType":"INT"}`).status(http.StatusCreated)
	api.do("POST", "/api/variables", `{"name":"M1","dataType":"Motor"}`).status(http.StatusCreated)
	api.do("POST", "/api/variables", `{"name":"Values","dataType":"ARRAY[1..3] OF INT"}`).status(http.StatusCreated)

	api.do("PUT", "/api/values", `{"Counter":5,"M1.speed":100,"Values[2]":9}`).status(http.StatusNoContent)

	var got map[string]interface{}
	api.do("GET", "/api/values?names=Counter,M1.speed,Values%5B2%5D", "").status(http.StatusOK).json(&got)
	want := map[string]interface{}{"Counter": float64(5), "M1.speed": float64(100), "Values[2]": float64(9)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("まとめて読んだ値 = %v, want %v", got, want)
	}
	// names を省くと、すべての変数
	got = nil
	api.do("GET", "/api/values", "").status(http.StatusOK).json(&got)
	if len(got) != 3 || got["Counter"] != float64(5) {
		t.Errorf("すべての変数 = %v", got)
	}

	// ない変数が 1 つでもあれば、1 つも書かない
	api.do("PUT", "/api/values", `{"Counter":6,"Nope":1}`).isError(http.StatusNotFound)
	got = nil
	api.do("GET", "/api/values?names=Counter", "").status(http.StatusOK).json(&got)
	if got["Counter"] != float64(5) {
		t.Errorf("ない変数があったのに書かれた: %v", got)
	}
	api.do("GET", "/api/values?names=Nope", "").isError(http.StatusNotFound)
}

// 名前に "." を含む変数も、フィールドの区切りと取り違えない
func TestResolveValueKey(t *testing.T) {
	vars := []*application.VariableDTO{{ID: "1", Name: "A"}, {ID: "2", Name: "A.b"}, {ID: "3", Name: "Arr"}}
	cases := []struct{ key, id, path string }{
		{"A", "1", ""},
		{"A.b", "2", ""},
		{"A.b.c", "2", "c"},
		{"A.x", "1", "x"},
		{"Arr[1].y", "3", "[1].y"},
	}
	for _, c := range cases {
		v, path, ok := resolveValueKey(vars, c.key)
		if !ok || v.ID != c.id || path != c.path {
			t.Errorf("%s: id=%v path=%q ok=%v, want id=%s path=%q", c.key, v, path, ok, c.id, c.path)
		}
	}
	if _, _, ok := resolveValueKey(vars, "B.c"); ok {
		t.Error("ない変数が見つかった")
	}
}

func TestVariables_MappingsAndTypes(t *testing.T) {
	api := newTestAPIWithServer(t)
	api.do("POST", "/api/variables", `{"name":"X","dataType":"INT"}`).status(http.StatusCreated)

	var mappings []application.ProtocolMappingDTO
	api.do("GET", "/api/variables/X/mappings", "").status(http.StatusOK).json(&mappings)
	if len(mappings) != 0 {
		t.Fatalf("最初はマッピングなし: %+v", mappings)
	}
	api.do("PUT", "/api/variables/X/mappings", `[{"protocolType":"fake-a","memoryArea":"holdingRegisters","address":10,"endianness":"big"}]`).status(http.StatusNoContent)
	api.do("GET", "/api/variables/X/mappings", "").status(http.StatusOK).json(&mappings)
	if len(mappings) != 1 || mappings[0].Address != 10 {
		t.Fatalf("マッピング: %+v", mappings)
	}
	// マッピングしたので、変数を書くとレジスタに出る
	api.do("PUT", "/api/variables/X/value", `{"value":321}`).status(http.StatusNoContent)
	var words struct{ Values []int }
	api.do("GET", "/api/memory/fake-a/holdingRegisters/words?address=10", "").status(http.StatusOK).json(&words)
	if !reflect.DeepEqual(words.Values, []int{321}) {
		t.Errorf("レジスタ = %v, want [321]", words.Values)
	}
	api.do("PUT", "/api/variables/X/mappings", `{"bad":1}`).isError(http.StatusBadRequest)
	api.do("GET", "/api/variables/Nope/mappings", "").isError(http.StatusNotFound)

	var targets []application.MappingTargetDTO
	api.do("GET", "/api/mapping-targets", "").status(http.StatusOK).json(&targets)
	if len(targets) != 1 || targets[0].ProtocolType != "fake-a" {
		t.Errorf("マッピング先: %+v", targets)
	}

	var dataTypes application.DataTypesDTO
	api.do("GET", "/api/data-types", "").status(http.StatusOK).json(&dataTypes)

	var statuses []application.VariableDeviceStatusDTO
	api.do("GET", "/api/variable-statuses", "").status(http.StatusOK).json(&statuses)
	if statuses == nil || len(statuses) != 0 {
		t.Errorf("同期の状態は空の配列: %+v", statuses)
	}
}

func TestStructTypes(t *testing.T) {
	api := newTestAPI(t)
	var types []application.StructTypeDTO
	api.do("GET", "/api/struct-types", "").status(http.StatusOK).json(&types)
	if types == nil || len(types) != 0 {
		t.Fatalf("最初は空の配列: %+v", types)
	}
	var created application.StructTypeDTO
	api.do("POST", "/api/struct-types", `{"name":"P","fields":[{"name":"a","dataType":"INT"},{"name":"b","dataType":"DINT"}]}`).status(http.StatusCreated).json(&created)
	if created.WordCount != 3 || created.Fields[1].Offset != 1 {
		t.Errorf("登録した構造体型: %+v", created)
	}
	api.do("POST", "/api/struct-types", `{"name":"Q","fields":[{"name":"a","dataType":"NOPE"}]}`).isError(http.StatusBadRequest)
	api.do("DELETE", "/api/struct-types/P", "").status(http.StatusNoContent)
	api.do("DELETE", "/api/struct-types/P", "").isError(http.StatusBadRequest)
}

// ===== クライアント =====

func newTestAPIWithClients(t *testing.T) (*testAPI, *protocoltest.ClientFactory) {
	api := newTestAPI(t)
	f := protocoltest.NewClientFactory("fake-client")
	api.svc.RegisterClientFactory(f)
	t.Cleanup(func() {
		for _, c := range api.svc.GetClients() {
			_ = api.svc.RemoveClient(c.ID)
		}
	})
	return api, f
}

func TestClients_CRUD(t *testing.T) {
	api, _ := newTestAPIWithClients(t)

	var protocols []application.ClientProtocolDTO
	api.do("GET", "/api/client-protocols", "").status(http.StatusOK).json(&protocols)
	if len(protocols) != 1 || protocols[0].ProtocolType != "fake-client" {
		t.Fatalf("プロトコル: %+v", protocols)
	}

	var c application.ClientDTO
	api.do("POST", "/api/clients", `{"protocolType":"fake-client","name":"PLC1","settings":{"target":"dev1"}}`).status(http.StatusCreated).json(&c)
	if c.ID == "" || c.Name != "PLC1" || c.Settings["target"] != "dev1" || c.Settings["timeoutMs"] != float64(100) {
		t.Fatalf("追加した接続先: %+v", c)
	}
	api.do("POST", "/api/clients", `{"protocolType":"nope"}`).isError(http.StatusBadRequest)

	for _, ref := range []string{c.ID, "PLC1"} {
		var got application.ClientDTO
		api.do("GET", "/api/clients/"+ref, "").status(http.StatusOK).json(&got)
		if got.ID != c.ID {
			t.Errorf("%s で引いた接続先が違う: %+v", ref, got)
		}
	}

	// 省いた設定の項目は、いまの値のまま
	var updated application.ClientDTO
	api.do("PUT", "/api/clients/PLC1", `{"settings":{"timeoutMs":500}}`).status(http.StatusOK).json(&updated)
	if updated.Name != "PLC1" || updated.Settings["target"] != "dev1" || updated.Settings["timeoutMs"] != float64(500) {
		t.Errorf("設定を変えたあと: %+v", updated)
	}
	api.do("PUT", "/api/clients/PLC1", `{"name":"PLC2"}`).status(http.StatusOK).json(&updated)
	if updated.Name != "PLC2" || updated.Settings["timeoutMs"] != float64(500) {
		t.Errorf("名前を変えたあと: %+v", updated)
	}
	api.do("PUT", "/api/clients/PLC2", `{"settings":{"target":""}}`).isError(http.StatusBadRequest)

	api.do("POST", "/api/clients/PLC2/disconnect", "").status(http.StatusNoContent)
	api.do("DELETE", "/api/clients/PLC2", "").status(http.StatusNoContent)
	api.do("GET", "/api/clients/PLC2", "").isError(http.StatusNotFound)
	api.do("POST", "/api/clients/PLC2/send", `{"request":"41"}`).isError(http.StatusNotFound)
}

func TestClients_BuildAndSend(t *testing.T) {
	api, _ := newTestAPIWithClients(t)
	api.do("POST", "/api/clients", `{"protocolType":"fake-client","name":"PLC"}`).status(http.StatusCreated)

	var built map[string]string
	api.do("POST", "/api/clients/PLC/build", `{"commandId":"echo","params":{"data":"AB"}}`).status(http.StatusOK).json(&built)
	if built["request"] != "41 42" {
		t.Errorf("組み立てたリクエスト = %q, want \"41 42\"", built["request"])
	}
	api.do("POST", "/api/clients/PLC/build", `{"commandId":"nope"}`).isError(http.StatusBadRequest)

	var res application.ClientCommandLogDTO
	api.do("POST", "/api/clients/PLC/send", `{"commandId":"echo","params":{"data":"AB"}}`).status(http.StatusOK).json(&res)
	if res.Status != "ok" || res.Request != "41 42" {
		t.Errorf("組み立てて送った結果: %+v", res)
	}
	api.do("POST", "/api/clients/PLC/send", `{"request":"43"}`).status(http.StatusOK).json(&res)
	if res.Status != "ok" || res.Received != "43" {
		t.Errorf("16 進で送った結果: %+v", res)
	}
	api.do("POST", "/api/clients/PLC/send", `{}`).isError(http.StatusBadRequest)
	api.do("POST", "/api/clients/PLC/send", `{"request":"0G"}`).isError(http.StatusBadRequest)

	var logs []application.ClientCommandLogDTO
	api.do("GET", "/api/client-logs", "").status(http.StatusOK).json(&logs)
	if len(logs) != 2 {
		t.Errorf("履歴 = %d 件, want 2", len(logs))
	}
	api.do("DELETE", "/api/client-logs", "").status(http.StatusNoContent)
	api.do("GET", "/api/client-logs", "").status(http.StatusOK).json(&logs)
	if logs == nil || len(logs) != 0 {
		t.Errorf("消したあとは空の配列: %+v", logs)
	}

	var entries []application.ClientBrowseEntryDTO
	api.do("GET", "/api/clients/PLC/browse?parent=folder", "").status(http.StatusOK).json(&entries)
	if len(entries) != 2 {
		t.Errorf("参照: %+v", entries)
	}
}

func TestClients_SavedCommands(t *testing.T) {
	api, _ := newTestAPIWithClients(t)
	api.do("POST", "/api/clients", `{"protocolType":"fake-client","name":"PLC"}`).status(http.StatusCreated)

	var cmd application.ClientSavedCommandDTO
	api.do("POST", "/api/clients/PLC/commands", `{"name":"読む","mode":"build","commandId":"echo","params":{"data":"R"}}`).status(http.StatusCreated).json(&cmd)
	if cmd.ID == "" || cmd.Request != "52" {
		t.Fatalf("保存したコマンド: %+v", cmd)
	}

	// 名前で送る
	var res application.ClientCommandLogDTO
	api.do("POST", "/api/clients/PLC/commands/%E8%AA%AD%E3%82%80/send", "").status(http.StatusOK).json(&res) // 読む
	if res.Status != "ok" || res.Label == "" {
		t.Errorf("保存したコマンドを送った結果: %+v", res)
	}

	// mode を省けば名前だけ変える
	api.do("PUT", "/api/clients/PLC/commands/"+cmd.ID, `{"name":"R2"}`).status(http.StatusNoContent)
	api.do("PUT", "/api/clients/PLC/commands/R2", `{"name":"R3","mode":"hex","request":"53"}`).status(http.StatusNoContent)
	var cmds []application.ClientSavedCommandDTO
	api.do("GET", "/api/clients/PLC/commands", "").status(http.StatusOK).json(&cmds)
	if len(cmds) != 1 || cmds[0].Name != "R3" || cmds[0].Request != "53" {
		t.Errorf("置き換えたあと: %+v", cmds)
	}

	api.do("POST", "/api/clients/PLC/commands/nope/send", "").isError(http.StatusNotFound)
	api.do("DELETE", "/api/clients/PLC/commands/R3", "").status(http.StatusNoContent)
	api.do("DELETE", "/api/clients/PLC/commands/R3", "").isError(http.StatusNotFound)
}

func TestClients_Polling(t *testing.T) {
	api, _ := newTestAPIWithClients(t)
	api.do("POST", "/api/clients", `{"protocolType":"fake-client","name":"PLC"}`).status(http.StatusCreated)

	var item application.ClientPollItemDTO
	api.do("POST", "/api/clients/PLC/polling/items", `{"commandId":"echo","params":{"data":"A"},"format":"dec"}`).status(http.StatusCreated).json(&item)
	if item.ID == "" {
		t.Fatalf("足した項目: %+v", item)
	}
	api.do("POST", "/api/clients/PLC/polling/items", `{"commandId":"write","params":{"data":"W"}}`).isError(http.StatusBadRequest) // ポーリングに使えない
	api.do("PUT", "/api/clients/PLC/polling/items/"+item.ID, `{"commandId":"echo","params":{"data":"B"},"format":"hex"}`).status(http.StatusNoContent)

	var p application.ClientPollingDTO
	api.do("PUT", "/api/clients/PLC/polling", `{"intervalMs":200,"failThreshold":5}`).status(http.StatusOK).json(&p)
	if p.IntervalMs != 200 || p.FailThreshold != 5 || len(p.Items) != 1 || p.Items[0].Format != "hex" {
		t.Errorf("ポーリングの状態: %+v", p)
	}
	api.do("PUT", "/api/clients/PLC/polling", `{"intervalMs":1}`).isError(http.StatusBadRequest)

	api.do("POST", "/api/clients/PLC/polling/start", "").status(http.StatusNoContent)
	api.do("GET", "/api/clients/PLC/polling", "").status(http.StatusOK).json(&p)
	if !p.Running {
		t.Error("始めたのに Running でない")
	}
	api.do("POST", "/api/clients/PLC/polling/stop", "").status(http.StatusNoContent)
	api.do("DELETE", "/api/clients/PLC/polling/items/"+item.ID, "").status(http.StatusNoContent)
	api.do("DELETE", "/api/clients/PLC/polling/items/"+item.ID, "").isError(http.StatusBadRequest)
}

func TestClients_Subscription(t *testing.T) {
	api, _ := newTestAPIWithClients(t)
	api.do("POST", "/api/clients", `{"protocolType":"fake-client","name":"PLC"}`).status(http.StatusCreated)

	var items []application.ClientSubscriptionItemDTO
	api.do("POST", "/api/clients/PLC/subscription/items", `{"params":{"keys":"a"}}`).status(http.StatusCreated).json(&items)
	if len(items) == 0 {
		t.Fatalf("足した監視項目: %+v", items)
	}
	var sub application.ClientSubscriptionDTO
	api.do("PUT", "/api/clients/PLC/subscription", `{"intervalMs":500}`).status(http.StatusOK).json(&sub)
	if sub.IntervalMs != 500 {
		t.Errorf("間隔: %+v", sub)
	}
	api.do("POST", "/api/clients/PLC/subscription/start", "").status(http.StatusNoContent)
	api.do("POST", "/api/clients/PLC/subscription/stop", "").status(http.StatusNoContent)
	api.do("DELETE", "/api/clients/PLC/subscription/items/"+items[0].ID, "").status(http.StatusNoContent)
	api.do("GET", "/api/clients/PLC/subscription", "").status(http.StatusOK).json(&sub)
	if len(sub.Items) != len(items)-1 {
		t.Errorf("削除したあとの項目: %+v", sub.Items)
	}
}
