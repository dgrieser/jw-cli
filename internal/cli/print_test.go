package cli

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
)

// A listing that could not include everything says so under its rows, and
// -o json carries the same notes.
func TestListingNotes(t *testing.T) {
	rs := results.ResultSet{
		Kind:  "wol-search",
		Items: []model.Result{{Kind: "article", Title: "A Watchtower article"}},
		Notes: []string{"The videos quoting Matthew 24:14 could not be searched: HTTP 503"},
	}
	for _, format := range []string{"markdown", "json"} {
		a := app.New(app.Flags{Output: format, CacheDir: t.TempDir()})
		var out bytes.Buffer
		a.Stdout = &out
		if err := writeListing(a, rs, "1 publication citing Matthew 24:14"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "could not be searched: HTTP 503") {
			t.Errorf("-o %s: the note is missing:\n%s", format, out.String())
		}
	}
}

// A listing is styled for a terminal and plain everywhere else. The test
// harness captures stdout in a buffer, which is not a terminal, so nothing the
// listing writes may carry an escape sequence.
func TestListingIsPlainOffTerminal(t *testing.T) {
	var (
		queries []string
		docs    atomic.Int64
	)
	out, err := runCmd(t, citedMuxDocs(t, &queries, &docs), "bible", "cited", "Jeremiah 31:15", "-l", "en")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("piped listing carries ANSI:\n%q", out)
	}
	// and the quote bar is part of that styling, not of the layout
	if strings.Contains(out, "│") {
		t.Errorf("piped listing drew the excerpt bar:\n%s", out)
	}
	// with no terminal to click in, the target is spelled out on its own line
	if !strings.Contains(out, "/de/wol/d/r10/lp-x/202026249") {
		t.Errorf("piped listing dropped the link line:\n%s", out)
	}
}
