// Package decision は判断箱（decisions.json）を読み、確かめ、書く。
// ボスのセッションが問いを置き、人が画面で選ぶかメモを書く。ボスがそれを読んで係に伝え、伝えたら印を付ける。
// 画面から係に出す指示（Note）も、ボスが伝える。オフィスは Claude を呼ばず、どのセッションにも直接は送らない。
package decision

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jose-sugisawa/claude-office/internal/atomicfile"
)

// 選択肢のほかに、いつでも選べる答え（claude.ai の判断箱とそろえる）。
const (
	Later = "later" // あとで（ボスは触らない）
	Ask   = "ask"   // 相談したい（ボスが報告に載せる）
	Memo  = "memo"  // 選ばずにメモだけ書いた
)

// Option は問いの選択肢。V はボスが答えを読むときの値。
type Option struct {
	V     string `json:"v"`
	Label string `json:"label"`
	Rec   bool   `json:"rec,omitempty"` // 係のおすすめ
}

// Link は問いに添えるリンク（PR など）。http(s) だけ。
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Auto は「期限までに答えがなければ、おすすめで進める」の約束。オフィスは表示するだけで、進めるのはボス。
type Auto struct {
	Choice string `json:"choice"`
	At     string `json:"at"` // ISO 8601
	Undo   string `json:"undo,omitempty"`
}

// Question は判断箱の問い1つ。Status は open（答え待ち）・closed（伝え終えた）・auto（期限で進めた）。
type Question struct {
	ID      string   `json:"id"`
	Island  string   `json:"island"`
	Color   string   `json:"color,omitempty"`
	Session string   `json:"session,omitempty"`
	Title   string   `json:"title"`
	Context string   `json:"context,omitempty"`
	Options []Option `json:"options"`
	Links   []Link   `json:"links,omitempty"`
	Due     string   `json:"due,omitempty"` // YYYY-MM-DD
	Order   int      `json:"order,omitempty"`
	Status  string   `json:"status"`
	Created string   `json:"created,omitempty"` // YYYY-MM-DD
	Auto    *Auto    `json:"auto,omitempty"`
}

// Answer は問いへの答え。By は答えた場所（office か claude.ai）。Relayed はボスが係に伝えたか。
type Answer struct {
	Choice  string `json:"choice"`
	Note    string `json:"note,omitempty"`
	At      int64  `json:"at"` // ミリ秒
	By      string `json:"by"`
	Relayed bool   `json:"relayed"`
}

// Note は画面から出した指示。To は係名（空ならボスあて）。
type Note struct {
	ID      string `json:"id"`
	To      string `json:"to,omitempty"`
	Text    string `json:"text"`
	At      int64  `json:"at"`
	Relayed bool   `json:"relayed"`
}

// Box は decisions.json の中身。
type Box struct {
	Questions []Question        `json:"questions"`
	Answers   map[string]Answer `json:"answers"`
	Notes     []Note            `json:"notes"`
}

var (
	idRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
	hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	dateRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// 文字数の上限（貼り付けすぎでファイルが大きくならないように）。
const (
	maxTitle   = 200
	maxContext = 4000
	maxLabel   = 200
	maxText    = 2000
	keepNotes  = 200 // 伝え終えた指示は新しい順にこれだけ残す
)

// Store は decisions.json を1つのプロセスの中で順番に読み書きする。
type Store struct {
	path string
	mu   sync.Mutex
	now  func() time.Time
}

// NewStore は path の判断箱を開く（ファイルが無ければ空の箱として読む）。
func NewStore(path string) *Store { return &Store{path: path, now: time.Now} }

// Load は判断箱を読む。問いは open を先に、期日・order・作った日の順に並べる。
func (s *Store) Load() (Box, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.read()
	if err != nil {
		return Box{}, err
	}
	sortQuestions(b.Questions)
	return b, nil
}

// PutQuestions は問いを足すか、同じ id の問いを置き換える（答えはそのまま）。
func (s *Store) PutQuestions(in []Question) error {
	for i := range in {
		if err := clean(&in[i]); err != nil {
			return fmt.Errorf("%d 番目の問い：%w", i+1, err)
		}
	}
	return s.update(func(b *Box) error {
		at := map[string]int{}
		for i, q := range b.Questions {
			at[q.ID] = i
		}
		for _, q := range in {
			if i, ok := at[q.ID]; ok {
				b.Questions[i] = q
			} else {
				at[q.ID] = len(b.Questions)
				b.Questions = append(b.Questions, q)
			}
		}
		return nil
	})
}

// SetAnswer は問いに答える。choice は選択肢の v・later・ask、または選ばずにメモだけなら空。
// 伝え終えた問いには答えられない（ボスがもう係に伝えたので、変えるなら新しい問いにする）。
func (s *Store) SetAnswer(id, choice, note, by string) error {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxText {
		return fmt.Errorf("メモは %d 文字までです", maxText)
	}
	if by != "claude.ai" {
		by = "office"
	}
	return s.update(func(b *Box) error {
		q := find(b.Questions, id)
		if q == nil {
			return errors.New("その問いはありません")
		}
		if q.Status != "open" {
			return errors.New("この問いはもう伝え終えています")
		}
		switch {
		case choice == "" && note == "":
			return errors.New("選ぶか、メモを書いてください")
		case choice == "":
			choice = Memo
		case choice != Later && choice != Ask && !hasOption(q, choice):
			return errors.New("その選択肢はありません")
		}
		b.Answers[id] = Answer{Choice: choice, Note: note, At: s.now().UnixMilli(), By: by}
		return nil
	})
}

// AddNote は画面から出した指示を足す。to は係名（空ならボスあて）。
func (s *Store) AddNote(to, text string) (Note, error) {
	to, text = strings.TrimSpace(to), strings.TrimSpace(text)
	if text == "" {
		return Note{}, errors.New("指示が空です")
	}
	if utf8.RuneCountInString(text) > maxText || utf8.RuneCountInString(to) > maxLabel {
		return Note{}, fmt.Errorf("指示は %d 文字までです", maxText)
	}
	now := s.now().UnixMilli()
	var n Note
	err := s.update(func(b *Box) error {
		for _, x := range b.Notes { // 二重送信（連打）は断る
			if x.To == to && x.Text == text && now-x.At < 10_000 {
				return errors.New("同じ指示をいま送ったところです")
			}
		}
		n = Note{ID: "n" + strconv.FormatInt(now, 36) + "-" + strconv.Itoa(len(b.Notes)), To: to, Text: text, At: now}
		b.Notes = append(b.Notes, n)
		return nil
	})
	return n, err
}

// MarkRelayed は、ボスが係に伝え終えた答えと指示に印を付ける。
// 選んだ答え（あとで・相談したい以外）を伝えたら、その問いは closed にする。
func (s *Store) MarkRelayed(answers, notes []string) error {
	return s.update(func(b *Box) error {
		for _, id := range answers {
			a, ok := b.Answers[id]
			if !ok {
				continue
			}
			a.Relayed = true
			b.Answers[id] = a
			if q := find(b.Questions, id); q != nil && a.Choice != Later && a.Choice != Ask {
				q.Status = "closed"
			}
		}
		done := map[string]bool{}
		for _, id := range notes {
			done[id] = true
		}
		for i := range b.Notes {
			if done[b.Notes[i].ID] {
				b.Notes[i].Relayed = true
			}
		}
		b.Notes = trimNotes(b.Notes)
		return nil
	})
}

func (s *Store) update(f func(*Box) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.read()
	if err != nil {
		return err
	}
	if err := f(&b); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(b, "", " ")
	return atomicfile.Write(s.path, append(out, '\n'))
}

func (s *Store) read() (Box, error) {
	b := Box{Answers: map[string]Answer{}}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return b, fmt.Errorf("decisions.json が JSON として読めません: %w", err)
	}
	if b.Answers == nil {
		b.Answers = map[string]Answer{}
	}
	return b, nil
}

// clean は問いを確かめ、空白を落とす。画面はリンクを href に入れるので http(s) 以外は断る。
func clean(q *Question) error {
	q.ID, q.Title, q.Island = strings.TrimSpace(q.ID), strings.TrimSpace(q.Title), strings.TrimSpace(q.Island)
	if !idRe.MatchString(q.ID) {
		return fmt.Errorf("id %q は英数字と . _ : - の80文字までにしてください", q.ID)
	}
	if q.Title == "" || utf8.RuneCountInString(q.Title) > maxTitle {
		return fmt.Errorf("%s：title は1〜%d文字です", q.ID, maxTitle)
	}
	if utf8.RuneCountInString(q.Context) > maxContext {
		return fmt.Errorf("%s：context は %d 文字までです", q.ID, maxContext)
	}
	if q.Color != "" && !hexColor.MatchString(q.Color) {
		return fmt.Errorf("%s：color は #RRGGBB です", q.ID)
	}
	if (q.Due != "" && !dateRe.MatchString(q.Due)) || (q.Created != "" && !dateRe.MatchString(q.Created)) {
		return fmt.Errorf("%s：due と created は YYYY-MM-DD です", q.ID)
	}
	switch q.Status {
	case "":
		q.Status = "open"
	case "open", "closed", "auto":
	default:
		return fmt.Errorf("%s：status は open・closed・auto のどれかです", q.ID)
	}
	seen := map[string]bool{Later: true, Ask: true, Memo: true}
	for _, o := range q.Options {
		if o.V == "" || seen[o.V] || o.Label == "" || utf8.RuneCountInString(o.Label) > maxLabel {
			return fmt.Errorf("%s：選択肢 %q は v（重ならない・later/ask/memo 以外）と label が要ります", q.ID, o.V)
		}
		seen[o.V] = true
	}
	for _, l := range q.Links {
		u, err := url.Parse(l.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("%s：リンク %q は http(s) の URL にしてください", q.ID, l.URL)
		}
	}
	return nil
}

func find(qs []Question, id string) *Question {
	for i := range qs {
		if qs[i].ID == id {
			return &qs[i]
		}
	}
	return nil
}

func hasOption(q *Question, v string) bool {
	for _, o := range q.Options {
		if o.V == v {
			return true
		}
	}
	return false
}

// trimNotes は伝え終えた指示を新しい keepNotes 件だけ残す（伝え待ちは全部残す）。
func trimNotes(ns []Note) []Note {
	done := 0
	for i := len(ns) - 1; i >= 0; i-- {
		if ns[i].Relayed {
			done++
			if done > keepNotes {
				ns = append(ns[:i], ns[i+1:]...)
			}
		}
	}
	return ns
}

func sortQuestions(qs []Question) {
	key := func(q Question) string { // 期日の無いものは後ろ
		if q.Due == "" {
			return "9999-99-99"
		}
		return q.Due
	}
	sort.SliceStable(qs, func(i, j int) bool {
		a, b := qs[i], qs[j]
		if (a.Status == "open") != (b.Status == "open") {
			return a.Status == "open"
		}
		if key(a) != key(b) {
			return key(a) < key(b)
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Created < b.Created
	})
}
