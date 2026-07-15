package cli

import (
	"net/http"
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
		    <figure><img src="https://cms-imgp.example/caleb_lg.jpg" alt="Caleb"/><figcaption>Caleb in Hebron</figcaption></figure>
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
