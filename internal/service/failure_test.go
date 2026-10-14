package service

import (
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

// A note standing in for what could not be read says why without the address
// that was asked: --no-urls promises none, and a reader has no use for it.
func TestFailureCauseLeavesTheAddressOut(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&httpx.StatusError{URL: "https://wol.jw.org/wol/s/r1/lp-e", StatusCode: 503}, "HTTP 503"},
		{fmt.Errorf("fetch search token: %w", &httpx.StatusError{URL: "http://127.0.0.1/tokens", StatusCode: 404}), "HTTP 404"},
		{&url.Error{Op: "Get", URL: "https://wol.jw.org/x", Err: errors.New("connection refused")}, "connection refused"},
		{errors.New("no citation content at https://wol.jw.org/wol/bc/r1/lp-e/1/2"), "no citation content at …"},
	} {
		if got := failureCause(tc.err); got != tc.want {
			t.Errorf("failureCause(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
