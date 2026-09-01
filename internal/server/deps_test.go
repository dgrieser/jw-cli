package server_test

import (
	"os/exec"
	"strings"
	"testing"
)

// terminalOnly lists packages that drive or take over a terminal. The service
// and server packages back an HTTP surface: none of these may be reachable
// from them. (glamour is linked through internal/render for the CLI's
// terminal styling, but the server only ever calls render's sanitizing path,
// so it is deliberately not on this list.)
var terminalOnly = []string{
	"charm.land/bubbletea/v2",
	"charm.land/bubbles/v2",
	"github.com/schollz/progressbar/v3",
	"github.com/dgrieser/jw-cli/internal/tui",
}

func TestServerStaysHeadless(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not available")
	}
	for _, root := range []string{
		"github.com/dgrieser/jw-cli/internal/service",
		"github.com/dgrieser/jw-cli/internal/server",
	} {
		out, err := exec.Command("go", "list", "-deps", root).Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", root, err)
		}
		deps := make(map[string]bool)
		for line := range strings.SplitSeq(string(out), "\n") {
			deps[strings.TrimSpace(line)] = true
		}
		for _, pkg := range terminalOnly {
			if deps[pkg] {
				t.Errorf("%s reaches %s: terminal code must stay out of the server path", root, pkg)
			}
		}
	}
}
