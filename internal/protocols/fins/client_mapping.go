package fins

import (
	"karakuri/internal/domain/protocol"
)

// FINS クライアントの、変数マッピング（protocol.MappableClientFactory）の実装。
// エリアはサーバー（DataStore）と同じ。ビットのエリアのアドレスは「ワード × 16 + ビット」、
// フラグのエリア（タイマ・カウンタのアップフラグ）のアドレスは番号

var _ protocol.MappableClientFactory = (*ClientFactory)(nil)

// MappingAreas はマッピングできるメモリエリアを返す
func (f *ClientFactory) MappingAreas() []protocol.MemoryArea {
	return memoryAreas()
}

// MaxReadCount は 1 回のリクエストで読める個数
func (f *ClientFactory) MaxReadCount(area string) int {
	if _, err := targetFromStore(area, 0); err != nil {
		return 0
	}
	return maxReadCount
}

// MaxWriteCount は 1 回のリクエストで書ける個数
func (f *ClientFactory) MaxWriteCount(area string) int {
	if _, err := targetFromStore(area, 0); err != nil {
		return 0
	}
	return maxWriteCount
}

// BuildReadRequest は area の address から count 個を読むリクエストを作る
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
