package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	r, _ := http.NewRequest("POST", "/api/islands", nil)
	if !sameOrigin(r, "127.0.0.1:7777") {
		t.Error("Origin なし（curl など）は通すはず")
	}
	for o, want := range map[string]bool{"http://127.0.0.1:7777": true, "http://localhost:7777": true, "http://[::1]:7777": true, "https://evil.example": false, "http://127.0.0.1:8080": false} {
		r.Header.Set("Origin", o)
		if got := sameOrigin(r, "127.0.0.1:7777"); got != want {
			t.Errorf("Origin %s: got %v", o, got)
		}
	}
}

func TestGuardRejectsForeignHost(t *testing.T) {
	ok := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(http.StatusOK) })
	for host, want := range map[string]int{
		"127.0.0.1:7777": 200, "localhost:7777": 200, "LOCALHOST": 200, "[::1]:7777": 200,
		"attacker.example:7777": 403, "192.168.1.10:7777": 403, "127.0.0.1.attacker.example": 403, "": 403,
	} {
		r := httptest.NewRequest("GET", "/api/crew", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		guard(ok, false).ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("Host %q: got %d, want %d", host, rec.Code, want)
		}
		if want == 200 && rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("Host %q: 枠に埋め込ませないヘッダーが無い", host)
		}
	}
	r := httptest.NewRequest("GET", "/api/crew", nil)
	r.Host = "192.168.1.10:7777"
	rec := httptest.NewRecorder()
	guard(ok, true).ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Errorf("-allow-remote なのに LAN の名前を断った: %d", rec.Code)
	}
}

func TestCheckLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:7777": true, "localhost:7777": true, "[::1]:7777": true,
		"0.0.0.0:7777": false, ":7777": false, "192.168.1.10:7777": false, "[::]:7777": false, "7777": false,
		// ポートの所に別の引数や改行を入れて、自動起動の設定に書かせない
		"127.0.0.1:7777 -allow-remote": false, "127.0.0.1:7777\nExecStartPre=/bin/sh": false, "127.0.0.1:0": false, "127.0.0.1:99999": false, "127.0.0.1:07777": false,
	} {
		if got := CheckLoopback(addr) == nil; got != want {
			t.Errorf("%s: got %v, want %v", addr, got, want)
		}
	}
}

func TestHandlerDemo(t *testing.T) {
	cfg := Config{Addr: "127.0.0.1:7777", Islands: filepath.Join(t.TempDir(), "islands.json"), Demo: true}
	os.WriteFile(cfg.Islands, demoIslands, 0o644)
	h := Handler(cfg, nil) // -demo は Watcher を使わない
	do := func(method, path, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader("[]"))
		r.Host = "127.0.0.1:7777"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	for path, want := range map[string]string{"/": "Claude オフィス", "/api/crew": "app@review", "/api/islands": "ネットショップ", "/api/usage": "five_hour"} {
		if rec := do("GET", path, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("GET %s: %d に %q が無い", path, rec.Code, want)
		}
	}
	if rec := do("GET", "/nope", ""); rec.Code != 404 {
		t.Errorf("知らないパスが %d", rec.Code)
	}
	if rec := do("POST", "/api/islands", "https://evil.example"); rec.Code != 403 {
		t.Errorf("ほかのサイトからの書き換えが %d", rec.Code)
	}
	if rec := do("POST", "/api/islands", "http://127.0.0.1:7777"); rec.Code != 204 {
		t.Errorf("この画面からの書き換えが %d: %s", rec.Code, rec.Body)
	}
}
