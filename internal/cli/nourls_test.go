package cli

import (
	"strings"
	"testing"
)

// noURLMarkers are the shapes a target can take in any human-readable output:
// a spelled-out URL, and the markdown syntax that would carry a link or an
// image.
var noURLMarkers = []string{"http://", "https://", "](", "!["}

func assertNoURLs(t *testing.T, out string) {
	t.Helper()
	for _, marker := range noURLMarkers {
		if strings.Contains(out, marker) {
			t.Errorf("--no-urls output still contains %q:\n%s", marker, out)
		}
	}
}

func TestNoURLsArticle(t *testing.T) {
	out, err := runCmd(t, articleMux(t), "article", "2024360", "-l", "en", "--no-urls")
	if err != nil {
		t.Fatal(err)
	}
	// the citation is what the link carried, so it has to survive the target
	for _, want := range []string{"# Caleb—He Fought Loyally", "CALEB trusted in Jehovah", "Num. 14:24"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	assertNoURLs(t, out)
}

func TestNoURLsArticleHTML(t *testing.T) {
	out, err := runCmd(t, articleMux(t), "article", "2024360", "-l", "en", "-o", "html", "--no-urls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Num. 14:24") {
		t.Errorf("link text dropped along with its target:\n%s", out)
	}
	for _, unwanted := range []string{"href=", "<img", "http://", "https://"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("html still contains %q:\n%s", unwanted, out)
		}
	}
}

func TestNoURLsScriptureRefs(t *testing.T) {
	out, err := runCmd(t, articleMux(t), "article", "2024360", "-l", "en", "--refs", "--no-urls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Num. 14:24") {
		t.Errorf("refs output missing the citation:\n%s", out)
	}
	assertNoURLs(t, out)
}

func TestNoURLsListing(t *testing.T) {
	out, err := runCmd(t, searchMux(t), "search", "-l", "de", "-t", "videos", "--no-urls", "Schöpfung")
	if err != nil {
		t.Fatal(err)
	}
	// the index still identifies the result for jw show|open|download
	if !strings.Contains(out, "[video] Daniel 7:27 Schöpfung") || !strings.Contains(out, "  1. ") {
		t.Errorf("listing lost its entry:\n%s", out)
	}
	assertNoURLs(t, out)
}

func TestNoURLsMediaInfo(t *testing.T) {
	out, err := runCmd(t, mediaMux(t), "media", "info", "pub-abc_1_VIDEO", "-l", "en", "--no-urls")
	if err != nil {
		t.Fatal(err)
	}
	// the renditions stay listed — their numbers are what jw download takes
	for _, want := range []string{"# A New Video", "720p", "240p", "jw download"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	assertNoURLs(t, out)
}

// TestNoURLsLeavesJSONAlone pins that --no-urls is a rendering choice, not a
// change to the data model: -o json stays the full record, so jw show, jw open
// and jw download keep working off it.
func TestNoURLsLeavesJSONAlone(t *testing.T) {
	out, err := runCmd(t, searchMux(t), "search", "-l", "de", "-t", "videos", "-o", "json", "--no-urls", "Schöpfung")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"jwLink": "https://`) {
		t.Errorf("json lost its URLs:\n%s", out)
	}
}
