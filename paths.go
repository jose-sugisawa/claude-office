package main

import (
	"os"
	"path/filepath"
)

// claudeDir は Claude Code の設定ディレクトリ。CLAUDE_CONFIG_DIR があればそれ、なければ ~/.claude。
func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// officeDir は、このプログラムが読み書きするディレクトリ（島の一覧・コンテキスト・使用量）。
func officeDir() string { return filepath.Join(claudeDir(), "office") }

// writeFileAtomic は、途中で止まっても壊れたファイルが残らないよう、別名で書いてから置き換える。
func writeFileAtomic(path string, b []byte) error {
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
