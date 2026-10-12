package server

import (
	"strings"
	"testing"
)

func TestFoldPrintEditions(t *testing.T) {
	in := `<header><h1>T</h1></header>` +
		`<div class="clearfix alternatePresentation clickable"><div class="thumbnail"><img src="a.jpg"/></div><div class="title">Printed edition</div></div>` +
		`<div class="clearfix alternatePresentation clickable hidden"><div class="thumbnail"><img src="b.jpg"/></div><div class="title">Printed edition</div></div>` +
		`<p>Text</p>`
	got := foldPrintEditions(in)
	want := `<header><h1>T</h1></header><details class="section print-edition"><summary>Printed edition</summary>` +
		`<div class="section-body"><img src="a.jpg"/><img src="b.jpg"/></div></details><p>Text</p>`
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
