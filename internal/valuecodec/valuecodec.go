// Package valuecodec は、ホストとプラグインの間で変数の値を運ぶための MessagePack の変換を提供する。
// 両側が同じ設定で変換しないと、文字列やマップの型が食い違う。必ずこのパッケージを通す。
package valuecodec

import (
	"reflect"

	"github.com/ugorji/go/codec"
)

// handle は MessagePack の設定。
//   - 文字列は string にデコードする（RawToString。既定では []byte になる）
//   - マップは map[string]interface{} にデコードする（構造体の値。既定では map[interface{}]interface{} になる）
var handle = func() *codec.MsgpackHandle {
	h := new(codec.MsgpackHandle)
	h.TypeInfos = codec.NewTypeInfos([]string{"msgpack"})
	h.MapType = reflect.TypeOf(map[string]interface{}(nil))
	h.RawToString = true
	return h
}()

// Marshal は値を MessagePack にする
func Marshal(v interface{}) ([]byte, error) {
	var b []byte
	if err := codec.NewEncoderBytes(&b, handle).Encode(v); err != nil {
		return nil, err
	}
	return b, nil
}

// Unmarshal は MessagePack を v（ポインター）に読み込む。
// *interface{} に読み込むと、元の Go の型は残らない:
//   - 整数は int64（負の値と、127 以下の値）か uint64（128 以上の符号なしの値）
//   - 小数は float64
//   - 配列は []interface{}、マップは map[string]interface{}
//
// 受け取った側は、変数の型に合わせて変換する（variable.ConvertValue）。
func Unmarshal(b []byte, v interface{}) error {
	return codec.NewDecoderBytes(b, handle).Decode(v)
}
