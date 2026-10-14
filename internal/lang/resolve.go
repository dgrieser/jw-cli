package lang

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/text/language"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// Source lists all available content languages (implemented by the mediator
// client).
type Source interface {
	Languages(ctx context.Context) ([]model.Language, error)
}

// Resolver maps a user-supplied language spec to a model.Language.
type Resolver struct {
	Source Source
	Cache  *httpx.Cache
}

const cacheKey = "languages3"

// All returns every available content language, kept until the list changes
// upstream; while it cannot be read, the list last seen is used.
func (r *Resolver) All(ctx context.Context) ([]model.Language, error) {
	return httpx.Memo(ctx, r.Cache, cacheKey, func(ctx context.Context) ([]model.Language, error) {
		langs, err := r.Source.Languages(ctx)
		if err == nil && len(langs) == 0 {
			err = errors.New("the language list is empty")
		}
		return langs, err
	})
}

// Resolve maps spec (JW symbol like "X", locale like "de", or BCP-47 like
// "de-AT"; empty = system locale) to a language. Resolution order: exact JW
// symbol as written, exact locale, symbol in any case, then a BCP-47 match the
// matcher is sure of.
//
// Symbols are upper case and locales lower case, and the two overlap: "AS" is
// Assyrian, "as" Assamese; "RU" is Kirundi, "ru" Russian. So a symbol counts
// as written first, then the locale, and only then the symbol in another case
// ("x" for X). A spec nothing matches closely is refused rather than taken for
// whatever the matcher falls back to — that was English for "ach".
func (r *Resolver) Resolve(ctx context.Context, spec string) (model.Language, error) {
	auto := spec == ""
	if auto {
		spec = DetectBCP47()
	}
	langs, err := r.All(ctx)
	if err != nil {
		return model.Language{}, err
	}
	// exact symbol match (symbols are upper case: E, X, F, ...)
	for _, l := range langs {
		if l.Symbol == spec {
			return l, nil
		}
	}
	// exact locale match, however the region or script is joined on
	locale := strings.ReplaceAll(spec, "_", "-")
	for _, l := range langs {
		if strings.EqualFold(l.Locale, locale) {
			return l, nil
		}
	}
	// a symbol written in lower case
	for _, l := range langs {
		if strings.EqualFold(l.Symbol, spec) {
			return l, nil
		}
	}
	unknown := fmt.Errorf("unknown language %q (use a JW symbol like E, an ISO code like de, or run 'jw languages')", spec)
	// BCP-47 matcher over all locales
	want, err := language.Parse(locale)
	if err != nil {
		if auto {
			return english(langs, spec)
		}
		return model.Language{}, unknown
	}
	tags := make([]language.Tag, 0, len(langs))
	idx := make([]int, 0, len(langs))
	for i, l := range langs {
		t, err := language.Parse(l.Locale)
		if err != nil {
			continue
		}
		tags = append(tags, t)
		idx = append(idx, i)
	}
	m := language.NewMatcher(tags)
	// a regional variant of a language there is ("de-AT" for de, "zh-HK" for
	// cmn-hant) is a match; a guess is not, nor is the language the matcher
	// offers to readers of one it does not have — English for "ach", sure as
	// it is that they read it
	if _, i, conf := m.Match(want); conf >= language.High && sameBase(want, tags[i]) {
		return langs[idx[i]], nil
	}
	if auto {
		return english(langs, spec)
	}
	return model.Language{}, unknown
}

// sameBase reports whether two tags are of one language, a macrolanguage and
// its members counted as one: "zh" is "cmn", "nb" is "no".
func sameBase(a, b language.Tag) bool {
	base := func(t language.Tag) language.Base {
		t, _ = language.Macro.Canonicalize(t)
		b, _ := t.Base()
		return b
	}
	return base(a) == base(b)
}

// english is what a system locale no content language matches falls back to:
// nobody asked for that language, so the library's own first language serves.
func english(langs []model.Language, spec string) (model.Language, error) {
	for _, l := range langs {
		if l.Symbol == "E" {
			return l, nil
		}
	}
	return model.Language{}, fmt.Errorf("no content language matches the system locale %q (use --lang, or run 'jw languages')", spec)
}
