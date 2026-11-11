// Package appconfig はアプリケーションの設定フォルダを決める。
package appconfig

import (
	"io"
	"os"
	"path/filepath"
)

const (
	// dirName は設定フォルダ名（Windows なら %APPDATA%\Karakuri）
	dirName = "Karakuri"
	// legacyDirName は旧名のころの設定フォルダ名。初回だけここから設定を引き継ぐ
	legacyDirName = "PLCSimulator"
	// EnvDir を設定すると、そのフォルダを設定フォルダとして使う（テストや検証用）
	EnvDir = "KARAKURI_CONFIG_DIR"
)

// Dir は設定フォルダのパスを返す（なければ作る）。
// 設定フォルダがまだなく旧フォルダがある場合は、旧フォルダの設定ファイルをコピーして引き継ぐ。
func Dir() (string, error) {
	if dir := os.Getenv(EnvDir); dir != "" {
		return dir, os.MkdirAll(dir, 0755)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return dirIn(base)
}

// Path は設定フォルダ内のファイルのパスを返す
func Path(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func dirIn(base string) (string, error) {
	dir := filepath.Join(base, dirName)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	migrateLegacy(filepath.Join(base, legacyDirName), dir)
	return dir, nil
}

// migrateLegacy は旧フォルダ直下のファイルを新しいフォルダへコピーする。
// 旧フォルダは残す。コピーに失敗したファイルは既定値で始まるだけなので、エラーは無視する。
func migrateLegacy(legacyDir, dir string) {
	entries, err := os.ReadDir(legacyDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		_ = copyFile(filepath.Join(legacyDir, e.Name()), filepath.Join(dir, e.Name()))
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
