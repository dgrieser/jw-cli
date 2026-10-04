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

const cacheKey = "languages2"

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
// symbol, exact locale, then best BCP-47 match.
func (r *Resolver) Resolve(ctx context.Context, spec string) (model.Language, error) {
	if spec == "" {
		spec = DetectBCP47()
	}
	langs, err := r.All(ctx)
	if err != nil {
		return model.Language{}, err
	}
	// exact symbol match (symbols are upper case: E, X, F, ...)
	for _, l := range langs {
		if l.Symbol == strings.ToUpper(spec) {
			return l, nil
		}
	}
	// exact locale match
	for _, l := range langs {
		if strings.EqualFold(l.Locale, spec) {
			return l, nil
		}
	}
	// BCP-47 matcher over all locales
	want, err := language.Parse(spec)
	if err != nil {
		return model.Language{}, fmt.Errorf("unknown language %q (use a JW symbol like E, an ISO code like de, or run 'jw languages')", spec)
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
	if _, i, conf := m.Match(want); conf > language.No {
		return langs[idx[i]], nil
	}
	return model.Language{}, fmt.Errorf("no content language matches %q (run 'jw languages' to list available languages)", spec)
}
