package autostart

import (
	"testing"
)

func TestSystemdQuote(t *testing.T) {
	if got := systemdQuote(`/home/a b/100%/$x/claude-office`); got != `"/home/a b/100%%/$$x/claude-office"` {
		t.Errorf("got %s", got)
	}
}
