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
package main

import (
	"fmt"
	"os"
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

var version = "0.1.0"

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
		return uninstallAutostart()
	case "statusline":
		if len(args) > 0 && args[0] == "install" {
			return installStatusline(args[1:])
		}
		if len(args) > 0 && args[0] == "uninstall" {
			return uninstallStatusline()
		}
		runStatusline(os.Stdin, os.Stdout)
		return nil
	case "version", "-v", "--version":
		fmt.Println("claude-office", version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("知らないコマンド %q", cmd)
}
