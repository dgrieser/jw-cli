package server_test

import (
	"strings"
	"testing"
)

// The meetings go by the aliases the CLI's subcommands take: the API reads
// the meeting, the page goes on to the meeting's own address.
func TestMeetingAliases(t *testing.T) {
	srv := newTestServer(t, wolMux(t))
	resp, body := get(t, srv, "/api/v1/meetings/nope?lang=en")
	if resp.StatusCode != 400 || !strings.Contains(body, "unknown meeting") {
		t.Errorf("unknown meeting: status %d body %s", resp.StatusCode, body)
	}
	for _, alias := range []string{"mw", "mid", "we", "wt"} {
		// past the name, the meeting is looked for (and not found upstream)
		if resp, body = get(t, srv, "/api/v1/meetings/"+alias+"?lang=en"); resp.StatusCode == 400 {
			t.Errorf("alias %s refused: %s", alias, body)
		}
	}
	resp, _ = get(t, srv, "/meetings/mw?date=2026-08-10&lang=en")
	if loc := resp.Header.Get("Location"); resp.StatusCode != 301 || loc != "/meetings/midweek?date=2026-08-10&lang=en" {
		t.Errorf("alias page: status %d location %q", resp.StatusCode, loc)
	}
	if resp, _ = get(t, srv, "/meetings/nope?lang=en"); resp.StatusCode != 400 {
		t.Errorf("unknown meeting page: status %d", resp.StatusCode)
	}
}
