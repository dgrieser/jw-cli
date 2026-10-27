package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/server"
)

func newServeCmd(a *app.App) *cobra.Command {
	var (
		addr      string
		port      int
		authFile  string
		pageCache string
		glossary  string
		study     string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run jw as a web server: a web site UI and a JSON API",
		Long: `Serve every feature over HTTP: a browsable web site and a JSON API
under /api/v1 covering search, articles, bible reading with study material,
media, publications, the daily text and the meeting material. Downloads are
answered with a redirect to the file on the public CDN.

The server binds to localhost by default; --addr 0.0.0.0 exposes it to the
network. HTTP basic authentication protects every route when it is configured:
  --auth-file, or $JW_AUTH_FILE   an .htpasswd file (user:hash lines), or an
                                  .htaccess file whose AuthUserFile names one;
                                  a path that does not exist is skipped
  $JW_AUTH_USER, $JW_AUTH_PASSWORD   one more user, from the environment
Both may be set; any of their users gets in. Hashes may be bcrypt
(htpasswd -B), Apache MD5 (htpasswd -m) or SHA-1 (htpasswd -s).

Over HTTPS (or on localhost) the browser keeps the pages it read, so they
come back at once and offline. --page-cache says how that copy is used:
  first      from the copy while it is current, without asking the server
  fallback   the server first, every time; the copy only while it is down
  off        no copy
  auto       fallback behind a login — this server's, or a proxy's that the
             browser's requests show — first otherwise (the default)
Behind an authenticating proxy that the requests do not show (one that
checks a cookie and passes nothing on), set fallback, so a login revoked
there is never bypassed by the copy.

Words the glossaries of a language explain — the study bible's glossary,
Insight on the Scriptures, the Research Guide — are marked in the bible's
verses, on the bible pages and in references; pressing one opens its entry.
--glossary-langs names the languages indexed (default E,X: English and
German), "none" none.

Once a week, at night (1 to 6 o'clock, the server's local time), the week's
Bible reading is studied ahead: every chapter of it is unfolded a level deep,
as jw bible read --unfold 1 does — the study material, who quotes each verse,
the other translations and the glossary entries — for the cache to hold it
when it is read. It asks wol.jw.org one page every few seconds; a night that
does not see it through has the next one go on. --study-langs names the
languages (default E,X), "none" none.

The latest videos on the media page are jw.org's pick, followed by every
other video and audio recording the media library published in the last 30
days, found across all its categories. That list is found anew every three
hours during the day (6 to 21 o'clock, Berlin time), not at night, on the
server: it fills the cache and the list, and downloads none of the media.

The global flags apply: --lang sets the default content language (overridable
per request with ?lang=), -v logs the upstream requests.

Examples:
  jw serve
  jw serve --port 8100
  jw serve --addr 0.0.0.0 --port 8080
  jw serve -l de -v
  jw serve --addr 0.0.0.0 --auth-file /etc/jw/.htpasswd
  JW_AUTH_USER=me JW_AUTH_PASSWORD=secret jw serve --addr 0.0.0.0`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			listen := addr
			if !strings.Contains(listen, ":") {
				listen = net.JoinHostPort(listen, fmt.Sprint(port))
			}
			host, _, err := net.SplitHostPort(listen)
			if err != nil {
				return fmt.Errorf("invalid address %q: %w", listen, err)
			}
			auth, err := loadAuth(authFile)
			if err != nil {
				return err
			}
			if auth.Empty() && !loopbackHost(host) {
				fmt.Fprintf(a.Stderr, "warning: serving without authentication on %s — reachable by anyone on the network\n", listen)
			}
			if !slices.Contains(server.PageCachePolicies, pageCache) {
				return fmt.Errorf("invalid --page-cache %q: want one of %s", pageCache, strings.Join(server.PageCachePolicies, ", "))
			}
			// a server lives long enough to bring what it keeps up to date
			// behind its readers' backs, and to read ahead what they ask
			// for first
			a.Background = true
			srv := server.New(server.Config{
				Auth:          auth,
				PageCache:     pageCache,
				GlossaryLangs: glossaryLangs(glossary),
				StudyLangs:    glossaryLangs(study),
				Svc:           a.Service(),
				DefaultLang:   a.Flags.Lang,
				Logf: func(format string, args ...any) {
					fmt.Fprintf(a.Stderr, format+"\n", args...)
				},
			})
			hs := &http.Server{
				Addr:    listen,
				Handler: srv.Handler(),
				// no WriteTimeout: unfolds and cited listings legitimately run
				// long, and a gone client is caught by the request context
				ReadHeaderTimeout: 10 * time.Second,
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// an explicit listener rather than ListenAndServe, so --port 0
			// binds a free port and the startup line can name it
			ln, err := net.Listen("tcp", listen)
			if err != nil {
				return err
			}
			errc := make(chan error, 1)
			go func() { errc <- hs.Serve(ln) }()
			fmt.Fprintf(a.Stderr, "listening on http://%s\n", hostForURL(ln.Addr().String(), host))
			go srv.Warm(ctx)
			go srv.Study(ctx)
			go srv.Latest(ctx)

			select {
			case err := <-errc:
				return err
			case <-ctx.Done():
				stop()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := hs.Shutdown(shutdownCtx); err != nil {
					return err
				}
				if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
				}
				return nil
			}
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&addr, "addr", "127.0.0.1", "address to bind: a host, or host:port (keep it on localhost without authentication)")
	fl.IntVar(&port, "port", 8080, "port to bind when --addr names no port")
	fl.StringVar(&authFile, "auth-file", os.Getenv("JW_AUTH_FILE"), "basic auth users: an .htpasswd or .htaccess file, used when it exists ($JW_AUTH_FILE)")
	fl.StringVar(&pageCache, "page-cache", cmp.Or(os.Getenv("JW_PAGE_CACHE"), server.PageCacheAuto),
		"how the browser's copy of the pages is used: auto, first, fallback or off ($JW_PAGE_CACHE)")
	fl.StringVar(&glossary, "glossary-langs", cmp.Or(os.Getenv("JW_GLOSSARY_LANGS"), strings.Join(server.DefaultGlossaryLangs, ",")),
		`languages whose glossary words are marked in the pages, comma-separated, or "none" ($JW_GLOSSARY_LANGS)`)
	fl.StringVar(&study, "study-langs", cmp.Or(os.Getenv("JW_STUDY_LANGS"), strings.Join(server.DefaultStudyLangs, ",")),
		`languages the week's Bible reading is unfolded in at night, for the cache, comma-separated, or "none" ($JW_STUDY_LANGS)`)
	return cmd
}

// glossaryLangs splits a list of languages, --glossary-langs or
// --study-langs; "none" is no language.
func glossaryLangs(list string) []string {
	out := []string{}
	for spec := range strings.SplitSeq(list, ",") {
		if spec = strings.TrimSpace(spec); spec != "" && !strings.EqualFold(spec, "none") {
			out = append(out, spec)
		}
	}
	return out
}

// loadAuth gathers the basic auth users: the file, when it exists, and
// $JW_AUTH_USER/$JW_AUTH_PASSWORD, when set. Neither leaves auth off.
func loadAuth(file string) (*server.Credentials, error) {
	auth := &server.Credentials{}
	if file != "" {
		if _, err := os.Stat(file); err == nil {
			if err := auth.LoadFile(file); err != nil {
				return nil, fmt.Errorf("auth file: %w", err)
			}
			if auth.Empty() {
				return nil, fmt.Errorf("auth file %s names no users", file)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("auth file: %w", err)
		}
	}
	user, password := os.Getenv("JW_AUTH_USER"), os.Getenv("JW_AUTH_PASSWORD")
	switch {
	case user != "" && password != "":
		auth.Add(user, password)
	case user != "" || password != "":
		return nil, errors.New("basic auth needs both JW_AUTH_USER and JW_AUTH_PASSWORD")
	}
	return auth, nil
}

// loopbackHost reports whether host stays on this machine.
func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostForURL makes the startup line clickable: a wildcard bind is reached via
// localhost.
func hostForURL(listen, host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		_, port, err := net.SplitHostPort(listen)
		if err != nil {
			return listen
		}
		return net.JoinHostPort("localhost", port)
	}
	return listen
}
