package wol

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// wol sends a locale it has no library in to the English home page. That is
// no English library for the language asked: it is an error, not English
// content under the language's name. A regional variant sent to its language,
// or a locale sent to its own spelling, is that language.
func TestConfigForLanding(t *testing.T) {
	mux := http.NewServeMux()
	redirect := func(to string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, to, http.StatusTemporaryRedirect) }
	}
	home := func(w http.ResponseWriter, r *http.Request) {}
	mux.HandleFunc("/bez", redirect("/en/wol/h/r1/lp-e"))
	mux.HandleFunc("/es-es", redirect("/es/wol/h/r4/lp-s"))
	mux.HandleFunc("/pt-pt", redirect("/pt-PT/wol/h/r296/lp-tpo"))
	mux.HandleFunc("/en/wol/h/r1/lp-e", home)
	mux.HandleFunc("/es/wol/h/r4/lp-s", home)
	mux.HandleFunc("/pt-PT/wol/h/r296/lp-tpo", home)
	c := testClient(t, mux)
	ctx := context.Background()

	_, err := c.ConfigFor(ctx, "bez")
	if !errors.Is(err, ErrNoLibrary) || !errors.Is(err, ErrNoTranslation) {
		t.Errorf("bez: got %v, want ErrNoLibrary", err)
	}
	if err != nil && err.Error() != `the Watchtower Online Library has no edition in "bez"` {
		t.Errorf("bez: message %q", err)
	}
	for locale, want := range map[string]Config{
		"es-es": {Locale: "es", Rsconf: "r4", Lp: "lp-s"},
		"pt-pt": {Locale: "pt-PT", Rsconf: "r296", Lp: "lp-tpo"},
	} {
		got, err := c.ConfigFor(ctx, locale)
		if err != nil || got != want {
			t.Errorf("%s: got %+v, %v; want %+v", locale, got, err, want)
		}
	}
}
