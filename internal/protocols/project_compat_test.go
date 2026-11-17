package protocols_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"karakuri/internal/application"
)

// 前のバージョンで保存したプロジェクトのファイルが、いまのバージョンで読めるかを確かめる。
//
// testdata/projects/ の各ファイル（<名前>.json）を読み込み、保存し直したものが、元のファイルと同じになることを確かめる。
// 読み込みは、読めない変数・構造体型・コマンドなどを黙って読み飛ばすので、「エラーにならない」だけでは足りない。
//
//   - ファイルは、そのバージョンのアプリで保存したもの。一度置いたら書き換えない（利用者の手元にあるファイルと同じものとして残す）
//   - 保存の形を変えて、古いファイルを保存し直したものが元と違うようになったら（項目の名前を変えた・既定値を足したなど）、
//     違いが正しいことを確かめて、期待する内容を <名前>.expected.json に置く
//   - 保存の形を変えたら、新しいバージョンのファイルを足す:
//     KARAKURI_WRITE_PROJECT_FIXTURE=v0.0.49 go test -run TestWriteProjectFixture ./internal/protocols
func TestProjectFiles_LoadWithoutLoss(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "projects", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, file := range files {
		if strings.HasSuffix(file, ".expected.json") {
			continue
		}
		n++
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var data application.ProjectDataDTO
			if err := json.Unmarshal(raw, &data); err != nil {
				t.Fatalf("JSON として読めない: %v", err)
			}
			svc := newService(t)
			if err := svc.ImportProject(&data); err != nil {
				t.Fatalf("ImportProject: %v", err)
			}

			want := raw
			if exp, err := os.ReadFile(strings.TrimSuffix(file, ".json") + ".expected.json"); err == nil {
				want = exp
			}
			got, err := json.Marshal(svc.ExportProject())
			if err != nil {
				t.Fatal(err)
			}
			if g, w := normalizeProject(t, got), normalizeProject(t, want); g != w {
				t.Errorf("読み込んで保存し直したものが、元と違う（読み飛ばされたものがある？）\n%s", firstDiff(w, g))
			}
		})
	}
	if n == 0 {
		t.Fatal("testdata/projects にプロジェクトのファイルがない")
	}
}

// normalizeProject は、読み込むたびに変わるもの（変数・保存したコマンドの ID）を除き、
// 順番の決まらない並び（サーバーなど）を並べ替えて、比べられる形の JSON にする
func normalizeProject(t *testing.T, raw []byte) string {
	t.Helper()
	var p map[string]interface{}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	list := func(key string) []interface{} {
		l, _ := p[key].([]interface{})
		return l
	}
	for _, v := range list("variables") {
		delete(v.(map[string]interface{}), "id")
	}
	for _, c := range list("clients") {
		cmds, _ := c.(map[string]interface{})["savedCommands"].([]interface{})
		for _, cmd := range cmds {
			delete(cmd.(map[string]interface{}), "id")
		}
	}
	sortBy := func(key string, fields ...string) {
		l := list(key)
		sort.SliceStable(l, func(i, j int) bool {
			a, b := l[i].(map[string]interface{}), l[j].(map[string]interface{})
			for _, f := range fields {
				if x, y := fmt.Sprint(a[f]), fmt.Sprint(b[f]); x != y {
					return x < y
				}
			}
			return false
		})
	}
	sortBy("servers", "protocolType")
	sortBy("variables", "name")
	sortBy("structTypes", "name")
	sortBy("scripts", "id")
	sortBy("monitoringItems", "id")
	sortBy("clients", "id")
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// firstDiff は 2 つの文字列の、最初に違う行の前後を返す
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		line := func(l []string) string {
			if i < len(l) {
				return l[i]
			}
			return "（なし）"
		}
		if line(w) != line(g) {
			from := max(i-5, 0)
			return fmt.Sprintf("%d 行目から違う\n元:\n%s\n保存し直したもの:\n%s",
				i+1, strings.Join(w[from:min(i+6, len(w))], "\n"), strings.Join(g[from:min(i+6, len(g))], "\n"))
		}
	}
	return ""
}

// TestWriteProjectFixture は、いまのバージョンで、ほぼ全部の機能を使ったプロジェクトを作って保存する（ふだんは飛ばす）。
// 保存の形を変えたときに、新しいバージョンのファイルを足すのに使う
func TestWriteProjectFixture(t *testing.T) {
	name := os.Getenv("KARAKURI_WRITE_PROJECT_FIXTURE")
	if name == "" {
		t.Skip("KARAKURI_WRITE_PROJECT_FIXTURE（ファイルの名前。例: v0.0.48）を設定したときだけ動く")
	}
	path := filepath.Join("testdata", "projects", name+".json")
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s はもうある（一度置いたファイルは書き換えない）", path)
	}
	svc := newService(t)
	buildFullProject(t, svc)
	out, err := json.MarshalIndent(svc.ExportProject(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, append(out, '\n'), 0o644))
	t.Logf("%s を書いた", path)
}

// buildFullProject は、プロジェクトに保存されるものを、なるべく全部、既定値でない値で作る
func buildFullProject(t *testing.T, svc *application.PLCService) {
	t.Helper()

	// サーバー: 組み込みの全プロトコル。設定は既定値から変える（数値は +1、選択肢は最後のもの）
	for _, p := range svc.GetAvailableProtocols() {
		schema, err := svc.GetProtocolSchema(p.Type)
		must(t, err)
		variant := schema.Variants[0]
		must(t, svc.AddServer(p.Type, variant.ID))
		must(t, svc.UpdateServerConfig(&application.ServerConfigDTO{
			ProtocolType: p.Type, Variant: variant.ID, Settings: changedSettings(variant.Fields),
		}))
		if schema.Capabilities.SupportsUnitID {
			must(t, svc.SetDisabledUnitIDs(p.Type, []int{3, 7}))
		}
	}

	// 構造体型（入れ子・配列のフィールド）
	_, err := svc.RegisterStructType(application.StructTypeDTO{Name: "Point", Fields: []application.StructFieldDTO{
		{Name: "x", DataType: "INT"}, {Name: "y", DataType: "REAL"},
	}})
	must(t, err)
	_, err = svc.RegisterStructType(application.StructTypeDTO{Name: "Line", Fields: []application.StructFieldDTO{
		{Name: "name", DataType: "STRING[10]"},
		{Name: "points", DataType: "ARRAY[0..1] OF Point"},
		{Name: "flags", DataType: "ARRAY[1..3] OF BOOL"},
		{Name: "total", DataType: "LINT"},
	}})
	must(t, err)

	// 変数: 全部の型を、既定値でない値で
	vars := map[string]struct {
		dataType string
		value    interface{}
	}{
		"Bool": {"BOOL", true}, "Sint": {"SINT", -5}, "Int": {"INT", -1234}, "Dint": {"DINT", 123456},
		"Lint": {"LINT", "-9007199254740993"}, "Usint": {"USINT", 200}, "Uint": {"UINT", 65000},
		"Udint": {"UDINT", 4000000000}, "Ulint": {"ULINT", "18446744073709551615"},
		"Real": {"REAL", 1.5}, "Lreal": {"LREAL", 3.25},
		"Str": {"STRING", "hello"}, "StrN": {"STRING[8]", "日本"},
		"Time": {"TIME", "T#1h2m3s"}, "Date": {"DATE", "D#2024-01-02"},
		"Tod": {"TIME_OF_DAY", "TOD#12:30:15"}, "Dt": {"DATE_AND_TIME", "DT#2024-01-02-03:04:05"},
		"Values":   {"ARRAY[1..3] OF DINT", []interface{}{1, 2, 3}},
		"Grid":     {"ARRAY[0..1, 0..2] OF INT", []interface{}{[]interface{}{1, 2, 3}, []interface{}{4, 5, 6}}},
		"OldArray": {"ARRAY[INT;3]", []interface{}{7, 8, 9}}, // 旧形式の配列の型
		"Line1": {"Line", map[string]interface{}{
			"name":   "L1",
			"points": []interface{}{map[string]interface{}{"x": 1, "y": 0.5}, map[string]interface{}{"x": -2, "y": 2.5}},
			"flags":  []interface{}{true, false, true},
			"total":  "9007199254740993",
		}},
		"Points": {"ARRAY[1..2] OF Point", []interface{}{map[string]interface{}{"x": 10, "y": 1}, map[string]interface{}{"x": 20, "y": 2}}},
	}
	ids := map[string]string{}
	for name, v := range vars {
		dto, err := svc.CreateVariable(name, v.dataType, v.value)
		if err != nil {
			t.Fatalf("CreateVariable(%s): %v", name, err)
		}
		ids[name] = dto.ID
	}

	// 接続先: 組み込みの全プロトコル。保存したコマンド（全部のコマンドを項目の既定値で、と 16 進）・ポーリング・サブスクリプション
	clientIDs := map[string]string{}
	for _, cp := range svc.GetClientProtocols() {
		settings := changedSettings(cp.ConnectionFields)
		c, err := svc.AddClient(cp.ProtocolType, "接続先 "+cp.DisplayName, settings)
		if err != nil {
			t.Fatalf("AddClient(%s): %v", cp.ProtocolType, err)
		}
		clientIDs[cp.ProtocolType] = c.ID
		saved, polled := 0, 0
		for _, cmd := range cp.Commands {
			params := defaultParams(cmd.Fields)
			if _, err := svc.AddClientSavedCommand(c.ID, application.ClientSavedCommandInputDTO{
				Name: cmd.DisplayName, Mode: "build", CommandID: cmd.ID, Params: params,
			}); err == nil {
				saved++
			}
			if cmd.Pollable {
				if _, err := svc.AddClientPollItem(c.ID, cmd.ID, params, "hex"); err == nil {
					polled++
				}
			}
		}
		// 16 進で入れたコマンド（最初のコマンドを組み立てたもの）
		hex, err := svc.BuildClientRequest(cp.ProtocolType, cp.Commands[0].ID, defaultParams(cp.Commands[0].Fields))
		must(t, err)
		_, err = svc.AddClientSavedCommand(c.ID, application.ClientSavedCommandInputDTO{Name: "16 進", Mode: "hex", Request: hex})
		must(t, err)
		if saved == 0 || polled == 0 {
			t.Fatalf("%s: 保存したコマンド %d 個・読み取り項目 %d 個（項目の既定値で作れるコマンドがない）", cp.ProtocolType, saved, polled)
		}
		must(t, svc.SetClientPollInterval(c.ID, 250))
		must(t, svc.SetClientFailThreshold(c.ID, 5))
		if cp.SupportsSubscription {
			params := map[string]interface{}{}
			for _, f := range cp.MonitoredItemFields {
				params[f.Name] = "ns=2;s=Int\nns=2;s=Values"
			}
			_, err := svc.AddClientSubscriptionItems(c.ID, params)
			must(t, err)
			must(t, svc.SetClientSubscriptionInterval(c.ID, 500))
		}
	}

	// マッピング: サーバーと接続先のエリア（ワード・ビット、エンディアン）。ノードの公開
	targets := []string{"modbus-tcp", "fins", "slmp", "client:" + clientIDs["modbus-tcp"], "client:" + clientIDs["fins"]}
	for i, pt := range targets {
		word, bit := areasOf(t, svc, pt)
		must(t, svc.UpdateVariableMappings(ids["Dint"], appendMapping(svc, ids["Dint"], application.ProtocolMappingDTO{ProtocolType: pt, MemoryArea: word, Address: 10 + i, Endianness: "little"})))
		must(t, svc.UpdateVariableMappings(ids["Line1"], appendMapping(svc, ids["Line1"], application.ProtocolMappingDTO{ProtocolType: pt, MemoryArea: word, Address: 100, Endianness: "big"})))
		must(t, svc.UpdateVariableMappings(ids["Bool"], appendMapping(svc, ids["Bool"], application.ProtocolMappingDTO{ProtocolType: pt, MemoryArea: bit, Address: 5, Endianness: "big"})))
	}
	for _, name := range []string{"Int", "Line1", "Grid"} {
		must(t, svc.UpdateVariableNodePublishing(ids[name], opcUA, &application.NodePublishingDTO{ProtocolType: opcUA, Enabled: true, AccessMode: "read"}))
	}

	// スクリプトとモニタリング項目
	_, err = svc.CreateScript("カウンター", "vars.Int = vars.Int + 1;", 100)
	must(t, err)
	_, err = svc.CreateScript("BigInt", "vars.Lint = vars.Lint + 1n;\nconsole.log(vars.Lint);", 1000)
	must(t, err)
	word, bit := areasOf(t, svc, "modbus-tcp")
	_, err = svc.AddMonitoringItem(&application.MonitoringItemDTO{ProtocolType: "modbus-tcp", MemoryArea: word, Address: 10, BitWidth: 32, Endianness: "little", DisplayFormat: "hex"})
	must(t, err)
	_, err = svc.AddMonitoringItem(&application.MonitoringItemDTO{ProtocolType: "modbus-tcp", MemoryArea: bit, Address: 5, BitWidth: 1, Endianness: "big", DisplayFormat: "decimal"})
	must(t, err)
	// レジスタ以外のモニタリング項目: 変数と、接続先ごとの読み取り項目・監視項目（あれば、最初のもの）
	_, err = svc.AddMonitoringItem(&application.MonitoringItemDTO{Kind: application.MonitoringKindVariable, VariableID: ids["Int"]})
	must(t, err)
	_, err = svc.AddMonitoringItem(&application.MonitoringItemDTO{Kind: application.MonitoringKindVariable, VariableID: ids["Line1"], FieldPath: "motor.speed"})
	must(t, err)
	for _, c := range svc.GetClients() {
		if poll, err := svc.GetClientPolling(c.ID); err == nil && len(poll.Items) > 0 {
			_, err = svc.AddMonitoringItem(&application.MonitoringItemDTO{Kind: application.MonitoringKindPoll, ClientID: c.ID, Summary: poll.Items[0].Summary})
			must(t, err)
		}
		if sub, err := svc.GetClientSubscription(c.ID); err == nil && len(sub.Items) > 0 {
			_, err = svc.AddMonitoringItem(&application.MonitoringItemDTO{Kind: application.MonitoringKindSubscription, ClientID: c.ID, Summary: sub.Items[0].Summary})
			must(t, err)
		}
	}
}

// changedSettings は、項目の既定値から変えた設定を作る（数値は +1、選択肢は最後のもの、パスワードは決まった文字）
func changedSettings(fields []application.FieldDTO) map[string]interface{} {
	settings := map[string]interface{}{}
	for _, f := range fields {
		switch {
		case f.Type == "number":
			n := 0
			switch d := f.Default.(type) {
			case int:
				n = d
			case float64:
				n = int(d)
			}
			if f.Max == nil || n+1 <= *f.Max {
				n++
			}
			settings[f.Name] = n
		case f.Type == "password":
			settings[f.Name] = "secret"
		case f.Type == "rows":
			// 列も既定でない値にした 1 行（既定値のない文字の列は、列の名前 + 1）
			row := changedSettings(f.Columns)
			for _, c := range f.Columns {
				if c.Type == "text" && row[c.Name] == "" {
					row[c.Name] = c.Name + "1"
				}
			}
			settings[f.Name] = []interface{}{row}
		case len(f.Options) > 0:
			settings[f.Name] = f.Options[len(f.Options)-1].Value
		case f.Default != nil:
			settings[f.Name] = f.Default
		}
	}
	return settings
}

// defaultParams は、コマンドの項目の既定値を並べる。既定値のない文字の項目には、OPC UA の NodeId を入れる
func defaultParams(fields []application.FieldDTO) map[string]interface{} {
	params := map[string]interface{}{}
	for _, f := range fields {
		if f.Default != nil && f.Default != "" {
			params[f.Name] = f.Default
		} else if f.Type == "text" || f.Type == "textarea" {
			params[f.Name] = "ns=2;s=Int"
		}
	}
	return params
}

// areasOf は、マッピング先の最初のワードのエリアと、最初のビットのエリアを返す
func areasOf(t *testing.T, svc *application.PLCService, pt string) (word, bit string) {
	t.Helper()
	for _, a := range svc.GetMemoryAreas(pt) {
		if a.ReadOnly {
			continue
		}
		if a.IsBit && bit == "" {
			bit = a.ID
		}
		if !a.IsBit && word == "" {
			word = a.ID
		}
	}
	if word == "" || bit == "" {
		t.Fatalf("%s: 書けるワード・ビットのエリアがない", pt)
	}
	return word, bit
}

func appendMapping(svc *application.PLCService, id string, m application.ProtocolMappingDTO) []application.ProtocolMappingDTO {
	for _, v := range svc.GetVariables() {
		if v.ID == id {
			return append(append([]application.ProtocolMappingDTO(nil), v.Mappings...), m)
		}
	}
	return []application.ProtocolMappingDTO{m}
}
