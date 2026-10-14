package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/download"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
	"github.com/dgrieser/jw-cli/internal/service"
)

func newDownloadCmd(a *app.App) *cobra.Command {
	var (
		quality   string
		subtitles bool
		formats   []string
		issue     string
		booknum   int
		track     int
		dir       string
	)
	cmd := &cobra.Command{
		Use:   "download <index|LANK|url|publication-symbol>",
		Short: "Download videos, audio, and publication files",
		Long: `Download media. The target can be:
  - an index into the last listing (jw search, jw pub, jw media browse, ...)
  - a media LANK like pub-jwbcov_201505_1_VIDEO (from jw media browse/info)
  - a direct file URL
  - a publication symbol (like jw pub ... --download)
  - the key of a publication or a document as jw search -o json shows it,
    like pub-lmd or pa-2016330

A publication or a document — an index or a key — downloads every file
pub-media has for it; -F narrows that down to the formats asked for.

Examples:
  jw search "kingdom" -t videos && jw download 1 -q 720p
  jw download pub-jwbcov_201505_1_VIDEO -q best --subtitles
  jw download w --issue 202405 -F PDF
  jw download https://.../file.mp4 -d ~/Videos`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			ctx := cmd.Context()
			opts := downloadOpts{
				quality: quality, qualitySet: cmd.Flags().Changed("quality"),
				subtitles: subtitles, formats: service.SplitFormats(formats), dir: dir,
			}

			if idx, err := strconv.Atoi(target); err == nil {
				item, err := results.Resolve(a.Cache().Dir(), idx)
				if err != nil {
					return err
				}
				return downloadResult(ctx, a, item, opts)
			}
			if isMediaLANK(target) {
				return downloadLANK(ctx, a, target, opts)
			}
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
				path, err := downloadURL(ctx, a, target, "", 0, dir, "")
				if err != nil {
					return err
				}
				return a.Writef(a.Text().Downloaded+"\n", path)
			}
			// a search result's key, as -o json shows it, or a publication
			// symbol with the flags that narrow it down
			key, ok := docKey(target)
			if !ok {
				key = model.PubKey{Pub: target}
			}
			key.Issue = cmp.Or(key.Issue, issue)
			key.BookNum = cmp.Or(key.BookNum, booknum)
			key.Track = cmp.Or(key.Track, track)
			return downloadPub(ctx, a, key, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&quality, "quality", "q", "best", "video quality: best, worst, 1080p, 720p, 480p, 360p, 240p")
	fl.BoolVar(&subtitles, "subtitles", false, "also download subtitles (.vtt) when available")
	fl.StringSliceVarP(&formats, "file-format", "F", nil, "publication file formats, e.g. PDF,EPUB,MP3")
	fl.StringVar(&issue, "issue", "", "issue date for periodicals (YYYYMM)")
	fl.IntVar(&booknum, "booknum", 0, "bible book number 1-66")
	fl.IntVar(&track, "track", 0, "track number")
	fl.StringVarP(&dir, "dir", "d", "", "download directory (default current directory)")
	return cmd
}

// downloadOpts are the download flags that apply to whatever the target turns
// out to be.
type downloadOpts struct {
	quality string
	// qualitySet is whether --quality was given rather than defaulted: only
	// then does it override a listed rendition.
	qualitySet bool
	subtitles  bool
	formats    []string // publication file formats; empty takes them all
	dir        string
}

// isMediaLANK reports whether a natural key names a mediator media item — a
// video or an audio track. The jw.org search keys its other results too, and
// those name a publication or a document instead (see docKey).
func isMediaLANK(s string) bool {
	if strings.ContainsAny(s, "/ ") {
		return false
	}
	up := strings.ToUpper(s)
	return strings.HasSuffix(up, "_VIDEO") || strings.HasSuffix(up, "_AUDIO")
}

// docKey reads the publication or document out of the natural key the jw.org
// search gives a result that is not a media item: "pub-lmd" is a publication,
// "pi-wp_202005" an issue of a periodical, "pa-2016330" a document by its MEPS
// id. ok is false for any other key — a media item's, a bible verse's, a
// category's — which pub-media has no files for.
func docKey(lank string) (model.PubKey, bool) {
	if isMediaLANK(lank) {
		return model.PubKey{}, false
	}
	if id, ok := strings.CutPrefix(lank, "pa-"); ok {
		n, err := strconv.Atoi(id)
		return model.PubKey{DocID: n}, err == nil && n > 0
	}
	if rest, ok := strings.CutPrefix(lank, "pi-"); ok {
		sym, issue, ok := strings.Cut(rest, "_")
		return model.PubKey{Pub: sym, Issue: issue}, ok && sym != "" && issue != ""
	}
	if sym, ok := strings.CutPrefix(lank, "pub-"); ok && sym != "" {
		return model.PubKey{Pub: sym}, true
	}
	return model.PubKey{}, false
}

// downloadResult dispatches a cached listing item to the right downloader, by
// what the row names. A file is downloaded as is: rows from `jw media info`
// carry both a LANK and the exact rendition, and the row's rendition wins
// unless a quality was asked for. A media item — by its LANK, or a gallery
// video by its gallery page — is downloaded in the quality asked for; a
// publication or a document — a jw.org search finds those too — in the
// formats pub-media has for it.
func downloadResult(ctx context.Context, a *app.App, item model.Result, opts downloadOpts) error {
	media := isMediaLANK(item.LANK) || isGalleryVideo(item)
	if item.FileURL != "" && (!opts.qualitySet || !media) {
		path, err := downloadURL(ctx, a, item.FileURL, item.Checksum, item.Filesize, opts.dir, "")
		if err != nil {
			return err
		}
		return a.Writef(a.Text().Downloaded+"\n", path)
	}
	if mi, ok, err := mediaOf(ctx, a, item); ok {
		if err != nil {
			return err
		}
		return downloadMedia(ctx, a, mi, opts)
	}
	key, ok := docKey(item.LANK)
	switch {
	case ok:
	case item.Pub != nil:
		key = *item.Pub
	case item.DocID != 0:
		key = model.PubKey{DocID: item.DocID}
	default:
		return fmt.Errorf("result %d (%s) has nothing downloadable; try 'jw show %d'", item.Index, item.Title, item.Index)
	}
	if err := downloadPub(ctx, a, key, opts); err != nil {
		return fmt.Errorf("result %d (%s): %w", item.Index, item.Title, err)
	}
	return nil
}

// downloadPub downloads the files pub-media lists for a publication or a
// document, in the formats asked for.
func downloadPub(ctx context.Context, a *app.App, key model.PubKey, opts downloadOpts) error {
	lng, err := a.Lang(ctx)
	if err != nil {
		return err
	}
	pm, err := a.PubMedia().Links(ctx, pubmedia.Query{
		Pub: key.Pub, DocID: key.DocID, Issue: key.Issue, BookNum: key.BookNum, Track: key.Track,
		Formats: opts.formats, Lang: lng.Symbol,
	})
	if err != nil {
		return err
	}
	return downloadAll(ctx, a, service.PubFilesToResults(pm), opts.dir)
}

// downloadLANK fetches a mediator item and downloads the chosen rendition.
func downloadLANK(ctx context.Context, a *app.App, lank string, opts downloadOpts) error {
	lng, err := a.Lang(ctx)
	if err != nil {
		return err
	}
	item, err := a.Service().MediaItem(ctx, lng.Symbol, lank)
	if err != nil {
		return err
	}
	return downloadMedia(ctx, a, item, opts)
}

// downloadMedia downloads the rendition of a media item the quality asks
// for, and its subtitles with --subtitles.
func downloadMedia(ctx context.Context, a *app.App, item model.MediaItem, opts downloadOpts) error {
	file, err := download.PickVideo(item.Files, opts.quality)
	if err != nil {
		return fmt.Errorf("%s: %w", item.Title, err)
	}
	path, err := downloadURL(ctx, a, file.URL, file.Checksum, file.Filesize, opts.dir, "")
	if err != nil {
		return err
	}
	if err := a.Writef(a.Text().Downloaded+"\n", path); err != nil {
		return err
	}
	if opts.subtitles && file.SubtitlesURL != "" {
		sp, err := downloadURL(ctx, a, file.SubtitlesURL, "", 0, opts.dir, "")
		if err != nil {
			return fmt.Errorf("subtitles: %w", err)
		}
		return a.Writef(a.Text().Downloaded+"\n", sp)
	}
	return nil
}

// downloadURL runs one download job with a progress bar on stderr.
func downloadURL(ctx context.Context, a *app.App, url, checksum string, size int64, dir, name string) (string, error) {
	job := download.Job{URL: url, Dir: dir, Filename: name, Checksum: checksum, Size: size}
	return download.Run(ctx, a.HTTP(), job, cliProgress(a.Stderr, name, a.Text().DownloadingLabel))
}

func cliProgress(w io.Writer, desc, fallback string) download.Progress {
	if desc == "" {
		desc = fallback
	}
	var bar *progressbar.ProgressBar
	return func(written, total int64) {
		if bar == nil {
			if total <= 0 {
				total = -1
			}
			bar = progressbar.NewOptions64(total,
				progressbar.OptionSetWriter(w),
				progressbar.OptionSetDescription(desc),
				progressbar.OptionShowBytes(true),
				progressbar.OptionClearOnFinish(),
				progressbar.OptionThrottle(100_000_000), // 100ms
			)
		}
		_ = bar.Set64(written)
	}
}
