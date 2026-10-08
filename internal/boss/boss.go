// Package boss は、ほかのセッションを見回って指示を出す「ボス」のセッションを見分け、
// ボスが送った指示（Claude Code のセッション間メッセージ）を会話の記録から読む。
//
// ボスはセッション名で決める：boss、または boss@… のように boss で始まり、続きが英数字でない名前。
// 島の呼び名と同じ決まりなので、設定はいらない。
package boss

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Prefix はボスのセッション名の頭。島の呼び名には使えない。
const Prefix = "boss"

// Ask はボスから届いた1つの指示。
type Ask struct {
	At   int64  `json:"at"`   // 届いた時刻（ミリ秒）
	Line string `json:"line"` // 指示の最初の行（60文字まで）
}

// IsBoss は、セッション名がボスのものかを返す（大文字小文字は区別しない）。
func IsBoss(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if !strings.HasPrefix(n, Prefix) {
		return false
	}
	rest := n[len(Prefix):]
	return rest == "" || !(rest[0] >= 'a' && rest[0] <= 'z' || rest[0] >= '0' && rest[0] <= '9')
}

// Claude Code は、ほかのセッションからのメッセージを
// <cross-session-message from="…" from-name="送り主のセッション名" …>本文</cross-session-message> の形で会話に入れる
var message = regexp.MustCompile(`(?s)<cross-session-message\b([^>]*)>(.*?)</cross-session-message>`)
var fromName = regexp.MustCompile(`\bfrom-name="([^"]*)"`)

// Parse は、指示（人の発言として記録された文）のうち、ボスから届いたメッセージの最初の行を返す。
// ボスからでなければ ok は false。
func Parse(text string) (line string, ok bool) {
	if !strings.Contains(text, "<cross-session-message") {
		return "", false
	}
	for _, m := range message.FindAllStringSubmatch(text, -1) {
		f := fromName.FindStringSubmatch(m[1])
		if f == nil || !IsBoss(html.UnescapeString(f[1])) {
			continue
		}
		for _, l := range strings.Split(m[2], "\n") {
			if l = strings.TrimSpace(l); l != "" {
				return cut(l, 60), true
			}
		}
		return "", true
	}
	return "", false
}

func cut(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}
