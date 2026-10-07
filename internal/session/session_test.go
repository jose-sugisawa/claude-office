package session

import (
	"fmt"
	"os"
	"path/filepath"
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
