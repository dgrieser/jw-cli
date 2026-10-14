package service

import (
	"context"
	"strings"

	"github.com/dgrieser/jw-cli/internal/model"
)

// Languages lists every available content language (cached for a week).
func (s *Service) Languages(ctx context.Context) ([]model.Language, error) {
	return s.Langs.All(ctx)
}

// FilterLanguages keeps the languages matching q by name, vernacular name,
// symbol, or locale. An empty q keeps everything. The list is long — every
// language the library has — and a short q is in many names ("ru" is in
// Burundi and Belorussian), so the languages q names exactly come first, then
// those whose name starts with it, then the others, each in the list's order.
func FilterLanguages(langs []model.Language, q string) []model.Language {
	if q == "" {
		return langs
	}
	q = strings.ToLower(q)
	var exact, prefix, rest []model.Language
	for _, l := range langs {
		name, vernacular := strings.ToLower(l.Name), strings.ToLower(l.Vernacular)
		switch {
		case strings.EqualFold(l.Locale, q) || strings.EqualFold(l.Symbol, q) || name == q || vernacular == q:
			exact = append(exact, l)
		case strings.HasPrefix(name, q) || strings.HasPrefix(vernacular, q):
			prefix = append(prefix, l)
		case strings.Contains(name, q) || strings.Contains(vernacular, q):
			rest = append(rest, l)
		}
	}
	return append(append(exact, prefix...), rest...)
}
