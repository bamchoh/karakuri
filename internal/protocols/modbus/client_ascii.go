package modbus

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"karakuri/internal/domain/protocol"
	"karakuri/internal/protocols/modbus/rtu"
)

// Modbus ASCII のクライアント（マスター）。送受信はシリアルの共通の部分（client_serial.go）で、ここはフレームの形だけ。
//
// フレームは ':' + 16 進の文字（UnitID + PDU + LRC）+ CR LF。応答の終わりは CR LF で判断する
// （長さも無音の時間も使わない）。':' より前に届いたバイト（前の応答の残りなど）は読み飛ばす。

// maxASCIIFrame は ASCII のフレームの最大の長さ（':' + 2 × (UnitID 1 + PDU 253 + LRC 1) + CR LF = 513）
const maxASCIIFrame = 1 + 2*(1+maxSerialPDU+1) + 2

var asciiVariant = serialVariant{
	protocolType: ProtocolModbusASCII,
	displayName:  "Modbus ASCII",
	defaults:     rtu.SerialConfig{BaudRate: 9600, DataBits: 7, StopBits: 1, Parity: "E"},
	checkName:    "LRC",
	newFramer:    func(rtu.SerialConfig) serialFramer { return asciiFramer{} },
}

// ASCIIClientFactory は Modbus ASCII クライアントのファクトリー。コマンドとマッピングは pduClient（TCP と共通）
type ASCIIClientFactory struct {
	pduClient

	// open はポートを開く関数。nil なら実際のシリアルポートを開く（テストではフェイクのポートを渡す）
	open rtu.PortOpener
}

var _ protocol.MappableClientFactory = (*ASCIIClientFactory)(nil)

// ProtocolType はクライアントのプロトコルの種類を返す
func (f *ASCIIClientFactory) ProtocolType() protocol.ProtocolType { return asciiVariant.protocolType }

// DisplayName はプロトコルの表示名を返す
func (f *ASCIIClientFactory) DisplayName() string { return asciiVariant.displayName }

// ConnectionFields は接続先の設定項目を返す
func (f *ASCIIClientFactory) ConnectionFields() []protocol.ConfigField {
	return asciiVariant.connectionFields()
}

// NormalizeSettings は接続先の設定を検証し、ない項目を既定値で埋めたものを返す
func (f *ASCIIClientFactory) NormalizeSettings(settings map[string]interface{}) (map[string]interface{}, error) {
	return asciiVariant.normalizeSettings(settings)
}

// RequestHelp は、16 進で入れるリクエストの説明を返す
func (f *ASCIIClientFactory) RequestHelp() protocol.RequestHelp { return asciiVariant.requestHelp() }

// NewClient は接続先の設定からクライアントを作る。ポートを開くのは最初の送信のとき
func (f *ASCIIClientFactory) NewClient(settings map[string]interface{}) (protocol.Client, error) {
	return asciiVariant.newClient(f.open, settings)
}

// asciiFramer は ASCII のフレームの形
type asciiFramer struct{}

func (asciiFramer) encode(unitID byte, pdu []byte) []byte {
	return rtu.BuildASCIIFrame(append([]byte{unitID}, pdu...))
}

// responseLength は、CR LF が届いたら、そこまでの長さを返す（届くまでは 0）
func (asciiFramer) responseLength(buf []byte) int {
	if i := bytes.Index(buf, []byte{rtu.ASCIIFrameCR, rtu.ASCIIFrameLF}); i >= 0 {
		return i + 2
	}
	return 0
}

func (asciiFramer) maxFrame() int { return maxASCIIFrame }

// frameGap: ASCII はフレームの始まりと終わりが文字で決まるので、間を空けなくてよい
func (asciiFramer) frameGap() time.Duration { return 0 }

func (asciiFramer) decode(frame []byte) (byte, []byte, error) {
	start := bytes.IndexByte(frame, rtu.ASCIIFrameStart)
	if start < 0 {
		return 0, nil, fmt.Errorf("応答に ':' がありません（ASCII のフレームではありません）")
	}
	data, err := rtu.ParseASCIIFrame(frame[start:])
	switch {
	case errors.Is(err, rtu.ErrFrameTooShort):
		return 0, nil, fmt.Errorf("応答が短すぎます")
	case err != nil && bytes.Contains([]byte(err.Error()), []byte("LRC")):
		return 0, nil, fmt.Errorf("応答の LRC が合いません")
	case err != nil:
		return 0, nil, fmt.Errorf("応答を ASCII のフレームとして読めません（%v）", err)
	}
	return data[0], data[1:], nil
}
