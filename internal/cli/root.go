// Package cli defines the cobra command tree of the jw binary.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/version"
)

// Execute runs the root command and returns the process exit code.
func Execute() int {
	root := NewRootCmd(app.New(app.Flags{}))
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

// NewRootCmd builds the full command tree around the given app container.
// The app's flag values are bound to the persistent flags.
func NewRootCmd(a *app.App) *cobra.Command {
	root := &cobra.Command{
		Use:     "jw",
		Short:   "Access jw.org and wol.jw.org content from the command line",
		Long: `jw is a CLI for the public content of jw.org and wol.jw.org:
search, articles, Bible reading with study material, and downloads of
videos, audio, and publications (PDF, EPUB, ...).`,
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&a.Flags.Lang, "lang", "l", "", "content language: JW symbol (X), ISO code (de), or BCP-47 (de-AT); default: system locale")
	pf.StringVarP(&a.Flags.Output, "output", "o", "markdown", "output format: html, markdown, text, or json")
	pf.StringVarP(&a.Flags.File, "file", "f", "", "write output to file instead of stdout")
	pf.BoolVar(&a.Flags.NoColor, "no-color", false, "disable colored output")
	pf.BoolVarP(&a.Flags.Verbose, "verbose", "v", false, "log HTTP requests to stderr")
	pf.StringVar(&a.Flags.BaseCDN, "base-cdn", "", "override b.jw-cdn.org base URL")
	pf.StringVar(&a.Flags.BaseJWOrg, "base-jworg", "", "override www.jw.org base URL")
	pf.StringVar(&a.Flags.BaseWOL, "base-wol", "", "override wol.jw.org base URL")
	pf.StringVar(&a.Flags.CacheDir, "cache-dir", "", "override cache directory")
	for _, hidden := range []string{"base-cdn", "base-jworg", "base-wol", "cache-dir"} {
		_ = pf.MarkHidden(hidden)
	}

	root.AddCommand(
		newLanguagesCmd(a),
	)
	return root
}
