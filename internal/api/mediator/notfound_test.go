package mediator

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
)

// What the mediator answers with an empty list it does not have: an unknown
// media item or category is pubmedia.ErrNotFound (the server's 404), worded
// for the reader without the sentinel's own words.
func TestNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/mediator/v1/media-items/E/pub-nope_1_VIDEO", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"media": []}`))
	})
	mux.HandleFunc("/apis/mediator/v1/categories/E/Nope", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"category": {}}`))
	})
	c := testClient(t, mux)
	ctx := context.Background()

	_, err := c.MediaItem(ctx, "E", "pub-nope_1_VIDEO")
	if !errors.Is(err, pubmedia.ErrNotFound) || err.Error() != `media item "pub-nope_1_VIDEO" not found in language E` {
		t.Errorf("media item: %v", err)
	}
	for _, get := range []func() error{
		func() error { _, err := c.Category(ctx, "E", "Nope", 0, 0); return err },
		func() error { _, err := c.CategoryInfo(ctx, "E", "Nope"); return err },
	} {
		err := get()
		if !errors.Is(err, pubmedia.ErrNotFound) || strings.Contains(err.Error(), "publication") {
			t.Errorf("category: %v", err)
		}
	}
}
