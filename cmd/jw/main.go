package main

import (
	"os"

	"github.com/dgrieser/jw-cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
