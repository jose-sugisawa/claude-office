package statusline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStatuslineOddPercent(t *testing.T) {
	for _, pct := range []string{"-100", "1e308", "-1e308"} {
		got := render([]byte(`{"session_id":"x","session_name":"a","context_window":{"used_percentage":`+pct+`}}`), t.TempDir(), time.Unix(0, 0))
		if !strings.Contains(got, "a · ctx") {
			t.Errorf("used_percentage %s: %q", pct, got)
		}
	}
}

func TestIsOwnStatusline(t *testing.T) {
	for cmd, want := range map[string]bool{
		`'/home/a/.local/bin/claude-office' statusline`:        true,
		`"/Users/a/.local/bin/claude-office" statusline`:       true, // 前の版の書き方
		`"C:\\Users\\a\\claude-office.exe" statusline`:         true,
		`~/bin/my-claude-office-wrapper.sh`:                    false,
		`bash -c 'claude-office statusline; echo hi'`:          false,
		`/opt/claude-office-tools/other.sh statusline --fancy`: false,
	} {
		raw, _ := json.Marshal(map[string]string{"type": "command", "command": cmd})
		if got := isOwnStatusline(raw); got != want {
			t.Errorf("%s: got %v, want %v", cmd, got, want)
		}
	}
}

func TestSettingsSymlinkKept(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows のシンボリックリンクは権限が要る")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles-settings.json")
	link := filepath.Join(dir, "settings.json")
	os.WriteFile(real, []byte(`{"model":"opus"}`), 0o644)
	os.Symlink(real, link)
	if err := backupAndWrite(link, []kv{{"model", json.RawMessage(`"sonnet"`)}}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Lstat(link); st.Mode()&os.ModeSymlink == 0 {
		t.Error("シンボリックリンクが普通のファイルに置き換わった")
	}
	if b, _ := os.ReadFile(real); !strings.Contains(string(b), "sonnet") {
		t.Errorf("リンク先が書き換わっていない: %s", b)
	}
}

func TestBackupIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows はファイルの権限の仕組みが違う")
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"env":{"ANTHROPIC_API_KEY":"x"}}`), 0o644)
	os.WriteFile(path+".bak", []byte(`old`), 0o644) // 前の版が 0644 で残した控え
	if err := backupAndWrite(path, nil); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("控えをほかの人も読める: %v", st.Mode().Perm())
	}
}

func TestStatusline(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1_800_000_000, 0)
	in := []byte(`{"session_id":"abc-123","session_name":"app@review","context_window":{"context_window_size":1000000,"used_percentage":85,"current_usage":{"input_tokens":2,"cache_creation_input_tokens":10,"cache_read_input_tokens":849988}},"rate_limits":{"five_hour":{"used_percentage":42,"resets_at":1800003600}}}`)
	got := render(in, dir, now)
	if !strings.Contains(got, "app@review · ctx") || !strings.Contains(got, "85%") || !strings.Contains(got, "/compact") {
		t.Errorf("出力が違う: %q", got)
	}
	b, err := os.ReadFile(filepath.Join(dir, "ctx", "abc-123.json"))
	if err != nil || !strings.Contains(string(b), `"used":85`) || !strings.Contains(string(b), `"tokens":850000`) {
		t.Errorf("ctx のファイルが違う: %s %v", b, err)
	}
	if u, _ := os.ReadFile(filepath.Join(dir, "usage.json")); !strings.Contains(string(u), "five_hour") {
		t.Errorf("usage.json が書かれていない: %s", u)
	}
	// 始まったばかりで使用率がまだ無い・名前が無い・ID がファイル名に使えない
	if got := render([]byte(`{"session_id":"x","workspace":{"current_dir":"/a/proj"}}`), dir, now); got != "proj · ctx —" {
		t.Errorf("使用率が無いときの出力: %q", got)
	}
	render([]byte(`{"session_id":"../evil","context_window":{"used_percentage":5}}`), dir, now)
	if _, err := os.Stat(filepath.Join(dir, "evil.json")); err == nil {
		t.Error("セッション ID でディレクトリの外に書いた")
	}
}

func TestSettingsKeepOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"model":"opus","hooks":{"Stop":[]},"statusLine":{"type":"command","command":"other.sh"},"z":1}`), 0o644)
	pairs, err := readSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	pairs = set(pairs, "statusLine", json.RawMessage(`{"type":"command","command":"\"/x/claude-office\" statusline"}`))
	out := string(encodeSettings(pairs))
	if strings.Index(out, `"model"`) > strings.Index(out, `"hooks"`) || strings.Index(out, `"statusLine"`) > strings.Index(out, `"z"`) {
		t.Errorf("キーの並びが変わった:\n%s", out)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Errorf("書いたものが JSON として読めない: %v\n%s", err, out)
	}
}
