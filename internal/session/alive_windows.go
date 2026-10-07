//go:build windows

package session

import "os"

// Windows の os.FindProcess は、プロセスが無ければ失敗する（OpenProcess を呼ぶ）。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Release()
	return true
}
