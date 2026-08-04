package main

import (
	"os/exec"
	"strings"
	"testing"
)

// queriesTerminalOnInit lists packages whose init function asks the terminal for
// its background color. Bubble Tea v1 does that to warm up Lip Gloss/Termenv
// before a program takes over the terminal, and because init runs on import it
// happens for every invocation, including `jw --version`. In a pty without a
// terminal behind it (`unbuffer jw --version`) nobody answers, so the query
// bytes end up in the output and the binary stalls for termenv's five-second
// timeout. None of these may be reachable from the binary.
var queriesTerminalOnInit = []string{
	"github.com/charmbracelet/bubbletea",
}

func TestNoDependencyQueriesTerminalOnInit(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not available")
	}
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	deps := make(map[string]bool)
	for line := range strings.SplitSeq(string(out), "\n") {
		deps[strings.TrimSpace(line)] = true
	}
	for _, pkg := range queriesTerminalOnInit {
		if deps[pkg] {
			t.Errorf("%s is in the import graph: its init queries the terminal on every run", pkg)
		}
	}
}
