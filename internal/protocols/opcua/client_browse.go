package opcua

import (
	"context"
	"errors"
	"fmt"
	"strings"

	uaclient "github.com/bamchoh/uastack/client"
	"github.com/bamchoh/uastack/ua"
	uaid "github.com/bamchoh/uastack/ua/id"

	"karakuri/internal/domain/protocol"
)

// OPC UA クライアントのノードの参照（画面で、ノードをツリーから選ぶために使う）。
//
// 1 回の呼び出しで、親ノードの子（階層の参照の先）を返す。変数のノードは型（DataType・ValueRank）も読み、
// 画面がコマンドの入力欄に入れる内容（NodeId、書き込みの型など）を Fields に入れて返す。
// コマンドと同じ接続（セッション）を使う。

const (
	// maxBrowseEntries は、1 つの親について返す子ノードの上限
	maxBrowseEntries = 2000
	// browseBatch は、1 回のリクエストに入れるノードの数（サーバーの 1 回の上限を超えないようにする）
	browseBatch = 100
)

var _ protocol.BrowsableClient = (*Client)(nil)

// connectLocked は、コマンド用の接続を返す（なければつなぐ）。c.mu を持って呼ぶ
func (c *Client) connectLocked(ctx context.Context) (*uaclient.Client, error) {
	conn := c.conn.Load()
	if conn != nil && conn.State() != uaclient.StateConnected {
		c.closeLocked(closeWaitQuick)
		conn = nil
	}
	if conn != nil {
		return conn, nil
	}
	conn, err := c.dial(ctx)
	if err != nil {
		if isTimeout(err) {
			return nil, fmt.Errorf("%s に接続できませんでした（タイムアウト）", c.settings.EndpointURL)
		}
		return nil, fmt.Errorf("%s に接続できませんでした: %v", c.settings.EndpointURL, err)
	}
	c.conn.Store(conn)
	return conn, nil
}

// Browse は、parent の子ノードを返す。parent が空なら Objects フォルダの子を返す
func (c *Client) Browse(ctx context.Context, parent string) ([]protocol.BrowseEntry, error) {
	parentID := ua.NewNumericNodeId(0, uaid.ObjectsFolder)
	if strings.TrimSpace(parent) != "" {
		id, err := ua.ParseNodeId(strings.TrimSpace(parent))
		if err != nil {
			return nil, fmt.Errorf("NodeId として読めません: %q", parent)
		}
		parentID = id
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	conn, err := c.connectLocked(ctx)
	if err != nil {
		return nil, err
	}
	// 失敗したときの扱いは Send と同じ: タイムアウト・通信エラーなら接続を閉じる。サーバーが断っただけなら閉じない
	fail := func(what string, err error) error {
		var st ua.StatusCode
		switch {
		case isTimeout(err):
			c.closeLocked(closeWaitQuick)
			return fmt.Errorf("%s: %d ms 以内に応答がありませんでした", what, c.timeout.Milliseconds())
		case errors.As(err, &st):
			if sessionLost(st) {
				c.closeLocked(closeWaitQuick)
			}
			return fmt.Errorf("%s: %s", what, statusText(st))
		}
		c.closeLocked(closeWaitQuick)
		return fmt.Errorf("%s: %v", what, err)
	}

	refs, err := conn.BrowseChildren(ctx, parentID)
	if err != nil {
		return nil, fail("子ノードを読めませんでした", err)
	}

	entries := make([]protocol.BrowseEntry, 0, min(len(refs), maxBrowseEntries))
	ids := make([]ua.NodeId, 0, cap(entries))
	seen := make(map[ua.NodeId]bool, len(refs))
	for _, ref := range refs {
		// ほかのサーバーのノードは、このセッションでは読めないので出さない
		if !ref.NodeId.IsLocal() || ref.NodeId.NamespaceURI != "" || seen[ref.NodeId.NodeId] {
			continue
		}
		if len(entries) >= maxBrowseEntries {
			break
		}
		seen[ref.NodeId.NodeId] = true
		id := ref.NodeId.NodeId
		name := ref.DisplayName.Text
		if name == "" {
			name = ref.BrowseName.Name
		}
		kind := nodeClassNames[ref.NodeClass]
		if kind == "" {
			kind = fmt.Sprintf("NodeClass %d", ref.NodeClass)
		}
		entries = append(entries, protocol.BrowseEntry{
			Address:     id.String(),
			Name:        name,
			Kind:        kind,
			Selectable:  ref.NodeClass == ua.NodeClassVariable,
			HasChildren: true, // 下で調べる。調べられなかったら、開けるようにしておく
		})
		ids = append(ids, id)
	}

	// 子があるかどうか（ツリーで開けるか）は、失敗しても一覧は返す
	if has, err := c.hasChildren(ctx, conn, ids); err == nil {
		for i := range entries {
			entries[i].HasChildren = has[i]
		}
	} else if ctx.Err() != nil || conn.State() != uaclient.StateConnected {
		return nil, fail("子ノードを読めませんでした", err)
	}

	// 変数のノードは型を読み、入力欄に入れる内容を作る
	types, err := c.variableTypes(ctx, conn, entries, ids)
	if err != nil {
		return nil, fail("ノードの型を読めませんでした", err)
	}
	for i := range entries {
		if !entries[i].Selectable {
			continue
		}
		entries[i].Detail = types[i].text()
		entries[i].Fields = browseFields(entries[i].Address, types[i])
	}
	return entries, nil
}

// hasChildren は、ノードごとに、階層の参照の先が 1 つでもあるかを返す
func (c *Client) hasChildren(ctx context.Context, conn *uaclient.Client, ids []ua.NodeId) ([]bool, error) {
	result := make([]bool, len(ids))
	for start := 0; start < len(ids); start += browseBatch {
		batch := ids[start:min(start+browseBatch, len(ids))]
		req := &ua.BrowseRequest{RequestedMaxReferencesPerNode: 1}
		for _, id := range batch {
			req.NodesToBrowse = append(req.NodesToBrowse, ua.BrowseDescription{
				NodeId:          id,
				BrowseDirection: ua.BrowseDirectionForward,
				ReferenceTypeId: ua.NewNumericNodeId(0, uaid.HierarchicalReferences),
				IncludeSubtypes: true,
			})
		}
		resp, err := conn.Browse(ctx, req)
		if err != nil {
			return nil, err
		}
		if len(resp.Results) != len(batch) {
			return nil, fmt.Errorf("Browse の結果の数が合いません")
		}
		var points [][]byte
		for i, r := range resp.Results {
			// 調べられなかったノードは、開けるようにしておく
			result[start+i] = r.StatusCode.IsBad() || len(r.References) > 0
			if len(r.ContinuationPoint) > 0 {
				points = append(points, r.ContinuationPoint)
			}
		}
		if len(points) > 0 {
			_, _ = conn.BrowseNext(ctx, &ua.BrowseNextRequest{ReleaseContinuationPoints: true, ContinuationPoints: points})
		}
	}
	return result, nil
}

// variableType は、変数のノードの型（DataType と ValueRank）
type variableType struct {
	dataType ua.NodeId
	known    bool // dataType を読めた
	isArray  bool // ValueRank が 1 以上
}

// builtin は、組み込み型（書き込みのコマンドで選べる型）なら、その型を返す
func (t variableType) builtin() (ua.TypeID, bool) {
	if !t.known || t.dataType.Namespace != 0 || t.dataType.Type != ua.IDNumeric || t.dataType.Numeric < 1 || t.dataType.Numeric > 25 {
		return ua.TypeNull, false
	}
	typ := ua.TypeID(t.dataType.Numeric)
	if _, ok := writableType(typ.String()); !ok {
		return ua.TypeNull, false
	}
	return typ, true
}

// text は、ツリーに出す型の表示（"Int16"、"Int32[]"）
func (t variableType) text() string {
	if !t.known {
		return ""
	}
	name := t.dataType.String()
	if t.dataType.Namespace == 0 && t.dataType.Type == ua.IDNumeric && t.dataType.Numeric >= 1 && t.dataType.Numeric <= 25 {
		name = ua.TypeID(t.dataType.Numeric).String()
	}
	if t.isArray {
		name += "[]"
	}
	return name
}

// variableTypes は、変数のノード（entries で Selectable のもの）の DataType と ValueRank をまとめて読む
func (c *Client) variableTypes(ctx context.Context, conn *uaclient.Client, entries []protocol.BrowseEntry, ids []ua.NodeId) ([]variableType, error) {
	types := make([]variableType, len(ids))
	var targets []int // 変数のノードの添字
	for i := range entries {
		if entries[i].Selectable {
			targets = append(targets, i)
		}
	}
	for start := 0; start < len(targets); start += browseBatch {
		batch := targets[start:min(start+browseBatch, len(targets))]
		req := &ua.ReadRequest{TimestampsToReturn: ua.TimestampsToReturnNeither}
		for _, i := range batch {
			req.NodesToRead = append(req.NodesToRead,
				ua.ReadValueId{NodeId: ids[i], AttributeId: ua.AttributeIdDataType},
				ua.ReadValueId{NodeId: ids[i], AttributeId: ua.AttributeIdValueRank})
		}
		resp, err := conn.Read(ctx, req)
		if err != nil {
			return nil, err
		}
		if len(resp.Results) != len(req.NodesToRead) {
			return nil, fmt.Errorf("Read の結果の数が合いません")
		}
		for n, i := range batch {
			dt, rank := resp.Results[n*2], resp.Results[n*2+1]
			if id, ok := dt.Value.Value().(ua.NodeId); ok && !dt.Status.IsBad() {
				types[i].dataType, types[i].known = id, true
			}
			if r, ok := rank.Value.Value().(int32); ok && !rank.Status.IsBad() {
				types[i].isArray = r >= 1
			}
		}
	}
	return types, nil
}

// browseFields は、選んだノードを入力欄に入れる内容を返す（キーは、コマンド・監視項目の項目の名前）。
// - nodeId: Read・Write・Browse・監視項目の NodeId
// - dataType / rank: Write の型（組み込み型のときだけ）
// - items: Write（複数のノード）の 1 行（値は空。「NodeId : 型 := 」）
func browseFields(address string, t variableType) map[string]string {
	fields := map[string]string{"nodeId": address, "items": address + " := "}
	if typ, ok := t.builtin(); ok {
		fields["dataType"] = typ.String()
		fields["rank"] = rankScalar
		suffix := ""
		if t.isArray {
			fields["rank"] = rankArray
			suffix = "[]"
		}
		fields["items"] = fmt.Sprintf("%s : %s%s := ", address, typ, suffix)
	}
	return fields
}
