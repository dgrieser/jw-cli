package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// marginalPath is the "+" after "only-begotten Son" in John 3:16, which wol
// answers with seven verses (testdata/citation_multi.json).
const marginalPath = "/en/wol/bc/r1/lp-e/1001070147/96"

// wolFile reads a fixture that lives with the wol client's own tests.
func wolFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "api", "wol", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// unfoldUpstream is wol as far as an expansion of John 3:16 reads it: the
// English library, the chapter of the study bible with the marginal reference
// put back into verse 16, and that reference's answer. handle adds more, and
// what it adds is asked before any of those.
func unfoldUpstream(t *testing.T, handle func(mux *http.ServeMux)) *Service {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/en", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/en/wol/h/r1/lp-e">home</a>`))
	})
	chapter := strings.Replace(string(wolFile(t, "chapter_john3.html")), "only-begotten Son,",
		`only-begotten Son,<a href="`+marginalPath+`" class="b">+</a>`, 1)
	mux.HandleFunc("/en/wol/b/r1/lp-e/nwtsty/43/3", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(chapter))
	})
	multi := wolFile(t, "citation_multi.json")
	mux.HandleFunc("/wol/bc/r1/lp-e/1001070147/96", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(multi)
	})
	own := http.NewServeMux()
	if handle != nil {
		handle(own)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := own.Handler(r); pattern != "" {
			own.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: srv.URL, JWOrg: srv.URL, WOL: srv.URL}),
		// a busy upstream is asked again at once: the pauses are the client's business
		httpx.WithRetryDelays(time.Millisecond, time.Millisecond))
	return New(hc, httpx.OpenCacheAt(t.TempDir()))
}

// marginalTargets are the seven verses the marginal reference names.
var marginalTargets = []string{
	"Genesis 22:2", "Genesis 22:16", "John 1:14", "Romans 5:8", "Romans 8:32", "1 John 4:9, 10", "1 John 4:19",
}

// jw bible read --unfold keeps every verse a marginal reference names, each
// headed as a marginal reference of its own.
func TestReadUnfoldKeepsEveryVerseOfAMarginalReference(t *testing.T) {
	s := unfoldUpstream(t, nil)
	for _, hops := range []bool{false, true} {
		res, err := s.ReadPassages(context.Background(), english, ReadRequest{
			Refs: "John 3:16", Edition: "nwtsty", NoOutlines: true,
			Unfold: UnfoldConfig{Depth: 1, Hops: hops},
		}, i18n.EN.Text())
		if err != nil {
			t.Fatal(err)
		}
		out := res.Passages[0].Verses[0].Unfold
		for _, target := range marginalTargets {
			want := "Marginal reference " + target + " of John 3:16"
			if hops {
				// nested under the heading naming the verse, as the page shows it
				want = "<h4>" + target + "</h4>"
			}
			if !strings.Contains(out, want) {
				t.Errorf("hops=%v: missing %q in:\n%s", hops, want, out)
			}
		}
		if !strings.Contains(out, "We love, because he first loved us") && !strings.Contains(out, "We love, becau") {
			t.Errorf("hops=%v: the last verse's text is missing", hops)
		}
	}
}

// The web page streams the marginal references of a verse one section each:
// every verse the reference names is a section of its own.
func TestStreamMarginalKeepsEveryVerse(t *testing.T) {
	s := unfoldUpstream(t, nil)
	var titles []string
	_, _, err := s.StreamPassageParts(context.Background(), english, "nwtsty",
		bibleref.Ref{Book: 43, Chapter: 3, VerseStart: 16, VerseEnd: 16}, PassageParts{Only: PartMarginal},
		UnfoldConfig{Depth: 1}, i18n.EN.Text(), UnfoldStream{Section: func(sec UnfoldSection) {
			titles = append(titles, sec.Title)
		}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(titles, "|"), strings.Join(marginalTargets, "|"); got != want {
		t.Errorf("sections = %s, want %s", got, want)
	}
}

// A marginal reference opened on a page of its own leads to every verse it
// names.
func TestCitationPageKeepsEveryVerse(t *testing.T) {
	s := unfoldUpstream(t, nil)
	refs, doc, err := s.CitationPage(context.Background(), english, marginalPath)
	if err != nil || doc != "" {
		t.Fatalf("refs=%v doc=%q err=%v", refs, doc, err)
	}
	table := bibleref.English()
	var names []string
	for _, ref := range refs {
		names = append(names, RefString(ref, table))
	}
	want := "Genesis 22:2|Genesis 22:16|John 1:14|Romans 5:8|Romans 8:32|1 John 4:9-10|1 John 4:19"
	if got := strings.Join(names, "|"); got != want {
		t.Errorf("refs = %s, want %s", got, want)
	}
}

// Every passage of a citation is read under the block citing it, each headed
// by its own reference: the citation's text names them all.
func TestInlineChildrenPlacesEveryPassage(t *testing.T) {
	content := `<p>First (<a href="/wol/bc/1">Ps 133:1-3</a>).</p><p>Then more.</p>`
	children := []unfold.Node{
		{Ref: unfold.Ref{Text: "Ps 133:1-3", Path: "/wol/bc/1"}, Item: 1, Title: "Psalm 133:1, 2", HTML: "<p>how good</p>"},
		{Ref: unfold.Ref{Text: "Ps 133:1-3", Path: unfold.ItemPath("/wol/bc/1", 2)}, Item: 2,
			Title: "Psalm 133:3", HTML: "<p>like the dew</p>"},
	}
	out, rest := inlineChildren(content, children, 3, "", i18n.EN.Text())
	if len(rest) != 0 {
		t.Errorf("left over: %+v", rest)
	}
	dew, more := strings.Index(out, "like the dew"), strings.Index(out, "Then more.")
	if dew < 0 || dew > more {
		t.Errorf("the second passage is not under the block citing it:\n%s", out)
	}
	for _, want := range []string{"<h3>Psalm 133:1, 2</h3>", "<h3>Psalm 133:3</h3>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A passage wol names without its text is read from its document; when that
// read fails, the gap is not kept as what the citation says: the next
// expansion reads it again.
func TestResolveDoesNotRememberAFailedPassage(t *testing.T) {
	fail := true
	s := unfoldUpstream(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/wol/pc/r1/lp-e/2024360/3/0", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"items":[{"title":"A box","content":"","url":"/wol/d/r1/lp-e/1102014000#h=2:0-2:0"}]}`))
		})
		mux.HandleFunc("/en/wol/d/r1/lp-e/1102014000", func(w http.ResponseWriter, r *http.Request) {
			if fail {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(`<article id="article"><p data-pid="2">What the box says.</p></article>`))
		})
	})
	resolve := func() string {
		tip, err := newTooltipResolver(s, english, nil).Resolve(context.Background(), "/en/wol/pc/r1/lp-e/2024360/3/0")
		if err != nil {
			t.Fatal(err)
		}
		return tip.ContentHTML
	}
	if got := resolve(); got != "" {
		t.Fatalf("a passage that could not be read: %q", got)
	}
	fail = false
	if got := resolve(); !strings.Contains(got, "What the box says.") {
		t.Errorf("the passage was not read again: %q", got)
	}
}
