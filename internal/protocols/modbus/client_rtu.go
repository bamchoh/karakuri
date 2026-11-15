package modbus

import (
	"fmt"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/protocols/modbus/rtu"
)

// Modbus RTU のクライアント（マスター）。送受信はシリアルの共通の部分（client_serial.go）で、ここはフレームの形だけ。
//
// フレームは UnitID（スレーブアドレス）+ PDU + CRC（バイナリ）。
// 応答の終わりは、ファンクションコードから決まる長さで判断する（無音の時間だけで区切らない。
// USB シリアル変換器は受信したバイトをまとめて渡すので、1 つの応答の途中に間が空くことがある）。
// 長さの決まらないファンクションコード（16 進で入れたもの）は、バイトが届かなくなった時点を終わりにする。

// maxRTUADU は RTU のフレーム（UnitID + PDU + CRC）の最大の長さ
const maxRTUADU = 256

var rtuVariant = serialVariant{
	protocolType: ProtocolModbusRTU,
	displayName:  "Modbus RTU",
	defaults:     rtu.SerialConfig{BaudRate: 115200, DataBits: 8, StopBits: 1, Parity: "N"},
	checkName:    "CRC",
	newFramer:    func(c rtu.SerialConfig) serialFramer { return rtuFramer{silence: rtuSilenceTime(c)} },
}

// RTUClientFactory は Modbus RTU クライアントのファクトリー。コマンドとマッピングは pduClient（TCP と共通）
type RTUClientFactory struct {
	pduClient

	// open はポートを開く関数。nil なら実際のシリアルポートを開く（テストではフェイクのポートを渡す）
	open rtu.PortOpener
}

// ProtocolType はクライアントのプロトコルの種類を返す
func (f *RTUClientFactory) ProtocolType() protocol.ProtocolType { return rtuVariant.protocolType }

// DisplayName はプロトコルの表示名を返す
func (f *RTUClientFactory) DisplayName() string { return rtuVariant.displayName }

// ConnectionFields は接続先の設定項目を返す
func (f *RTUClientFactory) ConnectionFields() []protocol.ConfigField {
	return rtuVariant.connectionFields()
}

// NormalizeSettings は接続先の設定を検証し、ない項目を既定値で埋めたものを返す
func (f *RTUClientFactory) NormalizeSettings(settings map[string]interface{}) (map[string]interface{}, error) {
	return rtuVariant.normalizeSettings(settings)
}

// RequestHelp は、16 進で入れるリクエストの説明を返す
func (f *RTUClientFactory) RequestHelp() protocol.RequestHelp { return rtuVariant.requestHelp() }

// NewClient は接続先の設定からクライアントを作る。ポートを開くのは最初の送信のとき
func (f *RTUClientFactory) NewClient(settings map[string]interface{}) (protocol.Client, error) {
	return rtuVariant.newClient(f.open, settings)
}

// rtuSilenceTime は、フレームの間に空ける時間（3.5 文字時間。19200bps より速ければ 1.75ms。Modbus の仕様）
func rtuSilenceTime(c rtu.SerialConfig) time.Duration {
	if c.BaudRate > 19200 {
		return 1750 * time.Microsecond
	}
	bits := 1 + c.DataBits + c.StopBits
	if c.Parity != "N" {
		bits++
	}
	return time.Duration(float64(bits) * 3.5 / float64(c.BaudRate) * float64(time.Second))
}

// rtuFramer は RTU のフレームの形
type rtuFramer struct {
	silence time.Duration
}

func (f rtuFramer) encode(unitID byte, pdu []byte) []byte {
	return rtu.AppendCRC(append([]byte{unitID}, pdu...))
}

func (f rtuFramer) responseLength(buf []byte) int { return rtuResponseLength(buf) }

func (f rtuFramer) maxFrame() int { return maxRTUADU }

func (f rtuFramer) frameGap() time.Duration { return f.silence }

func (f rtuFramer) decode(frame []byte) (byte, []byte, error) {
	if len(frame) < 4 {
		return 0, nil, fmt.Errorf("応答が短すぎます（%d バイト）", len(frame))
	}
	if !rtu.CheckCRC(frame) {
		return 0, nil, fmt.Errorf("応答の CRC が合いません")
	}
	return frame[0], frame[1 : len(frame)-2], nil
}

// rtuResponseLength は、届いたところまでの応答から、応答の長さ（UnitID から CRC まで）を返す。
// まだ決まらなければ 0、ファンクションコードから決まらなければ -1
func rtuResponseLength(frame []byte) int {
	if len(frame) < 2 {
		return 0
	}
	fc := frame[1]
	if fc&0x80 != 0 {
		return 5 // UnitID・FC・例外コード・CRC 2
	}
	switch fc {
	case fcReadCoils, fcReadDiscreteInputs, fcReadHoldingRegisters, fcReadInputRegisters:
		if len(frame) < 3 {
			return 0
		}
		return 5 + int(frame[2]) // UnitID・FC・バイト数・データ・CRC 2
	case fcWriteSingleCoil, fcWriteSingleRegister, fcWriteMultipleCoils, fcWriteMultipleRegisters:
		return 8 // UnitID・FC・アドレス 2・値（個数）2・CRC 2
	}
	return -1
}
