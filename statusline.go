package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Claude Code のステータスライン（各セッションのいちばん下の行）。
// Claude Code は描くたびに、このコマンドの標準入力へセッションの情報を JSON で渡す。
// ここでは「セッション名 · ctx ███████░░░ 72%」を出し、オフィスが読むよう次の2つを書く。
//   <Claude の設定>/office/ctx/<セッションID>.json  コンテキストの使用率（context_window.used_percentage）
//   <Claude の設定>/office/usage.json               使用量の枠（rate_limits。全セッション共通）

const (
	ctxWarn = 60 // これ以上で黄
	ctxHot  = 80 // これ以上で赤・「そろそろ /compact」
)

type statuslineInput struct {
	SessionID   string `json:"session_id"`
	SessionName string `json:"session_name"`
	Cwd         string `json:"cwd"`
	Workspace   struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	ContextWindow *struct {
		Size         int64    `json:"context_window_size"`
		UsedPercent  *float64 `json:"used_percentage"`
		CurrentUsage *struct {
			Input         int64 `json:"input_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`
	RateLimits json.RawMessage `json:"rate_limits"`
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// runStatusline は失敗しても何かを出して終わる（ステータスラインが空になるより、名前だけでも出るほうがよい）。
func runStatusline(in io.Reader, out io.Writer) {
	b, _ := io.ReadAll(io.LimitReader(in, 4<<20))
	fmt.Fprint(out, statusline(b, officeDir(), time.Now()))
}

func statusline(b []byte, dir string, now time.Time) string {
	var s statuslineInput
	if json.Unmarshal(b, &s) != nil {
		return "claude-office: 入力が読めません"
	}
	name := s.SessionName
	if name == "" {
		cwd := s.Workspace.CurrentDir
		if cwd == "" {
			cwd = s.Cwd
		}
		name = filepath.Base(cwd)
	}

	if len(s.RateLimits) > 0 && string(s.RateLimits) != "null" {
		u, _ := json.Marshal(map[string]any{"rate_limits": s.RateLimits, "at": now.Unix()})
		writeFileAtomic(filepath.Join(dir, "usage.json"), append(u, '\n'))
	}

	cw := s.ContextWindow
	if cw == nil || cw.UsedPercent == nil {
		return name + " · ctx —"
	}
	pct := *cw.UsedPercent
	if safeID.MatchString(s.SessionID) {
		var tokens int64
		if u := cw.CurrentUsage; u != nil {
			tokens = u.Input + u.CacheCreation + u.CacheRead
		}
		c, _ := json.Marshal(Ctx{Used: pct, Size: cw.Size, Tokens: tokens, At: now.Unix()})
		writeFileAtomic(filepath.Join(dir, "ctx", s.SessionID+".json"), append(c, '\n'))
	}

	p := int(pct + 0.5)
	if p < 0 || pct != pct { // 負の値と NaN（バーの長さが負になって落ちないように）
		p = 0
	} else if pct > 100 {
		p = 100
	}
	color := "\x1b[90m"
	switch {
	case p >= ctxHot:
		color = "\x1b[31m"
	case p >= ctxWarn:
		color = "\x1b[33m"
	}
	filled := (p + 5) / 10
	if filled > 10 {
		filled = 10
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
	hint := ""
	if p >= ctxHot {
		hint = " そろそろ /compact"
	}
	return fmt.Sprintf("%s · ctx %s%s %d%%%s\x1b[0m", name, color, bar, p, hint)
}

// ---- settings.json への登録 ----

func settingsPath() string { return filepath.Join(claudeDir(), "settings.json") }

// statuslineCommand は settings.json に書くコマンド。Claude Code はシェルで実行するので、
// パスに空白や $ があっても動くよう、macOS・Linux はシングルクォートで、Windows は二重引用符で囲む。
func statuslineCommand() (string, error) {
	exe, err := stableExecutable()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return strconv.Quote(exe) + " statusline", nil
	}
	return "'" + strings.ReplaceAll(exe, "'", `'\''`) + "' statusline", nil
}

// ownStatusline は、登録されているステータスラインが claude-office のもの（… claude-office statusline）かを見る。
// 「claude-office」という文字が入っているだけの別のスクリプトは、ほかのものとして扱う。
var ownStatusline = regexp.MustCompile(`claude-office(\.exe)?["']? statusline$`)

func isOwnStatusline(raw json.RawMessage) bool {
	var sl struct {
		Command string `json:"command"`
	}
	return json.Unmarshal(raw, &sl) == nil && ownStatusline.MatchString(strings.TrimSpace(sl.Command))
}

// 置き換えた元のステータスライン。uninstall で元に戻すために取っておく。
func prevStatuslinePath() string { return filepath.Join(officeDir(), "statusline-before.json") }

func installStatusline(args []string) error {
	fs := flag.NewFlagSet("statusline install", flag.ExitOnError)
	force := fs.Bool("force", false, "ほかのステータスラインが登録されていても置き換える")
	fs.Parse(args)
	cmd, err := statuslineCommand()
	if err != nil {
		return err
	}
	path := settingsPath()
	pairs, err := readSettings(path)
	if err != nil {
		return err
	}
	if old, ok := lookup(pairs, "statusLine"); ok && !isOwnStatusline(old) {
		if !*force {
			return fmt.Errorf("%s にはもう別のステータスラインが登録されています：%s\n置き換えるなら -force を付けてください（uninstall で元に戻せるよう取っておきます）", path, compact(old))
		}
		if err := writeFileAtomic(prevStatuslinePath(), old); err != nil {
			return fmt.Errorf("元のステータスラインを取っておけません: %w", err)
		}
	}
	sl, _ := json.Marshal(map[string]any{"type": "command", "command": cmd, "padding": 0})
	pairs = set(pairs, "statusLine", sl)
	if err := backupAndWrite(path, pairs); err != nil {
		return err
	}
	fmt.Printf("Claude Code のステータスラインに登録しました（%s）。\n動いているセッションにも、次に画面が動いたときから出ます。\n", path)
	return nil
}

func uninstallStatusline() error {
	path := settingsPath()
	pairs, err := readSettings(path)
	if err != nil {
		return err
	}
	old, ok := lookup(pairs, "statusLine")
	if !ok || !isOwnStatusline(old) {
		fmt.Println("claude-office のステータスラインは登録されていません。")
		return nil
	}
	// 置き換える前のものを取ってあれば戻し、なければキーごと外す
	prev, err := os.ReadFile(prevStatuslinePath())
	if err == nil && json.Valid(prev) {
		if err := backupAndWrite(path, set(pairs, "statusLine", bytes.TrimSpace(prev))); err != nil {
			return err
		}
		os.Remove(prevStatuslinePath())
		fmt.Println("ステータスラインを、claude-office を入れる前のものに戻しました。")
		return nil
	}
	var out []kv
	for _, p := range pairs {
		if p.k != "statusLine" {
			out = append(out, p)
		}
	}
	if err := backupAndWrite(path, out); err != nil {
		return err
	}
	fmt.Println("ステータスラインの登録を外しました。")
	return nil
}

// settings.json は、上の階層のキーの並びを変えずに1か所だけ書き換える（中の値は元の書き方のまま）。
type kv struct {
	k string
	v json.RawMessage
}

func readSettings(path string) ([]kv, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("%s が JSON のオブジェクトとして読めません", path)
	}
	var out []kv
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%s が読めません: %w", path, err)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%s が読めません: %w", path, err)
		}
		out = append(out, kv{t.(string), v})
	}
	return out, nil
}

func lookup(pairs []kv, k string) (json.RawMessage, bool) {
	for _, p := range pairs {
		if p.k == k {
			return p.v, true
		}
	}
	return nil, false
}

func set(pairs []kv, k string, v json.RawMessage) []kv {
	for i := range pairs {
		if pairs[i].k == k {
			pairs[i].v = v
			return pairs
		}
	}
	return append(pairs, kv{k, v})
}

func encodeSettings(pairs []kv) []byte {
	var buf bytes.Buffer
	buf.WriteString("{")
	for i, p := range pairs {
		if i > 0 {
			buf.WriteString(",")
		}
		key, _ := json.Marshal(p.k)
		buf.WriteString("\n  ")
		buf.Write(key)
		buf.WriteString(": ")
		var v bytes.Buffer
		if json.Indent(&v, p.v, "  ", "  ") != nil {
			v.Reset()
			v.Write(p.v)
		}
		buf.Write(v.Bytes())
	}
	if len(pairs) > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes()
}

func backupAndWrite(path string, pairs []kv) error {
	// dotfiles などでシンボリックリンクにしている人がいるので、リンクを普通のファイルで置き換えず、リンク先を書き換える
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	// settings.json の env に API キーなどを書く人がいるので、控えは本人だけが読めるように（0600）書く
	if b, err := os.ReadFile(path); err == nil {
		if err := writeFileAtomic(path+".bak", b); err != nil {
			return fmt.Errorf("元の設定を残せません: %w", err)
		}
	}
	return writeFileAtomic(path, encodeSettings(pairs))
}

func compact(b []byte) string {
	var buf bytes.Buffer
	if json.Compact(&buf, b) != nil {
		return string(b)
	}
	return buf.String()
}
