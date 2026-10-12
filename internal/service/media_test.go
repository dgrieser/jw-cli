package service

import (
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
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
