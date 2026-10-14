package service

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

// address is a link spelled out in an error's text.
var address = regexp.MustCompile(`https?://[^\s"']+`)

// failureCause is why something could not be read, as the note standing in
// for it says: the status upstream answered with, or what kept the request
// from being answered — never the address that was asked, which tells a
// reader nothing and which --no-urls promises to leave out.
func failureCause(err error) string {
	var se *httpx.StatusError
	if errors.As(err, &se) {
		return fmt.Sprintf("HTTP %d", se.StatusCode)
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return failureCause(ue.Err)
	}
	return address.ReplaceAllString(err.Error(), "…")
}
