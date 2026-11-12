package adapter

import (
	"karakuri/internal/domain/variable"
)

// 変数の値 ↔ ワード列の変換。サーバーのレジスタとの同期（VariableBackedDataStore）と、
// 外部の機器との同期（DeviceSync）で共通に使う。

// encodeVariableWords は変数の値を、マッピング先に書くワード列にする
func encodeVariableWords(varStore *variable.VariableStore, v *variable.Variable, endianness string) ([]uint16, bool) {
	switch {
	case v.DataType.IsArrayType():
		elemType, size, err := variable.ParseArrayType(v.DataType)
		if err != nil {
			return nil, false
		}
		return variable.ArrayValueToWords(v.Value, elemType, size, endianness, varStore.TypeResolver()), true
	case v.DataType.IsStructType():
		structDef, err := varStore.GetStructType(string(v.DataType))
		if err != nil || structDef == nil {
			return nil, false
		}
		return variable.StructValueToWords(v.Value, structDef, endianness, varStore.TypeResolver()), true
	default:
		return variable.ValueToWords(v.Value, v.DataType, endianness), true
	}
}

// decodeVariableWords はマッピング先から読んだワード列を、変数の値にする
func decodeVariableWords(varStore *variable.VariableStore, v *variable.Variable, words []uint16, endianness string) (interface{}, bool) {
	var value interface{}
	var err error
	switch {
	case v.DataType.IsArrayType():
		elemType, size, parseErr := variable.ParseArrayType(v.DataType)
		if parseErr != nil {
			return nil, false
		}
		value, err = variable.WordsToArrayValue(words, elemType, size, endianness, varStore.TypeResolver())
	case v.DataType.IsStructType():
		structDef, getErr := varStore.GetStructType(string(v.DataType))
		if getErr != nil || structDef == nil {
			return nil, false
		}
		value, err = variable.WordsToStructValue(words, structDef, endianness, varStore.TypeResolver())
	default:
		value, err = variable.WordsToValue(words, v.DataType, endianness)
	}
	return value, err == nil
}
