package opcua

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
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
		switch cmd.ID {
		case cmdWrite:
			if _, err := f.BuildRequest(cmd.ID, params); err == nil {
				t.Errorf("%s: NodeId が空なのに組み立てられた", cmd.ID)
			}
			params["nodeId"] = "ns=2;s=X"
		case cmdWriteMulti:
			if _, err := f.BuildRequest(cmd.ID, params); err == nil {
				t.Errorf("%s: 書き込む内容が空なのに組み立てられた", cmd.ID)
			}
			params["items"] = "ns=2;s=X := 1"
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
		// 複数のノード（1 行に 1 つ。空の行は読み飛ばす）
		{cmdRead, map[string]interface{}{"nodeId": "ns=2;s=A\r\n\n  ns=2;s=B  \n", "attribute": "Value"}, "Read 2 ノード（ns=2;s=A、ns=2;s=B）"},
		{cmdRead, map[string]interface{}{"nodeId": "i=1\ni=2\ni=3\ni=4", "attribute": "DataType"}, "Read 4 ノード（i=1、i=2、i=3、…）"},
		{cmdWriteMulti, map[string]interface{}{"items": "ns=2;s=A := 5", "dataType": "Int16"}, "Write ns=2;s=A = 5（Int16）"},
		{cmdWriteMulti, map[string]interface{}{"items": "ns=2;s=A := 5\n\nns=2;s=B : String := a := b\nns=2;s=C:Int32[]:=1, 2", "dataType": "Int16"}, "Write 3 ノード（ns=2;s=A、ns=2;s=B、ns=2;s=C）"},
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
		"NodeId が読めない":       {cmdRead, map[string]interface{}{"nodeId": "Motor1"}},
		"知らない属性":             {cmdRead, map[string]interface{}{"nodeId": "i=1", "attribute": "Foo"}},
		"型に入らない値":            {cmdWrite, map[string]interface{}{"nodeId": "i=1", "dataType": "SByte", "rank": "scalar", "value": "200"}},
		"数値でない値":             {cmdWrite, map[string]interface{}{"nodeId": "i=1", "dataType": "Double", "rank": "scalar", "value": "abc"}},
		"配列の 2 個目が不正":        {cmdWrite, map[string]interface{}{"nodeId": "i=1", "dataType": "Boolean", "rank": "array", "value": "true, maybe"}},
		"知らない型":              {cmdWrite, map[string]interface{}{"nodeId": "i=1", "dataType": "Guid", "rank": "scalar", "value": "x"}},
		"知らないコマンド":           {"call", map[string]interface{}{}},
		"2 行目の NodeId が読めない": {cmdRead, map[string]interface{}{"nodeId": "i=1\nMotor1", "attribute": "Value"}},
		"NodeId が空の行だけ":      {cmdRead, map[string]interface{}{"nodeId": " \n ", "attribute": "Value"}},
		":= がない":             {cmdWriteMulti, map[string]interface{}{"items": "ns=2;s=A = 5", "dataType": "Int16"}},
		"複数書き込みの値が型に入らない":    {cmdWriteMulti, map[string]interface{}{"items": "ns=2;s=A := 1\nns=2;s=B := 70000", "dataType": "Int16"}},
		"複数書き込みの NodeId が不正": {cmdWriteMulti, map[string]interface{}{"items": "Motor1 := 1", "dataType": "Int16"}},
	} {
		if _, err := f.BuildRequest(c.cmd, c.params); err == nil {
			t.Errorf("%s: エラーにならない", name)
		}
	}
}

// 複数書き込みの 1 行の読み方（型の指定・配列・NodeId の中の ":"）
func TestParseWriteLine(t *testing.T) {
	cases := []struct {
		line     string
		wantNode string
		wantType string
		wantText string
	}{
		{"ns=2;s=A := 5", "ns=2;s=A", "Int16", "5"},
		{"ns=2;s=A:=0x10", "ns=2;s=A", "Int16", "16"},
		{"ns=2;s=A : Double := 1.5", "ns=2;s=A", "Double", "1.5"},
		{"ns=2;s=A:Boolean:=true", "ns=2;s=A", "Boolean", "true"},
		{"ns=2;s=S : String :=  a := b ", "ns=2;s=S", "String", "a := b"},
		{"ns=2;s=V : Int32[] := 1, 2, 3", "ns=2;s=V", "Int32 の配列（3 個）", "[1, 2, 3]"},
		// 型の名前でないものは、NodeId の一部として読む
		{"ns=2;s=Line:Motor := 7", "ns=2;s=Line:Motor", "Int16", "7"},
		{"ns=2;s=Line:Motor : UInt16 := 7", "ns=2;s=Line:Motor", "UInt16", "7"},
	}
	for _, c := range cases {
		wv, err := parseWriteLine(c.line, ua.TypeInt16)
		if err != nil {
			t.Errorf("%q: %v", c.line, err)
			continue
		}
		if got := wv.NodeId.String(); got != c.wantNode {
			t.Errorf("%q: NodeId = %q, want %q", c.line, got, c.wantNode)
		}
		if got := variantTypeText(wv.Value.Value); got != c.wantType {
			t.Errorf("%q: 型 = %q, want %q", c.line, got, c.wantType)
		}
		if got := variantText(wv.Value.Value); got != c.wantText {
			t.Errorf("%q: 値 = %q, want %q", c.line, got, c.wantText)
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

	// 複数のノードを 1 回のリクエストで読む
	res = send(cmdRead, map[string]interface{}{"nodeId": node("Counter") + "\n" + node("Temp"), "attribute": "Value"})
	if res.Status != protocol.CommandOK || res.Summary != "Read 2 ノード（"+node("Counter")+"、"+node("Temp")+"）" ||
		valueOf(res.Values, node("Counter")+" Value（Int16）") != "-7" || valueOf(res.Values, node("Temp")+" Value（Float）") != "1.5" {
		t.Errorf("2 ノードの読み取り: %+v", res)
	}
	// 1 つでも読めなければ例外応答にする。読めたノードの値と、読めなかったノードのステータスは出す
	res = send(cmdRead, map[string]interface{}{"nodeId": node("Counter") + "\n" + node("NoSuchNode"), "attribute": "Value"})
	if res.Status != protocol.CommandException || !strings.Contains(res.Error, node("NoSuchNode")) ||
		valueOf(res.Values, node("Counter")+" Value（Int16）") != "-7" || !strings.Contains(valueOf(res.Values, node("NoSuchNode")+" ステータス"), "BadNodeIdUnknown") {
		t.Errorf("読めないノードを含む読み取り: %+v", res)
	}

	// 複数のノードへ 1 回のリクエストで書く（型の違うノードは行に型を書く）
	res = send(cmdWriteMulti, map[string]interface{}{
		"items":    node("Counter") + " := 11\n" + node("Values") + " : Int32[] := 7, 8, 9",
		"dataType": "Int16",
	})
	if res.Status != protocol.CommandOK || len(res.Values) != 2 || !strings.HasPrefix(valueOf(res.Values, node("Values")), "Good") {
		t.Fatalf("複数の書き込み: %+v", res)
	}
	if got := toInt64(acc.mustRead("v-int")); got != 11 {
		t.Errorf("ホストの値 = %v, want 11", got)
	}
	if got := fmt.Sprint(acc.mustRead("v-arr")); got != "[7 8 9]" {
		t.Errorf("ホストの配列 = %v", got)
	}
	// 一部が書けなければ例外応答にする。書けたノードは書かれている
	res = send(cmdWriteMulti, map[string]interface{}{
		"items":    node("Counter") + " := 12\n" + node("Temp") + " : Float := 2",
		"dataType": "Int16",
	})
	if res.Status != protocol.CommandException || !strings.Contains(res.Error, node("Temp")) || !strings.HasPrefix(valueOf(res.Values, node("Counter")), "Good") {
		t.Errorf("一部が書けない書き込み: %+v", res)
	}
	if got := toInt64(acc.mustRead("v-int")); got != 12 {
		t.Errorf("ホストの値 = %v, want 12", got)
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

// subscriptionRecorder は、RunSubscription からの知らせをためる
type subscriptionRecorder struct {
	mu     sync.Mutex
	state  protocol.CommandStatus // 監視全体の最後の状態
	stateN int                    // 「監視中」になった回数
	err    string
	items  map[string]protocol.SubscriptionEvent // 項目ごとの最後の知らせ
	counts map[string]int
}

func newSubscriptionRecorder() *subscriptionRecorder {
	return &subscriptionRecorder{items: map[string]protocol.SubscriptionEvent{}, counts: map[string]int{}}
}

func (r *subscriptionRecorder) onEvent(ev protocol.SubscriptionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ev.ItemID == "" {
		r.state, r.err = ev.Status, ev.Error
		if ev.Status == protocol.CommandOK {
			r.stateN++
		}
		return
	}
	r.items[ev.ItemID] = ev
	r.counts[ev.ItemID]++
}

func (r *subscriptionRecorder) item(id string) protocol.SubscriptionEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.items[id]
}

func (r *subscriptionRecorder) overall() (protocol.CommandStatus, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, r.stateN
}

func TestClientFactory_ExpandMonitoredItems(t *testing.T) {
	f := &ClientFactory{}
	specs, err := f.ExpandMonitoredItems(map[string]interface{}{"nodeId": " ns=2;s=A \n\ni=2258\n"})
	if err != nil || len(specs) != 2 || specs[0].Summary != "ns=2;s=A" || specs[0].Params["nodeId"] != "ns=2;s=A" || specs[1].Params["nodeId"] != "i=2258" {
		t.Fatalf("specs = %+v, %v", specs, err)
	}
	// できた項目の入力を、もう一度入れても同じ項目になる（プロジェクトの読み込み）
	again, err := f.ExpandMonitoredItems(specs[1].Params)
	if err != nil || len(again) != 1 || again[0].Summary != "i=2258" {
		t.Fatalf("again = %+v, %v", again, err)
	}
	for _, in := range []string{"", "ns=2;s=A\nMotor1"} {
		if _, err := f.ExpandMonitoredItems(map[string]interface{}{"nodeId": in}); err == nil {
			t.Errorf("%q: エラーにならない", in)
		}
	}
}

// サブスクリプションで、スカラー・配列・多次元配列の値の変化が届く。切れたら、つなぎ直して続ける
func TestClient_Subscription(t *testing.T) {
	acc := newFakeAccessor()
	acc.publish("v-int", "Counter", "INT", "readwrite", int64(42))
	acc.publish("v-arr", "Values", "ARRAY[0..2] OF DINT", "readwrite", []interface{}{int64(1), int64(2), int64(3)})
	acc.publish("v-mat", "Grid", "ARRAY[0..1, 0..1] OF INT", "readwrite", []interface{}{
		[]interface{}{int64(1), int64(2)}, []interface{}{int64(3), int64(4)},
	})
	srv, _, ns := startTestServer(t, acc)
	url := fmt.Sprintf("opc.tcp://127.0.0.1:%d", srv.config.Port)
	c := newTestClient(t, map[string]interface{}{"endpointUrl": url, "timeoutMs": 3000})
	node := func(name string) map[string]interface{} {
		return map[string]interface{}{"nodeId": fmt.Sprintf("ns=%d;s=%s", ns, name)}
	}

	rec := newSubscriptionRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.(protocol.SubscribingClient).RunSubscription(ctx, protocol.SubscriptionRequest{
			Interval: 50 * time.Millisecond,
			Items: []protocol.MonitoredItemRequest{
				{ID: "counter", Params: node("Counter")},
				{ID: "values", Params: node("Values")},
				{ID: "grid", Params: node("Grid")},
				{ID: "missing", Params: node("NoSuchNode")},
				{ID: "broken", Params: map[string]interface{}{"nodeId": "???"}},
			},
		}, rec.onEvent)
	}()
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			cancel()
			<-done
		}
	}
	defer stop()

	// 始めると、いまの値が届く
	waitFor(t, func() bool {
		st, _ := rec.overall()
		return st == protocol.CommandOK && valueOf(rec.item("counter").Values, "Value（Int16）") == "42" &&
			len(rec.item("values").Values) == 4 && len(rec.item("grid").Values) == 5
	})
	if !c.Connected() {
		t.Error("監視中は接続中")
	}
	if ev := rec.item("values"); valueOf(ev.Values, "Value") != "Int32 の配列（3 個）" || valueOf(ev.Values, "[2]") != "3" {
		t.Errorf("配列: %+v", ev)
	}
	if ev := rec.item("grid"); valueOf(ev.Values, "Value") != "Int16 の配列（2 × 2）" || valueOf(ev.Values, "[1][0]") != "3" {
		t.Errorf("多次元配列: %+v", ev)
	}
	// サーバーにないノード・読めない NodeId は、その項目だけ断られる（ほかの項目は監視できる）
	if ev := rec.item("missing"); ev.Status != protocol.CommandException || !strings.Contains(ev.Error, "BadNodeIdUnknown") {
		t.Errorf("ないノード: %+v", ev)
	}
	if ev := rec.item("broken"); ev.Status != protocol.CommandError || ev.Error == "" {
		t.Errorf("読めない NodeId: %+v", ev)
	}

	// 値が変わると届く
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(acc.WriteVariableValue("v-int", int64(43)))
	must(acc.WriteVariableValue("v-arr", []interface{}{int64(1), int64(20), int64(3)}))
	must(acc.WriteVariableValue("v-mat", []interface{}{[]interface{}{int64(1), int64(2)}, []interface{}{int64(30), int64(4)}}))
	waitFor(t, func() bool {
		return valueOf(rec.item("counter").Values, "Value（Int16）") == "43" &&
			valueOf(rec.item("values").Values, "[1]") == "20" && valueOf(rec.item("grid").Values, "[1][0]") == "30"
	})

	// コマンドがタイムアウト・失敗して接続を閉じても、監視は続く（別のセッションで動く）
	_ = c.Close()
	must(acc.WriteVariableValue("v-int", int64(44)))
	waitFor(t, func() bool { return valueOf(rec.item("counter").Values, "Value（Int16）") == "44" })

	// サーバーを止めると「つながっていない」になり、起動し直すと、つなぎ直して値が届く
	_, okCount := rec.overall()
	must(srv.Stop())
	waitFor(t, func() bool { st, _ := rec.overall(); return st == protocol.CommandError })
	must(acc.WriteVariableValue("v-int", int64(45)))
	must(srv.Start(context.Background()))
	if srv.nodes.ns != ns {
		t.Fatalf("名前空間の番号が変わった: %d → %d", ns, srv.nodes.ns)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		st, n := rec.overall()
		if st == protocol.CommandOK && n > okCount && valueOf(rec.item("counter").Values, "Value（Int16）") == "45" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("起動し直しても戻らない: state=%s counter=%+v", st, rec.item("counter"))
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 止めると、接続を閉じて戻る
	stop()
	if c.Connected() {
		t.Error("止めたら未接続")
	}
}

// つながらない相手でも、取り消せばすぐに戻る。つながらない間は、理由を知らせる
func TestClient_Subscription_Unreachable(t *testing.T) {
	c := newTestClient(t, map[string]interface{}{"endpointUrl": fmt.Sprintf("opc.tcp://127.0.0.1:%d", freePort(t)), "timeoutMs": 500})
	rec := newSubscriptionRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.(protocol.SubscribingClient).RunSubscription(ctx, protocol.SubscriptionRequest{
			Interval: 100 * time.Millisecond,
			Items:    []protocol.MonitoredItemRequest{{ID: "a", Params: map[string]interface{}{"nodeId": "i=2258"}}},
		}, rec.onEvent)
	}()
	waitFor(t, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return rec.state == protocol.CommandError && strings.Contains(rec.err, "接続できませんでした")
	})
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("取り消しても戻らない")
	}
}
