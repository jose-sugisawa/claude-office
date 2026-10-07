// Package atomicfile は、途中で止まっても壊れたファイルが残らない書き方をまとめる。
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write は、途中で止まっても壊れたファイルが残らないよう、同じディレクトリに別名で書いてから置き換える。
// 書いたファイルは本人だけが読める（0600。os.CreateTemp の作るまま）。
func Write(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
