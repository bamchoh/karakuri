package opcua

import (
	"context"
	"fmt"
	"testing"

	"github.com/bamchoh/uastack/ua"

	"karakuri/internal/domain/variable"
	"karakuri/internal/infrastructure/adapter"
)

// OPC UA クライアント → OPC UA サーバー → ホストの変数ストア、という実際の経路を通すテスト。
// 他のテストはフェイクの変数ストアを使うため、変数ストアが持つ値の型（int16、uint16 など）や
// 変数ストアの型変換を通らない。
func TestServer_ClientWriteReachesHostVariableStore(t *testing.T) {
	// ホスト側の変数ストア
	vars := variable.NewVariableStore()
	motor, err := variable.NewStructTypeDef("Motor", []variable.StructField{
		{Name: "speed", DataType: "UINT"},
		{Name: "label", DataType: "STRING[8]"},
	}, vars)
	if err != nil {
		t.Fatal(err)
	}
	if err := vars.RegisterStructType(motor); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for name, dataType := range map[string]string{
		"Text": "STRING[20]", "Delay": "TIME", "Small": "USINT", "Count": "UINT", "Total": "UDINT", "Big": "ULINT",
		"Names": "ARRAY[0..1] OF STRING[5]", "Counts": "ARRAY[0..1] OF UINT", "M": "Motor",
	} {
		v, err := vars.CreateVariable(name, variable.DataType(dataType), nil)
		if err != nil {
			t.Fatalf("CreateVariable(%s): %v", name, err)
		}
		vars.SetNodePublishing(v.ID, string(ProtocolOPCUA), &variable.NodePublishing{Enabled: true, AccessMode: "readwrite"})
		ids[name] = v.ID
	}

	// アプリと同じ accessor（変数ストアを直接読み書きする）で OPC UA サーバーを動かす
	srv := NewServer(testConfig(freePort(t)), adapter.NewVariableStoreAccessor(vars))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	c := dialTestClient(t, srv)
	ns := srv.nodes.ns
	ctx := context.Background()

	hostValue := func(name string) string {
		v, err := vars.GetVariable(ids[name])
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(v.Value)
	}

	cases := []struct {
		node     string
		value    interface{}
		variable string
		want     string // 書いたあとのホストの変数の値
	}{
		{"Text", "hello", "Text", "hello"},
		{"Delay", "T#1s", "Delay", "T#1s"},
		{"Small", uint8(200), "Small", "200"},
		{"Count", uint16(60000), "Count", "60000"},
		{"Total", uint32(4000000000), "Total", "4000000000"},
		{"Big", uint64(18000000000000000000), "Big", "18000000000000000000"},
		{"Names", []string{"a", "b"}, "Names", "[a b]"},
		{"Names[1]", "xyz", "Names", "[a xyz]"},
		{"Counts", []uint16{1, 60000}, "Counts", "[1 60000]"},
		{"Counts[0]", uint16(50000), "Counts", "[50000 60000]"},
		{"M", `{"speed": 40000, "label": "main"}`, "M", "map[label:main speed:40000]"},
		{"M.speed", uint16(65535), "M", "map[label:main speed:65535]"},
		{"M.label", "sub", "M", "map[label:sub speed:65535]"},
	}
	for _, tc := range cases {
		if err := c.WriteValue(ctx, ua.NewStringNodeId(ns, tc.node), ua.NewVariant(tc.value)); err != nil {
			t.Errorf("%s に %v (%T) を書けない: %v", tc.node, tc.value, tc.value, err)
			continue
		}
		if got := hostValue(tc.variable); got != tc.want {
			t.Errorf("%s に %v を書いたあとのホストの値 = %s, want %s", tc.node, tc.value, got, tc.want)
		}
	}

	// 書いた値は OPC UA からも読める
	waitFor(t, func() bool {
		dv, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "Text"))
		return err == nil && dv.Value.Value() == "hello"
	})
	waitFor(t, func() bool {
		dv, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "Count"))
		return err == nil && dv.Value.Value() == uint16(60000)
	})
}
