package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
)

func newPubCmd(a *app.App) *cobra.Command {
	var (
		docid    int
		issue    string
		booknum  int
		track    int
		formats  []string
		allLangs bool
		doDL     bool
		dir      string
	)
	cmd := &cobra.Command{
		Use:   "pub [publication-symbol]",
		Short: "List or download publication files (PDF, EPUB, MP3, ...)",
		Long: `Query the publication media API for downloadable files of a publication.

Examples:
  jw pub nwt -F PDF,EPUB              files of the New World Translation
  jw pub w --issue 202405             Watchtower May 2024 in all formats
  jw pub nwt --booknum 40 -F MP3      Matthew audio chapters
  jw pub sjj --track 1 -F MP3         song no. 1
  jw pub w --issue 202405 --download  download instead of listing`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pub := ""
			if len(args) == 1 {
				pub = args[0]
			}
			if pub == "" && docid == 0 {
				return fmt.Errorf("provide a publication symbol (e.g. nwt, w, g) or --docid")
			}
			lng, err := a.Lang(cmd.Context())
			if err != nil {
				return err
			}
			q := pubmedia.Query{
				Pub: pub, DocID: docid, Issue: issue, BookNum: booknum, Track: track,
				Formats: splitFormats(formats), Lang: lng.Symbol, AllLangs: allLangs,
			}
			pm, err := a.PubMedia().Links(cmd.Context(), q)
			if err != nil {
				return err
			}
			items := pubFilesToResults(pm)
			rs := results.ResultSet{Kind: "pub-files", Query: pub, Lang: lng.Symbol, Items: items}
			if doDL {
				return downloadAll(cmd.Context(), a, items, dir)
			}
			header := pm.PubName
			if pm.ParentPubName != "" && pm.ParentPubName != pm.PubName {
				header = pm.ParentPubName + " — " + pm.PubName
			}
			return writeListing(a, rs, header)
		},
	}
	fl := cmd.Flags()
	fl.IntVar(&docid, "docid", 0, "MEPS document id instead of a publication symbol")
	fl.StringVar(&issue, "issue", "", "issue date for periodicals (YYYYMM, e.g. 202405)")
	fl.IntVar(&booknum, "booknum", 0, "bible book number 1-66 (with pub nwt etc.)")
	fl.IntVar(&track, "track", 0, "track number (audio/chapter)")
	fl.StringSliceVarP(&formats, "file-format", "F", nil, "file formats, e.g. PDF,EPUB,JWPUB,MP3,MP4")
	fl.BoolVar(&allLangs, "all-langs", false, "include download links for every language")
	fl.BoolVar(&doDL, "download", false, "download the files instead of listing them")
	fl.StringVarP(&dir, "dir", "d", "", "download directory (default current directory)")
	return cmd
}

func splitFormats(in []string) []string {
	var out []string
	for _, f := range in {
		for _, part := range strings.Split(f, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, strings.ToUpper(part))
			}
		}
	}
	return out
}

// pubFilesToResults flattens the lang→format→files map into a stable listing.
func pubFilesToResults(pm model.PubMedia) []model.Result {
	var items []model.Result
	langs := make([]string, 0, len(pm.Files))
	for sym := range pm.Files {
		langs = append(langs, sym)
	}
	sort.Strings(langs)
	for _, sym := range langs {
		formats := make([]string, 0, len(pm.Files[sym]))
		for f := range pm.Files[sym] {
			formats = append(formats, f)
		}
		sort.Strings(formats)
		for _, format := range formats {
			for _, f := range pm.Files[sym][format] {
				title := f.Title
				if title == "" {
					title = pm.PubName
				}
				ctx := format
				if f.Label != "" {
					ctx += " " + f.Label
				}
				if len(langs) > 1 {
					ctx += ", " + sym
				}
				if f.Track > 0 {
					ctx += fmt.Sprintf(", track %d", f.Track)
				}
				items = append(items, model.Result{
					Kind:     "file",
					Title:    title,
					Context:  ctx,
					FileURL:  f.URL,
					Checksum: f.Checksum,
					Filesize: f.Filesize,
					DocID:    f.DocID,
					Pub:      &model.PubKey{Pub: pm.Pub, Issue: pm.Issue, BookNum: f.BookNum, Track: f.Track},
				})
			}
		}
	}
	return items
}

func downloadAll(ctx context.Context, a *app.App, items []model.Result, dir string) error {
	if len(items) == 0 {
		return fmt.Errorf("no files matched")
	}
	for _, it := range items {
		path, err := downloadURL(ctx, a, it.FileURL, it.Checksum, it.Filesize, dir, "")
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "Downloaded %s\n", path)
	}
	return nil
}
