package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/render"
)

// excerptProgress is the counter that runs while excerpt documents are read.
// There is nobody to show it to when stderr is redirected, and it would pile up
// line by line in a log.
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
