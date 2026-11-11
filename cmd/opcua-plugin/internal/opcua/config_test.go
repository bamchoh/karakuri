package opcua

import (
	"context"
	"fmt"
	"testing"

	"github.com/bamchoh/uastack/client"
	"github.com/bamchoh/uastack/ua"
	"github.com/bamchoh/uastack/ua/id"
	"github.com/bamchoh/uastack/uacp"
)

func TestFactory_MapToConfig_MissingKeysUseDefaults(t *testing.T) {
	// アプリケーション情報の項目を追加する前に保存された設定（host / port のみ）
	f := &Factory{}
	got, err := f.MapToConfig("", map[string]interface{}{"host": "127.0.0.1", "port": float64(4841)})
	if err != nil {
		t.Fatalf("MapToConfig: %v", err)
	}
	cfg := got.(*Config)
	if cfg.Host != "127.0.0.1" || cfg.Port != 4841 {
		t.Errorf("host/port = %s:%d", cfg.Host, cfg.Port)
	}
	if cfg.ApplicationName != DefaultApplicationName || cfg.ManufacturerName != DefaultManufacturerName ||
		cfg.ProductURI != DefaultProductURI || cfg.NamespaceURI != DefaultNamespaceURI {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

func TestFactory_ConfigMapRoundTrip(t *testing.T) {
	f := &Factory{}
	want := &Config{
		Host: "0.0.0.0", Port: 4850, EndpointHost: "plc.local",
		ApplicationName: "Line A", ManufacturerName: "ACME", ProductURI: "urn:acme:sim", NamespaceURI: "urn:acme:line-a",
	}
	m := f.ConfigToMap(want)
	// 旧 C# 版 opcua-plugin と同じキー名で保存されること
	for _, key := range []string{"application_name", "manufacture_name", "product_uri", "namespace"} {
		if _, ok := m[key]; !ok {
			t.Errorf("key %q missing from %v", key, m)
		}
	}
	got, err := f.MapToConfig("", m)
	if err != nil {
		t.Fatalf("MapToConfig: %v", err)
	}
	if *got.(*Config) != *want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestFactory_MapToConfig_TrimsAndValidates(t *testing.T) {
	f := &Factory{}
	got, err := f.MapToConfig("", map[string]interface{}{"application_name": "  Line A  "})
	if err != nil {
		t.Fatalf("MapToConfig: %v", err)
	}
	if name := got.(*Config).ApplicationName; name != "Line A" {
		t.Errorf("ApplicationName = %q, want trimmed", name)
	}

	invalid := []map[string]interface{}{
		{"application_name": "   "},
		{"manufacture_name": ""},
		{"product_uri": ""},
		{"namespace": ""},
		{"namespace": "http://opcfoundation.org/UA/"},
	}
	for _, settings := range invalid {
		if _, err := f.MapToConfig("", settings); err == nil {
			t.Errorf("MapToConfig(%v) succeeded, want error", settings)
		}
	}
}

func TestFactory_ConfigFieldsHaveCategories(t *testing.T) {
	for _, field := range (&Factory{}).GetConfigFields("") {
		if field.Category == "" {
			t.Errorf("field %q has no category", field.Name)
		}
	}
}

func TestServer_PublishesConfiguredIdentity(t *testing.T) {
	acc := newFakeAccessor()
	acc.publish("v-int", "Counter", "INT", "readwrite", int64(1))
	cfg := testConfig(freePort(t))
	cfg.ApplicationName = "Line A Simulator"
	cfg.ManufacturerName = "ACME"
	cfg.ProductURI = "urn:acme:sim"
	cfg.NamespaceURI = "urn:acme:line-a"
	_, c, ns := startTestServerWithConfig(t, acc, cfg)
	ctx := context.Background()

	read := func(n uint32) interface{} {
		dv, err := c.ReadValue(ctx, ua.NewNumericNodeId(0, n))
		if err != nil {
			t.Fatalf("read i=%d: %v", n, err)
		}
		return dv.Value.Value()
	}
	if got := read(id.Server_ServerStatus_BuildInfo_ProductName); got != "Line A Simulator" {
		t.Errorf("ProductName = %#v", got)
	}
	if got := read(id.Server_ServerStatus_BuildInfo_ManufacturerName); got != "ACME" {
		t.Errorf("ManufacturerName = %#v", got)
	}
	if got := read(id.Server_ServerStatus_BuildInfo_ProductUri); got != "urn:acme:sim" {
		t.Errorf("ProductUri = %#v", got)
	}

	// 変数ノードは設定した名前空間に置かれる
	nsArray, _ := read(id.Server_NamespaceArray).([]string)
	if int(ns) >= len(nsArray) || nsArray[ns] != "urn:acme:line-a" {
		t.Fatalf("namespace array = %v, variable ns = %d", nsArray, ns)
	}
	if _, err := c.ReadValue(ctx, ua.NewStringNodeId(ns, "v-int")); err != nil {
		t.Errorf("read variable in configured namespace: %v", err)
	}

	// GetEndpoints の ApplicationDescription にも反映される
	eps, err := client.GetEndpoints(ctx, fmt.Sprintf("opc.tcp://127.0.0.1:%d", cfg.Port), uacp.ClientLimits{})
	if err != nil {
		t.Fatalf("GetEndpoints: %v", err)
	}
	if len(eps) == 0 || eps[0].Server.ApplicationName.Text != "Line A Simulator" || eps[0].Server.ProductUri != "urn:acme:sim" {
		t.Errorf("endpoint server = %+v", eps)
	}
}
