package opcua

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bamchoh/uastack/ua"
	uaid "github.com/bamchoh/uastack/ua/id"
	"github.com/bamchoh/uastack/uasc"

	"karakuri/internal/domain/protocol"
)

// OPC UA のクライアント。外部の機器（OPC UA サーバー）につないで、サービスのリクエストを送る。
//
// リクエストは「サービスのリクエスト本体」の UA Binary（型の NodeId + 構造体）で受け取る。
// セキュアチャネル・セッション・リクエストヘッダー（認証トークン・ハンドル・時刻）はクライアントが受け持つ。
// 結果の Sent / Received も同じ形（リクエスト本体・応答本体）で、チャネルのヘッダーや暗号化は含まない。
//
// このファイルは、接続先の設定・コマンドの定義・リクエストの組み立てと要約を持つ。送受信は client_conn.go。

const (
	defaultClientEndpoint  = "opc.tcp://127.0.0.1:4840"
	defaultClientTimeoutMs = 3000
	minClientTimeoutMs     = 100
	maxClientTimeoutMs     = 60000

	policyNone = "None"
	modeSign   = "Sign"
	modeSignEn = "SignAndEncrypt"

	cmdRead       = "read"
	cmdWrite      = "write"
	cmdWriteMulti = "writeMulti"
	cmdBrowse     = "browse"

	rankScalar = "scalar"
	rankArray  = "array"
)

// securityPolicies は、設定で選べるセキュリティポリシー（設定の値 → URI）
var securityPolicies = []struct{ name, uri string }{
	{policyNone, uasc.SecurityPolicyNone},
	{"Basic256Sha256", uasc.SecurityPolicyBasic256Sha256},
	{"Aes128_Sha256_RsaOaep", uasc.SecurityPolicyAes128Sha256RsaOaep},
	{"Aes256_Sha256_RsaPss", uasc.SecurityPolicyAes256Sha256RsaPss},
	{"ECC_nistP256", uasc.SecurityPolicyEccNistP256},
	{"ECC_nistP384", uasc.SecurityPolicyEccNistP384},
}

var _ protocol.ClientFactory = (*ClientFactory)(nil)
var _ protocol.RequestHelpProvider = (*ClientFactory)(nil)

// ClientFactory は OPC UA クライアントのファクトリー
type ClientFactory struct{}

// ProtocolType はクライアントのプロトコルの種類を返す
func (f *ClientFactory) ProtocolType() protocol.ProtocolType { return ProtocolOPCUA }

// DisplayName はプロトコルの表示名を返す
func (f *ClientFactory) DisplayName() string { return "OPC UA" }

// RequestHelp は、16 進で入れるリクエストの説明を返す
func (f *ClientFactory) RequestHelp() protocol.RequestHelp {
	return protocol.RequestHelp{
		Description: "サービスのリクエスト本体（型の NodeId + 構造体）を UA Binary で入れます。" +
			"リクエストヘッダーの認証トークン・ハンドル・時刻はクライアントが入れ直します。",
		Example: "01 00 77 02 …",
	}
}

// ConnectionFields は接続先の設定項目を返す
func (f *ClientFactory) ConnectionFields() []protocol.ConfigField {
	policies := make([]protocol.FieldOption, len(securityPolicies))
	for i, p := range securityPolicies {
		policies[i] = protocol.FieldOption{Value: p.name, Label: p.name}
	}
	return []protocol.ConfigField{
		{Name: "endpointUrl", Label: "エンドポイント URL", Description: "接続先のサーバーの URL（opc.tcp://ホスト:ポート）。標準ポートは 4840 です。", Type: "text", Required: true, Default: defaultClientEndpoint},
		{Name: "securityPolicy", Label: "セキュリティポリシー", Description: "None 以外では、クライアントの証明書を exe と同じフォルダの pki-client に作ります（サーバーの側で信頼する設定が要ることがあります）。サーバーの証明書は確かめずに受け入れます。", Type: "select", Required: true, Default: policyNone, Options: policies},
		{Name: "securityMode", Label: "セキュリティモード", Description: "セキュリティポリシーが None のときは使いません。", Type: "select", Required: true, Default: modeSignEn, Options: []protocol.FieldOption{
			{Value: modeSignEn, Label: "SignAndEncrypt（署名と暗号化）"},
			{Value: modeSign, Label: "Sign（署名だけ）"},
		}},
		{Name: "userName", Label: "ユーザー名", Description: "空なら匿名（Anonymous）でつなぎます。", Type: "text", Default: ""},
		{Name: "password", Label: "パスワード", Description: "ユーザー名を入れたときに使います。プロジェクトのファイルには、そのまま（暗号化せずに）保存します。", Type: "password", Default: ""},
		{Name: "timeoutMs", Label: "タイムアウト（ms）", Description: "接続（セッションの作成まで）と応答を待つ時間。過ぎるとタイムアウトにして接続を閉じます。", Type: "number", Required: true, Default: defaultClientTimeoutMs, Min: intPtr(minClientTimeoutMs), Max: intPtr(maxClientTimeoutMs)},
	}
}

type clientSettings struct {
	EndpointURL    string
	SecurityPolicy string // securityPolicies の name
	SecurityMode   string
	UserName       string
	Password       string
	TimeoutMs      int
}

func (s clientSettings) policyURI() string {
	for _, p := range securityPolicies {
		if p.name == s.SecurityPolicy {
			return p.uri
		}
	}
	return uasc.SecurityPolicyNone
}

func (s clientSettings) messageSecurityMode() ua.MessageSecurityMode {
	switch {
	case s.SecurityPolicy == policyNone:
		return ua.MessageSecurityModeNone
	case s.SecurityMode == modeSign:
		return ua.MessageSecurityModeSign
	}
	return ua.MessageSecurityModeSignAndEncrypt
}

func parseClientSettings(settings map[string]interface{}) (clientSettings, error) {
	s := clientSettings{
		EndpointURL: defaultClientEndpoint, SecurityPolicy: policyNone, SecurityMode: modeSignEn,
		TimeoutMs: defaultClientTimeoutMs,
	}
	str := func(key string, dst *string) {
		if v, ok := settings[key].(string); ok {
			*dst = v
		}
	}
	str("endpointUrl", &s.EndpointURL)
	str("securityPolicy", &s.SecurityPolicy)
	str("securityMode", &s.SecurityMode)
	str("userName", &s.UserName)
	str("password", &s.Password)
	switch v := settings["timeoutMs"].(type) {
	case float64:
		s.TimeoutMs = int(v)
	case int:
		s.TimeoutMs = v
	case int64:
		s.TimeoutMs = int(v)
	}
	s.EndpointURL = strings.TrimSpace(s.EndpointURL)
	s.UserName = strings.TrimSpace(s.UserName)

	knownPolicy := false
	for _, p := range securityPolicies {
		knownPolicy = knownPolicy || p.name == s.SecurityPolicy
	}
	switch {
	case s.EndpointURL == "":
		return s, fmt.Errorf("エンドポイント URL を入力してください")
	case !strings.HasPrefix(s.EndpointURL, "opc.tcp://") || len(s.EndpointURL) == len("opc.tcp://"):
		return s, fmt.Errorf("エンドポイント URL は opc.tcp://ホスト:ポート の形で入力してください")
	case !knownPolicy:
		return s, fmt.Errorf("不明なセキュリティポリシーです: %s", s.SecurityPolicy)
	case s.SecurityMode != modeSign && s.SecurityMode != modeSignEn:
		return s, fmt.Errorf("不明なセキュリティモードです: %s", s.SecurityMode)
	case s.TimeoutMs < minClientTimeoutMs || s.TimeoutMs > maxClientTimeoutMs:
		return s, fmt.Errorf("タイムアウトは %d〜%d ms で指定してください", minClientTimeoutMs, maxClientTimeoutMs)
	}
	return s, nil
}

// NormalizeSettings は接続先の設定を検証し、ない項目を既定値で埋めたものを返す
func (f *ClientFactory) NormalizeSettings(settings map[string]interface{}) (map[string]interface{}, error) {
	s, err := parseClientSettings(settings)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"endpointUrl":    s.EndpointURL,
		"securityPolicy": s.SecurityPolicy,
		"securityMode":   s.SecurityMode,
		"userName":       s.UserName,
		"password":       s.Password,
		"timeoutMs":      s.TimeoutMs,
	}, nil
}

// NewClient は接続先の設定からクライアントを作る。つなぐのは最初の送信のとき
func (f *ClientFactory) NewClient(settings map[string]interface{}) (protocol.Client, error) {
	s, err := parseClientSettings(settings)
	if err != nil {
		return nil, err
	}
	return &Client{settings: s, timeout: time.Duration(s.TimeoutMs) * time.Millisecond}, nil
}

// clientPKIDir はクライアントの証明書ストアの場所（アプリの exe と同じフォルダの pki-client）。
// サーバーの pki と分けるのは、アプリケーション URI の違う証明書を取り違えないため
func clientPKIDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "pki-client"
	}
	return filepath.Join(filepath.Dir(exe), "pki-client")
}

// --- コマンド ---

// attributes は、読み取りのコマンドで選べる属性（よく使うもの。ほかの属性は 16 進で入れる）
var attributes = []struct {
	name string
	id   uint32
}{
	{"Value", ua.AttributeIdValue},
	{"DataType", ua.AttributeIdDataType},
	{"ValueRank", ua.AttributeIdValueRank},
	{"ArrayDimensions", ua.AttributeIdArrayDimensions},
	{"AccessLevel", ua.AttributeIdAccessLevel},
	{"UserAccessLevel", ua.AttributeIdUserAccessLevel},
	{"NodeClass", ua.AttributeIdNodeClass},
	{"BrowseName", ua.AttributeIdBrowseName},
	{"DisplayName", ua.AttributeIdDisplayName},
	{"Description", ua.AttributeIdDescription},
}

func attributeName(id uint32) string {
	for _, a := range attributes {
		if a.id == id {
			return a.name
		}
	}
	return fmt.Sprintf("属性 %d", id)
}

// writableTypes は、書き込みのコマンドで選べる型
var writableTypes = []ua.TypeID{
	ua.TypeBoolean, ua.TypeSByte, ua.TypeByte, ua.TypeInt16, ua.TypeUInt16, ua.TypeInt32, ua.TypeUInt32,
	ua.TypeInt64, ua.TypeUInt64, ua.TypeFloat, ua.TypeDouble, ua.TypeString, ua.TypeDateTime,
}

func writableType(name string) (ua.TypeID, bool) {
	for _, t := range writableTypes {
		if t.String() == name {
			return t, true
		}
	}
	return ua.TypeNull, false
}

const nodeIDDescription = "NodeId（例: ns=2;s=Motor1、i=2258）。ns= の番号はサーバーごとに決まります（ブラウズで調べられます）。"

func nodeIDField(def string) protocol.ConfigField {
	return protocol.ConfigField{Name: "nodeId", Label: "NodeId", Description: nodeIDDescription, Type: "text", Required: true, Default: def}
}

// nodeIDsField は、NodeId をいくつも入れられる欄（1 行に 1 つ）
func nodeIDsField(def string) protocol.ConfigField {
	return protocol.ConfigField{Name: "nodeId", Label: "NodeId", Description: nodeIDDescription + "いくつも読むときは、1 行に 1 つずつ入れます（1 回のリクエストでまとめて読みます）。", Type: "textarea", Required: true, Default: def}
}

const writeItemsDescription = "1 行に 1 つ、「NodeId := 値」の形で入れます（例: ns=2;s=Counter := 5）。" +
	"下の「データ型」と違う型のノードは「NodeId : 型 := 値」（例: ns=2;s=Label : String := abc）、" +
	"配列は型に [] を付けて値をカンマで区切ります（例: ns=2;s=Values : Int32[] := 1, 2, 3）。1 回のリクエストでまとめて書きます。"

// Commands は組み立てられるコマンドの一覧を返す
func (f *ClientFactory) Commands() []protocol.CommandDefinition {
	attrOptions := make([]protocol.FieldOption, len(attributes))
	for i, a := range attributes {
		attrOptions[i] = protocol.FieldOption{Value: a.name, Label: a.name}
	}
	typeOptions := make([]protocol.FieldOption, len(writableTypes))
	for i, t := range writableTypes {
		typeOptions[i] = protocol.FieldOption{Value: t.String(), Label: t.String()}
	}
	return []protocol.CommandDefinition{
		{
			ID: cmdRead, DisplayName: "Read（ノードの読み取り）", Pollable: true,
			Fields: []protocol.ConfigField{
				nodeIDsField("i=2258"), // Server_ServerStatus_CurrentTime（どのサーバーにもある）
				{Name: "attribute", Label: "属性", Type: "select", Required: true, Default: "Value", Options: attrOptions},
			},
		},
		{
			ID: cmdWrite, DisplayName: "Write（ノードの値の書き込み）",
			Fields: []protocol.ConfigField{
				nodeIDField(""),
				{Name: "dataType", Label: "データ型", Description: "ノードの型と同じ型を選びます（違うと、サーバーが BadTypeMismatch を返します）。", Type: "select", Required: true, Default: ua.TypeInt16.String(), Options: typeOptions},
				{Name: "rank", Label: "スカラー / 配列", Type: "select", Required: true, Default: rankScalar, Options: []protocol.FieldOption{
					{Value: rankScalar, Label: "スカラー"},
					{Value: rankArray, Label: "配列（値をカンマで区切る）"},
				}},
				{Name: "value", Label: "値", Description: "整数は 0x で始めると 16 進、0b で始めると 2 進。Boolean は true / false。DateTime は 2026-01-02T03:04:05Z の形。", Type: "text", Required: true, Default: "0"},
			},
		},
		{
			ID: cmdWriteMulti, DisplayName: "Write（複数のノードへの書き込み）",
			Fields: []protocol.ConfigField{
				{Name: "items", Label: "書き込む内容", Description: writeItemsDescription, Type: "textarea", Required: true, Default: ""},
				{Name: "dataType", Label: "データ型", Description: "行に型を書かなかったノードの型。", Type: "select", Required: true, Default: ua.TypeInt16.String(), Options: typeOptions},
			},
		},
		{
			ID: cmdBrowse, DisplayName: "Browse（子ノードの一覧）",
			Fields: []protocol.ConfigField{
				nodeIDField("i=85"), // Objects フォルダ
			},
		},
	}
}

func textParam(params map[string]interface{}, key string) string {
	switch v := params[key].(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func nodeIDParam(params map[string]interface{}) (ua.NodeId, error) {
	s := strings.TrimSpace(textParam(params, "nodeId"))
	if s == "" {
		return ua.NodeId{}, fmt.Errorf("NodeId を入力してください")
	}
	n, err := ua.ParseNodeId(s)
	if err != nil {
		return ua.NodeId{}, fmt.Errorf("NodeId として読めません: %q（例: ns=2;s=Motor1、i=2258）", s)
	}
	return n, nil
}

// nodeIDsParam は、1 行に 1 つずつ入れた NodeId を読む（空の行は読み飛ばす）
func nodeIDsParam(params map[string]interface{}) ([]ua.NodeId, error) {
	var ids []ua.NodeId
	for i, line := range strings.Split(textParam(params, "nodeId"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n, err := ua.ParseNodeId(line)
		if err != nil {
			return nil, fmt.Errorf("%d 行目: NodeId として読めません: %q（例: ns=2;s=Motor1、i=2258）", i+1, line)
		}
		ids = append(ids, n)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("NodeId を入力してください")
	}
	return ids, nil
}

// parseWriteLine は「NodeId := 値」「NodeId : 型 := 値」「NodeId : 型[] := 値, 値」の 1 行を読む。
// 型を書いていなければ defaultType のスカラーにする
func parseWriteLine(line string, defaultType ua.TypeID) (ua.WriteValue, error) {
	left, value, ok := strings.Cut(line, ":=")
	if !ok {
		return ua.WriteValue{}, fmt.Errorf("「NodeId := 値」の形で入れてください")
	}
	left = strings.TrimSpace(left)
	typ, isArray := defaultType, false
	// NodeId の中にも ":" がありうるので、最後の ":" のあとが型の名前のときだけ、型の指定として読む
	if i := strings.LastIndex(left, ":"); i >= 0 {
		name := strings.TrimSpace(left[i+1:])
		arr := strings.HasSuffix(name, "[]")
		if t, known := writableType(strings.TrimSpace(strings.TrimSuffix(name, "[]"))); known {
			typ, isArray = t, arr
			left = strings.TrimSpace(left[:i])
		}
	}
	nodeID, err := ua.ParseNodeId(left)
	if err != nil {
		return ua.WriteValue{}, fmt.Errorf("NodeId として読めません: %q（例: ns=2;s=Motor1）", left)
	}
	v, err := parseVariant(typ, isArray, strings.TrimSpace(value))
	if err != nil {
		return ua.WriteValue{}, fmt.Errorf("値: %w", err)
	}
	return ua.WriteValue{NodeId: nodeID, AttributeId: ua.AttributeIdValue, Value: ua.NewDataValue(v)}, nil
}

// BuildRequest は、コマンドと項目の値からリクエスト本体を作る
func (f *ClientFactory) BuildRequest(commandID string, params map[string]interface{}) ([]byte, error) {
	switch commandID {
	case cmdRead:
		nodeIDs, err := nodeIDsParam(params)
		if err != nil {
			return nil, err
		}
		name := textParam(params, "attribute")
		if name == "" {
			name = "Value"
		}
		attr := uint32(0)
		for _, a := range attributes {
			if a.name == name {
				attr = a.id
			}
		}
		if attr == 0 {
			return nil, fmt.Errorf("不明な属性です: %s", name)
		}
		nodes := make([]ua.ReadValueId, len(nodeIDs))
		for i, id := range nodeIDs {
			nodes[i] = ua.ReadValueId{NodeId: id, AttributeId: attr}
		}
		return encodeMessage(&ua.ReadRequest{TimestampsToReturn: ua.TimestampsToReturnNeither, NodesToRead: nodes}), nil

	case cmdWrite:
		nodeID, err := nodeIDParam(params)
		if err != nil {
			return nil, err
		}
		typ, ok := writableType(textParam(params, "dataType"))
		if !ok {
			return nil, fmt.Errorf("不明なデータ型です: %s", textParam(params, "dataType"))
		}
		v, err := parseVariant(typ, textParam(params, "rank") == rankArray, textParam(params, "value"))
		if err != nil {
			return nil, fmt.Errorf("値: %w", err)
		}
		return encodeMessage(&ua.WriteRequest{NodesToWrite: []ua.WriteValue{
			{NodeId: nodeID, AttributeId: ua.AttributeIdValue, Value: ua.NewDataValue(v)},
		}}), nil

	case cmdWriteMulti:
		typ, ok := writableType(textParam(params, "dataType"))
		if !ok {
			return nil, fmt.Errorf("不明なデータ型です: %s", textParam(params, "dataType"))
		}
		var nodes []ua.WriteValue
		for i, line := range strings.Split(textParam(params, "items"), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			wv, err := parseWriteLine(line, typ)
			if err != nil {
				return nil, fmt.Errorf("%d 行目: %w", i+1, err)
			}
			nodes = append(nodes, wv)
		}
		if len(nodes) == 0 {
			return nil, fmt.Errorf("書き込む内容を入力してください（例: ns=2;s=Counter := 5）")
		}
		return encodeMessage(&ua.WriteRequest{NodesToWrite: nodes}), nil

	case cmdBrowse:
		nodeID, err := nodeIDParam(params)
		if err != nil {
			return nil, err
		}
		return encodeMessage(&ua.BrowseRequest{NodesToBrowse: []ua.BrowseDescription{{
			NodeId:          nodeID,
			BrowseDirection: ua.BrowseDirectionForward,
			ReferenceTypeId: ua.NewNumericNodeId(0, uaid.HierarchicalReferences),
			IncludeSubtypes: true,
			ResultMask:      uint32(ua.BrowseResultMaskAll),
		}}}), nil
	}
	return nil, fmt.Errorf("不明なコマンドです: %s", commandID)
}

// parseScalar は文字列を、型 t の Go の値にする
func parseScalar(t ua.TypeID, s string) (interface{}, error) {
	trimmed := strings.TrimSpace(s)
	if t != ua.TypeString && trimmed == "" {
		return nil, fmt.Errorf("値を入力してください")
	}
	parseInt := func(bits int) (int64, error) {
		n, err := strconv.ParseInt(trimmed, 0, bits)
		if err != nil {
			return 0, fmt.Errorf("%q は %s に入る整数ではありません", trimmed, t)
		}
		return n, nil
	}
	parseUint := func(bits int) (uint64, error) {
		n, err := strconv.ParseUint(trimmed, 0, bits)
		if err != nil {
			return 0, fmt.Errorf("%q は %s に入る整数ではありません", trimmed, t)
		}
		return n, nil
	}
	switch t {
	case ua.TypeBoolean:
		switch strings.ToLower(trimmed) {
		case "true", "1", "on":
			return true, nil
		case "false", "0", "off":
			return false, nil
		}
		return nil, fmt.Errorf("%q は true / false ではありません", trimmed)
	case ua.TypeSByte:
		n, err := parseInt(8)
		return int8(n), err
	case ua.TypeByte:
		n, err := parseUint(8)
		return uint8(n), err
	case ua.TypeInt16:
		n, err := parseInt(16)
		return int16(n), err
	case ua.TypeUInt16:
		n, err := parseUint(16)
		return uint16(n), err
	case ua.TypeInt32:
		n, err := parseInt(32)
		return int32(n), err
	case ua.TypeUInt32:
		n, err := parseUint(32)
		return uint32(n), err
	case ua.TypeInt64:
		return parseInt(64)
	case ua.TypeUInt64:
		return parseUint(64)
	case ua.TypeFloat:
		x, err := strconv.ParseFloat(trimmed, 32)
		if err != nil {
			return nil, fmt.Errorf("%q は数値ではありません", trimmed)
		}
		return float32(x), nil
	case ua.TypeDouble:
		x, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return nil, fmt.Errorf("%q は数値ではありません", trimmed)
		}
		return x, nil
	case ua.TypeString:
		return s, nil
	case ua.TypeDateTime:
		tm, err := time.Parse(time.RFC3339Nano, trimmed)
		if err != nil {
			return nil, fmt.Errorf("%q は日時ではありません（例: 2026-01-02T03:04:05Z）", trimmed)
		}
		return tm, nil
	}
	return nil, fmt.Errorf("%s は書き込みのコマンドで使えません", t)
}

// parseVariant は文字列を、型 t のスカラーか配列（カンマ区切り）の Variant にする
func parseVariant(t ua.TypeID, isArray bool, s string) (ua.Variant, error) {
	if !isArray {
		v, err := parseScalar(t, s)
		if err != nil {
			return ua.Variant{}, err
		}
		return ua.TryVariant(v)
	}
	var parts []string
	if strings.TrimSpace(s) != "" {
		parts = strings.Split(s, ",")
	}
	elems := make([]interface{}, len(parts))
	for i, p := range parts {
		if t == ua.TypeString {
			p = strings.TrimSpace(p)
		}
		v, err := parseScalar(t, p)
		if err != nil {
			return ua.Variant{}, fmt.Errorf("%d 個目: %w", i+1, err)
		}
		elems[i] = v
	}
	switch t {
	case ua.TypeByte:
		// []byte をそのまま渡すと ByteString になるので、Byte の配列として作る
		return ua.NewByteArrayVariant(typedSlice(elems, t).([]byte)), nil
	case ua.TypeDateTime:
		times := make([]time.Time, len(elems))
		for i, e := range elems {
			times[i] = e.(time.Time)
		}
		return ua.TryVariant(times)
	}
	return ua.TryVariant(typedSlice(elems, t))
}

// --- リクエスト本体の符号化と要約 ---

// encodeMessage はサービスのリクエスト・応答を、型の NodeId + 構造体の UA Binary にする
func encodeMessage(s ua.Structure) []byte {
	e := ua.NewEncoder(256)
	e.WriteNodeId(s.BinaryEncodingID())
	s.Encode(e)
	return e.Bytes()
}

// decodeRequest は、リクエスト本体（encodeMessage の形）を読む。外から来たバイト列なので、読めなければエラーにする
func decodeRequest(b []byte) (req ua.Request, err error) {
	defer func() {
		if r := recover(); r != nil {
			req, err = nil, fmt.Errorf("リクエストを解釈できません")
		}
	}()
	if len(b) == 0 {
		return nil, fmt.Errorf("リクエストが空です")
	}
	d := ua.NewDecoder(b)
	typeID := d.ReadNodeId()
	if d.Err() != nil {
		return nil, fmt.Errorf("先頭の型の NodeId を読めません")
	}
	s := ua.NewStructureByEncodingID(typeID)
	if s == nil {
		return nil, fmt.Errorf("知らない型です（%s）", typeID)
	}
	r, ok := s.(ua.Request)
	if !ok {
		return nil, fmt.Errorf("サービスのリクエストではありません（%s）", structName(s))
	}
	d.ReadStruct(r)
	if d.Err() != nil {
		return nil, fmt.Errorf("%s の本体を読めません（長さか内容が合いません）", structName(s))
	}
	if n := d.Remaining(); n > 0 {
		return nil, fmt.Errorf("%s のあとに余分な %d バイトがあります", structName(s), n)
	}
	return r, nil
}

// structName は構造体の型の名前（"ReadRequest" など）を返す
func structName(s ua.Structure) string {
	return strings.TrimPrefix(fmt.Sprintf("%T", s), "*ua.")
}

// DescribeRequest はリクエストの内容を 1 行の要約にする
func (f *ClientFactory) DescribeRequest(request []byte) string {
	req, err := decodeRequest(request)
	if err != nil {
		return fmt.Sprintf("解釈できないリクエスト（%d バイト）", len(request))
	}
	return describeRequest(req)
}

func describeRequest(req ua.Request) string {
	switch r := req.(type) {
	case *ua.ReadRequest:
		if len(r.NodesToRead) == 1 {
			n := r.NodesToRead[0]
			return fmt.Sprintf("Read %s の %s", n.NodeId, attributeName(n.AttributeId))
		}
		ids := make([]ua.NodeId, len(r.NodesToRead))
		for i, n := range r.NodesToRead {
			ids[i] = n.NodeId
		}
		return fmt.Sprintf("Read %d ノード（%s）", len(ids), nodeList(ids))
	case *ua.WriteRequest:
		if len(r.NodesToWrite) == 1 {
			n := r.NodesToWrite[0]
			text := fmt.Sprintf("Write %s", n.NodeId)
			if n.AttributeId != ua.AttributeIdValue {
				text += " の " + attributeName(n.AttributeId)
			}
			return fmt.Sprintf("%s = %s（%s）", text, shorten(variantText(n.Value.Value), 60), variantTypeText(n.Value.Value))
		}
		ids := make([]ua.NodeId, len(r.NodesToWrite))
		for i, n := range r.NodesToWrite {
			ids[i] = n.NodeId
		}
		return fmt.Sprintf("Write %d ノード（%s）", len(ids), nodeList(ids))
	case *ua.BrowseRequest:
		if len(r.NodesToBrowse) == 1 {
			return fmt.Sprintf("Browse %s", r.NodesToBrowse[0].NodeId)
		}
		return fmt.Sprintf("Browse %d ノード", len(r.NodesToBrowse))
	}
	return strings.TrimSuffix(structName(req), "Request")
}

// nodeList は NodeId を「、」で並べる（3 つまで。残りは「…」）
func nodeList(ids []ua.NodeId) string {
	parts := make([]string, 0, 4)
	for i, id := range ids {
		if i == 3 {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, id.String())
	}
	return strings.Join(parts, "、")
}

func shorten(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
