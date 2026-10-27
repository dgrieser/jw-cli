package service

import (
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

// A chapter keeps the parts about it, each with the items about it; a
// heading whose lines name no verses keeps them all, and one naming none
// itself is kept for the lines about the chapter.
func TestChapterParts(t *testing.T) {
	parts := []model.StudyPart{
		{Title: "a", Chapters: []int{1, 2}, Items: []model.StudyPart{{Title: "a1"}, {Title: "a2"}}},
		{Title: "b", Chapters: []int{3}, Items: []model.StudyPart{{Title: "b1", Chapters: []int{3}}}},
		{Title: "c", Items: []model.StudyPart{{Title: "c1", Chapters: []int{2}}, {Title: "c2", Chapters: []int{1, 3}}}},
	}
	got := ChapterParts(parts, 2)
	if len(got) != 2 || got[0].Title != "a" || len(got[0].Items) != 2 || got[1].Title != "c" || len(got[1].Items) != 1 || got[1].Items[0].Title != "c1" {
		t.Errorf("chapter 2: %+v", got)
	}
	if got := ChapterParts(parts, 4); len(got) != 0 {
		t.Errorf("chapter 4: %+v", got)
	}
	if parts[2].Items[1].Title != "c2" || len(parts[2].Items) != 2 {
		t.Errorf("the parts given are changed: %+v", parts)
	}
}
