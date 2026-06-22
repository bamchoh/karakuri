package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"modbus_simulator/cmd/modbus-plugin/plugin"
	"modbus_simulator/internal/pluginruntime"
)

func main() {
	protocolType := flag.String("protocol-type", "modbus-tcp", "プロトコルタイプ (modbus-tcp, modbus-rtu, modbus-ascii)")

	flag.Parse()

	factory := plugin.ResolveFactory(*protocolType)

	manifest := pluginruntime.Manifest{
		Name:         fmt.Sprintf("%s Plugin", factory.DisplayName()),
		EntryPoint:   "modbus-plugin.exe",
		Version:      factory.Version(),
		ProtocolType: string(factory.ProtocolType()),
		DisplayName:  factory.DisplayName(),
		Variants:     []string{},
		Capabilities: factory.GetProtocolCapabilities(),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(manifest)
}
