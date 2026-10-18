package server

import (
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

func TestFoldPrintEditions(t *testing.T) {
	in := `<header><h1>T</h1></header>` +
		`<div class="clearfix alternatePresentation clickable"><div class="thumbnail"><img src="a.jpg"/></div><div class="title">Printed edition</div></div>` +
		`<div class="clearfix alternatePresentation clickable hidden"><div class="thumbnail"><img src="b.jpg"/></div><div class="title">Printed edition</div></div>` +
		`<p>Text</p>`
	got := foldPrintEditions(in)
	want := `<header><h1>T</h1></header><p>Text</p><details class="fold print-edition"><summary><h2>Printed edition</h2></summary>` +
		`<div class="fold-body"><img src="a.jpg"/><img src="b.jpg"/></div></details>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if plain := "<p>Text</p>"; foldPrintEditions(plain) != plain {
		t.Error("a document without printed pages changed")
	}
	if strings.Count(got, "<details") != 1 {
		t.Error("one disclosure for the run")
	}
}

func TestWithoutPrintEditions(t *testing.T) {
	body := foldPrintEditions(`<p>Text</p><div class="alternatePresentation"><div class="thumbnail"><img src="https://x/a.jpg"/></div><div class="title">Printed edition</div></div>`)
	images := []model.MediaAsset{{URL: "https://x/a.jpg"}, {URL: "https://x/photo.jpg"}}
	got := withoutPrintEditions(images, body)
	if len(got) != 1 || got[0].URL != "https://x/photo.jpg" {
		t.Errorf("got %+v, want only the photo", got)
	}
	if got := withoutPrintEditions(images, "<p>Text</p>"); len(got) != 2 {
		t.Errorf("a document without printed pages lost images: %+v", got)
	}
}
