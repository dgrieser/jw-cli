package cli

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func articleMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/en", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/en/wol/h/r1/lp-e">home</a>`))
	})
	mux.HandleFunc("/en/wol/d/r1/lp-e/2024360", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>t</title></head><body>
		<div id="article">
		  <header><h1>Caleb—He Fought Loyally</h1></header>
		  <div class="bodyTxt">
		    <p>CALEB trusted in Jehovah. (<a href="/en/wol/bc/r1/lp-e/2024360/0/0" data-bid="1-1" class="b">Num. 14:24</a>)</p>
		    <figure><img src="https://cms-imgp.example/caleb_lg.jpg" alt="Caleb" width="1200" height="675"/><p class="imgCredit">© Example Picture Library</p><figcaption>Caleb in Hebron</figcaption></figure>
		    <figure><img src="https://cms-imgp.example/nameless.jpg"/></figure>
		  </div>
		</div></body></html>`))
	})
	return mux
}

func TestArticleByDocID(t *testing.T) {
	out, err := runCmd(t, articleMux(t), "article", "2024360", "-l", "en")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Caleb—He Fought Loyally", "CALEB trusted in Jehovah", "[Num. 14:24]("} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
}

func TestArticleRefs(t *testing.T) {
	out, err := runCmd(t, articleMux(t), "article", "2024360", "-l", "en", "--refs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Num. 14:24") || !strings.Contains(out, "jw bible read") {
		t.Errorf("refs output:\n%s", out)
	}
}

func TestArticleImagesListing(t *testing.T) {
	out, err := runCmd(t, articleMux(t), "article", "2024360", "-l", "en", "--images")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[image] Caleb in Hebron") || !strings.Contains(out, "caleb_lg.jpg") {
		t.Errorf("images output:\n%s", out)
	}
	// the words and the pixel size the figure states beside the picture
	for _, want := range []string{"Alt text: Caleb", "Credit: © Example Picture Library", "Size: 1200×675 px"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// The metadata is what the listing has to show once the URLs are gone: a
// picture that says nothing about itself falls back to its index, which is
// still what jw show|download takes — the URL is never the title.
func TestArticleImagesListingNoURLs(t *testing.T) {
	out, err := runCmd(t, articleMux(t), "article", "2024360", "-l", "en", "--images", "--no-urls")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[image] Caleb in Hebron", "Credit: © Example Picture Library", "[image] Image 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	assertNoURLs(t, out)
}

// TestArticleRawOutput pins -o raw to the same body as -o markdown to a pipe:
// the markdown is never styled, so scripts and files keep the exact source.
func TestArticleRawOutput(t *testing.T) {
	raw, err := runCmd(t, articleMux(t), "article", "2024360", "-l", "en", "-o", "raw")
	if err != nil {
		t.Fatal(err)
	}
	md, err := runCmd(t, articleMux(t), "article", "2024360", "-l", "en", "-o", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	// each run gets its own mock server port, so compare host-independently
	port := regexp.MustCompile(`127\.0\.0\.1:\d+`)
	if port.ReplaceAllString(raw, "host") != port.ReplaceAllString(md, "host") {
		t.Errorf("-o raw differs from -o markdown on a pipe:\n%q\n%q", raw, md)
	}
	if !strings.Contains(raw, "# Caleb—He Fought Loyally") || !strings.Contains(raw, "[Num. 14:24](") {
		t.Errorf("raw markdown not verbatim:\n%s", raw)
	}
	if strings.Contains(raw, "\x1b[") {
		t.Errorf("raw output must not contain ANSI escapes:\n%q", raw)
	}
}

func TestArticleTextOutput(t *testing.T) {
	out, err := runCmd(t, articleMux(t), "article", "2024360", "-l", "en", "-o", "text")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<") || !strings.Contains(out, "CALEB trusted in Jehovah") {
		t.Errorf("text output:\n%s", out)
	}
}
