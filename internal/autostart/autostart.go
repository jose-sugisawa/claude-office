// Package autostart は、ログイン時に claude-office serve を自動で起動するよう OS に登録する。
//
//	macOS   ~/Library/LaunchAgents/dev.claude-office.plist（launchd。止まっても立ち上げ直す）
//	Linux   ~/.config/systemd/user/claude-office.service（systemd --user。止まっても立ち上げ直す）
//	Windows スタートアップフォルダの claude-office.vbs（ログイン時に窓を出さずに起動）
package autostart

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jose-sugisawa/claude-office/internal/atomicfile"
	"github.com/jose-sugisawa/claude-office/internal/server"
)

const launchdLabel = "dev.claude-office"

// Install は、ログイン時に exe serve -addr addr を起動するよう登録し、今すぐ起動して開けるかを確かめる。
func Install(out io.Writer, exe, addr string) error {
	// 自動起動は手元だけ（serve が断って、起動に失敗し続けるのを防ぐ）
	if err := server.CheckLoopback(addr); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		err = installLaunchd(out, exe, home, addr)
	case "linux":
		err = installSystemd(out, exe, home, addr)
	case "windows":
		err = installWindows(exe, addr)
	default:
		return fmt.Errorf("%s の自動起動には対応していません。claude-office を手で起動してください", runtime.GOOS)
	}
	if err != nil {
		return err
	}
	for i := 0; i < 20; i++ {
		if resp, err := http.Get("http://" + addr + "/api/islands"); err == nil {
			resp.Body.Close()
			fmt.Fprintf(out, "オフィスを起動しました: http://%s\n", addr)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("自動起動は登録しましたが、起動を確かめられませんでした。claude-office serve を手で実行して、エラーを見てください")
}

// Uninstall は自動起動の登録を外し、動いているオフィスを止める。
func Uninstall(out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel).Run()
		os.Remove(filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"))
	case "linux":
		exec.Command("systemctl", "--user", "disable", "--now", "claude-office").Run()
		os.Remove(filepath.Join(home, ".config", "systemd", "user", "claude-office.service"))
		exec.Command("systemctl", "--user", "daemon-reload").Run()
	case "windows":
		os.Remove(windowsStartupScript())
		killOtherWindows()
	default:
		return fmt.Errorf("%s には対応していません", runtime.GOOS)
	}
	fmt.Fprintln(out, "自動起動をやめ、オフィスを止めました。")
	return nil
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

func installLaunchd(out io.Writer, exe, home, addr string) error {
	plist := filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
	logf := filepath.Join(home, "Library", "Logs", "claude-office.log")
	os.MkdirAll(filepath.Dir(logf), 0o755)
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array><string>%s</string><string>serve</string><string>-addr</string><string>%s</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, launchdLabel, xmlEscape(exe), xmlEscape(addr), xmlEscape(logf), xmlEscape(logf))
	if err := atomicfile.Write(plist, []byte(body)); err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	exec.Command("launchctl", "bootout", domain+"/"+launchdLabel).Run()
	if err := portFree(addr); err != nil {
		return err
	}
	if msg, err := exec.Command("launchctl", "bootstrap", domain, plist).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap に失敗しました: %s", strings.TrimSpace(string(msg)))
	}
	fmt.Fprintln(out, "ログは", logf)
	return nil
}

// systemdQuote は ExecStart に書くパスの引用。systemd は % を指定子、$ を環境変数として読むので、%% と $$ にしてから二重引用符で囲む。
func systemdQuote(s string) string {
	return strconv.Quote(strings.NewReplacer("%", "%%", "$", "$$").Replace(s))
}

func installSystemd(out io.Writer, exe, home, addr string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemctl が見つかりません。ログイン時に claude-office serve を起動するよう、お使いの環境で設定してください")
	}
	unit := filepath.Join(home, ".config", "systemd", "user", "claude-office.service")
	body := fmt.Sprintf(`[Unit]
Description=claude-office（Claude Code のセッションをオフィスの絵で見る）

[Service]
ExecStart=%s serve -addr %s
Restart=always
RestartSec=3

[Install]
WantedBy=default.target
`, systemdQuote(exe), addr)
	if err := atomicfile.Write(unit, []byte(body)); err != nil {
		return err
	}
	exec.Command("systemctl", "--user", "stop", "claude-office").Run()
	if err := portFree(addr); err != nil {
		return err
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "claude-office"}, {"--user", "restart", "claude-office"}} {
		if msg, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s に失敗しました: %s", strings.Join(args, " "), strings.TrimSpace(string(msg)))
		}
	}
	fmt.Fprintln(out, "ログは journalctl --user -u claude-office で見られます")
	return nil
}

// killOtherWindows は、動いているほかの claude-office を止める（自分は止めない）。
func killOtherWindows() {
	exec.Command("taskkill", "/F", "/FI", "IMAGENAME eq claude-office.exe", "/FI", "PID ne "+strconv.Itoa(os.Getpid())).Run()
}

// portFree は、自動起動の前にポートが空いているかを確かめる（ほかのプログラムが使っていれば、起動に失敗し続けるため）。
func portFree(addr string) error {
	var err error
	for i := 0; i < 10; i++ {
		var ln net.Listener
		if ln, err = net.Listen("tcp", addr); err == nil {
			ln.Close()
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("%s をほかのプログラムが使っています。止めてからやり直すか、-addr で別のポートを指定してください", addr)
}

func windowsStartupScript() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "claude-office.vbs")
}

func installWindows(exe, addr string) error {
	if os.Getenv("APPDATA") == "" {
		return errors.New("APPDATA が分からないため、スタートアップに登録できません")
	}
	q := func(s string) string { return strings.ReplaceAll(s, `"`, `""`) }
	vbs := fmt.Sprintf("CreateObject(\"WScript.Shell\").Run \"\"\"%s\"\" serve -addr %s\", 0, False\r\n", q(exe), q(addr))
	path := windowsStartupScript()
	if err := atomicfile.Write(path, []byte(vbs)); err != nil {
		return err
	}
	killOtherWindows()
	if err := portFree(addr); err != nil {
		return err
	}
	return exec.Command("wscript", path).Start()
}
