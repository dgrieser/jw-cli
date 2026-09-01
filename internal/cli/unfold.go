package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/service"
)

// unfoldConfig is how the CLI runs an expansion: the depth asked for, a
// confirmation on stdin before a level that needs many requests, and a status
// line on stderr while a level is spent.
func unfoldConfig(a *app.App, depth int, assumeYes bool) service.UnfoldConfig {
	return service.UnfoldConfig{
		Depth:    depth,
		Confirm:  unfoldConfirmer(a, assumeYes),
		Progress: unfoldProgress(a, a.Text()),
	}
}

// unfoldProgress reports a level being spent on one line of stderr — the
// document itself belongs to stdout — where every count overwrites the one
// before it and the last one is erased again, so what is left above the rendered
// output is nothing. Erased with spaces rather than an escape sequence, so a
// terminal that takes no escapes, or was asked for none, is left as clean as any
// other.
//
// A stderr that is not a terminal has nothing to overwrite, where the line would
// pile up once per request, so progress goes unreported there.
func unfoldProgress(a *app.App, txt *i18n.Messages) func(level, done, total int) {
	if !render.IsTerminal(a.Stderr) {
		return nil
	}
	return statusLine(a.Stderr, txt)
}

// statusLine is that line, on any writer.
func statusLine(w io.Writer, txt *i18n.Messages) func(level, done, total int) {
	written := 0
	return func(level, done, total int) {
		if done == total {
			// the level is through: take its line back
			fmt.Fprintf(w, "\r%s\r", strings.Repeat(" ", written))
			written = 0
			return
		}
		line := fmt.Sprintf(txt.UnfoldProgress, level, done, total)
		width := render.StringWidth(line)
		// a shorter count than the one before it would leave the tail of the
		// longer line standing
		fmt.Fprintf(w, "\r%s%s", line, strings.Repeat(" ", max(written-width, 0)))
		written = max(written, width)
	}
}

// unfoldConfirmer asks before a level that needs a lot of requests. There is
// nobody to ask when stdin is not a terminal, so a script has to say up front
// with --yes that the traffic is wanted.
func unfoldConfirmer(a *app.App, assumeYes bool) func(level, requests int) (bool, error) {
	if assumeYes {
		return nil
	}
	if !render.IsTerminal(os.Stdin) {
		return func(level, requests int) (bool, error) {
			return false, fmt.Errorf("unfolding level %d needs %d requests to wol.jw.org; "+
				"pass --yes to allow that without being asked", level, requests)
		}
	}
	// one reader for the whole run: a fresh one per level would throw away
	// anything already buffered
	in := bufio.NewReader(os.Stdin)
	return func(level, requests int) (bool, error) {
		fmt.Fprintf(a.Stderr, a.Text().UnfoldConfirm, level, requests)
		line, err := in.ReadString('\n')
		if err != nil {
			return false, nil // no answer: stop rather than spend the requests
		}
		return affirmative(line), nil
	}
}

// affirmative reports whether an answer to the unfold prompt means yes. Both
// catalog languages are accepted either way round, so a German prompt still
// takes "y" and an English one "j".
func affirmative(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "j", "ja":
		return true
	}
	return false
}
