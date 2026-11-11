package application

import (
	"os"
	"testing"

	"karakuri/internal/infrastructure/appconfig"
)

// TestMain は設定フォルダを一時フォルダに差し替える。
// テストが開発 PC の実際の設定（モニタリング項目など）を書き換えないようにするため。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "karakuri-test-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv(appconfig.EnvDir, dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
