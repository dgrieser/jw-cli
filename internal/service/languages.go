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
// symbol, or locale. An empty q keeps everything.
func FilterLanguages(langs []model.Language, q string) []model.Language {
	if q == "" {
		return langs
	}
	q = strings.ToLower(q)
	filtered := langs[:0]
	for _, l := range langs {
		if strings.Contains(strings.ToLower(l.Name), q) ||
			strings.Contains(strings.ToLower(l.Vernacular), q) ||
			strings.EqualFold(l.Locale, q) ||
			strings.EqualFold(l.Symbol, q) {
			filtered = append(filtered, l)
		}
	}
	return filtered
}
