package cli

import (
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
  - a media LANK like pub-jwb_202401_1_VIDEO (from jw media browse/info)
  - a direct file URL
  - a publication symbol (like jw pub ... --download)

Examples:
  jw search "kingdom" -t videos && jw download 1 -q 720p
  jw download pub-jwbcov_201505_1_VIDEO -q best --subtitles
  jw download w --issue 202405 -F PDF
  jw download https://.../file.mp4 -d ~/Videos`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			ctx := cmd.Context()

			if idx, err := strconv.Atoi(target); err == nil {
				item, err := results.Resolve(a.Cache().Dir(), idx)
				if err != nil {
					return err
				}
				return downloadResult(ctx, a, item, quality, subtitles, dir)
			}
			if looksLikeLANK(target) {
				return downloadLANK(ctx, a, target, quality, subtitles, dir)
			}
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
				path, err := downloadURL(ctx, a, target, "", 0, dir, "")
				if err != nil {
					return err
				}
				fmt.Fprintf(a.Stdout, "Downloaded %s\n", path)
				return nil
			}
			// publication symbol
			lng, err := a.Lang(ctx)
			if err != nil {
				return err
			}
			pm, err := a.PubMedia().Links(ctx, pubmedia.Query{
				Pub: target, Issue: issue, BookNum: booknum, Track: track,
				Formats: splitFormats(formats), Lang: lng.Symbol,
			})
			if err != nil {
				return err
			}
			return downloadAll(ctx, a, pubFilesToResults(pm), dir)
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

func looksLikeLANK(s string) bool {
	if strings.ContainsAny(s, "/ ") {
		return false
	}
	up := strings.ToUpper(s)
	return strings.HasSuffix(up, "_VIDEO") || strings.HasSuffix(up, "_AUDIO") ||
		((strings.HasPrefix(s, "pub-") || strings.HasPrefix(s, "docid-")) && strings.Contains(s, "_"))
}

// downloadResult dispatches a cached listing item to the right downloader.
func downloadResult(ctx context.Context, a *app.App, item model.Result, quality string, subtitles bool, dir string) error {
	switch {
	case item.LANK != "" && (item.Kind == "video" || item.Kind == "audio"):
		return downloadLANK(ctx, a, item.LANK, quality, subtitles, dir)
	case item.FileURL != "":
		path, err := downloadURL(ctx, a, item.FileURL, item.Checksum, item.Filesize, dir, "")
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "Downloaded %s\n", path)
		return nil
	case item.LANK != "":
		return downloadLANK(ctx, a, item.LANK, quality, subtitles, dir)
	case item.Pub != nil:
		lng, err := a.Lang(ctx)
		if err != nil {
			return err
		}
		pm, err := a.PubMedia().Links(ctx, pubmedia.Query{
			Pub: item.Pub.Pub, DocID: item.Pub.DocID, Issue: item.Pub.Issue,
			BookNum: item.Pub.BookNum, Track: item.Pub.Track, Lang: lng.Symbol,
		})
		if err != nil {
			return err
		}
		return downloadAll(ctx, a, pubFilesToResults(pm), dir)
	case item.DocID != 0:
		lng, err := a.Lang(ctx)
		if err != nil {
			return err
		}
		pm, err := a.PubMedia().Links(ctx, pubmedia.Query{DocID: item.DocID, Lang: lng.Symbol})
		if err != nil {
			return err
		}
		return downloadAll(ctx, a, pubFilesToResults(pm), dir)
	}
	return fmt.Errorf("result %d (%s) has nothing downloadable; try 'jw show %d'", item.Index, item.Title, item.Index)
}

// downloadLANK fetches a mediator item and downloads the chosen rendition.
func downloadLANK(ctx context.Context, a *app.App, lank, quality string, subtitles bool, dir string) error {
	lng, err := a.Lang(ctx)
	if err != nil {
		return err
	}
	item, err := a.Mediator().MediaItem(ctx, lng.Symbol, lank)
	if err != nil {
		return err
	}
	file, err := download.PickVideo(item.Files, quality)
	if err != nil {
		return fmt.Errorf("%s: %w", item.Title, err)
	}
	path, err := downloadURL(ctx, a, file.URL, file.Checksum, file.Filesize, dir, "")
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Downloaded %s\n", path)
	if subtitles && file.SubtitlesURL != "" {
		sp, err := downloadURL(ctx, a, file.SubtitlesURL, "", 0, dir, "")
		if err != nil {
			return fmt.Errorf("subtitles: %w", err)
		}
		fmt.Fprintf(a.Stdout, "Downloaded %s\n", sp)
	}
	return nil
}

// downloadURL runs one download job with a progress bar on stderr.
func downloadURL(ctx context.Context, a *app.App, url, checksum string, size int64, dir, name string) (string, error) {
	job := download.Job{URL: url, Dir: dir, Filename: name, Checksum: checksum, Size: size}
	return download.Run(ctx, a.HTTP(), job, cliProgress(a.Stderr, name))
}

func cliProgress(w io.Writer, desc string) download.Progress {
	if desc == "" {
		desc = "downloading"
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
