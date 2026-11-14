package slmp

import (
	"karakuri/internal/domain/protocol"
)

// SLMP クライアントの、変数マッピング（protocol.MappableClientFactory）の実装。
// エリアはサーバー（DataStore）と同じで、デバイス 1 つがエリア 1 つ。
// ビットデバイスはビット単位、ワードデバイスはワード単位で読み書きする

var _ protocol.MappableClientFactory = (*ClientFactory)(nil)

// MappingAreas はマッピングできるメモリエリアを返す
func (f *ClientFactory) MappingAreas() []protocol.MemoryArea {
	return memoryAreas()
}

// MaxReadCount は 1 回のリクエストで読める点数
func (f *ClientFactory) MaxReadCount(area string) int {
	def, ok := deviceByID(area)
	if !ok {
		return 0
	}
	return maxCount(def)
}

// MaxWriteCount は 1 回のリクエストで書ける点数
func (f *ClientFactory) MaxWriteCount(area string) int {
	return f.MaxReadCount(area)
}

// BuildReadRequest は area の address から count 点を読むリクエストを作る
func (f *ClientFactory) BuildReadRequest(area string, address uint32, count int) ([]byte, error) {
	t, err := targetFromStore(area, address)
	if err != nil {
		return nil, err
	}
	return readRequest(t, count)
}

// BuildWriteRequest は area の address からワードかビットを書くリクエストを作る
func (f *ClientFactory) BuildWriteRequest(area string, address uint32, words []uint16, bits []bool) ([]byte, error) {
	t, err := targetFromStore(area, address)
	if err != nil {
		return nil, err
	}
	return writeRequest(t, words, bits)
}
