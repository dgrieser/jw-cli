package pubmedia

import (
	"context"
	"strings"
	"testing"
)

// pub-media writes its names as HTML would have them: "(2013&nbsp;Revision)".
// They are read as the text they stand for.
func TestLinksDecodesNames(t *testing.T) {
	c, _ := testClient(t, serveFile(t, "testdata/nwt_E.json"))
	pm, err := c.Links(context.Background(), Query{Pub: "nwt", Lang: "E", Formats: []string{"PDF"}})
	if err != nil {
		t.Fatal(err)
	}
	const want = "New World Translation of the Holy Scriptures (2013 Revision)"
	if pm.PubName != want || pm.ParentPubName != want {
		t.Errorf("names = %q, %q, want %q", pm.PubName, pm.ParentPubName, want)
	}
	pdfs := pm.Files["E"]["PDF"]
	if len(pdfs) != 1 || pdfs[0].Title != want {
		t.Fatalf("files = %+v", pdfs)
	}
	if strings.Contains(pdfs[0].Title+pm.PubName, "&") {
		t.Errorf("an entity is left: %q", pdfs[0].Title)
	}
}
