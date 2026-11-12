package opcua

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	uaclient "github.com/bamchoh/uastack/client"
	"github.com/bamchoh/uastack/ua"

	"karakuri/internal/domain/protocol"
)

// OPC UA クライアントの送受信と、応答の解釈。

const (
	// maxResultRows は、結果に入れる行（配列の要素・ブラウズの参照）の上限
	maxResultRows = 1000
	// 接続を閉じるとき、セッションを閉じる応答を待つ時間。応答しないサーバーを閉じるとき（タイムアウトのあと）は短くする
	closeWait      = time.Second
	closeWaitQuick = 200 * time.Millisecond
)

// Client は OPC UA のクライアント。接続（セキュアチャネルとセッション）は最初の Send でつなぎ、
// タイムアウト・通信エラーになったら閉じる（次の Send でつなぎ直す）
type Client struct {
	settings clientSettings
	timeout  time.Duration

	mu   sync.Mutex                      // Send と Close を 1 つずつにする
	conn atomic.Pointer[uaclient.Client] // 書くのは mu の中だけ。Connected はロックなしで読む
}

var _ protocol.Client = (*Client)(nil)

// Connected は接続中かどうかを返す
func (c *Client) Connected() bool {
	conn := c.conn.Load()
	return conn != nil && conn.State() == uaclient.StateConnected
}

// Close は接続を閉じる（送信中なら、終わるまで待つ）
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked(closeWait)
	return nil
}

func (c *Client) closeLocked(wait time.Duration) {
	conn := c.conn.Swap(nil)
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_ = conn.Close(ctx)
}

func (c *Client) dial(ctx context.Context) (*uaclient.Client, error) {
	host, _ := os.Hostname()
	if host == "" {
		host = "localhost"
	}
	cfg := uaclient.Config{
		ApplicationName: "Karakuri",
		ApplicationURI:  "urn:" + host + ":karakuri:client",
		ProductURI:      "urn:karakuri",
		SecurityPolicy:  c.settings.policyURI(),
		SecurityMode:    c.settings.messageSecurityMode(),
		RequestTimeout:  c.timeout,
	}
	if c.settings.SecurityPolicy != policyNone {
		cfg.PKIDir = clientPKIDir()
		cfg.AutoAcceptUntrustedCertificates = true
	}
	if c.settings.UserName != "" {
		cfg.Identity = uaclient.UserName(c.settings.UserName, c.settings.Password)
	}
	return uaclient.Dial(ctx, c.settings.EndpointURL, cfg)
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}

// sessionLost は、セッションやチャネルが使えなくなったことを示すステータスかどうか（つなぎ直す）
func sessionLost(st ua.StatusCode) bool {
	switch st.Code() {
	case ua.StatusBadSessionIdInvalid, ua.StatusBadSessionClosed, ua.StatusBadSessionNotActivated,
		ua.StatusBadSecureChannelIdInvalid, ua.StatusBadSecureChannelClosed:
		return true
	}
	return false
}

func statusText(st ua.StatusCode) string {
	return fmt.Sprintf("%s (0x%08X)", st, uint32(st))
}

// Send はリクエスト本体を送って応答を待つ
func (c *Client) Send(ctx context.Context, request []byte) (result protocol.CommandResult) {
	req, err := decodeRequest(request)
	if err != nil {
		result.Summary = fmt.Sprintf("解釈できないリクエスト（%d バイト）", len(request))
		result.Status = protocol.CommandError
		result.Error = err.Error()
		return result
	}
	result.Summary = describeRequest(req)

	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	fail := func(status protocol.CommandStatus, msg string, wait time.Duration) protocol.CommandResult {
		c.closeLocked(wait)
		result.Status = status
		result.Error = msg
		result.Elapsed = time.Since(start)
		return result
	}

	conn := c.conn.Load()
	if conn != nil && conn.State() != uaclient.StateConnected {
		// サーバーの側から切られていた。つなぎ直す
		c.closeLocked(closeWaitQuick)
		conn = nil
	}
	if conn == nil {
		conn, err = c.dial(ctx)
		if err != nil {
			url := c.settings.EndpointURL
			switch {
			case parent.Err() != nil:
				return fail(protocol.CommandError, "取り消されました", 0)
			case isTimeout(err):
				return fail(protocol.CommandTimeout, fmt.Sprintf("%s に接続できませんでした（タイムアウト）", url), 0)
			}
			return fail(protocol.CommandError, fmt.Sprintf("%s に接続できませんでした: %v", url, err), 0)
		}
		c.conn.Store(conn)
	}

	resp, err := conn.Send(ctx, req)
	result.Elapsed = time.Since(start)
	result.Sent = encodeMessage(req) // ヘッダー（認証トークン・ハンドル・時刻）はクライアントが入れたあとのもの
	if resp != nil {
		result.Received = encodeMessage(resp)
	}
	if err != nil {
		var st ua.StatusCode
		switch {
		case parent.Err() != nil:
			return fail(protocol.CommandError, "取り消されました", closeWaitQuick)
		case isTimeout(err):
			return fail(protocol.CommandTimeout, fmt.Sprintf("%d ms 以内に応答がありませんでした", c.timeout.Milliseconds()), closeWaitQuick)
		case errors.As(err, &st):
			// サーバーがサービス全体を失敗にした（ServiceFault など）。接続はそのまま使える
			result.Status = protocol.CommandException
			result.Error = "サーバーがエラーを返しました: " + statusText(st)
			result.Values = []protocol.ResultValue{{Label: "ServiceResult", Value: statusText(st)}}
			if sessionLost(st) {
				c.closeLocked(closeWaitQuick)
			}
			return result
		}
		return fail(protocol.CommandError, fmt.Sprintf("応答を受け取れませんでした: %v", err), closeWaitQuick)
	}

	result.Status, result.Values, result.Error = describeResponse(req, resp)

	// ブラウズの続き（ContinuationPoint）は使わないので、サーバーに捨ててもらう
	if br, ok := resp.(*ua.BrowseResponse); ok {
		var points [][]byte
		for _, r := range br.Results {
			if len(r.ContinuationPoint) > 0 {
				points = append(points, r.ContinuationPoint)
			}
		}
		if len(points) > 0 {
			_, _ = conn.BrowseNext(ctx, &ua.BrowseNextRequest{ReleaseContinuationPoints: true, ContinuationPoints: points})
		}
	}
	return result
}

// --- 応答の解釈 ---

// describeResponse は応答の内容を、結果の種類・値の一覧・エラーの文にする
func describeResponse(req ua.Request, resp ua.Response) (protocol.CommandStatus, []protocol.ResultValue, string) {
	switch r := resp.(type) {
	case *ua.ReadResponse:
		rr, _ := req.(*ua.ReadRequest)
		return describeRead(rr, r)
	case *ua.WriteResponse:
		wr, _ := req.(*ua.WriteRequest)
		return describeWrite(wr, r)
	case *ua.BrowseResponse:
		return describeBrowse(r)
	}
	return protocol.CommandOK, []protocol.ResultValue{{Label: "応答", Value: structName(resp)}}, ""
}

func describeRead(req *ua.ReadRequest, resp *ua.ReadResponse) (protocol.CommandStatus, []protocol.ResultValue, string) {
	if len(resp.Results) == 0 {
		return protocol.CommandError, nil, "応答に結果がありません"
	}
	status := protocol.CommandOK
	errMsg := ""
	var values []protocol.ResultValue
	for i, dv := range resp.Results {
		attr := "Value"
		prefix := ""
		if req != nil && i < len(req.NodesToRead) {
			attr = attributeName(req.NodesToRead[i].AttributeId)
			if len(resp.Results) > 1 {
				prefix = req.NodesToRead[i].NodeId.String() + " "
			}
		}
		if dv.Status.IsBad() {
			if errMsg == "" {
				errMsg = "読めませんでした: " + statusText(dv.Status)
				if prefix != "" {
					errMsg = fmt.Sprintf("読めませんでした（%s）: %s", strings.TrimSpace(prefix), statusText(dv.Status))
				}
			}
			status = protocol.CommandException
			values = append(values, protocol.ResultValue{Label: prefix + "ステータス", Value: statusText(dv.Status)})
			continue
		}
		values = append(values, variantRows(prefix, attr, dv.Value)...)
		if dv.Status != 0 {
			values = append(values, protocol.ResultValue{Label: prefix + "ステータス", Value: statusText(dv.Status)})
		}
	}
	return status, values, errMsg
}

func describeWrite(req *ua.WriteRequest, resp *ua.WriteResponse) (protocol.CommandStatus, []protocol.ResultValue, string) {
	if len(resp.Results) == 0 {
		return protocol.CommandError, nil, "応答に結果がありません"
	}
	status := protocol.CommandOK
	errMsg := ""
	values := make([]protocol.ResultValue, len(resp.Results))
	for i, st := range resp.Results {
		label := "結果"
		if len(resp.Results) > 1 {
			label = fmt.Sprintf("結果 %d", i+1)
			if req != nil && i < len(req.NodesToWrite) {
				label = req.NodesToWrite[i].NodeId.String()
			}
		}
		values[i] = protocol.ResultValue{Label: label, Value: statusText(st)}
		if st.IsBad() {
			status = protocol.CommandException
			if errMsg == "" {
				errMsg = "書けませんでした: " + statusText(st)
				if len(resp.Results) > 1 {
					errMsg = fmt.Sprintf("書けませんでした（%s）: %s", label, statusText(st))
				}
			}
		}
	}
	return status, values, errMsg
}

var nodeClassNames = map[ua.NodeClass]string{
	ua.NodeClassObject: "Object", ua.NodeClassVariable: "Variable", ua.NodeClassMethod: "Method",
	ua.NodeClassObjectType: "ObjectType", ua.NodeClassVariableType: "VariableType",
	ua.NodeClassReferenceType: "ReferenceType", ua.NodeClassDataType: "DataType", ua.NodeClassView: "View",
}

func describeBrowse(resp *ua.BrowseResponse) (protocol.CommandStatus, []protocol.ResultValue, string) {
	if len(resp.Results) == 0 {
		return protocol.CommandError, nil, "応答に結果がありません"
	}
	status := protocol.CommandOK
	errMsg := ""
	var values []protocol.ResultValue
	total, more := 0, false
	for _, r := range resp.Results {
		if r.StatusCode.IsBad() {
			status = protocol.CommandException
			if errMsg == "" {
				errMsg = "ブラウズできませんでした: " + statusText(r.StatusCode)
			}
			values = append(values, protocol.ResultValue{Label: "ステータス", Value: statusText(r.StatusCode)})
			continue
		}
		more = more || len(r.ContinuationPoint) > 0
		for _, ref := range r.References {
			total++
			if len(values) >= maxResultRows {
				more = true
				continue
			}
			name := ref.DisplayName.Text
			if name == "" {
				name = ref.BrowseName.Name
			}
			class := nodeClassNames[ref.NodeClass]
			if class == "" {
				class = fmt.Sprintf("NodeClass %d", ref.NodeClass)
			}
			values = append(values, protocol.ResultValue{Label: name, Value: fmt.Sprintf("%s（%s）", ref.NodeId, class)})
		}
	}
	if status == protocol.CommandOK && total == 0 {
		values = append(values, protocol.ResultValue{Label: "子ノード", Value: "なし"})
	}
	if more {
		values = append(values, protocol.ResultValue{Label: "続き", Value: fmt.Sprintf("あり（%d 個まで表示しています）", total)})
	}
	return status, values, errMsg
}

// variantRows は読み取った値を行にする。スカラーは 1 行（「Value（Int16）」）、配列は型の行と、要素ごとの行
func variantRows(prefix, attr string, v ua.Variant) []protocol.ResultValue {
	if v.IsNull() {
		return []protocol.ResultValue{{Label: prefix + attr, Value: "null"}}
	}
	if !v.IsArray() {
		return []protocol.ResultValue{{
			Label: fmt.Sprintf("%s%s（%s）", prefix, attr, v.Type()),
			Value: valueText(v.Value()),
		}}
	}
	rv := reflect.ValueOf(v.Value())
	n := rv.Len()
	rows := make([]protocol.ResultValue, 0, min(n, maxResultRows)+2)
	rows = append(rows, protocol.ResultValue{Label: prefix + attr, Value: variantTypeText(v)})
	dims := v.Dimensions()
	for i := 0; i < n && i < maxResultRows; i++ {
		rows = append(rows, protocol.ResultValue{Label: prefix + indexLabel(i, dims), Value: valueText(rv.Index(i).Interface())})
	}
	if n > maxResultRows {
		rows = append(rows, protocol.ResultValue{Label: prefix + "…", Value: fmt.Sprintf("ほか %d 個", n-maxResultRows)})
	}
	return rows
}

// indexLabel は、平らに並べた配列の i 番目の添字（"[3]"、多次元なら "[1][0]"）を返す
func indexLabel(i int, dims []int32) string {
	if len(dims) < 2 {
		return fmt.Sprintf("[%d]", i)
	}
	idx := make([]int, len(dims))
	for d := len(dims) - 1; d >= 0; d-- {
		size := int(dims[d])
		if size <= 0 {
			return fmt.Sprintf("[%d]", i)
		}
		idx[d] = i % size
		i /= size
	}
	var sb strings.Builder
	for _, x := range idx {
		fmt.Fprintf(&sb, "[%d]", x)
	}
	return sb.String()
}

// variantTypeText は Variant の型（"Int16"、"Int16 の配列（3 個）"）を返す
func variantTypeText(v ua.Variant) string {
	if v.IsNull() {
		return "null"
	}
	if !v.IsArray() {
		return v.Type().String()
	}
	if dims := v.Dimensions(); len(dims) > 1 {
		parts := make([]string, len(dims))
		for i, d := range dims {
			parts[i] = strconv.Itoa(int(d))
		}
		return fmt.Sprintf("%s の配列（%s）", v.Type(), strings.Join(parts, " × "))
	}
	return fmt.Sprintf("%s の配列（%d 個）", v.Type(), v.ArrayLength())
}

// variantText は Variant の値を 1 行の文字列にする（配列は "[1, 2, 3]"）
func variantText(v ua.Variant) string {
	if v.IsNull() {
		return "null"
	}
	if !v.IsArray() {
		return valueText(v.Value())
	}
	rv := reflect.ValueOf(v.Value())
	parts := make([]string, 0, rv.Len())
	for i := 0; i < rv.Len() && i < 20; i++ {
		parts = append(parts, valueText(rv.Index(i).Interface()))
	}
	if rv.Len() > 20 {
		parts = append(parts, "…")
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// valueText は組み込み型の値 1 つを表示用の文字列にする
func valueText(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case time.Time:
		if x.IsZero() {
			return "（なし）"
		}
		return x.Format(time.RFC3339Nano)
	case []byte:
		if len(x) == 0 {
			return "（空）"
		}
		return strings.ToUpper(hex.EncodeToString(x))
	case ua.LocalizedText:
		return x.Text
	case ua.QualifiedName:
		return x.String()
	case ua.NodeId:
		// DataType 属性などで返る組み込み型の NodeId（i=1〜25）には、型の名前を添える
		if x.Namespace == 0 && x.Type == ua.IDNumeric && x.Numeric >= 1 && x.Numeric <= 25 {
			return fmt.Sprintf("%s（%s）", x, ua.TypeID(x.Numeric))
		}
		return x.String()
	case ua.StatusCode:
		return statusText(x)
	case ua.ExtensionObject:
		if x.Value == nil {
			return "null"
		}
		if b, ok := x.Value.([]byte); ok {
			return fmt.Sprintf("ExtensionObject %s（%d バイト）", x.TypeID, len(b))
		}
		return shorten(fmt.Sprintf("%+v", x.Value), 200)
	}
	return fmt.Sprint(v)
}
