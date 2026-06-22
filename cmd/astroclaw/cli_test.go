package main

import (
	"os"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain lets the test binary impersonate the astroclaw CLI when
// testscript exec's "astroclaw <args>" from a .txtar script. When run
// normally, it falls through to m.Run().
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"astroclaw": func() { os.Exit(run()) },
	})
}

// TestScripts runs every .txtar in testdata/scripts/ against a real
// deployed backend. The deploy URL and admin password come from
// ASTROCLAW_API_URL and ASTROCLAW_ADMIN_PASSWORD; when either is missing the suite
// is skipped so plain `go test ./...` stays green.
func TestScripts(t *testing.T) {
	apiURL := os.Getenv("ASTROCLAW_API_URL")
	if apiURL == "" {
		t.Skip("ASTROCLAW_API_URL not set; skipping e2e suite")
	}
	adminPassword := os.Getenv("ASTROCLAW_ADMIN_PASSWORD")
	if adminPassword == "" {
		t.Skip("ASTROCLAW_ADMIN_PASSWORD not set; skipping e2e suite")
	}

	testscript.Run(t, testscript.Params{
		Dir: "testdata/scripts",
		Setup: func(env *testscript.Env) error {
			env.Setenv("ASTROCLAW_API_URL", apiURL)
			env.Setenv("ASTROCLAW_ADMIN_PASSWORD", adminPassword)
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			// setenvfile VAR FILE reads FILE and sets the env var VAR to
			// its (trimmed) contents. Lets .txtar scripts pipe one command's
			// stdout into the next command's environment.
			"setenvfile": func(ts *testscript.TestScript, neg bool, args []string) {
				if len(args) != 2 {
					ts.Fatalf("usage: setenvfile VAR FILE")
				}
				data, err := os.ReadFile(ts.MkAbs(args[1]))
				if err != nil {
					ts.Fatalf("setenvfile: %v", err)
				}
				ts.Setenv(args[0], strings.TrimSpace(string(data)))
			},
		},
	})
}
