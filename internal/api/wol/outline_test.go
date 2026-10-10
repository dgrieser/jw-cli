package wol

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

func outlineClient(t *testing.T) *Client {
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/bibledocument/r1/lp-e/nwtsty/1/outline", serveFile(t, "testdata/outline_gen.html"))
	mux.HandleFunc("/en/wol/bibledocument/r1/lp-e/nwtsty/19/outline", serveFile(t, "testdata/outline_ps.html"))
	mux.HandleFunc("/en/wol/bibledocument/r1/lp-e/nwtsty/65/outline", serveFile(t, "testdata/outline_jude.html"))
	// the Gospels have an overview instead, and the outline they lack is
	// answered with the library's home page
	mux.HandleFunc("/en/wol/bibledocument/r1/lp-e/nwtsty/40/outline", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="dailyText"></div></body></html>`))
	})
	mux.HandleFunc("/en/wol/bibledocument/r1/lp-e/nwtsty/40/overview", serveFile(t, "testdata/overview_mt.html"))
	return testClient(t, mux)
}

// outlineLines is a compact picture of the headings: depth, title, span.
func outlineLines(items []model.OutlineItem) string {
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "%s%s [%s] %d-%d\n", strings.Repeat("  ", it.Depth), it.Title, it.Label, it.Start, it.End)
	}
	return b.String()
}

func TestOutlineChapters(t *testing.T) {
	items, err := outlineClient(t).Outline(context.Background(), cfgEN, "nwtsty", 1)
	if err != nil {
		t.Fatal(err)
	}
	got := outlineLines(items)
	for _, want := range []string{
		"Creation of heavens and earth [1, 2] 1001001-1001002\n",
		"Six days of preparing the earth [3-31] 1001003-1001031\n",
		"  Day 1: light; day and night [3-5] 1001003-1001005\n",
		"God rests on the seventh day [1-3] 1002001-1002003\n",
		"  Expulsion from Eden [23, 24] 1003023-1003024\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	// the chapter numbers are where the verses count, not headings
	for _, it := range items {
		if it.Title == "1" || it.Title == "2" || it.Title == "3" {
			t.Errorf("chapter number kept as a heading: %+v", it)
		}
	}
}

func TestOutlineHeadingWithoutVerses(t *testing.T) {
	items, err := outlineClient(t).Outline(context.Background(), cfgEN, "nwtsty", 19)
	if err != nil {
		t.Fatal(err)
	}
	got := outlineLines(items)
	// a psalm's theme names no verses: it covers the whole psalm
	for _, want := range []string{
		"Two ways contrasted [] 19001000-19001999\n",
		"  Happiness by reading God’s law [2] 19001002-19001002\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestOutlineSingleChapterBook(t *testing.T) {
	items, err := outlineClient(t).Outline(context.Background(), cfgEN, "nwtsty", 65)
	if err != nil {
		t.Fatal(err)
	}
	want := "Greetings [1, 2] 65001001-65001002\n" +
		"Judgment of false teachers certain [3-16] 65001003-65001016\n" +
		"  Michael’s dispute with the Devil [9] 65001009-65001009\n" +
		"  Enoch’s prophecy [14, 15] 65001014-65001015\n" +
		"Keep yourselves in God’s love [17-23] 65001017-65001023\n" +
		"Ascribing glory to God [24, 25] 65001024-65001025\n"
	if got := outlineLines(items); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestOutlineOverview(t *testing.T) {
	items, err := outlineClient(t).Outline(context.Background(), cfgEN, "nwtsty", 40)
	if err != nil {
		t.Fatal(err)
	}
	got := outlineLines(items)
	for _, want := range []string{
		"A. Genealogy of Jesus Christ [1:1-17] 40001001-40001017\n",
		"B. From Events Surrounding Jesus’ Birth to His Baptism [1:18–3:17] 40001018-40003017\n",
		"  Astrologers’ visit and Herod’s murderous plan [2:1-12] 40002001-40002012\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestOutlineMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="dailyText"></div></body></html>`))
	})
	items, err := testClient(t, mux).Outline(context.Background(), cfgEN, "Rbi8", 1)
	if err != nil || len(items) != 0 {
		t.Errorf("an edition without outlines: got %v, %v", items, err)
	}
}

func TestParseOutlineSpan(t *testing.T) {
	for _, tc := range []struct {
		label      string
		start, end int
	}{
		{"4", 1002004, 1002004},
		{"1, 2", 1002001, 1002002},
		{"37-46a", 1002037, 1002046},
		{"46b-57", 1002046, 1002057},
		{"1:18–3:17", 1001018, 1003017},
		{"5:1, 2", 1005001, 1005002},
	} {
		got, ok := parseOutlineSpan(tc.label, 1, 2)
		if !ok || got.start != tc.start || got.end != tc.end {
			t.Errorf("%q: got %+v %v, want %d-%d", tc.label, got, ok, tc.start, tc.end)
		}
	}
	if _, ok := parseOutlineSpan("", 1, 2); ok {
		t.Error("no verses named: want !ok")
	}
}
