package opcua

import (
	"testing"

	"github.com/bamchoh/uastack/ua"
)

// ファズテスト。利用者が 16 進で入れたリクエスト本体を読んで要約しても、panic せず、
// でたらめな配列の長さで大きなメモリを取らない（recover では止められない）ことを確かめる。
// ふだんの go test では種（f.Add）だけを流す。探すときは go test -fuzz=FuzzDescribeRequest -fuzztime=1m ./internal/protocols/opcua
func FuzzDescribeRequest(f *testing.F) {
	ns := ua.NewStringNodeId(2, "Counter")
	f.Add(encodeMessage(&ua.ReadRequest{TimestampsToReturn: ua.TimestampsToReturnNeither, NodesToRead: []ua.ReadValueId{{NodeId: ns, AttributeId: 13}}}))
	f.Add(encodeMessage(&ua.WriteRequest{NodesToWrite: []ua.WriteValue{{NodeId: ns, AttributeId: 13, Value: ua.DataValue{Value: ua.NewVariant(int16(1))}}}}))
	f.Add(encodeMessage(&ua.BrowseRequest{NodesToBrowse: []ua.BrowseDescription{{NodeId: ns}}}))
	f.Add(encodeMessage(&ua.RegisterNodesRequest{NodesToRegister: []ua.NodeId{ns}}))
	f.Add(encodeMessage(&ua.ReadResponse{}))
	f.Add([]byte{0x01, 0x00, 0x77, 0x02, 0xFF, 0xFF, 0xFF, 0x7F}) // ReadRequest の型で、配列の長さが大きい
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		_ = (&ClientFactory{}).DescribeRequest(b)
	})
}
