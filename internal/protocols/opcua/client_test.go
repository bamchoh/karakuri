package opcua

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	uaserver "github.com/bamchoh/uastack/server"
	"github.com/bamchoh/uastack/ua"
	"github.com/bamchoh/uastack/uasc"

	"karakuri/internal/domain/protocol"
)

func newTestClient(t *testing.T, settings map[string]interface{}) protocol.Client {
	t.Helper()
	f := &ClientFactory{}
	normalized, err := f.NormalizeSettings(settings)
	if err != nil {
		t.Fatalf("NormalizeSettings: %v", err)
	}
	c, err := f.NewClient(normalized)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func buildRequest(t *testing.T, commandID string, params map[string]interface{}) []byte {
	t.Helper()
	req, err := (&ClientFactory{}).BuildRequest(commandID, params)
	if err != nil {
		t.Fatalf("BuildRequest(%s, %v): %v", commandID, params, err)
	}
	return req
}

func valueOf(values []protocol.ResultValue, label string) string {
	for _, v := range values {
		if v.Label == label {
			return v.Value
		}
	}
	return "（" + label + " がない）"
}

func TestClientFactory_NormalizeSettings(t *testing.T) {
	f := &ClientFactory{}
	got, err := f.NormalizeSettings(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got["endpointUrl"] != defaultClientEndpoint || got["securityPolicy"] != policyNone || got["timeoutMs"] != defaultClientTimeoutMs {
		t.Errorf("既定値 = %v", got)
	}
	// 設定項目の既定値は、検証を通る
	defaults := map[string]interface{}{}
	for _, field := range f.ConnectionFields() {
		defaults[field.Name] = field.Default
	}
	if _, err := f.NormalizeSettings(defaults); err != nil {
		t.Errorf("設定項目の既定値が通らない: %v", err)
	}

	for name, s := range map[string]map[string]interface{}{
		"URL が空":      {"endpointUrl": " "},
		"opc.tcp でない": {"endpointUrl": "http://localhost:4840"},
		"知らないポリシー":    {"securityPolicy": "Basic128"},
		"知らないモード":     {"securityMode": "None"},
		"タイムアウトが短い":   {"timeoutMs": float64(1)},
	} {
		if _, err := f.NormalizeSettings(s); err == nil {
			t.Errorf("%s: エラーにならない", name)
		}
	}
}

func TestClientFactory_BuildAndDescribe(t *testing.T) {
	f := &ClientFactory{}

	// コマンドの項目の既定値で組み立てられる（書き込みは NodeId が必須）
	for _, cmd := range f.Commands() {
		params := map[string]interface{}{}
		for _, field := range cmd.Fields {
			params[field.Name] = field.Default
		}
		if cmd.ID == cmdWrite {
			if _, err := f.BuildRequest(cmd.ID, params); err == nil {
				t.Errorf("%s: NodeId が空なのに組み立てられた", cmd.ID)
			}
			params["nodeId"] = "ns=2;s=X"
		}
		if _, err := f.BuildRequest(cmd.ID, params); err != nil {
			t.Errorf("%s: 既定値で組み立てられない: %v", cmd.ID, err)
		}
	}

	cases := []struct {
		cmd    string
		params map[string]interface{}
		want   string
	}{
		{cmdRead, map[string]interface{}{"nodeId": "ns=2;s=Motor1", "attribute": "Value"}, "Read ns=2;s=Motor1 の Value"},
		{cmdRead, map[string]interface{}{"nodeId": " i=2258 ", "attribute": "DataType"}, "Read i=2258 の DataType"},
		{cmdWrite, map[string]interface{}{"nodeId": "ns=2;s=C", "dataType": "Int16", "rank": "scalar", "value": "-5"}, "Write ns=2;s=C = -5（Int16）"},
		{cmdWrite, map[string]interface{}{"nodeId": "ns=2;s=C", "dataType": "UInt16", "rank": "scalar", "value": "0xFFFF"}, "Write ns=2;s=C = 65535（UInt16）"},
		{cmdWrite, map[string]interface{}{"nodeId": "ns=2;s=A", "dataType": "Byte", "rank": "array", "value": "1, 2,3"}, "Write ns=2;s=A = [1, 2, 3]（Byte の配列（3 個））"},
		{cmdWrite, map[string]interface{}{"nodeId": "ns=2;s=S", "dataType": "String", "rank": "scalar", "value": " a b "}, "Write ns=2;s=S =  a b （String）"},
		{cmdBrowse, map[string]interface{}{"nodeId": "i=85"}, "Browse i=85"},
	}
	for _, c := range cases {
		req := buildRequest(t, c.cmd, c.params)
		if got := f.DescribeRequest(req); got != c.want {
			t.Errorf("%s %v: 要約 = %q, want %q", c.cmd, c.params, got, c.want)
		}
	}

	for name, c := range map[string]struct {
		cmd    string
		params map[string]interface{}
	}{
		"NodeId が読めない": {cmdRead, map[string]interface{}{"nodeId": "Motor1"}},
		"知らない属性":       {cmdRead, map[string]interface{}{"nodeId": "i=1", "attribute": "Foo"}},
		"型に入らない値":      {cmdWrite, map[string]interface{}{"nodeId": "i=1", "dataType": "SByte", "rank": "scalar", "value": "200"}},
		"数値でない値":       {cmdWrite, map[string]interface{}{"nodeId": "i=1", "dataType": "Double", "rank": "scalar", "value": "abc"}},
		"配列の 2 個目が不正":  {cmdWrite, map[string]interface{}{"nodeId": "i=1", "dataType": "Boolean", "rank": "array", "value": "true, maybe"}},
		"知らない型":        {cmdWrite, map[string]interface{}{"nodeId": "i=1", "dataType": "Guid", "rank": "scalar", "value": "x"}},
		"知らないコマンド":     {"call", map[string]interface{}{}},
	} {
		if _, err := f.BuildRequest(c.cmd, c.params); err == nil {
			t.Errorf("%s: エラーにならない", name)
		}
	}
}

// 16 進で入れたリクエストは外から来たバイト列なので、どんな内容でも panic せず、エラーにする
func TestDecodeRequest_RejectsBrokenBytes(t *testing.T) {
	good := buildRequest(t, cmdRead, map[string]interface{}{"nodeId": "ns=2;s=Motor1", "attribute": "Value"})
	if _, err := decodeRequest(good); err != nil {
		t.Fatalf("組み立てたリクエストを読めない: %v", err)
	}
	for name, b := range map[string][]byte{
		"空":         nil,
		"途中で切れている":  good[:len(good)-3],
		"余分なバイトがある": append(append([]byte{}, good...), 0x00),
		"知らない型":     {0x01, 0x00, 0xFF, 0xFF},
		"応答の型":      encodeMessage(&ua.ReadResponse{}),
		"リクエストでない型": encodeMessage(&ua.ReadValueId{}),
		"でたらめ":      {0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		"長さだけ大きい配列": append(append([]byte{}, good[:len(good)-20]...), 0xFF, 0xFF, 0xFF, 0x7F),
	} {
		if _, err := decodeRequest(b); err == nil {
			t.Errorf("%s: エラーにならない", name)
		}
		if got := (&ClientFactory{}).DescribeRequest(b); !strings.HasPrefix(got, "解釈できないリクエスト") {
			t.Errorf("%s: 要約 = %q", name, got)
		}
	}
	// 1 バイトずつ削っても壊しても panic しない
	for i := range good {
		_, _ = decodeRequest(good[:i])
		broken := append([]byte{}, good...)
		broken[i] ^= 0xFF
		_, _ = decodeRequest(broken)
	}

	c := newTestClient(t, map[string]interface{}{"endpointUrl": "opc.tcp://127.0.0.1:1"})
	if res := c.Send(context.Background(), []byte{0x01, 0x02}); res.Status != protocol.CommandError || res.Sent != nil {
		t.Errorf("読めないリクエストは送らない: %+v", res)
	}
}

// Karakuri の OPC UA サーバーにつないで、読み取り・書き込み・ブラウズをする
func TestClient_ReadWriteBrowse(t *testing.T) {
	acc := newFakeAccessor()
	acc.publish("v-int", "Counter", "INT", "readwrite", int64(42))
	acc.publish("v-real", "Temp", "REAL", "read", float64(1.5))
	acc.publish("v-arr", "Values", "ARRAY[0..2] OF DINT", "readwrite", []interface{}{int64(1), int64(2), int64(3)})
	acc.publish("v-mat", "Grid", "ARRAY[0..1, 0..1] OF INT", "read", []interface{}{
		[]interface{}{int64(1), int64(2)}, []interface{}{int64(3), int64(4)},
	})
	srv, _, ns := startTestServer(t, acc)
	url := fmt.Sprintf("opc.tcp://127.0.0.1:%d", srv.config.Port)
	c := newTestClient(t, map[string]interface{}{"endpointUrl": url, "timeoutMs": 3000})
	ctx := context.Background()
	node := func(name string) string { return fmt.Sprintf("ns=%d;s=%s", ns, name) }
	send := func(cmd string, params map[string]interface{}) protocol.CommandResult {
		t.Helper()
		return c.Send(ctx, buildRequest(t, cmd, params))
	}

	if c.Connected() {
		t.Fatal("送る前は未接続")
	}
	res := send(cmdRead, map[string]interface{}{"nodeId": node("Counter"), "attribute": "Value"})
	if res.Status != protocol.CommandOK || valueOf(res.Values, "Value（Int16）") != "42" {
		t.Fatalf("スカラーの読み取り: %+v", res)
	}
	if !c.Connected() || len(res.Sent) == 0 || len(res.Received) == 0 || res.Summary != "Read "+node("Counter")+" の Value" {
		t.Errorf("接続・送受信のバイト列・要約: connected=%v %+v", c.Connected(), res)
	}
	// 送ったバイト列（ヘッダーを入れたあと）も、リクエストとして読める
	if _, err := decodeRequest(res.Sent); err != nil {
		t.Errorf("送ったバイト列を読めない: %v", err)
	}

	res = send(cmdRead, map[string]interface{}{"nodeId": node("Values"), "attribute": "Value"})
	if res.Status != protocol.CommandOK || valueOf(res.Values, "Value") != "Int32 の配列（3 個）" || valueOf(res.Values, "[2]") != "3" {
		t.Errorf("配列の読み取り: %+v", res)
	}
	res = send(cmdRead, map[string]interface{}{"nodeId": node("Grid"), "attribute": "Value"})
	if res.Status != protocol.CommandOK || valueOf(res.Values, "Value") != "Int16 の配列（2 × 2）" || valueOf(res.Values, "[1][0]") != "3" {
		t.Errorf("多次元配列の読み取り: %+v", res)
	}
	res = send(cmdRead, map[string]interface{}{"nodeId": node("Counter"), "attribute": "DataType"})
	if res.Status != protocol.CommandOK || valueOf(res.Values, "DataType（NodeId）") != "i=4（Int16）" {
		t.Errorf("DataType の読み取り: %+v", res)
	}

	// 書き込みがホストに届く
	res = send(cmdWrite, map[string]interface{}{"nodeId": node("Counter"), "dataType": "Int16", "rank": "scalar", "value": "-7"})
	if res.Status != protocol.CommandOK {
		t.Fatalf("書き込み: %+v", res)
	}
	if got := toInt64(acc.mustRead("v-int")); got != -7 {
		t.Errorf("ホストの値 = %v, want -7", got)
	}
	res = send(cmdWrite, map[string]interface{}{"nodeId": node("Values"), "dataType": "Int32", "rank": "array", "value": "10, 20, 0x1E"})
	if res.Status != protocol.CommandOK {
		t.Fatalf("配列の書き込み: %+v", res)
	}
	if got := fmt.Sprint(acc.mustRead("v-arr")); got != "[10 20 30]" {
		t.Errorf("ホストの配列 = %v", got)
	}

	// サーバーが断った操作は例外応答にする。接続は閉じない
	for name, params := range map[string]map[string]interface{}{
		"型が違う":   {"nodeId": node("Counter"), "dataType": "String", "rank": "scalar", "value": "x"},
		"読み取り専用": {"nodeId": node("Temp"), "dataType": "Float", "rank": "scalar", "value": "2"},
	} {
		if res := send(cmdWrite, params); res.Status != protocol.CommandException || res.Error == "" {
			t.Errorf("%s: %+v", name, res)
		}
	}
	res = send(cmdRead, map[string]interface{}{"nodeId": node("NoSuchNode"), "attribute": "Value"})
	if res.Status != protocol.CommandException || !strings.Contains(res.Error, "BadNodeIdUnknown") {
		t.Errorf("ないノードの読み取り: %+v", res)
	}
	if !c.Connected() {
		t.Error("例外応答では接続を閉じない")
	}

	// ブラウズ
	res = send(cmdBrowse, map[string]interface{}{"nodeId": node(folderNodeName)})
	if res.Status != protocol.CommandOK || valueOf(res.Values, "Counter") != node("Counter")+"（Variable）" {
		t.Errorf("ブラウズ: %+v", res)
	}
	res = send(cmdBrowse, map[string]interface{}{"nodeId": node("NoSuchNode")})
	if res.Status != protocol.CommandException {
		t.Errorf("ないノードのブラウズ: %+v", res)
	}

	// 組み立てられないリクエスト（ここでは 2 ノードの読み取り）も、16 進で入れれば送れる
	raw := encodeMessage(&ua.ReadRequest{NodesToRead: []ua.ReadValueId{
		{NodeId: ua.NewStringNodeId(ns, "Counter"), AttributeId: ua.AttributeIdValue},
		{NodeId: ua.NewStringNodeId(ns, "Temp"), AttributeId: ua.AttributeIdValue},
	}})
	res = c.Send(ctx, raw)
	if res.Status != protocol.CommandOK || res.Summary != "Read 2 ノード" ||
		valueOf(res.Values, node("Counter")+" Value（Int16）") != "-7" || valueOf(res.Values, node("Temp")+" Value（Float）") != "1.5" {
		t.Errorf("2 ノードの読み取り: %+v", res)
	}
	// 解釈を持たないサービスは、応答の型だけ出す
	res = c.Send(ctx, encodeMessage(&ua.RegisterNodesRequest{NodesToRegister: []ua.NodeId{ua.NewStringNodeId(ns, "Counter")}}))
	if res.Status != protocol.CommandOK || valueOf(res.Values, "応答") != "RegisterNodesResponse" {
		t.Errorf("RegisterNodes: %+v", res)
	}

	// 切断しても、次の送信でつなぎ直す
	if err := c.Close(); err != nil || c.Connected() {
		t.Fatalf("Close: %v, connected=%v", err, c.Connected())
	}
	if res := send(cmdRead, map[string]interface{}{"nodeId": node("Counter"), "attribute": "Value"}); res.Status != protocol.CommandOK {
		t.Errorf("つなぎ直し: %+v", res)
	}

	// サーバーを止めると失敗し、起動し直すとまた読める
	if err := srv.Stop(); err != nil {
		t.Fatal(err)
	}
	res = send(cmdRead, map[string]interface{}{"nodeId": node("Counter"), "attribute": "Value"})
	if res.Status != protocol.CommandError && res.Status != protocol.CommandTimeout {
		t.Errorf("停止後: %+v", res)
	}
	if c.Connected() {
		t.Error("失敗したら接続を閉じる")
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// 名前空間の番号は起動のたびに決まるので、読み直す
	node2 := fmt.Sprintf("ns=%d;s=Counter", srv.nodes.ns)
	if res := send(cmdRead, map[string]interface{}{"nodeId": node2, "attribute": "Value"}); res.Status != protocol.CommandOK {
		t.Errorf("起動し直したあと: %+v", res)
	}
}

// 応答しない相手はタイムアウトにして、接続を閉じる。取り消しは、待たずに終わる
func TestClient_TimeoutAndCancel(t *testing.T) {
	// つながるが、何も返さないサーバー
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()
	url := "opc.tcp://" + ln.Addr().String()
	req := buildRequest(t, cmdRead, map[string]interface{}{"nodeId": "i=2258", "attribute": "Value"})

	c := newTestClient(t, map[string]interface{}{"endpointUrl": url, "timeoutMs": 200})
	start := time.Now()
	res := c.Send(context.Background(), req)
	if res.Status != protocol.CommandTimeout || c.Connected() {
		t.Errorf("応答しないサーバー: %+v", res)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("タイムアウトまで %v かかった", d)
	}

	slow := newTestClient(t, map[string]interface{}{"endpointUrl": url, "timeoutMs": 30000})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start = time.Now()
	res = slow.Send(ctx, req)
	if res.Status != protocol.CommandError || res.Error != "取り消されました" {
		t.Errorf("取り消し: %+v", res)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("取り消しまで %v かかった", d)
	}

	// 待ち受けていないポートは、接続できないエラー
	refused := newTestClient(t, map[string]interface{}{"endpointUrl": fmt.Sprintf("opc.tcp://127.0.0.1:%d", freePort(t)), "timeoutMs": 2000})
	if res := refused.Send(context.Background(), req); res.Status == protocol.CommandOK || res.Status == protocol.CommandException || res.Error == "" {
		t.Errorf("待ち受けていないポート: %+v", res)
	}
}

// セキュリティポリシーとユーザー名・パスワードを使うサーバーにつなぐ
func TestClient_SecurityAndUserName(t *testing.T) {
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv, err := uaserver.New(uaserver.Config{
		ListenAddr:  addr,
		EndpointURL: "opc.tcp://" + addr,
		Security: []uaserver.SecurityConfig{
			// None は、エンドポイントの一覧を読む（GetEndpoints）のに要る
			{PolicyURI: uasc.SecurityPolicyNone, Modes: []ua.MessageSecurityMode{ua.MessageSecurityModeNone}},
			{PolicyURI: uasc.SecurityPolicyBasic256Sha256, Modes: []ua.MessageSecurityMode{ua.MessageSecurityModeSign, ua.MessageSecurityModeSignAndEncrypt}},
		},
		UserTokens:                      []uaserver.UserTokenType{uaserver.UserTokenUserName},
		AuthenticateUser:                func(user, password string) bool { return user == "operator" && password == "secret" },
		PKIDir:                          t.TempDir(),
		AutoAcceptUntrustedCertificates: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = srv.Close()
		<-done
	})

	req := buildRequest(t, cmdRead, map[string]interface{}{"nodeId": "i=2258", "attribute": "Value"})
	settings := func(mode, user, password string) map[string]interface{} {
		return map[string]interface{}{
			"endpointUrl": "opc.tcp://" + addr, "securityPolicy": "Basic256Sha256", "securityMode": mode,
			"userName": user, "password": password, "timeoutMs": 10000,
		}
	}
	for _, mode := range []string{modeSign, modeSignEn} {
		c := newTestClient(t, settings(mode, "operator", "secret"))
		res := c.Send(context.Background(), req)
		if res.Status != protocol.CommandOK || len(res.Values) != 1 || !strings.HasPrefix(res.Values[0].Label, "Value（DateTime）") {
			t.Errorf("%s: %+v", mode, res)
		}
	}

	// パスワードが違う・匿名・サーバーにないポリシーでは、つながらない（サーバーはユーザー名のトークンだけ受け付ける）
	for name, s := range map[string]map[string]interface{}{
		"パスワードが違う": settings(modeSignEn, "operator", "wrong"),
		"匿名":       settings(modeSignEn, "", ""),
		"None で匿名": {"endpointUrl": "opc.tcp://" + addr, "timeoutMs": 10000},
		"ないポリシー":   {"endpointUrl": "opc.tcp://" + addr, "securityPolicy": "Aes256_Sha256_RsaPss", "userName": "operator", "password": "secret", "timeoutMs": 10000},
	} {
		c := newTestClient(t, s)
		if res := c.Send(context.Background(), req); res.Status != protocol.CommandError || c.Connected() {
			t.Errorf("%s: %+v", name, res)
		}
	}
}
