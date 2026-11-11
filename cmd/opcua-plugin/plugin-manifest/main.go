package main

import (
	"encoding/json"
	"os"

	"karakuri/cmd/opcua-plugin/internal/opcua"
	"karakuri/internal/pluginruntime"
)

func main() {
	manifest := pluginruntime.NewManifest(&opcua.Factory{}, "opcua-plugin.exe")

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(manifest)
}
