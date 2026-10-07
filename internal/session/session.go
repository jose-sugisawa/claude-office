// Package session は、手元で動いている Claude Code のセッションを ~/.claude から読み、画面に出す1人ずつにする。
package session

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jose-sugisawa/claude-office/internal/claudehome"
)

// Member は画面に出す1人（1つの Claude セッション）。
type Member struct {
	Key     string          `json:"key"`     // sessionId
	Name    string          `json:"name"`    // claude -n で付けた係名（なければ Claude Code が付けた名前）
	Status  string          `json:"status"`  // busy / idle / waiting（許可などの確認待ち）は Claude Code が書いたまま。終了したら gone
	Since   int64           `json:"since"`   // 今の状態になった時刻（ミリ秒）
	Line    string          `json:"line"`    // 最後の返事の1行目
	Excerpt string          `json:"excerpt"` // 最後の返事の頭の数行（カードに出す）
	Dir     string          `json:"dir"`     // 作業ディレクトリの名前
	Cwd     string          `json:"cwd"`     // 作業ディレクトリ（ホームは ~）
	Named   bool            `json:"named"`   // claude -n や /rename で名前を付けたか（false は自動の名前）
	Waiting string          `json:"waiting"` // status が waiting のとき、何を待っているか（permission など）
	Ctx     *claudehome.Ctx `json:"ctx"`     // コンテキストの使用率（ステータスラインが書いたもの。まだ無ければ null）
}

func (w *Watcher) ctxOf(sessionID string) *claudehome.Ctx {
	path, ok := claudehome.CtxPath(claudehome.Office(w.root), sessionID)
	if !ok {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c claudehome.Ctx
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	return &c
}

// sessionFile は ~/.claude/sessions/<pid>.json のうち使うところ。
type sessionFile struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	Name            string `json:"name"`
	NameSource      string `json:"nameSource"`
	WaitingFor      string `json:"waitingFor"`
	Status          string `json:"status"`
	Cwd             string `json:"cwd"`
	StartedAt       int64  `json:"startedAt"`
	UpdatedAt       int64  `json:"updatedAt"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
}

type logCache struct {
	path    string
	size    int64
	line    string
	excerpt string
}

// goneKeep は閉じた人を空いた席として残す長さ。
const goneKeep = time.Hour

// Watcher は呼ばれるたびに sessions を読み直す。今日いた人は、閉じてから goneKeep のあいだ gone として残す。
type Watcher struct {
	root string // ~/.claude

	mu   sync.Mutex
	day  string
	seen map[string]Member   // 今日いた人（sessionId ごと）
	gone map[string]int64    // 閉じたと気づいた時刻
	logs map[string]logCache // sessionId ごとの会話ログの読み取り結果
}

// NewWatcher は root（~/.claude）を読む Watcher を作る。
func NewWatcher(root string) *Watcher {
	return &Watcher{root: root, seen: map[string]Member{}, gone: map[string]int64{}, logs: map[string]logCache{}}
}

// Snapshot は、動いている人と今日いて閉じた人を、名前の順に返す。
func (w *Watcher) Snapshot(now time.Time) []Member {
	w.mu.Lock()
	defer w.mu.Unlock()

	if d := now.Format("2006-01-02"); d != w.day {
		w.day, w.seen, w.gone = d, map[string]Member{}, map[string]int64{}
	}

	files, _ := filepath.Glob(filepath.Join(w.root, "sessions", "*.json"))
	var out []Member
	alive := map[string]bool{}
	aliveNames := map[string]bool{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s sessionFile
		if json.Unmarshal(b, &s) != nil || s.SessionID == "" || !processAlive(s.PID) {
			continue
		}
		line, excerpt := w.lastReplyOf(s.SessionID)
		m := Member{
			Key:     s.SessionID,
			Name:    s.Name,
			Status:  s.Status,
			Since:   firstNonZero(s.StatusUpdatedAt, s.UpdatedAt, s.StartedAt),
			Line:    line,
			Excerpt: excerpt,
			Dir:     filepath.Base(s.Cwd),
			Cwd:     tildePath(s.Cwd, filepath.Dir(w.root)),
			Named:   s.NameSource == "user",
			Waiting: s.WaitingFor,
			Ctx:     w.ctxOf(s.SessionID),
		}
		if m.Name == "" {
			m.Name = m.Dir
		}
		out = append(out, m)
		alive[m.Key] = true
		aliveNames[m.Name] = true
		w.seen[m.Key] = m
		delete(w.gone, m.Key)
	}

	// 今日いて、もう動いていない人は1時間だけ空いた席として残す（同じ係名で開き直していれば出さない）
	for key, m := range w.seen {
		if alive[key] || aliveNames[m.Name] {
			continue
		}
		if _, ok := w.gone[key]; !ok {
			w.gone[key] = now.UnixMilli()
		}
		if now.UnixMilli()-w.gone[key] > goneKeep.Milliseconds() {
			continue
		}
		m.Status, m.Since = "gone", w.gone[key]
		out = append(out, m)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Dismiss は閉じた人の空席を、1時間を待たずに消す。動いている人には何もしない（次に読んだときにまた出る）。
func (w *Watcher) Dismiss(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.gone[key]; ok {
		delete(w.seen, key)
		delete(w.gone, key)
	}
}

// lastReplyOf は会話ログの末尾から最後の返事の1行目と頭の数行を返す。ファイルの大きさが変わらなければ前の結果を使う。
func (w *Watcher) lastReplyOf(sessionID string) (string, string) {
	c := w.logs[sessionID]
	if c.path == "" {
		if !claudehome.ValidSessionID(sessionID) { // ファイル名と Glob に使うので、* や ../ を含む ID は読まない
			return "", ""
		}
		found, _ := filepath.Glob(filepath.Join(w.root, "projects", "*", sessionID+".jsonl"))
		if len(found) == 0 {
			return "", ""
		}
		c.path = found[0]
	}
	st, err := os.Stat(c.path)
	if err != nil || st.Size() == c.size {
		return c.line, c.excerpt
	}
	f, err := os.Open(c.path)
	if err != nil {
		return c.line, c.excerpt
	}
	defer f.Close()
	const tail = 512 << 10
	if st.Size() > tail {
		f.Seek(st.Size()-tail, io.SeekStart)
	}
	data, _ := io.ReadAll(f)
	text := lastReply(data)
	c.size, c.line, c.excerpt = st.Size(), firstLine(text), excerpt(text)
	w.logs[sessionID] = c
	return c.line, c.excerpt
}

// lastReply は会話ログ（JSON Lines）の末尾から、Claude の最後の文章の返事を探して返す。
func lastReply(data []byte) string {
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var e struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			Message     struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[i], &e) != nil || e.Type != "assistant" || e.IsSidechain {
			continue
		}
		if t := textOf(e.Message.Content); firstLine(t) != "" {
			return t
		}
	}
	return ""
}

func textOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(raw, &blocks)
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// cleanLine は1行から Markdown の飾り（太字・コード・見出し・箇条書き）を外す。
func cleanLine(l string) string {
	l = strings.NewReplacer("**", "", "`", "").Replace(l)
	l = strings.TrimSpace(l)
	if h := strings.TrimLeft(l, "#"); h != l && strings.HasPrefix(h, " ") { // 見出しの # だけ外す（#259 は残す）
		l = h
	}
	l = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(l), "- "), "* ")
	return strings.TrimSpace(l)
}

// firstLine は通知（claude-notify.sh）と同じく、飾りを外した最初の行を60文字までにする。
func firstLine(text string) string {
	for _, l := range strings.Split(text, "\n") {
		if l = cleanLine(l); l != "" {
			return cut(l, 60)
		}
	}
	return ""
}

// excerpt はカードに出す頭の数行（空行とコードの囲みを除いて8行・400文字まで）。
func excerpt(text string) string {
	var out []string
	n := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			continue
		}
		if l = cleanLine(l); l == "" {
			continue
		}
		l = cut(l, 400-n)
		out = append(out, l)
		n += utf8.RuneCountInString(l)
		if len(out) == 8 || n >= 400 {
			break
		}
	}
	return strings.Join(out, "\n")
}

func cut(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}

func tildePath(p, home string) string {
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func firstNonZero(vs ...int64) int64 {
	for _, v := range vs {
		if v != 0 {
			return v
		}
	}
	return 0
}
