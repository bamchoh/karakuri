package opcua

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bamchoh/uastack/server"
	"github.com/bamchoh/uastack/ua"

	"karakuri/internal/domain/protocol"
)

const (
	// folderNodeName は公開変数をまとめるフォルダ（Objects 直下）
	folderNodeName = "PLCVariables"
	// maxTypeDepth は構造体・配列を展開する最大の深さ（循環定義の保険）
	maxTypeDepth = 32
	// resyncInterval は全変数をホストから読み直す間隔（変更通知の取りこぼし対策）
	resyncInterval = time.Second
)

// changeSubscriber はホストの変数変更ストリームを購読できる accessor（pluginruntime の実装が満たす）
type changeSubscriber interface {
	SubscribeVariableChanges(ctx context.Context, onChange func(variableID string)) error
}

// valueStep はルート値の中をたどる 1 ステップ（構造体フィールドまたは 0 始まりの配列インデックス）
type valueStep struct {
	field   string
	index   int
	isIndex bool
}

// plannedNode は公開する 1 つの変数ノード（ルートまたは子）
type plannedNode struct {
	nodeID    ua.NodeId
	parent    ua.NodeId
	name      string
	dataType  string      // このノードの PLC 型
	shape     nodeShape   // OPC UA での公開形
	steps     []valueStep // ルート値からこのノードの値までのパス
	fieldPath string      // ホストに渡すフィールドパス（外部インデックス）。ルートは ""
}

// publishedVar は公開中の 1 変数とそのノード群
type publishedVar struct {
	info      protocol.NodePublishingInfo
	signature string // ノード構成が変わったかの判定用
	nodes     []*plannedNode
}

// plcNodes は VariableStore の公開変数を uastack のアドレス空間に反映する
type plcNodes struct {
	as           *server.AddressSpace
	ns           uint16
	folder       ua.NodeId
	accessor     protocol.VariableStoreAccessor
	protocolType string
	logf         func(format string, args ...any)

	mu   sync.Mutex
	vars map[string]*publishedVar // variableID → 公開情報

	dirtyMu sync.Mutex
	dirty   map[string]struct{}
	wake    chan struct{}
}

// newPLCNodes は namespaceURI の名前空間に公開変数フォルダを作る
func newPLCNodes(as *server.AddressSpace, namespaceURI string, accessor protocol.VariableStoreAccessor, logf func(string, ...any)) (*plcNodes, error) {
	ns := as.RegisterNamespace(namespaceURI)
	folder := ua.NewStringNodeId(ns, folderNodeName)
	if _, err := as.AddFolder(server.ObjectsFolder, folder, ua.NewQualifiedName(ns, folderNodeName)); err != nil {
		return nil, err
	}
	return &plcNodes{
		as:           as,
		ns:           ns,
		folder:       folder,
		accessor:     accessor,
		protocolType: string(protocol.ProtocolOPCUA),
		logf:         logf,
		vars:         map[string]*publishedVar{},
		dirty:        map[string]struct{}{},
		wake:         make(chan struct{}, 1),
	}, nil
}

// ===== ノード構成の計画 =====

// planVar は変数 1 つ分のノード構成（ルート + 構造体フィールド / 配列要素の子ノード）を作る
func (p *plcNodes) planVar(info protocol.NodePublishingInfo, fieldCache map[string][]protocol.StructFieldInfo) []*plannedNode {
	var out []*plannedNode
	var walk func(nodeStr string, parent ua.NodeId, name, dataType string, steps []valueStep, fieldPath string, depth int)
	walk = func(nodeStr string, parent ua.NodeId, name, dataType string, steps []valueStep, fieldPath string, depth int) {
		nid := ua.NewStringNodeId(p.ns, nodeStr)
		out = append(out, &plannedNode{
			nodeID: nid, parent: parent, name: name, dataType: dataType,
			shape: shapeOf(dataType), steps: steps, fieldPath: fieldPath,
		})
		if depth >= maxTypeDepth {
			return
		}
		if elem, lower, size, ok := parseArrayType(dataType); ok {
			for i := 0; i < size; i++ {
				ext := lower + i
				label := fmt.Sprintf("[%d]", ext)
				walk(nodeStr+label, nid, label, elem, appendStep(steps, valueStep{index: i, isIndex: true}), fieldPath+label, depth+1)
			}
			return
		}
		if isStructType(dataType) {
			fields, ok := fieldCache[dataType]
			if !ok {
				fields = p.accessor.GetStructFields(dataType)
				fieldCache[dataType] = fields
			}
			for _, f := range fields {
				fp := f.Name
				if fieldPath != "" {
					fp = fieldPath + "." + f.Name
				}
				walk(nodeStr+"."+f.Name, nid, f.Name, f.DataType, appendStep(steps, valueStep{field: f.Name}), fp, depth+1)
			}
		}
	}
	walk(info.VariableID, p.folder, info.VariableName, info.DataType, nil, "", 0)
	return out
}

func appendStep(steps []valueStep, s valueStep) []valueStep {
	out := make([]valueStep, len(steps), len(steps)+1)
	copy(out, steps)
	return append(out, s)
}

func signatureOf(info protocol.NodePublishingInfo, nodes []*plannedNode) string {
	var sb strings.Builder
	sb.WriteString(info.VariableName + "|" + info.AccessMode)
	for _, n := range nodes {
		sb.WriteString("|" + n.nodeID.Str + ":" + n.dataType)
	}
	return sb.String()
}

// ===== アドレス空間への反映 =====

// rebuild はホストから公開変数一覧を取り直し、差分だけノードを作り直す
func (p *plcNodes) rebuild() {
	if p.accessor == nil {
		return
	}
	infos := p.accessor.GetEnabledNodePublishings(p.protocolType)
	fieldCache := map[string][]protocol.StructFieldInfo{}

	next := make(map[string]*publishedVar, len(infos))
	for _, info := range infos {
		nodes := p.planVar(info, fieldCache)
		next[info.VariableID] = &publishedVar{info: info, nodes: nodes, signature: signatureOf(info, nodes)}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// 削除・変更された変数のノードを消す
	for varID, old := range p.vars {
		if nv, ok := next[varID]; ok && nv.signature == old.signature {
			next[varID] = old // 変わっていなければ既存ノードをそのまま使う
			continue
		}
		p.deleteNodes(old)
	}
	// 新規・変更された変数のノードを作る
	ids := make([]string, 0, len(next))
	for varID := range next {
		ids = append(ids, varID)
	}
	sort.Strings(ids)
	for _, varID := range ids {
		pv := next[varID]
		if old, ok := p.vars[varID]; ok && old == pv {
			continue
		}
		p.addNodes(pv)
	}
	p.vars = next
}

func (p *plcNodes) deleteNodes(pv *publishedVar) {
	for i := len(pv.nodes) - 1; i >= 0; i-- {
		p.as.DeleteNode(pv.nodes[i].nodeID)
	}
}

func (p *plcNodes) addNodes(pv *publishedVar) {
	root, err := p.accessor.ReadVariableValue(pv.info.VariableID)
	if err != nil {
		p.logf("変数 %s の値を読めません: %v", pv.info.VariableName, err)
	}
	access := accessLevelOf(pv.info.AccessMode)
	for _, n := range pv.nodes {
		if err := p.addNode(pv.info.VariableID, n, access, navigate(root, n.steps)); err != nil {
			p.logf("ノード %s を追加できません: %v", n.nodeID, err)
		}
	}
}

func (p *plcNodes) addNode(varID string, n *plannedNode, access byte, value interface{}) error {
	// ValueRank: -1 = スカラー、1 = 1 次元配列、n = n 次元の Matrix。
	// ArrayDimensions は各次元の要素数（uastack は書き込み時にこれを上限として検査する）
	rank := int32(-1)
	var dims []uint32
	if n.shape.isArray() {
		rank = int32(len(n.shape.dims))
		dims = make([]uint32, len(n.shape.dims))
		for i, d := range n.shape.dims {
			dims[i] = uint32(d)
		}
	}
	node := &server.Node{
		ID:              n.nodeID,
		Class:           ua.NodeClassVariable,
		BrowseName:      ua.NewQualifiedName(p.ns, n.name),
		DisplayName:     ua.NewLocalizedText(n.name),
		Description:     ua.NewLocalizedText(n.dataType),
		DataType:        dataTypeNodeID(n.shape.builtin),
		ValueRank:       rank,
		ArrayDimensions: dims,
		AccessLevel:     access,
		UserAccessLevel: access,
		OnWrite: func(_ server.ReadContext, dv ua.DataValue) ua.StatusCode {
			return p.write(varID, n, dv.Value)
		},
	}
	// 値と型定義を設定してから親の参照を張り、完成したノードだけがブラウズで見えるようにする
	if err := p.as.AddNode(node); err != nil {
		return err
	}
	_ = p.as.SetValue(n.nodeID, toVariant(value, n.shape))
	p.as.AddReference(n.nodeID, server.RefHasTypeDefinition, ua.NewExpandedNodeId(server.TypeBaseDataVariable), true)
	refType := server.RefHasComponent
	if n.parent == p.folder {
		refType = server.RefOrganizes
	}
	p.as.AddReference(n.parent, refType, ua.NewExpandedNodeId(n.nodeID), true)
	return nil
}

func accessLevelOf(mode string) byte {
	switch mode {
	case "read":
		return server.AccessCurrentRead
	case "write":
		return server.AccessCurrentWrite
	default:
		return server.AccessCurrentRead | server.AccessCurrentWrite
	}
}

// write は OPC UA クライアントからの書き込みをホストの VariableStore に反映する
func (p *plcNodes) write(varID string, n *plannedNode, v ua.Variant) ua.StatusCode {
	val, err := fromVariant(v, n.dataType)
	if err != nil {
		p.logf("ノード %s への書き込み値を変換できません: %v", n.nodeID, err)
		return ua.StatusBadTypeMismatch
	}
	if n.fieldPath == "" {
		err = p.accessor.WriteVariableValue(varID, val)
	} else {
		err = p.accessor.WriteVariableFieldValue(varID, n.fieldPath, val)
	}
	if err != nil {
		p.logf("ノード %s への書き込みが拒否されました: %v", n.nodeID, err)
		return ua.StatusBadTypeMismatch
	}
	// 親ノード・子ノードの値も更新する
	p.markDirty(varID)
	return ua.StatusGood
}

// navigate はルート値からステップに沿って子の値を取り出す。見つからなければ nil。
func navigate(v interface{}, steps []valueStep) interface{} {
	for _, s := range steps {
		if s.isIndex {
			arr, ok := v.([]interface{})
			if !ok || s.index < 0 || s.index >= len(arr) {
				return nil
			}
			v = arr[s.index]
			continue
		}
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil
		}
		v = m[s.field]
	}
	return v
}

// ===== 値の同期（ホスト → OPC UA）=====

// refresh は変数の現在値をホストから読み、ルートと全子ノードの値を更新する。
// 値が変わったノードだけ uastack がサブスクリプションに通知する。
func (p *plcNodes) refresh(varID string) {
	p.mu.Lock()
	pv := p.vars[varID]
	p.mu.Unlock()
	if pv == nil {
		return
	}
	root, err := p.accessor.ReadVariableValue(varID)
	if err != nil {
		return
	}
	for _, n := range pv.nodes {
		_ = p.as.SetValue(n.nodeID, toVariant(navigate(root, n.steps), n.shape))
	}
}

func (p *plcNodes) refreshAll() {
	p.mu.Lock()
	ids := make([]string, 0, len(p.vars))
	for id := range p.vars {
		ids = append(ids, id)
	}
	p.mu.Unlock()
	for _, id := range ids {
		p.refresh(id)
	}
}

func (p *plcNodes) markDirty(varID string) {
	p.dirtyMu.Lock()
	p.dirty[varID] = struct{}{}
	p.dirtyMu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *plcNodes) takeDirty() []string {
	p.dirtyMu.Lock()
	defer p.dirtyMu.Unlock()
	ids := make([]string, 0, len(p.dirty))
	for id := range p.dirty {
		ids = append(ids, id)
	}
	clear(p.dirty)
	return ids
}

// run はホストの変更通知と定期的な再同期で、ノードの値を最新に保つ。ctx が終わるまでブロックする。
func (p *plcNodes) run(ctx context.Context) {
	if p.accessor == nil {
		return
	}
	if sub, ok := p.accessor.(changeSubscriber); ok {
		go p.subscribe(ctx, sub)
	}
	ticker := time.NewTicker(resyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
			for _, id := range p.takeDirty() {
				p.refresh(id)
			}
		case <-ticker.C:
			p.refreshAll()
		}
	}
}

func (p *plcNodes) subscribe(ctx context.Context, sub changeSubscriber) {
	for {
		err := sub.SubscribeVariableChanges(ctx, p.markDirty)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			p.logf("変数変更ストリームが切断されました（再接続します）: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
