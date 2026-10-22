package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jose-sugisawa/claude-office/internal/decision"
)

// handleDecisions は判断箱の API を mux に足す。
//
//	GET  /api/decisions           箱の中身（問い・答え・指示）
//	POST /api/decisions/questions 問いを足す・置き換える（ボスが curl で）  本文: [Question]
//	POST /api/decisions/answer    答える（画面から。ボスが claude.ai の答えを写すときは by: "claude.ai"）  本文: {id, choice, note, by}
//	POST /api/decisions/notes     係への指示を出す（画面から）  本文: {to, text}
//	POST /api/decisions/relayed   ボスが伝え終えた印  本文: {answers: [id], notes: [id]}
//
// 書き換えは、ほかのサイトのページから送られたものを断る（sameOrigin）。
func handleDecisions(mux *http.ServeMux, cfg Config) {
	store := decision.NewStore(cfg.Decisions)
	post := func(path string, max int64, f func(*http.Request) (any, error)) {
		mux.HandleFunc(path, func(rw http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || !sameOrigin(r, cfg.Addr) {
				http.Error(rw, "このページからの POST だけ受け付けます", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(rw, r.Body, max)
			v, err := f(r)
			if err != nil {
				http.Error(rw, err.Error(), http.StatusBadRequest)
				return
			}
			if v == nil {
				rw.WriteHeader(http.StatusNoContent)
				return
			}
			writeJSON(rw, v)
		})
	}
	mux.HandleFunc("/api/decisions", func(rw http.ResponseWriter, r *http.Request) {
		b, err := store.Load()
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(rw, b)
	})
	post("/api/decisions/questions", 1<<20, func(r *http.Request) (any, error) {
		var qs []decision.Question
		if err := json.NewDecoder(r.Body).Decode(&qs); err != nil {
			return nil, err
		}
		return nil, store.PutQuestions(qs)
	})
	post("/api/decisions/answer", 64<<10, func(r *http.Request) (any, error) {
		var in struct{ ID, Choice, Note, By string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			return nil, err
		}
		return nil, store.SetAnswer(in.ID, in.Choice, in.Note, in.By)
	})
	post("/api/decisions/notes", 64<<10, func(r *http.Request) (any, error) {
		var in struct{ To, Text string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			return nil, err
		}
		return store.AddNote(in.To, in.Text)
	})
	post("/api/decisions/relayed", 64<<10, func(r *http.Request) (any, error) {
		var in struct{ Answers, Notes []string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			return nil, err
		}
		return nil, store.MarkRelayed(in.Answers, in.Notes)
	})
}

// demoQuestions は -demo で見せる判断箱。
func demoQuestions() []decision.Question {
	today := time.Now().Format("2006-01-02")
	soon := time.Now().Add(30 * time.Hour)
	return []decision.Question{
		{ID: "app-42", Island: "アプリ開発", Color: "#4C7FD6", Session: "app@review", Title: "PR #42（ページ送り）をマージしてよいか",
			Context: "CI は通っていて、レビューの指摘（2件）も直した。マージすると次のリリースに入る。",
			Options: []decision.Option{{V: "o1", Label: "マージしてよい", Rec: true}, {V: "o2", Label: "まだ（メモに直す所）"}},
			Links:   []decision.Link{{Label: "PR #42", URL: "https://github.com/example/app/pull/42"}}, Due: today, Created: today},
		{ID: "research-angle", Island: "調べもの", Color: "#E3B341", Session: "research@market", Title: "競合3社を、どの観点で深掘りするか",
			Context: "値段・機能・口コミの3つで比べた表はできた。深掘りは1つに絞ると今日中に終わる。",
			Options: []decision.Option{{V: "o1", Label: "値段", Rec: true}, {V: "o2", Label: "機能"}, {V: "o3", Label: "口コミ"}}, Created: today,
			Auto: &decision.Auto{Choice: "o1", At: soon.Format(time.RFC3339), Undo: "表の下書きだけなので消せば戻る"}},
		{ID: "blog-title", Island: "ブログ", Color: "#3FB36B", Session: "blog@draft", Title: "記事のタイトルをどちらにするか",
			Options: []decision.Option{{V: "o1", Label: "はじめての家計簿アプリ"}, {V: "o2", Label: "3分で続く家計簿"}}, Created: today},
	}
}
