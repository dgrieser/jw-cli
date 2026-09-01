package server

import (
	"embed"
	"html/template"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// htmlTemplate keeps server.go readable without importing html/template there.
type htmlTemplate = template.Template

// uiPages are the page templates, each parsed together with the base layout
// and the shared listing partial.
var uiPages = []string{
	"index", "search", "article", "bible", "dailytext", "meetings",
	"media", "media_category", "media_item", "pub", "languages", "error",
}

// parseTemplates builds one template set per page at startup, so a parse error
// fails the process (and the tests) rather than a request.
func parseTemplates() map[string]*template.Template {
	out := make(map[string]*template.Template, len(uiPages))
	for _, page := range uiPages {
		out[page] = template.Must(template.New("base.html").ParseFS(templatesFS,
			"templates/base.html", "templates/listing.html", "templates/"+page+".html"))
	}
	return out
}
