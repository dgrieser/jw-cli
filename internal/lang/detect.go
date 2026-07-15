// Package lang detects the system locale and resolves user language input
// (JW symbol, ISO code, or BCP-47 tag) to a concrete content language.
package lang

import (
	"os"
	"strings"
)

// DetectBCP47 derives a BCP-47 tag from the POSIX locale environment
// (LC_ALL > LC_MESSAGES > LANG), e.g. "de_DE.UTF-8" -> "de-DE".
// Falls back to "en".
func DetectBCP47() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(key)
		if v == "" {
			continue
		}
		if tag := posixToBCP47(v); tag != "" {
			return tag
		}
	}
	return "en"
}

func posixToBCP47(v string) string {
	// strip encoding and modifier: "de_DE.UTF-8@euro" -> "de_DE"
	if i := strings.IndexAny(v, ".@"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "C") || strings.EqualFold(v, "POSIX") {
		return ""
	}
	return strings.ReplaceAll(v, "_", "-")
}
