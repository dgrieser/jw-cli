package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"

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
