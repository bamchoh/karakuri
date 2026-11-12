package rtu

// LRC（Longitudinal Redundancy Check）計算
// Modbus ASCII用のエラーチェック

// LRC はデータのLRCを計算する
// 全バイトの合計の2の補数（下位8ビット）
func LRC(data []byte) byte {
	var sum byte
	for _, b := range data {
		sum += b
	}
	// 2の補数 = ビット反転 + 1
	return (^sum) + 1
}

// CheckLRC はフレームのLRCを検証する
// data: LRCを含まないデータ部分
// lrc: 受信したLRC値
func CheckLRC(data []byte, lrc byte) bool {
	return LRC(data) == lrc
}
