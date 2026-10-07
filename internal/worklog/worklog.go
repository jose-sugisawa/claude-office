// Package worklog は、会話の記録（~/.claude/projects/*/<セッションID>.jsonl）の時刻から、
// 今日それぞれのセッションが動いていた時間と、指示した回数を数える。Claude は呼ばないのでトークンは使わない。
//
// 動いていた時間は「指示を出してから、返事が終わる（turn_duration の印）まで」。
// 印の無い古い記録では次の指示までに Claude が最後に何かした時刻まで。
// どちらも、あいだに Gap より長く何も起きなかったら（確認の返事待ちなど）、そこで区切る。
package worklog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Gap は、1つの指示の中でもこれより長く何も起きなければ、動いていないとみなす長さ。
const Gap = 30 * time.Minute

// Span は動いていた1区間。Name はそのときのセッション名（/rename で島を移ると、そこから変わる）。
type Span struct {
	Name    string `json:"name"`
	Start   int64  `json:"start"`   // ミリ秒
	End     int64  `json:"end"`     // ミリ秒
	Prompts int    `json:"prompts"` // この区間を始めた指示の数（0 か 1）
}

// Session は1つのセッションの今日の区間。
type Session struct {
	Key   string `json:"key"` // セッション ID
	Spans []Span `json:"spans"`
}

// Day は今日の分（From＝手元の時刻の0時から Now まで）。
type Day struct {
	From     int64     `json:"from"`
	Now      int64     `json:"now"`
	Sessions []Session `json:"sessions"`
}

// file は1つの会話の記録を、どこまで読んだかと、そこまでの区間。
type file struct {
	offset int64
	name   string // 今のセッション名（custom-title）
	first  string // 最初に付いた名前（名前を付ける前の区間に使う）
	spans  []Span
	cur    *Span
}

// Reader は会話の記録を、前に読んだ続きから読む。日付が変わったら読み直す。
type Reader struct {
	root string // ~/.claude

	mu    sync.Mutex
	from  time.Time
	files map[string]*file
}

// NewReader は root（~/.claude）の会話の記録を読む Reader を作る。
func NewReader(root string) *Reader { return &Reader{root: root, files: map[string]*file{}} }

// Today は、今日（手元の時刻の0時から now まで）動いていたセッションの区間を返す。
func (r *Reader) Today(now time.Time) Day {
	r.mu.Lock()
	defer r.mu.Unlock()

	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if !from.Equal(r.from) {
		r.from, r.files = from, map[string]*file{}
	}
	paths, _ := filepath.Glob(filepath.Join(r.root, "projects", "*", "*.jsonl"))
	day := Day{From: from.UnixMilli(), Now: now.UnixMilli(), Sessions: []Session{}}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || st.ModTime().Before(from) {
			continue
		}
		f := r.files[p]
		if f == nil || st.Size() < f.offset { // 縮んだ（書き直された）ら最初から
			f = &file{}
			r.files[p] = f
		}
		if st.Size() > f.offset {
			f.read(p, from.UnixMilli())
		}
		if spans := f.result(now.UnixMilli()); len(spans) > 0 {
			day.Sessions = append(day.Sessions, Session{Key: strings.TrimSuffix(filepath.Base(p), ".jsonl"), Spans: spans})
		}
	}
	sort.Slice(day.Sessions, func(i, j int) bool { return day.Sessions[i].Key < day.Sessions[j].Key })
	return day
}

// read は offset から読み、終わりまで書かれた行だけを使う（書きかけの最後の行は次に読む）。
func (f *file) read(path string, from int64) {
	fh, err := os.Open(path)
	if err != nil {
		return
	}
	defer fh.Close()
	if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
		return
	}
	br := bufio.NewReaderSize(fh, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil { // 改行で終わっていない行は書きかけ
			return
		}
		f.offset += int64(len(line))
		f.add(line, from)
	}
}

type entry struct {
	Type        string    `json:"type"`
	Subtype     string    `json:"subtype"`
	Timestamp   time.Time `json:"timestamp"`
	IsMeta      bool      `json:"isMeta"`
	IsSidechain bool      `json:"isSidechain"`
	CustomTitle string    `json:"customTitle"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// add は1行を区間に足す。
func (f *file) add(line []byte, from int64) {
	if !bytes.Contains(line, []byte(`"type":"user"`)) && !bytes.Contains(line, []byte(`"type":"assistant"`)) &&
		!bytes.Contains(line, []byte(`"type":"custom-title"`)) && !bytes.Contains(line, []byte(`"turn_duration"`)) {
		return // 速くするため、使わない行は JSON として読まない
	}
	var e entry
	if json.Unmarshal(line, &e) != nil {
		return
	}
	// 返事が終わった印。決まった時刻に動く確認（/loop など）が数分おきに来ても、つなげて数えない
	if e.Type == "system" && e.Subtype == "turn_duration" {
		if f.cur != nil && !e.Timestamp.IsZero() && e.Timestamp.UnixMilli() > f.cur.End {
			f.cur.End = e.Timestamp.UnixMilli()
		}
		f.close()
		return
	}
	if e.Type == "custom-title" {
		if e.CustomTitle != "" {
			f.name = e.CustomTitle
			if f.first == "" {
				f.first = e.CustomTitle
			}
		}
		return
	}
	if (e.Type != "user" && e.Type != "assistant") || e.IsMeta || e.Timestamp.IsZero() {
		return
	}
	t := e.Timestamp.UnixMilli()
	if t < from {
		return
	}
	start, prompt := false, false
	if e.Type == "user" && !e.IsSidechain {
		start, prompt = kindOf(e.Message.Content)
	}
	if start || f.cur == nil || t-f.cur.End > Gap.Milliseconds() {
		f.close()
		n := 0
		if prompt {
			n = 1
		}
		f.cur = &Span{Name: f.name, Start: t, End: t, Prompts: n}
		return
	}
	if t > f.cur.End {
		f.cur.End = t
	}
}

func (f *file) close() {
	if f.cur != nil && (f.cur.End > f.cur.Start || f.cur.Prompts > 0) {
		f.spans = append(f.spans, *f.cur)
	}
	f.cur = nil
}

// result は今までの区間（読みかけの区間も含む）。名前を付ける前の区間は、最初に付いた名前にする。
func (f *file) result(now int64) []Span {
	out := append([]Span(nil), f.spans...)
	if f.cur != nil && (f.cur.End > f.cur.Start || f.cur.Prompts > 0) {
		out = append(out, *f.cur)
	}
	for i := range out {
		if out[i].Name == "" {
			out[i].Name = f.first
		}
		if out[i].End > now {
			out[i].End = now
		}
	}
	return out
}

// kindOf は、user の行が新しい区間を始めるか（start）と、人が打った指示か（prompt）を返す。
// ツールの結果は Claude の作業の続き。スラッシュコマンドや裏で動いた作業の知らせは、区間は始めるが指示には数えない。
func kindOf(raw json.RawMessage) (start, prompt bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return true, isPrompt(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return false, false
	}
	text := ""
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			return false, false
		case "text":
			if text == "" {
				text = b.Text
			}
		case "image":
			if text == "" {
				text = "（画像）"
			}
		}
	}
	if text == "" {
		return false, false
	}
	return true, isPrompt(text)
}

func isPrompt(s string) bool {
	s = strings.TrimSpace(s)
	for _, p := range []string{"<command-", "<local-command-", "<task-notification", "<system-reminder", "[Request interrupted"} {
		if strings.HasPrefix(s, p) {
			return false
		}
	}
	return s != ""
}
