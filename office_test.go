package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLastReply(t *testing.T) {
	log := `{"type":"user","message":{"content":"進めて"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"## **#258** を直しました\n\n詳しくは下に"}]}}
{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"サブエージェントの返事"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"}]}}
{"type":"system"}
`
	if got, want := firstLine(lastReply([]byte(log))), "#258 を直しました"; got != want {
		t.Errorf("lastReply = %q, want %q", got, want)
	}
	if got := excerpt(lastReply([]byte(log))); got != "#258 を直しました\n詳しくは下に" {
		t.Errorf("excerpt = %q", got)
	}
	if got := lastReply([]byte(`{"type":"assistant","message":{"content":"文字列の返事"}}`)); got != "文字列の返事" {
		t.Errorf("文字列の content = %q", got)
	}
	// 末尾を切り出したとき、先頭の行は途中から始まる
	if got := lastReply([]byte(`ge":{"content":"x"}}` + "\n")); got != "" {
		t.Errorf("壊れた行 = %q, want 空", got)
	}
}

func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"\n\n- `go test` が通りました": "go test が通りました",
		"# 見出し\n本文":              "見出し",
		"#259 を直しました":            "#259 を直しました",
		"":                       "",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
	long := ""
	for i := 0; i < 70; i++ {
		long += "あ"
	}
	if got := []rune(firstLine(long)); len(got) != 60 || got[59] != '…' {
		t.Errorf("長い行が60文字に切れていない: %d", len(got))
	}
}

func TestSnapshotKeepsClosedSessionsForToday(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	os.MkdirAll(sessions, 0o755)
	// 動いているプロセスとして、このテスト自身の pid を使う
	write := func(file, id, name, status string) string {
		p := filepath.Join(sessions, file)
		body := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"name":%q,"status":%q,"cwd":"/x/app","statusUpdatedAt":1000}`, os.Getpid(), id, name, status)
		os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	write("a.json", "s1", "app-review", "idle")
	p2 := write("b.json", "s2", "shop-page", "busy")

	w := NewWatcher(root)
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.Local)
	if got := w.Snapshot(now); len(got) != 2 {
		t.Fatalf("2人のはずが %d 人: %+v", len(got), got)
	}

	// s2 を閉じる → 同じ日のうちは gone で残る
	os.Remove(p2)
	got := w.Snapshot(now.Add(time.Minute))
	if len(got) != 2 || got[1].Name != "shop-page" || got[1].Status != "gone" {
		t.Fatalf("閉じた人が gone で残っていない: %+v", got)
	}
	if got[0].Status != "idle" || got[0].Since != 1000 {
		t.Errorf("動いている人の状態が違う: %+v", got[0])
	}

	// 退出させると、1時間を待たずに消える。動いている人は消えない
	w2 := NewWatcher(root)
	write("b.json", "s2", "shop-page", "busy")
	w2.Snapshot(now)
	os.Remove(p2)
	w2.Snapshot(now)
	w2.Dismiss("s2")
	w2.Dismiss("s1")
	if got := w2.Snapshot(now); len(got) != 1 || got[0].Key != "s1" {
		t.Errorf("退出させた人が残っている、または動いている人が消えた: %+v", got)
	}

	// 閉じてから1時間を過ぎたら消える
	if got := w.Snapshot(now.Add(time.Minute + time.Hour + time.Second)); len(got) != 1 {
		t.Errorf("1時間を過ぎても閉じた人が残っている: %+v", got)
	}

	// 次の日には消える
	if got := w.Snapshot(now.Add(24 * time.Hour)); len(got) != 1 {
		t.Errorf("日が変わっても閉じた人が残っている: %+v", got)
	}
}

func TestSnapshotSkipsDeadProcess(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sessions"), 0o755)
	os.WriteFile(filepath.Join(root, "sessions", "1.json"), []byte(`{"pid":-5,"sessionId":"s","name":"x","status":"idle"}`), 0o644)
	if got := NewWatcher(root).Snapshot(time.Now()); len(got) != 0 {
		t.Errorf("動いていないプロセスが出ている: %+v", got)
	}
}

func TestLoadIslands(t *testing.T) {
	got, err := LoadIslands([]byte(`[{"id":"app","name":"APP","color":"green"},{"id":"web","name":"Web","color":"#4C7FD6","prefixes":["Web-App","web"]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Hex != "#3FB36B" || got[0].Color != "green" || got[0].Prefixes[0] != "app" {
		t.Errorf("色の名前か prefixes の省略が効いていない: %+v", got[0])
	}
	if got[1].Prefixes[0] != "web-app" {
		t.Errorf("prefixes が小文字になっていない: %+v", got[1])
	}
	for _, bad := range []string{
		`[{"id":"x","name":"X","color":"pink"}]`,
		`[{"id":"x","color":"green"}]`,
		`[{"id":"x","name":"X","color":"green"},{"id":"x","name":"Y","color":"red"}]`,
		`{"id":"x"}`,
	} {
		if _, err := LoadIslands([]byte(bad)); err == nil {
			t.Errorf("誤りを見逃した: %s", bad)
		}
	}
	if _, err := LoadIslands(exampleIslands); err != nil {
		t.Errorf("同梱の見本が読めない: %v", err)
	}
}

func TestSaveIslandsKeepsColorNamesAndChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "islands.json")
	list := []Island{
		{ID: "app", Name: "APP", Color: "green", Prefixes: []string{"app"}, Hex: "#3FB36B"},
		{ID: "web-app", Name: "ウェブ", Color: "blue", Prefixes: []string{"web-app", "web"}},
	}
	if err := SaveIslands(path, list); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	want := "[\n  {\"id\":\"app\",\"name\":\"APP\",\"color\":\"green\"},\n  {\"id\":\"web-app\",\"name\":\"ウェブ\",\"color\":\"blue\",\"prefixes\":[\"web-app\",\"web\"]}\n]\n"
	if string(b) != want {
		t.Errorf("書いた中身が違う:\n%s", b)
	}
	// 頭が重なる・id が日本語、は書かずに断る
	for _, bad := range [][]Island{
		{{ID: "app", Name: "A", Color: "green"}, {ID: "x", Name: "B", Color: "red", Prefixes: []string{"app"}}},
		{{ID: "日本語", Name: "A", Color: "green"}},
	} {
		if err := SaveIslands(path, bad); err == nil {
			t.Errorf("誤りを見逃した: %+v", bad)
		}
	}
	if b2, _ := os.ReadFile(path); string(b2) != want {
		t.Errorf("断ったのにファイルが変わった")
	}
}

func TestSameOrigin(t *testing.T) {
	r, _ := http.NewRequest("POST", "/api/islands", nil)
	if !sameOrigin(r, "127.0.0.1:7777") {
		t.Error("Origin なし（curl など）は通すはず")
	}
	for o, want := range map[string]bool{"http://127.0.0.1:7777": true, "http://localhost:7777": true, "http://[::1]:7777": true, "https://evil.example": false, "http://127.0.0.1:8080": false} {
		r.Header.Set("Origin", o)
		if got := sameOrigin(r, "127.0.0.1:7777"); got != want {
			t.Errorf("Origin %s: got %v", o, got)
		}
	}
}

func TestGuardRejectsForeignHost(t *testing.T) {
	ok := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(http.StatusOK) })
	for host, want := range map[string]int{
		"127.0.0.1:7777": 200, "localhost:7777": 200, "LOCALHOST": 200, "[::1]:7777": 200,
		"attacker.example:7777": 403, "192.168.1.10:7777": 403, "127.0.0.1.attacker.example": 403, "": 403,
	} {
		r := httptest.NewRequest("GET", "/api/crew", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		guard(ok, false).ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("Host %q: got %d, want %d", host, rec.Code, want)
		}
		if want == 200 && rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("Host %q: 枠に埋め込ませないヘッダーが無い", host)
		}
	}
	r := httptest.NewRequest("GET", "/api/crew", nil)
	r.Host = "192.168.1.10:7777"
	rec := httptest.NewRecorder()
	guard(ok, true).ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Errorf("-allow-remote なのに LAN の名前を断った: %d", rec.Code)
	}
}

func TestCheckLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:7777": true, "localhost:7777": true, "[::1]:7777": true,
		"0.0.0.0:7777": false, ":7777": false, "192.168.1.10:7777": false, "[::]:7777": false, "7777": false,
		// ポートの所に別の引数や改行を入れて、自動起動の設定に書かせない
		"127.0.0.1:7777 -allow-remote": false, "127.0.0.1:7777\nExecStartPre=/bin/sh": false, "127.0.0.1:0": false, "127.0.0.1:99999": false, "127.0.0.1:07777": false,
	} {
		if got := checkLoopback(addr) == nil; got != want {
			t.Errorf("%s: got %v, want %v", addr, got, want)
		}
	}
}

func TestStatuslineOddPercent(t *testing.T) {
	for _, pct := range []string{"-100", "1e308", "-1e308"} {
		got := statusline([]byte(`{"session_id":"x","session_name":"a","context_window":{"used_percentage":`+pct+`}}`), t.TempDir(), time.Unix(0, 0))
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

func TestSystemdQuote(t *testing.T) {
	if got := systemdQuote(`/home/a b/100%/$x/claude-office`); got != `"/home/a b/100%%/$$x/claude-office"` {
		t.Errorf("got %s", got)
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

func TestCtxOf(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "office", "ctx"), 0o755)
	os.WriteFile(filepath.Join(root, "office", "ctx", "s1.json"), []byte(`{"used":72,"size":1000000,"tokens":720000,"at":1}`), 0o644)
	w := NewWatcher(root)
	if c := w.ctxOf("s1"); c == nil || c.Used != 72 || c.Size != 1000000 {
		t.Errorf("ctx が読めない: %+v", c)
	}
	if c := w.ctxOf("none"); c != nil {
		t.Errorf("無いのに読めた: %+v", c)
	}
	os.WriteFile(filepath.Join(root, "office", "s2.json"), []byte(`{"used":1}`), 0o644)
	if c := w.ctxOf("../s2"); c != nil {
		t.Errorf("セッション ID でディレクトリの外を読んだ: %+v", c)
	}
}

func TestStatusline(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1_800_000_000, 0)
	in := []byte(`{"session_id":"abc-123","session_name":"app@review","context_window":{"context_window_size":1000000,"used_percentage":85,"current_usage":{"input_tokens":2,"cache_creation_input_tokens":10,"cache_read_input_tokens":849988}},"rate_limits":{"five_hour":{"used_percentage":42,"resets_at":1800003600}}}`)
	got := statusline(in, dir, now)
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
	if got := statusline([]byte(`{"session_id":"x","workspace":{"current_dir":"/a/proj"}}`), dir, now); got != "proj · ctx —" {
		t.Errorf("使用率が無いときの出力: %q", got)
	}
	statusline([]byte(`{"session_id":"../evil","context_window":{"used_percentage":5}}`), dir, now)
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

func TestVersionString(t *testing.T) {
	defer func(v string) { version = v }(version)
	version = "1.2.3"
	if got := versionString(); got != "1.2.3" {
		t.Errorf("ldflags で入れた版 = %q, want 1.2.3", got)
	}
	version = ""
	if got := versionString(); got != "dev" { // go test は (devel) として組み立てる
		t.Errorf("版が無いとき = %q, want dev", got)
	}
}
