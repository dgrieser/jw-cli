package service

import (
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
)

var genesisOutline = []model.OutlineItem{
	{Depth: 0, Title: "Creation of heavens and earth", Label: "1, 2", Start: 1001001, End: 1001002},
	{Depth: 0, Title: "Six days of preparing the earth", Label: "3-31", Start: 1001003, End: 1001031},
	{Depth: 1, Title: "Day 1: light; day and night", Label: "3-5", Start: 1001003, End: 1001005},
	{Depth: 1, Title: "Day 2: expanse", Label: "6-8", Start: 1001006, End: 1001008},
	{Depth: 0, Title: "God rests on the seventh day", Label: "1-3", Start: 1002001, End: 1002003},
}

func versesOf(ids ...int) []Verse {
	out := make([]Verse, len(ids))
	for i, id := range ids {
		out[i] = Verse{Verse: model.Verse{ID: id}}
	}
	return out
}

func outlineTitles(items []model.OutlineItem) string {
	var t []string
	for _, it := range items {
		t = append(t, it.Title)
	}
	return strings.Join(t, " | ")
}

func TestPlaceOutline(t *testing.T) {
	// a passage opened in the middle of a heading is headed by it
	verses := versesOf(1001005, 1001006, 1001007)
	placeOutline(genesisOutline, verses)
	if got, want := outlineTitles(verses[0].Outline), "Six days of preparing the earth | Day 1: light; day and night"; got != want {
		t.Errorf("first verse: got %q, want %q", got, want)
	}
	if got, want := outlineTitles(verses[1].Outline), "Day 2: expanse"; got != want {
		t.Errorf("verse 6: got %q, want %q", got, want)
	}
	if len(verses[2].Outline) != 0 {
		t.Errorf("verse 7: got %q, want nothing", outlineTitles(verses[2].Outline))
	}
}

func TestOutlineHTML(t *testing.T) {
	md, err := render.Render(outlineHTML(genesisOutline[1:3], render.Markdown)+"<span>3 And God said</span>",
		render.Markdown, render.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"- *Six days of preparing the earth* (3-31)",
		"  - *Day 1: light; day and night* (3-5)",
		"3 And God said",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown misses %q:\n%s", want, md)
		}
	}
	txt, err := render.Render("<span>5 a first day.</span>"+outlineHTML(genesisOutline[3:4], render.Text), render.Text, render.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// plain text keeps the depth as indentation, even with the parent elsewhere
	if !strings.Contains(txt, "\u00a0\u00a0Day 2: expanse (6-8)") {
		t.Errorf("text: got %q", txt)
	}
	if outlineHTML(nil, render.Markdown) != "" {
		t.Error("no headings: want no block")
	}
}
