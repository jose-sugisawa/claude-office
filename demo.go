package main

import (
	"encoding/json"
	"time"
)

// -demo で使う見本。Claude Code のセッションを読まずに、画面の見え方を確かめるためのもの。

var demoIslands = []byte(`[
  {"id":"app","name":"アプリ開発","color":"blue"},
  {"id":"blog","name":"ブログ","color":"green"},
  {"id":"research","name":"調べもの","color":"yellow"},
  {"id":"shop","name":"ネットショップ","color":"magenta","prefixes":["shop","ec"]}
]
`)

func demoCrew(now time.Time) []Member {
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	ctx := func(p float64) *Ctx { return &Ctx{Used: p, Size: 1_000_000, Tokens: int64(p * 10_000), At: now.Unix()} }
	return []Member{
		{Key: "demo-1", Name: "app@review", Status: "idle", Since: ms(3 * time.Minute), Line: "PR #42 はマージしてよいですか？ CI は通っています。", Excerpt: "PR #42 はマージしてよいですか？\nCI は通っていて、レビューの指摘も直しました。", Dir: "app", Cwd: "~/src/app", Named: true, Ctx: ctx(34)},
		{Key: "demo-2", Name: "app@api", Status: "busy", Since: ms(12 * time.Minute), Dir: "app", Cwd: "~/src/app", Named: true, Ctx: ctx(58)},
		{Key: "demo-3", Name: "app@ios", Status: "waiting", Waiting: "permission", Since: ms(1 * time.Minute), Dir: "app", Cwd: "~/src/app", Named: true, Ctx: ctx(22)},
		{Key: "demo-4", Name: "app@docs", Status: "idle", Since: ms(2 * time.Hour), Line: "README を直しました。", Dir: "app", Cwd: "~/src/app", Named: true, Ctx: ctx(12)},
		{Key: "demo-5", Name: "blog@draft", Status: "busy", Since: ms(4 * time.Minute), Dir: "blog", Cwd: "~/src/blog", Named: true, Ctx: ctx(86)},
		{Key: "demo-6", Name: "research@market", Status: "idle", Since: ms(18 * time.Minute), Line: "競合を3社まとめました。どの観点で深掘りしますか？", Dir: "notes", Cwd: "~/notes", Named: true, Ctx: ctx(67)},
		{Key: "demo-7", Name: "shop@inventory", Status: "gone", Since: ms(10 * time.Minute), Dir: "shop", Cwd: "~/src/shop", Named: true},
		{Key: "demo-8", Name: "src-4f", Status: "idle", Since: ms(5 * time.Hour), Line: "片づけが終わりました。", Dir: "src", Cwd: "~/src", Named: false},
	}
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
