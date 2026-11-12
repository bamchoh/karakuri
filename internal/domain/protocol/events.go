package protocol

// CommunicationEventEmitter は通信イベントを発行するインターフェース
type CommunicationEventEmitter interface {
	EmitRx()
	EmitTx()
	EmitConnection(count int)
}

// CommunicationEventAware は通信イベントの送信先を受け取る ProtocolServer 用インターフェース。
// プラグイン内では pluginruntime が、ホストへイベントを届ける送信先を起動前に渡す。
type CommunicationEventAware interface {
	SetEventEmitter(emitter CommunicationEventEmitter)
}
