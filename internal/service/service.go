// Package service holds the per-command orchestration shared by the CLI and
// the web server: context and typed parameters in, typed values out. It knows
// nothing about terminals, flags, or HTTP handlers — presentation stays with
// the caller.
package service

import (
	"context"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/jworg"
	"github.com/dgrieser/jw-cli/internal/api/mediator"
	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/api/search"
	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/lang"
	"github.com/dgrieser/jw-cli/internal/model"
)

// Service bundles the API clients every operation draws on. One Service serves
// any number of languages and callers: the clients are stateless per call, and
// the shared httpx.Client paces all outbound traffic together.
type Service struct {
	HTTP     *httpx.Client
	Cache    *httpx.Cache
	Mediator *mediator.Client
	PubMedia *pubmedia.Client
	Search   *search.Client
	WOL      *wol.Client
	JWOrg    *jworg.Client
	Langs    *lang.Resolver
}

// New builds a Service and all its clients around one HTTP client and cache.
func New(hc *httpx.Client, cache *httpx.Cache) *Service {
	m := mediator.New(hc)
	return &Service{
		HTTP:     hc,
		Cache:    cache,
		Mediator: m,
		PubMedia: pubmedia.New(hc),
		Search:   search.New(hc),
		WOL:      wol.New(hc, cache),
		JWOrg:    jworg.New(hc),
		Langs:    &lang.Resolver{Source: m, Cache: cache},
	}
}

// Language resolves a language spec (JW symbol, ISO code, or BCP-47 tag;
// empty = system locale) to a content language.
func (s *Service) Language(ctx context.Context, spec string) (model.Language, error) {
	return s.Langs.Resolve(ctx, spec)
}

// WOLConfig discovers the wol library config for a language.
func (s *Service) WOLConfig(ctx context.Context, lng model.Language) (wol.Config, error) {
	return s.WOL.ConfigFor(ctx, lng.Locale)
}

// BookTable returns the bible reference table, merged with localized book
// names for lng when they can be fetched (best effort). A zero language keeps
// the English table.
func (s *Service) BookTable(ctx context.Context, lng model.Language) *bibleref.Table {
	t := bibleref.English()
	if lng.Locale == "" || lng.Locale == "en" {
		return t
	}
	cfg, err := s.WOL.ConfigFor(ctx, lng.Locale)
	if err != nil {
		return t
	}
	if names, err := s.WOL.LocalizedBookNames(ctx, cfg); err == nil {
		t.Merge(names)
	}
	return t
}

// ParseRefs parses a bible reference string against the (localized) book table.
func (s *Service) ParseRefs(ctx context.Context, lng model.Language, input string) ([]bibleref.Ref, *bibleref.Table, error) {
	t := s.BookTable(ctx, lng)
	refs, err := bibleref.Parse(input, t)
	if err != nil {
		return nil, nil, err
	}
	return refs, t, nil
}

// Chapter fetches the wol chapter page containing ref.
func (s *Service) Chapter(ctx context.Context, lng model.Language, edition string, ref bibleref.Ref) (*wol.ChapterDoc, error) {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return nil, err
	}
	return s.WOL.Chapter(ctx, cfg, edition, ref.Book, ref.Chapter)
}

// ArticleBase is the base URL an article's relative links resolve against:
// wol.jw.org unless the article came from www.jw.org.
func (s *Service) ArticleBase(art model.Article) string {
	if strings.Contains(art.URL, s.HTTP.Base.JWOrg) {
		return s.HTTP.Base.JWOrg
	}
	return s.HTTP.Base.WOL
}
