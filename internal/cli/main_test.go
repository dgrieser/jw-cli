package cli

import (
	"os"
	"strings"
	"testing"
)

// TestMain keeps the tests from the environment of whoever runs them: a
// $JW_SERVER there would send every "local" run to that server.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "JW_") {
			_ = os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}
