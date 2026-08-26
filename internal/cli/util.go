package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
)

// resolveIndexArg parses a 1-based index argument and resolves it against the
// last saved listing.
func resolveIndexArg(a *app.App, arg string) (model.Result, error) {
	idx, err := strconv.Atoi(arg)
	if err != nil {
		return model.Result{}, fmt.Errorf("expected a result index (a number from the last listing), got %q", arg)
	}
	return results.Resolve(a.Cache().Dir(), idx)
}

// linkTarget passes a URL through, or drops it under --no-urls. Callers hand
// their target through it so a link degrades to its own text instead of
// disappearing along with the words it carries.
func linkTarget(a *app.App, url string) string {
	if a.Flags.NoURLs {
		return ""
	}
	return url
}

// mdLinked renders text as a markdown link to url, or as plain text when there
// is no url. Either way the result is safe as the first thing in a list item:
// bible citations like "1. Kor. 8:4" would otherwise start an ordered list.
func mdLinked(text, url string) string {
	text = strings.TrimSpace(text)
	if url == "" {
		return escapeListMarker(text)
	}
	return "[" + linkTextEscaper.Replace(text) + "](" + url + ")"
}

// linkTextEscaper escapes the brackets that would end the link text early.
var linkTextEscaper = strings.NewReplacer("[", `\[`, "]", `\]`)

// listMarker matches a leading enumerator ("1." / "1)") that markdown would read
// as the start of an ordered list.
var listMarker = regexp.MustCompile(`^(\d+)([.)])`)

func escapeListMarker(text string) string {
	return listMarker.ReplaceAllString(text, `$1\$2`)
}

func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return errors.New("opening a browser is not supported on this platform; the link is printed instead")
	}
	return cmd.Start()
}
