package worklog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 手元の時刻で 2026-10-07 の朝から
var day = time.Date(2026, 10, 7, 0, 0, 0, 0, time.Local)

func at(h, m int) string {
	return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute).UTC().Format(time.RFC3339Nano)
}

func user(ts, text string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":%q}}`, ts, text)
}
func toolResult(ts string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`, ts)
}
func assistant(ts string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"content":[{"type":"text","text":"はい"}]}}`, ts)
}
func title(name string) string { return fmt.Sprintf(`{"type":"custom-title","customTitle":%q}`, name) }

func writeLog(t *testing.T, root, id string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(root, "projects", "-src-app")
	os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	noon := day.Add(12 * time.Hour) // 書かれたのは「今日」（テストを走らせる日に左右されないように）
	os.Chtimes(p, noon, noon)
	return p
}

func TestToday(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "s1",
		user(day.Add(-time.Hour).UTC().Format(time.RFC3339Nano), "昨日の指示"), // 0時より前は数えない
		assistant(day.Add(-30*time.Minute).UTC().Format(time.RFC3339Nano)),
		user(at(9, 0), "テストを足して"), // 名前を付ける前：あとで付いた名前にする
		toolResult(at(9, 5)),
		assistant(at(9, 10)),
		title("app@api"),
		user(at(9, 30), "<command-name>/usage</command-name>"), // 区間は始めるが指示には数えない
		user(at(10, 0), "次はドキュメント"),
		assistant(at(10, 20)),
		assistant(at(11, 0)), // 40分あいた：確認待ちなどとみなして区切る
		assistant(at(11, 5)),
		title("blog@draft"),
		user(at(13, 0), "ブログを書いて"),
		assistant(at(13, 25)),
	)
	r := NewReader(root)
	d := r.Today(day.Add(14 * time.Hour))
	if len(d.Sessions) != 1 || d.Sessions[0].Key != "s1" {
		t.Fatalf("sessions = %+v", d.Sessions)
	}
	type want struct {
		name       string
		start, end string
		prompts    int
	}
	ms := func(s string) int64 { tt, _ := time.Parse(time.RFC3339Nano, s); return tt.UnixMilli() }
	wants := []want{
		{"app@api", at(9, 0), at(9, 10), 1},
		{"app@api", at(10, 0), at(10, 20), 1},
		{"app@api", at(11, 0), at(11, 5), 0},
		{"blog@draft", at(13, 0), at(13, 25), 1},
	}
	got := d.Sessions[0].Spans
	if len(got) != len(wants) {
		t.Fatalf("spans = %+v", got)
	}
	for i, w := range wants {
		if got[i].Name != w.name || got[i].Start != ms(w.start) || got[i].End != ms(w.end) || got[i].Prompts != w.prompts {
			t.Errorf("span %d = %+v, want %+v", i, got[i], w)
		}
	}
}

// /loop などで5分おきに数秒だけ動くセッションを、ひと続きに数えない
func TestTodayClosesAtTurnEnd(t *testing.T) {
	root := t.TempDir()
	var lines []string
	lines = append(lines, title("app@watch"))
	for i := 0; i < 6; i++ {
		ts := day.Add(9*time.Hour + time.Duration(i)*5*time.Minute)
		s := func(sec int) string { return ts.Add(time.Duration(sec) * time.Second).UTC().Format(time.RFC3339Nano) }
		lines = append(lines,
			fmt.Sprintf(`{"type":"user","isMeta":true,"timestamp":%q,"message":{"content":"check"}}`, s(0)),
			assistant(s(3)), toolResult(s(5)), assistant(s(8)),
			fmt.Sprintf(`{"type":"system","subtype":"turn_duration","timestamp":%q}`, s(9)))
	}
	writeLog(t, root, "w1", lines...)
	d := NewReader(root).Today(day.Add(10 * time.Hour))
	var total int64
	for _, sp := range d.Sessions[0].Spans {
		total += sp.End - sp.Start
	}
	if n := len(d.Sessions[0].Spans); n != 6 || total != 6*6e3 {
		t.Fatalf("区間 %d 個・合計 %dms, want 6 個・36000ms: %+v", n, total, d.Sessions[0].Spans)
	}
}

func TestTodayReadsOnlyNewLines(t *testing.T) {
	root := t.TempDir()
	p := writeLog(t, root, "s2", title("app@review"), user(at(9, 0), "見て"), assistant(at(9, 3)))
	r := NewReader(root)
	if d := r.Today(day.Add(10 * time.Hour)); len(d.Sessions[0].Spans) != 1 || d.Sessions[0].Spans[0].End-d.Sessions[0].Spans[0].Start != 3*60e3 {
		t.Fatalf("1回目 = %+v", d.Sessions)
	}
	// 続きが書かれる（最後の行は書きかけ）
	fh, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString(assistant(at(9, 8)) + "\n" + `{"type":"assistant","timest`)
	fh.Close()
	noon := day.Add(12 * time.Hour)
	os.Chtimes(p, noon, noon)
	d := r.Today(day.Add(10 * time.Hour))
	if s := d.Sessions[0].Spans; len(s) != 1 || s[0].End-s[0].Start != 8*60e3 {
		t.Fatalf("2回目 = %+v", s)
	}
}

func TestTodaySkipsOldFiles(t *testing.T) {
	root := t.TempDir()
	p := writeLog(t, root, "old", user(at(9, 0), "見て"), assistant(at(9, 3)))
	old := day.Add(-2 * time.Hour)
	os.Chtimes(p, old, old)
	if d := NewReader(root).Today(day.Add(10 * time.Hour)); len(d.Sessions) != 0 {
		t.Fatalf("昨日から書かれていないファイルを読んだ: %+v", d.Sessions)
	}
}

func TestKindOf(t *testing.T) {
	cases := []struct {
		raw           string
		start, prompt bool
	}{
		{`"進めて"`, true, true},
		{`[{"type":"text","text":"これ見て"},{"type":"image"}]`, true, true},
		{`[{"type":"image"}]`, true, true},
		{`[{"type":"tool_result","content":"ok"}]`, false, false},
		{`"<task-notification>終わりました</task-notification>"`, true, false},
		{`"[Request interrupted by user]"`, true, false},
	}
	for _, c := range cases {
		if s, p := kindOf([]byte(c.raw)); s != c.start || p != c.prompt {
			t.Errorf("kindOf(%s) = %v,%v want %v,%v", c.raw, s, p, c.start, c.prompt)
		}
	}
}

func spansOf(t *testing.T, root string, now time.Time) []Span {
	t.Helper()
	var out []Span
	for _, s := range NewReader(root).Today(now).Sessions {
		out = append(out, s.Spans...)
	}
	return out
}

// サブエージェントや長いビルドで記録が30分以上止まっても、ツールの途中なら働いている
func TestTodayLongToolCall(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "t1", title("app@build"), user(at(10, 0), "ビルドして"),
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"content":[{"type":"tool_use","name":"Agent"}]}}`, at(10, 1)),
		toolResult(at(10, 46)), assistant(at(10, 50)))
	s := spansOf(t, root, day.Add(12*time.Hour))
	if len(s) != 1 || s[0].End-s[0].Start != 50*60e3 {
		t.Fatalf("spans = %+v", s)
	}
}

// 最後の返事のあと長く放っておいて、返事が終わった印が遅れて来ても、その間は数えない
func TestTodayTurnEndAfterIdle(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "t2", title("app@x"), user(at(10, 0), "見て"), assistant(at(10, 5)),
		fmt.Sprintf(`{"type":"system","subtype":"turn_duration","timestamp":%q}`, at(11, 30)))
	if s := spansOf(t, root, day.Add(12*time.Hour)); len(s) != 1 || s[0].End-s[0].Start != 5*60e3 {
		t.Fatalf("spans = %+v", s)
	}
}

// /compact の要約は指示に数えない。--fork-session で写された行は二度数えない
func TestTodayCompactAndFork(t *testing.T) {
	root := t.TempDir()
	withID := func(line, id string) string { return strings.Replace(line, "{", fmt.Sprintf(`{"uuid":%q,`, id), 1) }
	morning := []string{withID(user(at(9, 0), "朝の指示"), "u1"), withID(assistant(at(9, 10)), "a1")}
	writeLog(t, root, "orig", append([]string{title("app@a")}, morning...)...)
	writeLog(t, root, "fork", append(append([]string{title("app@a")}, morning...),
		fmt.Sprintf(`{"uuid":"c1","type":"user","isCompactSummary":true,"isVisibleInTranscriptOnly":true,"timestamp":%q,"message":{"content":"This session is being continued"}}`, at(13, 0)),
		withID(assistant(at(13, 5)), "a2"))...)
	var ms int64
	prompts := 0
	for _, sp := range spansOf(t, root, day.Add(14*time.Hour)) {
		ms += sp.End - sp.Start
		prompts += sp.Prompts
	}
	if ms != 15*60e3 || prompts != 1 {
		t.Fatalf("合計 %d 分・指示 %d 回, want 15 分・1 回", ms/60e3, prompts)
	}
}
