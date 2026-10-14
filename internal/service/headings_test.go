package service

import (
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// A citation naming no more than verses or a page says nothing on its own: it
// is headed by what it is.
func TestUnfoldHeadingOfACitationNamingVersesOrAPage(t *testing.T) {
	txt := i18n.EN.Text()
	for _, c := range []struct {
		name string
		node unfold.Node
		want string
	}{
		{"verses alone take the verse's title",
			unfold.Node{Ref: unfold.Ref{Text: "20-38", Path: "/wol/bc/1/3"}, Title: "Numbers 14:20-38"}, "Numbers 14:20-38"},
		{"a verse alone after a chapter",
			unfold.Node{Ref: unfold.Ref{Text: "34:18;", Path: "/wol/bc/1/4"}, Title: "Psalm 34:18"}, "Psalm 34:18"},
		{"a page alone takes its publication",
			unfold.Node{Ref: unfold.Ref{Text: "523", Path: "/wol/pc/1/2"}, Title: "Reconciliation",
				Publication: "Insight on the Scriptures, Volume 2"},
			"Insight on the Scriptures, Volume 2, 523 → Reconciliation"},
		{"a page spelled out is left as it is",
			unfold.Node{Ref: unfold.Ref{Text: "it-2 760", Path: "/wol/pc/1/2"}, Title: "Reconciliation",
				Publication: "Insight on the Scriptures, Volume 2"},
			"it-2 760 → Reconciliation"},
		{"no trailing punctuation",
			unfold.Node{Ref: unfold.Ref{Text: "w24.08 2-6;", Path: "/wol/pc/1/2"}, Title: "Love"}, "w24.08 2-6 → Love"},
		{"a verse citation naming its book stays",
			unfold.Node{Ref: unfold.Ref{Text: "4. Mo. 14:1-10,", Path: "/wol/bc/1/2"}, Title: "Numbers 14:1-10"}, "4. Mo. 14:1-10"},
	} {
		if got := unfoldHeading(c.node, "", txt); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// The publications index writes "ijwbq articles 120, 147" as an entry per
// link; the second is the 147th article of the same publication, not "147;".
func TestResearchItemsSpellOutEveryEntry(t *testing.T) {
	const guide, index = "Research Guide", "Publications Index"
	items := researchItems([]model.ResearchItem{
		{Title: "“God So Loved the World”", Source: guide},
		{Title: "ijwbq articles 120,", Source: index, PCPath: "/wol/pc/1/0"},
		{Title: "147;", Source: index, PCPath: "/wol/pc/1/2"},
		{Title: "it-2 53,", Source: index, PCPath: "/wol/pc/1/18"},
		{Title: "760,", Source: index, PCPath: "/wol/pc/1/20"},
		{Title: "1000,", Source: index, PCPath: "/wol/pc/1/22"},
		{Title: "12", Source: guide},
	})
	var got []string
	for _, item := range items {
		got = append(got, item.Title)
	}
	want := "“God So Loved the World”|ijwbq articles 120|ijwbq articles 147|it-2 53|it-2 760|it-2 1000|12"
	if strings.Join(got, "|") != want {
		t.Errorf("titles = %s\nwant      %s", strings.Join(got, "|"), want)
	}
}
