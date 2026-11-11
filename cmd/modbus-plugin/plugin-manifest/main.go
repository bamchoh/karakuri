package main

import (
	"encoding/json"
	"flag"
	"os"

	"karakuri/cmd/modbus-plugin/internal/modbus"
	"karakuri/cmd/modbus-plugin/plugin"
	"karakuri/internal/pluginruntime"
)

func main() {
	protocolType := flag.String("protocol-type", string(modbus.ProtocolModbusTCP), "プロトコルタイプ (modbus-tcp, modbus-rtu, modbus-ascii)")

	flag.Parse()

	factory := plugin.ResolveFactory(*protocolType)

	manifest := pluginruntime.NewManifest(factory, "modbus-plugin.exe")

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(manifest)
}
