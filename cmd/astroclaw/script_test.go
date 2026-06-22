package main

import (
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain lets the test binary impersonate the astroclaw CLI when
// testscript exec's "astroclaw <args>" from a .txtar script. When run
// normally (the default invocation), it falls through to m.Run().
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"astroclaw": func() { os.Exit(run()) },
	})
}
