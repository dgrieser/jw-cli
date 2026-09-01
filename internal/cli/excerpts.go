package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
)

// excerptWorkers is how many documents are read at once. The per-host limiter
// in internal/httpx caps the real rate; this only keeps a listing of several
// dozen results from taking one round trip after another.
const excerptWorkers = 8

// fillExcerpts replaces each result's teaser with the passage it was cut from.
// Best effort: a result whose document cannot be read, or whose fragment cannot
// be placed in it, keeps the teaser. Only wol results carry a document to read.
func fillExcerpts(ctx context.Context, a *app.App, items []model.Result) {
	todo := make([]int, 0, len(items))
	for i, r := range items {
		if r.WOLLink != "" && r.Snippet != "" {
			todo = append(todo, i)
		}
	}
	if len(todo) == 0 {
		return
	}
	report := excerptProgress(a)
	var (
		wg   sync.WaitGroup
		sem  = make(chan struct{}, excerptWorkers)
		mu   sync.Mutex
		done int
	)
	for _, i := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			blocks, err := a.WOL().Excerpts(ctx, items[i].WOLLink, items[i].Snippet)
			mu.Lock()
			defer mu.Unlock()
			if err == nil && len(blocks) > 0 {
				items[i].Excerpt = strings.Join(blocks, "\n")
			}
			done++
			if report != nil {
				report(done, len(todo))
			}
		}()
	}
	wg.Wait()
	if report != nil {
		report(len(todo), len(todo))
	}
}

// excerptProgress is the counter that runs while the documents are read. There
// is nobody to show it to when stderr is redirected, and it would pile up line
// by line in a log.
func excerptProgress(a *app.App) func(done, total int) {
	if !render.IsTerminal(a.Stderr) {
		return nil
	}
	return progressLine(a.Stderr, a.Text().ExcerptProgress)
}

// progressLine overwrites one line on w until done reaches total, then takes
// the line back. Same shape as statusLine, without the unfold level.
func progressLine(w io.Writer, format string) func(done, total int) {
	written := 0
	return func(done, total int) {
		if done >= total {
			fmt.Fprintf(w, "\r%s\r", strings.Repeat(" ", written))
			written = 0
			return
		}
		line := fmt.Sprintf(format, done, total)
		width := render.StringWidth(line)
		fmt.Fprintf(w, "\r%s%s", line, strings.Repeat(" ", max(written-width, 0)))
		written = max(written, width)
	}
}
