package opcua

import (
	"context"
	"reflect"
	"testing"

	"github.com/bamchoh/uastack/client"
	"github.com/bamchoh/uastack/server"
	"github.com/bamchoh/uastack/ua"
)

// readRankAndDims は ValueRank と ArrayDimensions 属性を読む（UaExpert が表示する値）
func readRankAndDims(t *testing.T, c *client.Client, nid ua.NodeId) (int32, []uint32) {
	t.Helper()
	resp, err := c.Read(context.Background(), &ua.ReadRequest{NodesToRead: []ua.ReadValueId{
		{NodeId: nid, AttributeId: server.AttrValueRank},
		{NodeId: nid, AttributeId: server.AttrArrayDimensions},
	}})
	if err != nil {
		t.Fatalf("read attributes of %v: %v", nid, err)
	}
	rank, _ := resp.Results[0].Value.Value().(int32)
	dims, _ := resp.Results[1].Value.Value().([]uint32)
	return rank, dims
}

func TestServer_MultiDimensionalArrayIsMatrix(t *testing.T) {
	const dataType = "ARRAY[0..1, 0..2, 0..3] OF INT"
	// 値は 100*i + 10*j + k
	val := make([]interface{}, 2)
	for i := range val {
		plane := make([]interface{}, 3)
		for j := range plane {
			row := make([]interface{}, 4)
			for k := range row {
				row[k] = int64(100*i + 10*j + k)
			}
			plane[j] = row
		}
		val[i] = plane
	}
	acc := newFakeAccessor()
	acc.publish("v-3d", "Cube", dataType, "readwrite", val)
	_, c, ns := startTestServer(t, acc)
	ctx := context.Background()
	root := ua.NewStringNodeId(ns, "v-3d")

	// ルートは 3 次元の Matrix
	if rank, dims := readRankAndDims(t, c, root); rank != 3 || !reflect.DeepEqual(dims, []uint32{2, 3, 4}) {
		t.Errorf("root ValueRank=%d ArrayDimensions=%v, want 3 / [2 3 4]", rank, dims)
	}
	dv, err := c.ReadValue(ctx, root)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	if !reflect.DeepEqual(dv.Value.Dimensions(), []int32{2, 3, 4}) {
		t.Errorf("value dimensions = %v", dv.Value.Dimensions())
	}
	flat, _ := dv.Value.Value().([]int16)
	if len(flat) != 24 || flat[1*12+2*4+3] != 123 { // [1][2][3] は行優先で 1*12 + 2*4 + 3
		t.Errorf("root value = %v", flat)
	}

	// 子ノードは 1 次元ずつ減る: [1] → 2 次元、[1][2] → 1 次元、[1][2][3] → スカラー
	if rank, dims := readRankAndDims(t, c, ua.NewStringNodeId(ns, "v-3d[1]")); rank != 2 || !reflect.DeepEqual(dims, []uint32{3, 4}) {
		t.Errorf("[1] ValueRank=%d ArrayDimensions=%v, want 2 / [3 4]", rank, dims)
	}
	if rank, dims := readRankAndDims(t, c, ua.NewStringNodeId(ns, "v-3d[1][2]")); rank != 1 || !reflect.DeepEqual(dims, []uint32{4}) {
		t.Errorf("[1][2] ValueRank=%d ArrayDimensions=%v, want 1 / [4]", rank, dims)
	}
	dv, err = c.ReadValue(ctx, ua.NewStringNodeId(ns, "v-3d[1][2][3]"))
	if err != nil || dv.Value.Value() != int16(123) {
		t.Errorf("[1][2][3] = %#v, %v", dv.Value.Value(), err)
	}

	// Matrix のままルートに書き込むと、ホストには入れ子の配列で届く
	newFlat := make([]int16, 24)
	for i := range newFlat {
		newFlat[i] = int16(-i)
	}
	mv, err := ua.NewMatrixVariant(newFlat, []int32{2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WriteValue(ctx, root, mv); err != nil {
		t.Fatalf("write matrix: %v", err)
	}
	host := acc.mustRead("v-3d").([]interface{})
	if got := toInt64(host[1].([]interface{})[2].([]interface{})[3]); got != -23 {
		t.Errorf("host [1][2][3] = %d, want -23", got)
	}

	// 1 次元の配列を 3 次元のノードに書くのは型の不一致として拒否される
	if err := c.WriteValue(ctx, root, ua.NewVariant(newFlat)); err == nil {
		t.Errorf("1-D write to 3-D node succeeded")
	}
}
