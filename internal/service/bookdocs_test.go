package service

import (
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

func TestMergeFacts(t *testing.T) {
	listed := []model.Fact{{Label: "Writer", Value: "Matthew"}, {Label: "Writing Completed", Value: "c. 41 C.E."}}
	table := []model.Fact{
		{Label: "Writer", Value: "Matthew"},
		{Label: "Place written", Value: "Israel"},
		{Label: "Writing completed (C.E.)", Value: "c. 41"},
		{Label: "Time covered", Value: "2 B.C.E.–33 C.E."},
	}
	got := MergeFacts(listed, table)
	want := []string{"Writer=Matthew", "Writing Completed=c. 41 C.E.", "Place written=Israel", "Time covered=2 B.C.E.–33 C.E."}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, f := range got {
		if f.Label+"="+f.Value != want[i] {
			t.Errorf("%d: got %s=%s, want %s", i, f.Label, f.Value, want[i])
		}
	}
	// a video-only introduction takes the table's row as it is
	if got := MergeFacts(nil, table); len(got) != 4 || got[1].Label != "Place written" {
		t.Errorf("no listed facts: got %+v", got)
	}
}
