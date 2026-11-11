package main

import (
	"flag"
	"fmt"
	"os"

	"karakuri/cmd/opcua-plugin/internal/opcua"
	"karakuri/internal/pluginruntime"
)

func main() {
	_ = flag.String("protocol-type", "opcua", "プロトコルタイプ（opcua 固定）")
	debug := flag.Bool("debug", false, "デバッグモード")
	_ = flag.String("host-grpc-addr", "", "未使用（ホストのアドレスは CreateAndStart で受け取る）")
	flag.Parse()

	fmt.Fprintln(os.Stderr, "OPC UA Plugin starting...")

	if err := pluginruntime.RunFactory(&opcua.Factory{}, *debug); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] プラグインランタイムエラー: %v\n", err)
		os.Exit(1)
	}
}
