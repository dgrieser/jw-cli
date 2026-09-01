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
	"github.com/dgrieser/jw-cli/internal/service"
)

// articleView is the set of things a command can do with an article once it has
// been fetched: print it, list the bible verses it cites, or list and download
// its images. Every command that ends in an article — article, dailytext,
// meetings and the two meeting parts — binds the same flags and shares the same
// behavior.
type articleView struct {
	refs      bool
	images    bool
	dlImages  bool
	dir       string
	unfold    int
	assumeYes bool
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
	fl.IntVar(&v.unfold, "unfold", 0, "print the text behind every citation, following references this many levels deep")
	fl.BoolVarP(&v.assumeYes, "yes", "y", false, "do not ask before an unfold that needs many requests")
}

// write renders art the way the flags ask for. The default is the document
// itself.
func (v *articleView) write(ctx context.Context, a *app.App, art model.Article) error {
	switch {
	case v.dlImages:
		if len(art.Images) == 0 {
			return fmt.Errorf("no images found in %q", art.Title)
		}
		return downloadAll(ctx, a, service.ImagesToResults(art), v.dir)
	case v.images:
		items := service.ImagesToResults(art)
		rs := results.ResultSet{Kind: "article-images", Query: art.Title, Items: items}
		return writeListing(a, rs, fmt.Sprintf(a.Text().ImagesIn, art.Title))
	case v.refs:
		return writeScriptureRefs(a, art)
	}
	if v.unfold > 0 {
		// best effort: study panes carry their own error notes when the
		// language cannot be resolved
		lng, _ := a.Lang(ctx)
		body, err := a.Service().UnfoldArticle(ctx, lng, art,
			unfoldConfig(a, v.unfold, v.assumeYes), a.Text())
		if err != nil {
			return err
		}
		art.HTML = body
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

// fetchArticle resolves a docid or URL to a parsed article. The language is
// only resolved — and only required — for a docid, which is looked up in the
// library of that language.
func fetchArticle(ctx context.Context, a *app.App, target string) (model.Article, error) {
	var lng model.Language
	if _, err := strconv.Atoi(target); err == nil {
		if lng, err = a.Lang(ctx); err != nil {
			return model.Article{}, err
		}
	}
	return a.Service().Article(ctx, lng, target)
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
	body, err := render.Render(art.HTML, format, a.RenderOptions(a.Service().ArticleBase(art)))
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
		fmt.Fprintf(&b, "- %s\n", mdLinked(r.Text, linkTarget(a, r.BCPath)))
	}
	b.WriteString("\n" + a.Text().ReadOneHint + "\n")
	return a.WriteMarkdown(b.String())
}
