// claude-office は、手元で動いている Claude Code のセッションをドット絵のオフィスに並べて見せる。
// 見るだけの画面で、Claude は呼ばない（トークンを使わない）。
//
//	claude-office                       http://127.0.0.1:7777 で開く（serve と同じ）
//	claude-office serve -demo           見本のデータで開く（Claude Code がなくても見た目を確かめられる）
//	claude-office install               ログイン時に自動で起動する（macOS・Linux・Windows）
//	claude-office uninstall             自動起動をやめる
//	claude-office statusline install    Claude Code のステータスラインに登録する（コンテキストと使用量が出る）
//	claude-office statusline uninstall  登録を外す
//	claude-office statusline            （Claude Code が呼ぶ。標準入力の JSON を読んで1行出す）
//
// ここではフラグを読んで、internal/ の各パッケージに渡すだけにする。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/jose-sugisawa/claude-office/internal/autostart"
	"github.com/jose-sugisawa/claude-office/internal/claudehome"
	"github.com/jose-sugisawa/claude-office/internal/server"
	"github.com/jose-sugisawa/claude-office/internal/statusline"
)

const usage = `claude-office — Claude Code のセッションをドット絵のオフィスで見る

使い方:
  claude-office [serve] [-addr 127.0.0.1:7777] [-islands パス] [-demo] [-allow-remote]
  claude-office install [-addr 127.0.0.1:7777]
  claude-office uninstall
  claude-office statusline install [-force]
  claude-office statusline uninstall
  claude-office version
`

// version はリリースのビルドが -ldflags "-X main.version=…" で入れる。
// 入っていなければ Go が埋め込んだ版を使う（go install …@vX ならその版。clone して go build したものは、
// Go 1.24 からは git から決めた仮の版 0.1.1-0.<日時>-<コミット> になる）。どちらも無ければ dev。
var version = ""

func versionString() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return "dev"
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		return serve(args)
	case "install":
		return installAutostart(args)
	case "uninstall":
		return autostart.Uninstall(os.Stdout)
	case "statusline":
		if len(args) > 0 && args[0] == "install" {
			return installStatusline(args[1:])
		}
		if len(args) > 0 && args[0] == "uninstall" {
			return statusline.Uninstall(os.Stdout, claudehome.Dir())
		}
		statusline.Run(os.Stdin, os.Stdout, claudehome.Office(claudehome.Dir()))
		return nil
	case "version", "-v", "--version":
		fmt.Println("claude-office", versionString())
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("知らないコマンド %q", cmd)
}

func serve(args []string) error {
	home := claudehome.Dir()
	cfg := server.Config{Home: home}
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	fs.StringVar(&cfg.Addr, "addr", "127.0.0.1:7777", "待ち受けるアドレス（手元だけで開く）")
	fs.StringVar(&cfg.Islands, "islands", filepath.Join(claudehome.Office(home), claudehome.IslandsFile), "島の一覧のファイル（無ければ見本を置く）")
	fs.StringVar(&cfg.Dev, "dev", "", "開発用：index.html をこのディレクトリから毎回読む（リポジトリでは -dev web）")
	fs.BoolVar(&cfg.Demo, "demo", false, "見本のデータで動かす（Claude Code のセッションを読まない）")
	fs.BoolVar(&cfg.AllowRemote, "allow-remote", false, "127.0.0.1・localhost 以外でも待ち受ける（パスワードは無いので、同じネットワークの誰でも会話の抜粋を見られます）")
	fs.Parse(args)
	return server.Run(cfg, os.Stdout, os.Stderr)
}

func installAutostart(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7777", "待ち受けるアドレス")
	fs.Parse(args)
	exe, err := stableExecutable()
	if err != nil {
		return err
	}
	return autostart.Install(os.Stdout, exe, *addr)
}

func installStatusline(args []string) error {
	fs := flag.NewFlagSet("statusline install", flag.ExitOnError)
	force := fs.Bool("force", false, "ほかのステータスラインが登録されていても置き換える")
	fs.Parse(args)
	exe, err := stableExecutable()
	if err != nil {
		return err
	}
	return statusline.Install(os.Stdout, claudehome.Dir(), exe, *force)
}

// stableExecutable は自分の実行ファイルの場所（自動起動とステータスラインの設定に書く）。
// go run の一時ファイルなら、消えてしまうので断る。
func stableExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	if strings.Contains(exe, "go-build") {
		return "", errors.New("go run から実行しています。先に go install か go build で実行ファイルを作り、その claude-office から実行してください")
	}
	return exe, nil
}
