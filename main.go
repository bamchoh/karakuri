package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// webview2PathEnv で WebView2 ランタイム（Fixed Version）のフォルダを指定すると、
// インストール済みの Evergreen ランタイムの代わりにそれを使う。未設定なら従来どおり。
const webview2PathEnv = "KARAKURI_WEBVIEW2_PATH"

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Karakuri",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 24, G: 27, B: 32, A: 1}, // 画面の --bg-panel と同じ色
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Windows: &windows.Options{
			WebviewBrowserPath: os.Getenv(webview2PathEnv),
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
