// Package app is the dependency container shared by all CLI commands. It is
// built once from the global flags and constructs API clients lazily.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/dgrieser/jw-cli/internal/api/jworg"
	"github.com/dgrieser/jw-cli/internal/api/mediator"
	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/api/search"
	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/lang"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
)

// Flags holds the values of the global persistent flags.
type Flags struct {
	Lang    string // -l|--lang
	Output  string // -o|--output: html|markdown|text|json
	File    string // -f|--file: write output to file instead of stdout
	NoColor bool
	Verbose bool

	// hidden overrides for testing against mock servers
	BaseCDN   string
	BaseJWOrg string
	BaseWOL   string
	CacheDir  string
}

type App struct {
	Flags  Flags
	Stdout io.Writer
	Stderr io.Writer

	once     sync.Once
	http     *httpx.Client
	cache    *httpx.Cache
	mediator *mediator.Client
	pubmedia *pubmedia.Client
	search   *search.Client
	wol      *wol.Client
	jworg    *jworg.Client
	resolver *lang.Resolver

	langOnce sync.Once
	language model.Language
	langErr  error
}

func New(f Flags) *App {
	return &App{Flags: f, Stdout: os.Stdout, Stderr: os.Stderr}
}

func (a *App) init() {
	a.once.Do(func() {
		base := httpx.DefaultBaseURLs()
		if a.Flags.BaseCDN != "" {
			base.CDN = a.Flags.BaseCDN
		}
		if a.Flags.BaseJWOrg != "" {
			base.JWOrg = a.Flags.BaseJWOrg
		}
		if a.Flags.BaseWOL != "" {
			base.WOL = a.Flags.BaseWOL
		}
		opts := []httpx.Option{httpx.WithBaseURLs(base)}
		if a.Flags.Verbose {
			opts = append(opts, httpx.WithVerbose(func(format string, args ...any) {
				fmt.Fprintf(a.Stderr, format+"\n", args...)
			}))
		}
		a.http = httpx.New(opts...)
		if a.Flags.CacheDir != "" {
			a.cache = httpx.OpenCacheAt(a.Flags.CacheDir)
		} else {
			a.cache = httpx.OpenCache()
		}
		a.mediator = mediator.New(a.http)
		a.pubmedia = pubmedia.New(a.http)
		a.search = search.New(a.http)
		a.wol = wol.New(a.http, a.cache)
		a.jworg = jworg.New(a.http)
		a.resolver = &lang.Resolver{Source: a.mediator, Cache: a.cache}
	})
}

func (a *App) HTTP() *httpx.Client {
	a.init()
	return a.http
}

func (a *App) Cache() *httpx.Cache {
	a.init()
	return a.cache
}

func (a *App) Mediator() *mediator.Client {
	a.init()
	return a.mediator
}

func (a *App) PubMedia() *pubmedia.Client {
	a.init()
	return a.pubmedia
}

func (a *App) Search() *search.Client {
	a.init()
	return a.search
}

func (a *App) WOL() *wol.Client {
	a.init()
	return a.wol
}

func (a *App) JWOrg() *jworg.Client {
	a.init()
	return a.jworg
}

// WOLConfig resolves the language then discovers the wol library config.
func (a *App) WOLConfig(ctx context.Context) (wol.Config, error) {
	lng, err := a.Lang(ctx)
	if err != nil {
		return wol.Config{}, err
	}
	return a.wol.ConfigFor(ctx, lng.Locale)
}

func (a *App) Resolver() *lang.Resolver {
	a.init()
	return a.resolver
}

// Lang resolves the --lang flag (or system locale) once and memoizes it.
func (a *App) Lang(ctx context.Context) (model.Language, error) {
	a.init()
	a.langOnce.Do(func() {
		a.language, a.langErr = a.resolver.Resolve(ctx, a.Flags.Lang)
	})
	return a.language, a.langErr
}

// Format parses the -o|--output flag.
func (a *App) Format() (render.Format, error) {
	return render.ParseFormat(a.Flags.Output)
}

// Write sends content to stdout or, with -f|--file, to a file.
func (a *App) Write(content string) error {
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content += "\n"
	}
	if a.Flags.File != "" {
		return os.WriteFile(a.Flags.File, []byte(content), 0o644)
	}
	_, err := io.WriteString(a.Stdout, content)
	return err
}

// WriteJSON marshals v with indentation and writes it like Write.
func (a *App) WriteJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return a.Write(string(b))
}
