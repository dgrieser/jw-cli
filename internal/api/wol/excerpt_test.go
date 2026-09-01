package wol

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A document with one block per locator: a paragraph the fragment names by id,
// one it names by the citation link it holds, one only its words can find, and
// a table whose cell holds a hit.
const excerptDoc = `<html><body><div id="article">
<h1>Fragen von Lesern</h1>
<p id="p3" data-pid="3">Rahel starb jedoch vor ihren beiden S&ouml;hnen, und was
Jeremia aufschrieb scheint deshalb ungenau zu sein.</p>
<p id="p5" data-pid="5">Rahels erster Sohn war Joseph. Warum hei&szlig;t es in
<a href="/de/wol/bc/r10/lp-x/2014927/2/0" class="b">Jeremia 31:15</a>, Rahel
weine um ihre S&ouml;hne, &bdquo;weil sie nicht mehr sind&ldquo;?</p>
<ul><li><p id="p7" data-pid="7">Als das Buch Jeremia geschrieben wurde, war
Ephraim l&auml;ngst der bedeutendste Stamm im Nordreich Israel geworden.</p>
<div class="gen-field"><textarea></textarea></div></li></ul>
<table><tbody>
  <tr><td>Rama</td><td id="p9" data-pid="9">In R. wird eine Stimme geh&ouml;rt</td></tr>
  <tr><td>Bethlehem</td><td>Ephrath</td></tr>
</tbody></table>
</div></body></html>`

func excerptClient(t *testing.T, hits *int) (*Client, string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/de/wol/d/r10/lp-x/2014927", func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Write([]byte(excerptDoc))
	})
	c := testClient(t, mux)
	return c, c.hc.Base.WOL + "/de/wol/d/r10/lp-x/2014927"
}

func TestExcerpts(t *testing.T) {
	tests := []struct {
		name     string
		fragment string
		want     []string
		wantNone bool
	}{
		{
			name:     "by paragraph id",
			fragment: `<p id="p3" data-pid="3">Rahel starb jedoch …</p>`,
			want:     []string{"Rahel starb jedoch vor ihren beiden Söhnen"},
		},
		{
			name:     "by citation link",
			fragment: `<p>Warum heißt es in <a href="/de/wol/bc/r10/lp-x/2014927/2/0" class="b">Jeremia 31:15</a> …</p>`,
			want:     []string{"Rahels erster Sohn war Joseph"},
		},
		{
			name:     "by its opening words",
			fragment: `<p>Als das Buch Jeremia geschrieben wurde, war Ephraim …</p>`,
			want:     []string{"Als das Buch Jeremia geschrieben wurde"},
		},
		{
			name:     "a cell brings its table",
			fragment: `<p id="p9" data-pid="9">In R. wird eine Stimme gehört</p>`,
			want:     []string{"<table>", "Bethlehem", "Ephrath"},
		},
		{
			name: "two hits, both blocks in document order",
			fragment: `<p id="p5" data-pid="5">Rahels erster Sohn …</p> <strong>...</strong> ` +
				`<p id="p3" data-pid="3">Rahel starb jedoch …</p>`,
			want: []string{"Rahel starb jedoch", "Rahels erster Sohn"},
		},
		{
			name:     "nothing to go on",
			fragment: `<p>30-31 □</p>`,
			wantNone: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			c, url := excerptClient(t, &hits)
			blocks, err := c.Excerpts(context.Background(), url, tc.fragment)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantNone {
				if len(blocks) != 0 {
					t.Fatalf("want no excerpt, got %q", blocks)
				}
				return
			}
			joined := strings.Join(blocks, "\n")
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Errorf("excerpt missing %q:\n%s", want, joined)
				}
			}
			if strings.Contains(joined, "<textarea") {
				t.Errorf("excerpt kept an answer box:\n%s", joined)
			}
		})
	}
}

// Two hits in one document come back once each, the containing block winning
// over a block inside it, and in the order the document reads.
func TestExcerptsOrderAndDedup(t *testing.T) {
	hits := 0
	c, url := excerptClient(t, &hits)
	frag := `<p id="p7" data-pid="7">Als das Buch …</p> <p id="p3" data-pid="3">Rahel starb …</p>` +
		` <p id="p3" data-pid="3">Rahel starb …</p>`
	blocks, err := c.Excerpts(context.Background(), url, frag)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d: %q", len(blocks), blocks)
	}
	if !strings.Contains(blocks[0], "Rahel starb") || !strings.Contains(blocks[1], "Als das Buch") {
		t.Errorf("out of document order: %q", blocks)
	}
}

// A document is read once and kept: a second excerpt, and any later reading of
// the same page, come out of the cache.
func TestExcerptsCacheDocument(t *testing.T) {
	hits := 0
	c, url := excerptClient(t, &hits)
	ctx := context.Background()
	frag := `<p id="p3" data-pid="3">Rahel starb …</p>`
	for range 2 {
		if _, err := c.Excerpts(ctx, url+"?q=%28Jer+31%3A15%29&p=par", frag); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.DocumentByURL(ctx, url); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("requests = %d, want 1", hits)
	}
}
