package decision

import (
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	s := NewStore(filepath.Join(t.TempDir(), "decisions.json"))
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	s.now = func() time.Time { at = at.Add(time.Minute); return at }
	return s
}

func q(id string) Question {
	return Question{ID: id, Island: "OHANA", Title: "問い " + id, Options: []Option{{V: "o1", Label: "案1", Rec: true}, {V: "o2", Label: "案2"}}}
}

func TestPutAndAnswer(t *testing.T) {
	s := newStore(t)
	if err := s.PutQuestions([]Question{q("a-1"), q("a-2")}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAnswer("a-1", "o2", " 色だけ直して ", "office"); err != nil {
		t.Fatal(err)
	}
	// 置き換えても答えは残る
	changed := q("a-1")
	changed.Title = "直した問い"
	if err := s.PutQuestions([]Question{changed}); err != nil {
		t.Fatal(err)
	}
	b, _ := s.Load()
	if len(b.Questions) != 2 || b.Questions[0].Title != "直した問い" || b.Questions[0].Status != "open" {
		t.Fatalf("問いが違う: %+v", b.Questions)
	}
	if a := b.Answers["a-1"]; a.Choice != "o2" || a.Note != "色だけ直して" || a.By != "office" || a.Relayed {
		t.Errorf("答えが違う: %+v", a)
	}

	// 選択肢にない値は断る。あとで・相談したい・メモだけは受ける
	if err := s.SetAnswer("a-2", "o9", "", ""); err == nil {
		t.Error("選択肢にない値を受けた")
	}
	if err := s.SetAnswer("a-2", "", "", ""); err == nil {
		t.Error("空の答えを受けた")
	}
	if err := s.SetAnswer("a-2", "", "メモだけ", "claude.ai"); err != nil {
		t.Fatal(err)
	}
	b, _ = s.Load()
	if a := b.Answers["a-2"]; a.Choice != Memo || a.By != "claude.ai" {
		t.Errorf("メモだけの答えが違う: %+v", a)
	}
	if err := s.SetAnswer("x", "o1", "", ""); err == nil {
		t.Error("無い問いに答えられた")
	}
}

func TestRelayClosesOnlyChosenAnswers(t *testing.T) {
	s := newStore(t)
	s.PutQuestions([]Question{q("b-1"), q("b-2")})
	s.SetAnswer("b-1", "o1", "", "")
	s.SetAnswer("b-2", Later, "", "")
	n, err := s.AddNote("ohana@実装", "テストを足して")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRelayed([]string{"b-1", "b-2", "none"}, []string{n.ID}); err != nil {
		t.Fatal(err)
	}
	b, _ := s.Load()
	st := map[string]string{}
	for _, x := range b.Questions {
		st[x.ID] = x.Status
	}
	if st["b-1"] != "closed" || st["b-2"] != "open" {
		t.Errorf("選んだ答えだけ closed になるはず: %v", st)
	}
	if !b.Notes[0].Relayed || b.Notes[0].To != "ohana@実装" {
		t.Errorf("指示に印が付いていない: %+v", b.Notes[0])
	}
	// 伝え終えた問いは答えを変えられない。open の問いが先に並ぶ
	if err := s.SetAnswer("b-1", "o2", "", ""); err == nil {
		t.Error("伝え終えた問いの答えを変えられた")
	}
	if b.Questions[0].ID != "b-2" {
		t.Errorf("open が先に並んでいない: %+v", b.Questions)
	}
}

func TestNoteRejectsDoubleSubmit(t *testing.T) {
	s := newStore(t)
	s.now = func() time.Time { return time.UnixMilli(1000) }
	if _, err := s.AddNote("", "朝の3件を出して"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote("", "朝の3件を出して"); err == nil {
		t.Error("連打した同じ指示を受けた")
	}
	if _, err := s.AddNote("", "  "); err == nil {
		t.Error("空の指示を受けた")
	}
}

func TestCleanRejectsBadQuestions(t *testing.T) {
	bad := map[string]func(*Question){
		"id":       func(x *Question) { x.ID = "../x" },
		"title":    func(x *Question) { x.Title = " " },
		"link":     func(x *Question) { x.Links = []Link{{Label: "x", URL: "javascript:alert(1)"}} },
		"color":    func(x *Question) { x.Color = "red" },
		"due":      func(x *Question) { x.Due = "10/22" },
		"status":   func(x *Question) { x.Status = "done" },
		"reserved": func(x *Question) { x.Options = []Option{{V: Later, Label: "あとで"}} },
		"dup":      func(x *Question) { x.Options = []Option{{V: "o1", Label: "a"}, {V: "o1", Label: "b"}} },
	}
	for name, f := range bad {
		x := q("c-1")
		f(&x)
		if err := clean(&x); err == nil {
			t.Errorf("%s: 断るはずが通った", name)
		}
	}
	ok := q("c-1")
	ok.Links = []Link{{Label: "PR", URL: "https://github.com/x/y/pull/1"}}
	if err := clean(&ok); err != nil {
		t.Errorf("正しい問いを断った: %v", err)
	}
}
