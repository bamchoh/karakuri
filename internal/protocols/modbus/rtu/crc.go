package rtu

// CRC-16 Modbus計算
// 多項式: 0xA001 (反転)
// 初期値: 0xFFFF

// CRC16 はModbus RTU用のCRC-16を計算する
func CRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)

	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&0x0001 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}

	return crc
}

// AppendCRC はデータにCRC-16を付加したバイト列を返す
func AppendCRC(data []byte) []byte {
	crc := CRC16(data)
	// CRCはリトルエンディアンで付加
	return append(data, byte(crc&0xFF), byte(crc>>8))
}

// CheckCRC はフレームのCRCを検証する
func CheckCRC(frame []byte) bool {
	if len(frame) < 3 {
		return false
	}

	dataLen := len(frame) - 2
	data := frame[:dataLen]
	receivedCRC := uint16(frame[dataLen]) | (uint16(frame[dataLen+1]) << 8)

	return CRC16(data) == receivedCRC
}
