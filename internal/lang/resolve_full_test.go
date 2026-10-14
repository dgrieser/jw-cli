package lang

import (
	"context"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// fullResolver resolves against a slice of the full language list, where
// symbols and locales overlap: "AS" is Assyrian and "as" Assamese, "EN" is
// Bena and "en" English, "RU" is Kirundi and "ru" Russian.
func fullResolver(t *testing.T) *Resolver {
	t.Helper()
	return &Resolver{
		Source: fakeSource{langs: []model.Language{
			{Symbol: "AS", Locale: "aii", Name: "Assyrian"},
			{Symbol: "AE", Locale: "as", Name: "Assamese"},
			{Symbol: "AC", Locale: "ach", Name: "Acholi"},
			{Symbol: "AH", Locale: "kac", Name: "Kachin"},
			{Symbol: "EN", Locale: "bez", Name: "Bena"},
			{Symbol: "E", Locale: "en", Name: "English"},
			{Symbol: "RU", Locale: "rn", Name: "Kirundi"},
			{Symbol: "U", Locale: "ru", Name: "Russian"},
			{Symbol: "X", Locale: "de", Name: "German"},
			{Symbol: "TPO", Locale: "pt-pt", Name: "Portuguese (Portugal)"},
			{Symbol: "CH", Locale: "cmn-hant", Name: "Chinese Mandarin (Traditional)"},
			{Symbol: "N", Locale: "no", Name: "Norwegian"},
		}},
		Cache: httpx.OpenCacheAt(t.TempDir()),
	}
}

func TestResolveFullList(t *testing.T) {
	r := fullResolver(t)
	for spec, want := range map[string]string{
		"as":    "AE", // a locale, not the symbol in lower case
		"AS":    "AS", // the symbol as written
		"kac":   "AH",
		"AH":    "AH",
		"ah":    "AH", // a symbol in lower case, where no locale is spelled so
		"ach":   "AC",
		"en":    "E",
		"EN":    "EN",
		"ru":    "U",
		"x":     "X",
		"de-AT": "X",
		"pt_PT": "TPO",
		"pt-PT": "TPO",
		"zh-TW": "CH", // a macrolanguage and its member are one language
		"zh-HK": "CH",
		"nb":    "N",
	} {
		got, err := r.Resolve(context.Background(), spec)
		if err != nil {
			t.Errorf("Resolve(%q): %v", spec, err)
			continue
		}
		if got.Symbol != want {
			t.Errorf("Resolve(%q) = %s (%s), want %s", spec, got.Symbol, got.Name, want)
		}
	}
}

// A spec nothing on the list matches closely is refused, never taken for the
// language the matcher falls back to: "ach" was English while Acholi was
// missing from the list.
func TestResolveRefusesAGuess(t *testing.T) {
	r := testResolver(t) // English, German, French, Portuguese
	for _, spec := range []string{"ach", "kac", "as", "zz-ZZ"} {
		got, err := r.Resolve(context.Background(), spec)
		if err == nil {
			t.Errorf("Resolve(%q) = %s, want an error", spec, got.Symbol)
			continue
		}
		if !strings.Contains(err.Error(), "unknown language") {
			t.Errorf("Resolve(%q): %v, want the unknown language error", spec, err)
		}
	}
}

// Nobody asked for the system's language, so one the list does not have falls
// back to English instead of failing every command.
func TestResolveSystemLocaleFallsBack(t *testing.T) {
	r := testResolver(t)
	t.Setenv("LC_ALL", "ach_UG.UTF-8")
	got, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Symbol != "E" {
		t.Errorf("got %s, want E", got.Symbol)
	}
}
