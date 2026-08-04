package version

import (
	"runtime"
	"strings"
	"testing"
)

// set overrides the injected values for the duration of the test.
func set(t *testing.T, ver, commit, date string) {
	t.Helper()
	oldVer, oldCommit, oldDate := Version, Commit, Date
	Version, Commit, Date = ver, commit, date
	t.Cleanup(func() { Version, Commit, Date = oldVer, oldCommit, oldDate })
}

func TestStringInjected(t *testing.T) {
	set(t, "v1.2.3", "", "")
	if got := String(); got != "v1.2.3" {
		t.Fatalf("String() = %q, want %q", got, "v1.2.3")
	}
}

func TestStringFallsBackToVCSStamp(t *testing.T) {
	set(t, "", "", "")
	got := String()
	// `go test` binaries carry no VCS stamp, so plain "dev" is also valid here.
	if got != dev && !strings.HasPrefix(got, dev+"+") {
		t.Fatalf("String() = %q, want %q or %q + commit", got, dev, dev+"+")
	}
	if rest, found := strings.CutPrefix(got, dev+"+"); found {
		hash := strings.TrimSuffix(rest, "-dirty")
		if len(hash) != shortLen {
			t.Fatalf("String() = %q, want a %d character hash", got, shortLen)
		}
	}
}

func TestStringIgnoresDevPlaceholder(t *testing.T) {
	set(t, dev, "", "")
	if got := String(); got == "" {
		t.Fatal("String() = \"\", want a fallback version")
	}
}

func TestFullIncludesDetails(t *testing.T) {
	set(t, "v1.2.3", "0123456789abcdef", "2026-08-04T10:00:00Z")
	got := Full()
	for _, want := range []string{
		"v1.2.3",
		"commit 0123456",
		"built 2026-08-04T10:00:00Z",
		runtime.Version(),
		runtime.GOOS + "/" + runtime.GOARCH,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Full() = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "0123456789abcdef") {
		t.Errorf("Full() = %q, want the commit shortened", got)
	}
}

func TestFullSkipsCommitAlreadyInVersion(t *testing.T) {
	set(t, "dev+0123456-dirty", "0123456789abcdef", "")
	if got := Full(); strings.Contains(got, "commit ") {
		t.Errorf("Full() = %q, want no repeated commit", got)
	}
}

func TestShort(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"abc", "abc"},
		{"0123456", "0123456"},
		{"0123456789abcdef", "0123456"},
	} {
		if got := short(tc.in); got != tc.want {
			t.Errorf("short(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
