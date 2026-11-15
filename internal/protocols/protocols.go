// Package protocols は、アプリに組み込むプロトコルの一覧を持つ。
//
// プロトコルの実装は internal/protocols/<名前>/ に置き、ここで登録する。
// ホスト（internal/application、app.go）はプロトコル固有の値を持たず、
// protocol.ServerFactory などのインターフェースだけでプロトコルを扱う。
package protocols

import (
	"karakuri/internal/domain/protocol"
	"karakuri/internal/protocols/fins"
	"karakuri/internal/protocols/modbus"
	"karakuri/internal/protocols/opcua"
	"karakuri/internal/protocols/slmp"
)

// Factories は組み込みのプロトコルのファクトリーを返す。呼ぶたびに新しいものを作る
func Factories() []protocol.ServerFactory {
	return []protocol.ServerFactory{
		&modbus.TCPFactory{},
		&modbus.RTUFactory{},
		&modbus.ASCIIFactory{},
		&opcua.Factory{},
		&fins.Factory{},
		&fins.HostLinkFactory{},
		&slmp.Factory{},
		&slmp.MCSerialFactory{},
	}
}

// ClientFactories は組み込みのクライアント（外部の機器へつなぐ側）のファクトリーを返す。呼ぶたびに新しいものを作る
func ClientFactories() []protocol.ClientFactory {
	return []protocol.ClientFactory{
		&modbus.TCPClientFactory{},
		&modbus.RTUClientFactory{},
		&modbus.ASCIIClientFactory{},
		&opcua.ClientFactory{},
		&fins.ClientFactory{},
		&fins.HostLinkClientFactory{},
		&slmp.ClientFactory{},
		&slmp.MCSerialClientFactory{},
	}
}
