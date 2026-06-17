package main

import (
	"flag"
	"fmt"
	"os"

	"modbus_simulator/cmd/modbus-plugin/plugin"
	"modbus_simulator/internal/pluginruntime"
)

func main() {
	protocolType := flag.String("protocol-type", "modbus-tcp", "プロトコルタイプ (modbus-tcp, modbus-rtu, modbus-ascii)")
	_ = flag.String("host-grpc-addr", "", "ホスト側 gRPC サーバーアドレス（Modbus プラグインでは未使用）")
	flag.Parse()

	fmt.Fprintln(os.Stderr, "Modbus Plugin starting... protocol-type="+*protocolType)

	factory := plugin.ResolveFactory(*protocolType)

	if err := pluginruntime.RunFactory(factory); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] プラグインランタイムエラー: %v\n", err)
		os.Exit(1)
	}
}
