// Package server は、オフィスの画面と API（/api/crew・/api/islands・/api/usage・/api/dismiss）を手元に出す。
// 読むだけの API と、この画面からの書き換えだけを受け付ける（guard・sameOrigin）。
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jose-sugisawa/claude-office/internal/claudehome"
	"github.com/jose-sugisawa/claude-office/internal/island"
	"github.com/jose-sugisawa/claude-office/internal/session"
	"github.com/jose-sugisawa/claude-office/web"
)

// Config は serve のフラグで決まる設定。
type Config struct {
	Addr        string // 待ち受けるアドレス（手元だけで開く）
	Home        string // Claude Code の設定ディレクトリ（~/.claude）
	Islands     string // 島の一覧のファイル（無ければ見本を置く）
	Dev         string // 開発用：index.html をこのディレクトリから毎回読む
	Demo        bool   // 見本のデータで動かす（Claude Code のセッションを読まない）
	AllowRemote bool   // 127.0.0.1・localhost 以外でも待ち受ける
}

// Run は cfg を確かめ、島の一覧を用意して、止められるまで待ち受ける。
func Run(cfg Config, stdout, stderr io.Writer) error {
	if _, err := CheckAddr(cfg.Addr); err != nil {
		return err
	}
	if !cfg.AllowRemote {
		if err := CheckLoopback(cfg.Addr); err != nil {
			return err
		}
	}

	if cfg.Demo {
		dir, err := os.MkdirTemp("", "claude-office-demo")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		cfg.Islands = filepath.Join(dir, claudehome.IslandsFile)
		if err := os.WriteFile(cfg.Islands, demoIslands, 0o644); err != nil {
			return err
		}
	} else if err := island.Ensure(cfg.Islands, island.Example); err != nil {
		return fmt.Errorf("島の一覧を置けません: %w", err)
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("%s で待ち受けられません（もう起動していませんか？）: %w", cfg.Addr, err)
	}
	if cfg.AllowRemote {
		fmt.Fprintf(stderr, "注意: %s で待ち受けます。パスワードは無いので、ここに届く人は誰でも会話の抜粋を見られます。\n", cfg.Addr)
	}
	fmt.Fprintf(stdout, "オフィスを開きました: http://%s （止めるときは Ctrl-C）\n", cfg.Addr)
	srv := &http.Server{Handler: Handler(cfg, session.NewWatcher(cfg.Home)), ReadHeaderTimeout: 5 * time.Second}
	return srv.Serve(ln)
}

// Handler は画面と API をまとめ、guard を前に置いたハンドラーを返す。
func Handler(cfg Config, w *session.Watcher) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(rw, r)
			return
		}
		page := web.Index
		if cfg.Dev != "" {
			page = readOr(filepath.Join(cfg.Dev, "index.html"), web.Index)
		}
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		rw.Header().Set("Cache-Control", "no-store")
		rw.Write(page)
	})
	mux.HandleFunc("/api/islands", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if !sameOrigin(r, cfg.Addr) {
				http.Error(rw, "このページ以外からの書き換えは受け付けません", http.StatusForbidden)
				return
			}
			var list []island.Island
			if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 64<<10)).Decode(&list); err != nil {
				http.Error(rw, "島の一覧が読めません: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := island.Save(cfg.Islands, list); err != nil {
				http.Error(rw, err.Error(), http.StatusBadRequest)
				return
			}
			rw.WriteHeader(http.StatusNoContent)
			return
		}
		islands, err := island.Load(readOr(cfg.Islands, island.Example))
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(rw, islands)
	})
	mux.HandleFunc("/api/crew", func(rw http.ResponseWriter, r *http.Request) {
		if cfg.Demo {
			writeJSON(rw, demoCrew(time.Now()))
			return
		}
		writeJSON(rw, w.Snapshot(time.Now()))
	})
	// 使用量の枠（5時間・1週間）。statusline が <Claude の設定>/office/usage.json に書いたものをそのまま返す（まだ無ければ null）
	mux.HandleFunc("/api/usage", func(rw http.ResponseWriter, r *http.Request) {
		b := []byte("null")
		if cfg.Demo {
			b = demoUsage(time.Now())
		} else if f, err := os.ReadFile(filepath.Join(claudehome.Office(cfg.Home), claudehome.UsageFile)); err == nil && json.Valid(f) {
			b = f
		}
		rw.Header().Set("Content-Type", "application/json; charset=utf-8")
		rw.Header().Set("Cache-Control", "no-store")
		rw.Write(b)
	})
	mux.HandleFunc("/api/dismiss", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !sameOrigin(r, cfg.Addr) {
			http.Error(rw, "このページからの POST だけ受け付けます", http.StatusForbidden)
			return
		}
		w.Dismiss(r.URL.Query().Get("key"))
		rw.WriteHeader(http.StatusNoContent)
	})
	return guard(mux, cfg.AllowRemote)
}

// CheckLoopback は、待ち受けるアドレスが手元（127.0.0.1・::1・localhost）だけかを確かめる。
// 0.0.0.0 や LAN の IP で待ち受けると、同じネットワークの誰でも会話の抜粋を見られてしまうため。
func CheckLoopback(addr string) error {
	host, err := CheckAddr(addr)
	if err != nil {
		return err
	}
	if !isLoopbackHost(host) {
		return fmt.Errorf("-addr %s は手元の外からも開けてしまいます。127.0.0.1 で待ち受けるか、分かったうえで -allow-remote を付けてください", addr)
	}
	return nil
}

// CheckAddr は -addr が「ホスト:ポート」の形で、ポートが 1〜65535 の数字かを確かめ、ホストを返す。
// 自動起動の設定（systemd の unit・vbs）にそのまま書くので、空白や改行の入った値をここで断る。
func CheckAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("-addr %q が読めません（127.0.0.1:7777 のように書いてください）: %w", addr, err)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return "", fmt.Errorf("-addr %q のポートは 1〜65535 の数字で書いてください", addr)
	}
	if strings.ContainsFunc(host, func(r rune) bool { return r <= ' ' || r == '"' || r == '\'' || r == '%' || r == '$' }) {
		return "", fmt.Errorf("-addr %q のホストに使えない文字があります", addr)
	}
	return host, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// guard はすべての応答の前に置く。
//   - Host が手元の名前でなければ断る（DNS リバインディング：ほかのサイトが自分の名前を 127.0.0.1 に向け直し、
//     ブラウザに手元の API を読ませるのを防ぐ。ブラウザからは同じサイトの中の読み取りに見えるので、Host でしか見分けられない）
//   - ほかのサイトに枠（iframe）で埋め込ませない・中身の種類を推測させない
//
// -allow-remote のときは、どの名前で開かれるか分からないので Host は見ない。
func guard(h http.Handler, allowRemote bool) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if !allowRemote && !loopbackRequest(r.Host) {
			http.Error(rw, "手元（127.0.0.1・localhost）から開いてください", http.StatusForbidden)
			return
		}
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		rw.Header().Set("X-Frame-Options", "DENY")
		rw.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		rw.Header().Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(rw, r)
	})
}

// loopbackRequest は、リクエストの Host（ポート付きのこともある）が手元の名前かを見る。
func loopbackRequest(reqHost string) bool {
	h := reqHost
	if hh, _, err := net.SplitHostPort(reqHost); err == nil {
		h = hh
	}
	return isLoopbackHost(strings.TrimSuffix(strings.TrimPrefix(h, "["), "]"))
}

func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(rw).Encode(v)
}

// sameOrigin は、ブラウザからの書き換えがこのページから来たものかを確かめる。
// ほかのサイトのページから手元のポートへ送られた書き換えを断るため。Origin が無いのはブラウザ以外（curl など）。
func sameOrigin(r *http.Request, addr string) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ok := []string{"http://127.0.0.1:" + port, "http://localhost:" + port, "http://[::1]:" + port} // guard が通す名前とそろえる
	if host != "" && host != "127.0.0.1" && host != "localhost" && host != "0.0.0.0" {
		ok = append(ok, "http://"+net.JoinHostPort(host, port))
	}
	for _, s := range ok {
		if strings.EqualFold(o, s) {
			return true
		}
	}
	return false
}

// readOr はファイルを読む。読めなければ fallback を返す。
func readOr(path string, fallback []byte) []byte {
	if b, err := os.ReadFile(path); err == nil {
		return b
	}
	return fallback
}
