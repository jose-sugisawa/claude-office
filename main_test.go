package main

import (
	"testing"
)

func TestVersionString(t *testing.T) {
	defer func(v string) { version = v }(version)
	version = "1.2.3"
	if got := versionString(); got != "1.2.3" {
		t.Errorf("ldflags で入れた版 = %q, want 1.2.3", got)
	}
	version = ""
	if got := versionString(); got != "dev" { // go test は (devel) として組み立てる
		t.Errorf("版が無いとき = %q, want dev", got)
	}
}
