// Package cli defines the cobra command tree of the jw binary.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/version"
)

// Execute runs the root command and returns the process exit code.
func Execute() int {
	a := app.New(app.Flags{})
	root := NewRootCmd(a)
	err := root.Execute()
	// close the blank-line frame even on failure, so a partially written listing
	// does not run straight into the error line
	_ = a.Flush()
	a.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

// NewRootCmd builds the full command tree around the given app container.
// The app's flag values are bound to the persistent flags.
func NewRootCmd(a *app.App) *cobra.Command {
	root := &cobra.Command{
		Use:   "jw",
		Short: "Access jw.org and wol.jw.org content from the command line",
		Long: `jw is a CLI for the public content of jw.org and wol.jw.org:
search, articles, Bible reading with study material, and downloads of
videos, audio, and publications (PDF, EPUB, ...).`,
		Version:       version.Full(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&a.Flags.Lang, "lang", "l", "", "content language: JW symbol (X), ISO code (de), or BCP-47 (de-AT); default: system locale")
	pf.StringVarP(&a.Flags.Output, "output", "o", "markdown", "output format: markdown (styled for the terminal), raw (unstyled markdown), html, text, or json")
	pf.StringVarP(&a.Flags.File, "file", "f", "", "write output to file instead of stdout")
	pf.BoolVar(&a.Flags.NoColor, "no-color", false, "disable colored output")
	pf.BoolVar(&a.Flags.NoURLs, "no-urls", false, "omit links and URLs from the output; link and image text is kept")
	pf.BoolVarP(&a.Flags.Verbose, "verbose", "v", false, "log HTTP requests to stderr")
	pf.StringVar(&a.Flags.BaseCDN, "base-cdn", "", "override b.jw-cdn.org base URL")
	pf.StringVar(&a.Flags.BaseJWOrg, "base-jworg", "", "override www.jw.org base URL")
	pf.StringVar(&a.Flags.BaseWOL, "base-wol", "", "override wol.jw.org base URL")
	pf.StringVar(&a.Flags.CacheDir, "cache-dir", "", "override cache directory")
	pf.DurationVar(&a.Flags.CacheTTL, "cache-ttl", httpx.DefaultFresh,
		"use what jw.org and wol.jw.org answered as is this long, then check with a HEAD request whether it changed (0 checks every time)")
	pf.Var(newSizeValue(&a.Flags.CacheMax, os.Getenv("JW_CACHE_MAX")), "cache-max",
		"bound the cache, dropping what was used least recently beyond it, e.g. 500MB or 2GB; 0 turns the cache off ($JW_CACHE_MAX)")
	for _, hidden := range []string{"base-cdn", "base-jworg", "base-wol", "cache-dir"} {
		_ = pf.MarkHidden(hidden)
	}

	root.AddCommand(
		newLanguagesCmd(a),
		newPubCmd(a),
		newDownloadCmd(a),
		newMediaCmd(a),
		newSearchCmd(a),
		newOpenCmd(a),
		newShowCmd(a),
		newArticleCmd(a),
		newBibleCmd(a),
		newDailyTextCmd(a),
		newMeetingsCmd(a),
		newServeCmd(a),
	)
	return root
}

// sizeValue is a byte count flag ("1GB", "500MB"), its default taken from the
// environment when that holds a valid size.
type sizeValue struct {
	n   *int64
	raw string
}

const defaultCacheMax = "4GB"

func newSizeValue(n *int64, env string) *sizeValue {
	v := &sizeValue{n: n}
	if env != "" && v.Set(env) == nil {
		return v
	}
	if env != "" {
		fmt.Fprintf(os.Stderr, "Warning: ignoring JW_CACHE_MAX=%q: not a size like 1GB or 500MB\n", env)
	}
	_ = v.Set(defaultCacheMax)
	return v
}

func (v *sizeValue) Set(s string) error {
	n, err := httpx.ParseSize(s)
	if err != nil {
		return err
	}
	*v.n, v.raw = n, s
	return nil
}

func (v *sizeValue) String() string {
	if v == nil || v.raw == "" {
		return defaultCacheMax
	}
	return v.raw
}

func (v *sizeValue) Type() string { return "size" }
