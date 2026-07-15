package cli

import (
	"context"
	"errors"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
)

// Placeholders replaced by the TUI milestone.

func runBrowseTUI(a *app.App, lng model.Language, key string) error {
	return errors.New("interactive mode not built yet")
}

func runSearchTUI(a *app.App, rs results.ResultSet, header string) error {
	return errors.New("interactive mode not built yet")
}

// searchWOL is implemented with the wol milestone.
func searchWOL(ctx context.Context, a *app.App, lng model.Language, query, scope, sortBy string, page int) (results.ResultSet, string, error) {
	return results.ResultSet{}, "", errors.New("wol search engine not built yet")
}
