package server

import (
	"encoding/json"
	"time"

	"github.com/jose-sugisawa/claude-office/internal/boss"
	"github.com/jose-sugisawa/claude-office/internal/claudehome"
	"github.com/jose-sugisawa/claude-office/internal/session"
	"github.com/jose-sugisawa/claude-office/internal/worklog"
)

// -demo で使う見本。Claude Code のセッションを読まずに、画面の見え方を確かめるためのもの。

var demoIslands = []byte(`[
  {"id":"app","name":"アプリ開発","color":"blue"},
  {"id":"blog","name":"ブログ","color":"green"},
  {"id":"research","name":"調べもの","color":"yellow"},
  {"id":"shop","name":"ネットショップ","color":"magenta","prefixes":["shop","ec"]}
]
`)

func demoCrew(now time.Time) []session.Member {
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	ask := func(d time.Duration, line string) *boss.Ask { return &boss.Ask{At: ms(d), Line: line} }
	ctx := func(p float64) *claudehome.Ctx {
		return &claudehome.Ctx{Used: p, Size: 1_000_000, Tokens: int64(p * 10_000), At: now.Unix()}
	}
	return []session.Member{
		{Key: "demo-1", Name: "app@review", Status: "idle", Since: ms(3 * time.Minute), Line: "PR #42 はマージしてよいですか？ CI は通っています。", Excerpt: "PR #42 はマージしてよいですか？\nCI は通っていて、レビューの指摘も直しました。", Dir: "app", Cwd: "~/src/app", Named: true, Ctx: ctx(34)},
		{Key: "demo-2", Name: "app@api", Status: "busy", Since: ms(12 * time.Minute), Dir: "app", Cwd: "~/src/app", Named: true, Ctx: ctx(58), Asked: ask(12*time.Minute, "ページ送りの API の続きを、テストが通るところまで進めてください。")},
		{Key: "demo-3", Name: "app@ios", Status: "waiting", Waiting: "permission", Since: ms(1 * time.Minute), Dir: "app", Cwd: "~/src/app", Named: true, Ctx: ctx(22)},
		{Key: "demo-4", Name: "app@docs", Status: "idle", Since: ms(2 * time.Hour), Line: "README を直しました。", Dir: "app", Cwd: "~/src/app", Named: true, Ctx: ctx(12), Asked: ask(150*time.Minute, "README を新しい API に合わせてください。")},
		{Key: "demo-5", Name: "blog@draft", Status: "busy", Since: ms(4 * time.Minute), Dir: "blog", Cwd: "~/src/blog", Named: true, Ctx: ctx(86), Asked: ask(4*time.Minute, "下書きの続きを、見出し3まで進めてください。")},
		{Key: "demo-6", Name: "research@market", Status: "idle", Since: ms(18 * time.Minute), Line: "競合を3社まとめました。どの観点で深掘りしますか？", Dir: "notes", Cwd: "~/notes", Named: true, Ctx: ctx(67)},
		{Key: "demo-7", Name: "shop@inventory", Status: "gone", Since: ms(10 * time.Minute), Dir: "shop", Cwd: "~/src/shop", Named: true},
		{Key: "demo-9", Name: "boss", Status: "idle", Since: ms(4 * time.Minute), Line: "api・docs・draft に続きを頼みました。", Excerpt: "api・docs・draft に続きを頼みました。\nreview は PR のマージ待ちなので触っていません。", Dir: "src", Cwd: "~/src", Named: true, Ctx: ctx(31)},
		{Key: "demo-8", Name: "src-4f", Status: "idle", Since: ms(5 * time.Hour), Line: "片づけが終わりました。", Dir: "src", Cwd: "~/src", Named: false},
	}
}

// demoToday は今日の日報の見本。今から8時間前を朝として、島ごとに働いた区間を置く。
func demoToday(now time.Time) worklog.Day {
	from := now.Add(-8 * time.Hour)
	at := func(h float64) int64 { return from.Add(time.Duration(h * float64(time.Hour))).UnixMilli() }
	span := func(name string, a, b float64, prompts int) worklog.Span {
		return worklog.Span{Name: name, Start: at(a), End: at(b), Prompts: prompts}
	}
	s := func(key string, spans ...worklog.Span) worklog.Session {
		return worklog.Session{Key: key, Spans: spans}
	}
	return worklog.Day{From: from.UnixMilli(), Now: now.UnixMilli(), Sessions: []worklog.Session{
		s("demo-1", span("app@review", 0.3, 1.1, 4), span("app@review", 5.2, 6.0, 3), span("app@review", 7.6, 7.9, 1)),
		{Key: "demo-2", Spans: []worklog.Span{span("app@api", 0.2, 0.9, 3), span("app@api", 1.5, 2.6, 5), span("app@api", 4.8, 6.2, 6), span("app@api", 7.6, 8, 2)},
			Asks: []boss.Ask{{At: at(7.8), Line: "ページ送りの API の続きを、テストが通るところまで進めてください。"}}},
		s("demo-3", span("app@ios", 2.0, 2.8, 4), span("app@ios", 7.8, 7.98, 1)),
		{Key: "demo-4", Spans: []worklog.Span{span("app@docs", 3.0, 3.6, 2), span("app@docs", 5.5, 5.8, 1)},
			Asks: []boss.Ask{{At: at(5.5), Line: "README を新しい API に合わせてください。"}}},
		{Key: "demo-5", Spans: []worklog.Span{span("blog@draft", 3.5, 5.1, 6), span("blog@draft", 7.2, 8, 2)},
			Asks: []boss.Ask{{At: at(7.93), Line: "下書きの続きを、見出し3まで進めてください。"}}},
		s("demo-6", span("research@market", 0.8, 1.4, 3), span("research@market", 5.6, 7.5, 5)),
		s("demo-7", span("shop@inventory", 6.3, 6.7, 2)),
		s("demo-8", span("src-4f", 2.4, 2.7, 1)),
		{Key: "demo-9", Spans: []worklog.Span{span("boss", 5.4, 5.5, 1), span("boss", 7.75, 7.8, 1), span("boss", 7.92, 7.94, 1)}},
	}}
}

func demoUsage(now time.Time) []byte {
	b, _ := json.Marshal(map[string]any{
		"rate_limits": map[string]any{
			"five_hour": map[string]any{"used_percentage": 42, "resets_at": now.Add(2*time.Hour + 15*time.Minute).Unix()},
			"seven_day": map[string]any{"used_percentage": 63, "resets_at": now.Add(4 * 24 * time.Hour).Unix()},
		},
		"at": now.Unix(),
	})
	return b
}
