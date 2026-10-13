package service

import (
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

func TestMediaBody(t *testing.T) {
	got := mediaBody([]model.MediaAsset{
		{URL: "https://img/large", ThumbnailURL: "https://img/thumb", Caption: "Winter"},
		{URL: "https://img/poster", FinderLink: "https://www.jw.org/finder?lank=x", Caption: "Video"},
	})
	for _, want := range []string{
		`<a class="gallery-tile" href="https://img/large"><span class="poster"><img src="https://img/thumb"`,
		`<a class="gallery-tile video" href="https://www.jw.org/finder?lank=x">`,
		`<span class="cap">Winter</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if !IsPart(PartMedia) {
		t.Error("media is no part a page can load")
	}
}

// A verse inside an expansion brings its pictures as a verse of the page
// does: tiles when its material is read, a heading loaded once opened when
// that material loads later — and nothing when it has none.
func TestHopVerseMedia(t *testing.T) {
	txt := i18n.DE.Text()
	verse := `<span id="v40-2-1-1" class="v">Text</span>`
	media := []model.MediaAsset{{URL: "https://img/large", Caption: "Winter"}}
	find := func(secs []UnfoldSection) *UnfoldSection {
		for i := range secs {
			if secs[i].Ref == MediaRef {
				return &secs[i]
			}
		}
		return nil
	}
	full := unfold.Node{HTML: verse, Title: "Mt 2:1", Media: media, Parts: &unfold.Parts{}}
	if sec := find(hopVerseSections(full, 2, txt)); sec == nil || !strings.Contains(sec.Body, "gallery-tile") {
		t.Errorf("no media tiles for a verse read in full: %+v", sec)
	}
	lazy := unfold.Node{HTML: verse, Title: "Mt 2:1", Media: media, Parts: &unfold.Parts{Lazy: true}}
	if sec := find(hopVerseSections(lazy, 2, txt)); sec == nil || !strings.Contains(sec.Lazy, "part=media") {
		t.Errorf("no lazy media heading: %+v", sec)
	}
	lazy.Media = nil
	if sec := find(hopVerseSections(lazy, 2, txt)); sec == nil {
		t.Error("no media heading for a verse whose media is not known yet")
	}
	lazy.MediaKnown = true
	if sec := find(hopVerseSections(lazy, 2, txt)); sec != nil {
		t.Errorf("a media heading for a verse known to have none: %+v", sec)
	}
}
