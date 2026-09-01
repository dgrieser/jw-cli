package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

func errMissing(name string) error {
	return fmt.Errorf("missing required parameter %q", name)
}

func errExclusive(a, b string) error {
	return fmt.Errorf("%s and %s cannot be combined", a, b)
}

// valueOr is a query parameter with a default for absent or empty.
func valueOr(r *http.Request, name, def string) string {
	if v := r.FormValue(name); v != "" {
		return v
	}
	return def
}

// splitCSV splits a comma-separated parameter into its trimmed parts.
func splitCSV(v string) []string {
	var out []string
	for part := range strings.SplitSeq(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// dateParam parses ?date=YYYY-MM-DD, defaulting to today.
func dateParam(r *http.Request) (time.Time, error) {
	v := r.FormValue("date")
	if v == "" {
		return time.Now(), nil
	}
	date, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", v)
	}
	return date, nil
}
