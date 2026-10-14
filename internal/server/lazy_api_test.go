package server_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// A markdown or text body keeps where the sections left to load come from,
// as the HTML body does: the heading is followed by the /unfold/… address.
func TestAPIBodiesKeepLazyAddresses(t *testing.T) {
	srv := newTestServer(t, hopsMux(t))
	for _, tc := range []struct{ path, want string }{
		{"/api/v1/bible/read?ref=John+3:16&lang=en&unfold=1&format=markdown", "[…](/unfold/cited?to=0&vid=43003016)"},
		{"/api/v1/bible/read?ref=John+3:16&lang=en&unfold=1&format=text", "… /unfold/cited?to=0&vid=43003016"},
		{"/api/v1/article?target=2024360&lang=en&unfold=1&format=markdown",
			"[…](/unfold/verse?bible=nwtsty&depth=1&part=notes&to=0&vid=43001029)"},
		{"/api/v1/article?target=2024360&lang=en&unfold=1&format=text",
			"… /unfold/verse?bible=nwtsty&depth=1&part=notes&to=0&vid=43001029"},
	} {
		resp, body := get(t, srv, tc.path)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d: %s", tc.path, resp.StatusCode, body)
		}
		var out struct {
			Body     string
			Passages []struct{ Verses []struct{ Unfold string } }
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.Body, tc.want) {
			t.Errorf("%s: missing %s in:\n%s", tc.path, tc.want, out.Body)
		}
		if strings.Contains(out.Body, "jwlazy") {
			t.Errorf("%s: a token was left in:\n%s", tc.path, out.Body)
		}
		// the passages go out as they were read
		for _, p := range out.Passages {
			for _, v := range p.Verses {
				if v.Unfold != "" && !strings.Contains(v.Unfold, `class="unfold-lazy"`) {
					t.Errorf("%s: the verse's expansion was changed: %s", tc.path, v.Unfold)
				}
			}
		}
	}
}
