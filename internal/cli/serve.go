package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/server"
)

func newServeCmd(a *app.App) *cobra.Command {
	var (
		addr     string
		port     int
		authFile string
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
			srv := server.New(server.Config{
				Auth:        auth,
				Svc:         a.Service(),
				DefaultLang: a.Flags.Lang,
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
	return cmd
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
