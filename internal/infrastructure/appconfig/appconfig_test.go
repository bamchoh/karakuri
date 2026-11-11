package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestDirIn_MigratesLegacyFilesOnce(t *testing.T) {
	base := t.TempDir()
	legacy := filepath.Join(base, legacyDirName)
	write(t, filepath.Join(legacy, "monitoring_config.json"), `{"items":[1]}`)
	write(t, filepath.Join(legacy, "httpapi_config.json"), `{"port":9000}`)
	write(t, filepath.Join(legacy, "certs", "own.pem"), "cert")

	dir, err := dirIn(base)
	if err != nil {
		t.Fatalf("dirIn: %v", err)
	}
	if dir != filepath.Join(base, dirName) {
		t.Errorf("dir = %s", dir)
	}
	if got := read(t, filepath.Join(dir, "monitoring_config.json")); got != `{"items":[1]}` {
		t.Errorf("monitoring_config.json = %s", got)
	}
	if got := read(t, filepath.Join(dir, "httpapi_config.json")); got != `{"port":9000}` {
		t.Errorf("httpapi_config.json = %s", got)
	}
	// サブフォルダはコピーしない。旧フォルダは残す
	if _, err := os.Stat(filepath.Join(dir, "certs")); !os.IsNotExist(err) {
		t.Errorf("subfolder was copied (err=%v)", err)
	}
	if got := read(t, filepath.Join(legacy, "httpapi_config.json")); got != `{"port":9000}` {
		t.Errorf("legacy file changed: %s", got)
	}

	// 2 回目以降は移行しない（新しいフォルダの内容を上書きしない）
	write(t, filepath.Join(dir, "httpapi_config.json"), `{"port":9100}`)
	write(t, filepath.Join(legacy, "httpapi_config.json"), `{"port":9999}`)
	if _, err := dirIn(base); err != nil {
		t.Fatalf("dirIn (2nd): %v", err)
	}
	if got := read(t, filepath.Join(dir, "httpapi_config.json")); got != `{"port":9100}` {
		t.Errorf("config overwritten on 2nd call: %s", got)
	}
}

func TestDirIn_NoLegacyDir(t *testing.T) {
	base := t.TempDir()
	dir, err := dirIn(base)
	if err != nil {
		t.Fatalf("dirIn: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Errorf("entries = %v, err = %v", entries, err)
	}
}

func TestDir_EnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom")
	t.Setenv(EnvDir, want)
	got, err := Dir()
	if err != nil || got != want {
		t.Fatalf("Dir = %s, %v; want %s", got, err, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("dir not created: %v", err)
	}
	if p, _ := Path("a.json"); p != filepath.Join(want, "a.json") {
		t.Errorf("Path = %s", p)
	}
}
