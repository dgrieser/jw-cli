package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/results"
)

// articleView is the set of things a command can do with an article once it has
// been fetched: print it, list the bible verses it cites, or list and download
// its images. Every command that ends in an article — article, dailytext,
// meetings and the two meeting parts — binds the same flags and shares the same
// behavior.
type articleView struct {
	refs     bool
	images   bool
	dlImages bool
	dir      string
}

// bind registers the view's flags on cmd. The wording names the document rather
// than "the article", since the same flags serve the daily text and the meeting
// material.
func (v *articleView) bind(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.BoolVar(&v.refs, "refs", false, "list the bible verses referenced in the document")
	fl.BoolVar(&v.images, "images", false, "list the images of the document (downloadable by index)")
	fl.BoolVar(&v.dlImages, "download-images", false, "download all images of the document")
	fl.StringVarP(&v.dir, "dir", "d", "", "download directory for --download-images")
}

// write renders art the way the flags ask for. The default is the document
// itself.
func (v *articleView) write(ctx context.Context, a *app.App, art model.Article) error {
	switch {
	case v.dlImages:
		if len(art.Images) == 0 {
			return fmt.Errorf("no images found in %q", art.Title)
		}
		return downloadAll(ctx, a, imagesToResults(art), v.dir)
	case v.images:
		items := imagesToResults(art)
		rs := results.ResultSet{Kind: "article-images", Query: art.Title, Items: items}
		return writeListing(a, rs, fmt.Sprintf(a.Text().ImagesIn, art.Title))
	case v.refs:
		return writeScriptureRefs(a, art)
	}
	return writeArticle(a, art)
}

func newArticleCmd(a *app.App) *cobra.Command {
	var view articleView
	cmd := &cobra.Command{
		Use:   "article <url|docid>",
		Short: "Read an article from wol.jw.org or www.jw.org",
		Long: `Fetch an article and print it in the chosen output format
(markdown by default; -o html, -o text, -o json).

The target is either a MEPS document id (looked up in the Watchtower Online
Library in your language) or a full URL from either site.

Examples:
  jw article 1102025912
  jw article https://wol.jw.org/en/wol/d/r1/lp-e/1102025912
  jw article 1102025912 --refs           list bible verses cited in the article
  jw article 1102025912 --images         list images (downloadable by index)
  jw article 1102025912 --download-images -d ./pics`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			art, err := fetchArticle(cmd.Context(), a, args[0])
			if err != nil {
				return err
			}
			return view.write(cmd.Context(), a, art)
		},
	}
	view.bind(cmd)
	return cmd
}

// fetchArticle resolves a docid or URL to a parsed article.
func fetchArticle(ctx context.Context, a *app.App, target string) (model.Article, error) {
	if docid, err := strconv.Atoi(target); err == nil {
		cfg, err := a.WOLConfig(ctx)
		if err != nil {
			return model.Article{}, err
		}
		return a.WOL().Document(ctx, cfg, docid)
	}
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return model.Article{}, fmt.Errorf("expected a document id or URL, got %q", target)
	}
	if strings.Contains(target, "wol.jw.org") || strings.Contains(target, "/wol/") {
		return a.WOL().DocumentByURL(ctx, target)
	}
	return a.JWOrg().ArticleByURL(ctx, target)
}

// writeArticle renders an article body in the selected format.
func writeArticle(a *app.App, art model.Article) error {
	format, err := a.Format()
	if err != nil {
		return err
	}
	if format == render.JSON {
		return a.WriteJSON(art)
	}
	base := a.HTTP().Base.WOL
	if strings.Contains(art.URL, a.HTTP().Base.JWOrg) {
		base = a.HTTP().Base.JWOrg
	}
	body, err := render.Render(art.HTML, format, render.Options{BaseURL: base})
	if err != nil {
		return err
	}
	switch format {
	case render.Markdown, render.Raw:
		if art.Title != "" && !strings.HasPrefix(body, "# ") {
			body = "# " + art.Title + "\n\n" + body
		}
	case render.Text:
		if art.Title != "" && !strings.HasPrefix(body, art.Title) {
			body = art.Title + "\n\n" + body
		}
	}
	return a.WriteMarkdown(body)
}

func imagesToResults(art model.Article) []model.Result {
	var items []model.Result
	for _, img := range art.Images {
		title := img.Caption
		if title == "" {
			title = img.Alt
		}
		if title == "" {
			title = img.URL
		}
		items = append(items, model.Result{
			Kind: "image", Title: title, FileURL: img.URL, ImageURL: img.URL,
		})
	}
	return items
}

func writeScriptureRefs(a *app.App, art model.Article) error {
	format, err := a.Format()
	if err != nil {
		return err
	}
	if format == render.JSON {
		return a.WriteJSON(art.ScriptureRefs)
	}
	if len(art.ScriptureRefs) == 0 {
		return a.Write(fmt.Sprintf(a.Text().NoBibleRefs, art.Title))
	}
	var b strings.Builder
	fmt.Fprintf(&b, a.Text().BibleRefsIn+"\n\n", art.Title)
	for _, r := range art.ScriptureRefs {
		fmt.Fprintf(&b, "- %s\n", mdLinked(r.Text, r.BCPath))
	}
	b.WriteString("\n" + a.Text().ReadOneHint + "\n")
	return a.WriteMarkdown(b.String())
}
