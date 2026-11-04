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

// demoQuestions は -demo で見せる判断箱。急ぎ3件・大事2件・やめてよいか5件（おすすめのある2件と、選んで取り下げる3件）。
func demoQuestions() []decision.Question {
	day := func(n int) string { return time.Now().AddDate(0, 0, -n).Format("2006-01-02") }
	today, soon := day(0), time.Now().Add(30*time.Hour)
	shop, blog, app, res := "#9B6FC2", "#3FB36B", "#4C7FD6", "#E3B341"
	drop := func(id, island, color, title string, age int) decision.Question {
		return decision.Question{ID: id, Island: island, Color: color, Title: title, Tier: decision.Droppable, Secs: 10, Created: day(age)}
	}
	return []decision.Question{
		{ID: "app-42", Island: "アプリ開発", Color: app, Session: "app@review", Title: "PR #42（ページ送り）をマージしてよいか", Tier: decision.Urgent, Secs: 30,
			Context: "CI は通っていて、レビューの指摘（2件）も直した。", Impact: "マージすると、次のリリースに入ります。",
			Options: []decision.Option{{V: "o1", Label: "マージしてよい", Rec: true, Note: "今日のリリースに入る"}, {V: "o2", Label: "まだ（メモに直す所）", Note: "明日のリリースに回る"}},
			Links:   []decision.Link{{Label: "PR #42", URL: "https://github.com/example/app/pull/42"}}, Due: today, Created: today},
		{ID: "research-angle", Island: "調べもの", Color: res, Session: "research@market", Title: "競合3社を、どの観点で深掘りするか", Tier: decision.Urgent, Secs: 30,
			Context: "値段・機能・口コミの3つで比べた表はできています。", Impact: "値段にすると、調べものの係がすぐ動けます。",
			Options: []decision.Option{{V: "o1", Label: "値段", Rec: true, Note: "今日中に終わる"}, {V: "o2", Label: "機能", Note: "明日の昼に終わる"}, {V: "o3", Label: "口コミ", Note: "あさって終わる"}}, Created: today,
			Auto: &decision.Auto{Choice: "o1", At: soon.Format(time.RFC3339), Undo: "表の下書きだけなので消せば戻る"}},
		{ID: "blog-title", Island: "ブログ", Color: blog, Session: "blog@draft", Title: "記事のタイトルをどちらにするか", Tier: decision.Urgent, Secs: 20,
			Options: []decision.Option{{V: "o1", Label: "はじめての家計簿アプリ", Rec: true}, {V: "o2", Label: "3分で続く家計簿"}}, Created: today},
		{ID: "shop-price", Island: "ネットショップ", Color: shop, Session: "shop@price", Title: "秋の値上げを何円にするか", Tier: decision.Important, Secs: 60,
			Context: "仕入れが1割上がった。競合は据え置き。", Options: []decision.Option{{V: "o1", Label: "100円", Rec: true}, {V: "o2", Label: "200円"}, {V: "o3", Label: "据え置き"}}, Created: day(2)},
		{ID: "app-icon", Island: "アプリ開発", Color: app, Title: "新しいアイコンの案をどれにするか", Tier: decision.Important, Secs: 45,
			Options: []decision.Option{{V: "o1", Label: "A（丸）", Rec: true}, {V: "o2", Label: "B（四角）"}}, Created: day(3)},
		{ID: "blog-series", Island: "ブログ", Color: blog, Title: "連載をやめてよいか", Tier: decision.Droppable, Secs: 15,
			Options: []decision.Option{{V: "o1", Label: "やめてよい", Rec: true}, {V: "o2", Label: "続ける"}}, Created: day(9)},
		{ID: "res-en", Island: "調べもの", Color: res, Title: "まとめを英語でも作るか", Tier: decision.Droppable, Secs: 15,
			Options: []decision.Option{{V: "o1", Label: "作らない", Rec: true}, {V: "o2", Label: "作る"}}, Created: day(7)},
		drop("shop-tax", "ネットショップ", shop, "送料の表示を税込にそろえるか", 14),
		drop("shop-wrap", "ネットショップ", shop, "包装紙の色をどちらにするか", 12),
		drop("blog-old", "ブログ", blog, "古い記事を消すか", 8),
	}
}
