// Package claudehome は、Claude Code の設定ディレクトリ（~/.claude）の場所と、
// claude-office がその下の office/ に置くファイルの形を1か所にまとめる。
// statusline が書いて session・server が読むので、ファイルの名前と中身はここで決める。
package claudehome

import (
	"os"
	"path/filepath"
	"regexp"
)

// Dir は Claude Code の設定ディレクトリ。CLAUDE_CONFIG_DIR があればそれ、なければ ~/.claude。
func Dir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// Office は、このプログラムが読み書きするディレクトリ（島の一覧・コンテキスト・使用量）。
func Office(root string) string { return filepath.Join(root, "office") }

// office/ の下に置くファイル。
const (
	IslandsFile = "islands.json" // 島の一覧
	UsageFile   = "usage.json"   // 使用量の枠（statusline が Claude Code の rate_limits をそのまま書く。全セッション共通）
)

// Ctx は statusline が office/ctx/<セッションID>.json に書く、コンテキストの使用率。
// Used は Claude Code の context_window.used_percentage そのまま。
type Ctx struct {
	Used   float64 `json:"used"`
	Size   int64   `json:"size"`
	Tokens int64   `json:"tokens"`
	At     int64   `json:"at"` // 書いた時刻（秒）
}

// CtxPath は、そのセッションの Ctx を置く場所。ID がファイル名に使えなければ ok は false。
func CtxPath(office, sessionID string) (path string, ok bool) {
	if !ValidSessionID(sessionID) {
		return "", false
	}
	return filepath.Join(office, "ctx", sessionID+".json"), true
}

var sessionID = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// ValidSessionID は、セッション ID をファイル名や Glob に使ってよいかを見る（* や ../ を含む ID は断る）。
func ValidSessionID(id string) bool { return sessionID.MatchString(id) }
