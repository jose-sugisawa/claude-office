package boss

import "testing"

func TestIsBoss(t *testing.T) {
	for name, want := range map[string]bool{
		"boss": true, "BOSS": true, "boss@見回り": true, "boss-main": true, " boss ": true,
		"bossy": false, "boss2": false, "app@boss": false, "": false,
	} {
		if got := IsBoss(name); got != want {
			t.Errorf("IsBoss(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	msg := func(from, body string) string {
		return "Another Claude session sent a message:\n<cross-session-message from=\"uds:/tmp/cc-socks/1.sock\" from-name=\"" + from + "\" from-mode=\"prompting\">\n" + body + "\n</cross-session-message>\n\nThis came from another Claude session."
	}
	if line, ok := Parse(msg("boss@見回り", "\n  API の続きを進めてください。\n- 決めることは止めて")); !ok || line != "API の続きを進めてください。" {
		t.Errorf("ボスからの指示が読めない: %q %v", line, ok)
	}
	if _, ok := Parse(msg("app@review", "レビューしました")); ok {
		t.Error("ボス以外のセッションからのメッセージをボスとみなした")
	}
	if _, ok := Parse("boss からの指示です"); ok {
		t.Error("ふつうの指示をボスからとみなした")
	}
	long := ""
	for i := 0; i < 80; i++ {
		long += "あ"
	}
	if line, _ := Parse(msg("boss", long)); len([]rune(line)) != 60 {
		t.Errorf("60文字に切っていない: %d", len([]rune(line)))
	}
}
