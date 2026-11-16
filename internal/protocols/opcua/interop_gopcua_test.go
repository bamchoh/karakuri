package opcua

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	gopcua "github.com/gopcua/opcua"
	gua "github.com/gopcua/opcua/ua"

	"karakuri/internal/domain/variable"
	"karakuri/internal/infrastructure/adapter"
)

// 他社製のクライアント（gopcua/opcua）から、Karakuri の OPC UA サーバーにつなぐ。
// Karakuri のサーバーとクライアントは同じライブラリ（uastack）を使うので、同じ読み違えをしていても、
// 自分同士の試験では見つからない。実際の変数ストアを使う
func TestInterop_GopcuaClient(t *testing.T) {
	vars := variable.NewVariableStore()
	motor, err := variable.NewStructTypeDef("Motor", []variable.StructField{
		{Name: "speed", DataType: "INT"},
		{Name: "on", DataType: "BOOL"},
	}, vars.TypeResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := vars.RegisterStructType(motor); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for name, def := range map[string]struct {
		dataType string
		value    interface{}
	}{
		"Counter": {"INT", int16(42)},
		"Temp":    {"REAL", float32(1.5)},
		"Big":     {"LINT", int64(-9007199254740993)},
		"Text":    {"STRING[20]", "hello"},
		"Values":  {"ARRAY[1..3] OF DINT", []interface{}{int32(1), int32(2), int32(3)}},
		"Grid":    {"ARRAY[0..1, 0..2] OF INT", nil},
		"M1":      {"Motor", nil},
	} {
		v, err := vars.CreateVariable(name, variable.DataType(def.dataType), def.value)
		if err != nil {
			t.Fatalf("CreateVariable(%s): %v", name, err)
		}
		vars.SetNodePublishing(v.ID, string(ProtocolOPCUA), &variable.NodePublishing{Enabled: true, AccessMode: "readwrite"})
		ids[name] = v.ID
	}

	srv := NewServer(testConfig(freePort(t)), adapter.NewVariableStoreAccessor(vars))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	ns := srv.nodes.ns
	url := fmt.Sprintf("opc.tcp://127.0.0.1:%d", srv.config.Port)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// エンドポイントを調べて、セキュリティなし・匿名でつなぐ（他社製のクライアントのふつうの手順）
	endpoints, err := gopcua.GetEndpoints(ctx, url)
	if err != nil {
		t.Fatalf("GetEndpoints: %v", err)
	}
	ep, err := gopcua.SelectEndpoint(endpoints, gua.SecurityPolicyURINone, gua.MessageSecurityModeNone)
	if err != nil {
		t.Fatalf("SelectEndpoint: %v（エンドポイント: %d 個）", err, len(endpoints))
	}
	ep.EndpointURL = url // サーバーが返す URL のホスト名は PC の名前なので、つないだ URL に置き換える
	c, err := gopcua.NewClient(url,
		gopcua.SecurityPolicy(gua.SecurityPolicyURINone),
		gopcua.SecurityMode(gua.MessageSecurityModeNone),
		gopcua.AuthAnonymous(),
		gopcua.SecurityFromEndpoint(ep, gua.UserTokenTypeAnonymous),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close(context.Background())

	node := func(s string) *gua.NodeID { return gua.NewStringNodeID(ns, s) }
	read := func(s string) interface{} {
		t.Helper()
		resp, err := c.Read(ctx, &gua.ReadRequest{NodesToRead: []*gua.ReadValueID{{NodeID: node(s), AttributeID: gua.AttributeIDValue}}})
		if err != nil {
			t.Fatalf("Read(%s): %v", s, err)
		}
		if st := resp.Results[0].Status; st != gua.StatusOK {
			t.Fatalf("Read(%s): %v", s, st)
		}
		return resp.Results[0].Value.Value()
	}
	write := func(s string, v interface{}) gua.StatusCode {
		t.Helper()
		variant, err := gua.NewVariant(v)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := c.Write(ctx, &gua.WriteRequest{NodesToWrite: []*gua.WriteValue{{
			NodeID: node(s), AttributeID: gua.AttributeIDValue,
			Value: &gua.DataValue{EncodingMask: gua.DataValueValue, Value: variant},
		}}})
		if err != nil {
			t.Fatalf("Write(%s): %v", s, err)
		}
		return resp.Results[0]
	}
	hostValue := func(name string) interface{} {
		v, err := vars.GetVariable(ids[name])
		if err != nil {
			t.Fatal(err)
		}
		return v.Value
	}

	t.Run("公開変数のフォルダをたどれる", func(t *testing.T) {
		resp, err := c.Browse(ctx, &gua.BrowseRequest{NodesToBrowse: []*gua.BrowseDescription{{
			NodeID: node(folderNodeName), BrowseDirection: gua.BrowseDirectionForward,
			IncludeSubtypes: true, ResultMask: uint32(gua.BrowseResultMaskAll),
		}}})
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		names := map[string]bool{}
		for _, r := range resp.Results[0].References {
			names[r.BrowseName.Name] = true
		}
		for _, want := range []string{"Counter", "Temp", "Big", "Text", "Values", "Grid", "M1"} {
			if !names[want] {
				t.Errorf("%s がない: %v", want, names)
			}
		}
	})

	t.Run("型どおりに読める", func(t *testing.T) {
		cases := map[string]interface{}{
			"Counter":   int16(42),
			"Temp":      float32(1.5),
			"Big":       int64(-9007199254740993),
			"Text":      "hello",
			"Values":    []int32{1, 2, 3},
			"Values[2]": int32(2),
			"M1.speed":  int16(0),
			"M1.on":     false,
		}
		for name, want := range cases {
			if got := read(name); !reflect.DeepEqual(got, want) {
				t.Errorf("%s = %#v, want %#v", name, got, want)
			}
		}
		// 多次元配列は Matrix（gopcua は入れ子のスライスで返す）
		if got := read("Grid"); !reflect.DeepEqual(got, [][]int16{{0, 0, 0}, {0, 0, 0}}) {
			t.Errorf("Grid = %#v", got)
		}
	})

	t.Run("書いた値が変数ストアに届く", func(t *testing.T) {
		if st := write("Counter", int16(-7)); st != gua.StatusOK {
			t.Fatalf("Counter: %v", st)
		}
		if got := hostValue("Counter"); got != int16(-7) {
			t.Errorf("Counter（ホスト） = %#v", got)
		}
		if st := write("Big", int64(9007199254740993)); st != gua.StatusOK {
			t.Fatalf("Big: %v", st)
		}
		if got := hostValue("Big"); got != int64(9007199254740993) {
			t.Errorf("Big（ホスト） = %#v（64 ビットの値が桁を落とした？）", got)
		}
		if st := write("Values[3]", int32(30)); st != gua.StatusOK {
			t.Fatalf("Values[3]: %v", st)
		}
		if got := hostValue("Values"); !reflect.DeepEqual(got, []interface{}{int32(1), int32(2), int32(30)}) {
			t.Errorf("Values（ホスト） = %#v", got)
		}
		if st := write("M1.speed", int16(100)); st != gua.StatusOK {
			t.Fatalf("M1.speed: %v", st)
		}
		if got := hostValue("M1").(map[string]interface{})["speed"]; got != int16(100) {
			t.Errorf("M1.speed（ホスト） = %#v", got)
		}
		if st := write("Values", []int32{7, 8, 9}); st != gua.StatusOK {
			t.Fatalf("Values（配列まるごと）: %v", st)
		}
		if got := read("Values"); !reflect.DeepEqual(got, []int32{7, 8, 9}) {
			t.Errorf("Values = %#v", got)
		}
	})

	t.Run("型の違う書き込みは断る", func(t *testing.T) {
		if st := write("Counter", "x"); st == gua.StatusOK {
			t.Error("INT のノードに文字列を書けた")
		}
		if st := write("Counter", int32(1)); st == gua.StatusOK {
			t.Error("INT（Int16）のノードに Int32 を書けた")
		}
		if got := hostValue("Counter"); got != int16(-7) {
			t.Errorf("断った書き込みで値が変わった: %#v", got)
		}
	})

	t.Run("サブスクリプションで変化が届く", func(t *testing.T) {
		notify := make(chan *gopcua.PublishNotificationData, 16)
		sub, err := c.Subscribe(ctx, &gopcua.SubscriptionParameters{Interval: 50 * time.Millisecond}, notify)
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		defer sub.Cancel(context.Background())
		res, err := sub.Monitor(ctx, gua.TimestampsToReturnBoth, gopcua.NewMonitoredItemCreateRequestWithDefaults(node("Counter"), gua.AttributeIDValue, 1))
		if err != nil || res.Results[0].StatusCode != gua.StatusOK {
			t.Fatalf("Monitor: %v %v", err, res)
		}
		// ホストの側（スクリプトや画面）で変えた値が届くまで待つ
		_ = vars.UpdateValue(ids["Counter"], int16(555))
		deadline := time.After(5 * time.Second)
		for {
			select {
			case n := <-notify:
				if n.Error != nil {
					t.Fatalf("通知のエラー: %v", n.Error)
				}
				if dc, ok := n.Value.(*gua.DataChangeNotification); ok {
					for _, item := range dc.MonitoredItems {
						if item.Value.Value.Value() == int16(555) {
							return
						}
					}
				}
			case <-deadline:
				t.Fatal("5 秒待っても、変えた値（555）の通知が届かない")
			}
		}
	})
}
