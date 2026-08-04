// Package version reports the build version of the jw binary.
//
// Release builds inject the values at link time:
//
//	-ldflags "-X github.com/dgrieser/jw-cli/internal/version.Version=v1.2.3"
//
// Builds without those flags fall back to the information the Go toolchain
// embeds: the module version for `go install module@version`, otherwise the
// VCS stamp of the working tree, yielding "dev+abcdef1" or "dev+abcdef1-dirty".
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Values injected at link time. Leave them empty to fall back to build info.
var (
	Version string
	Commit  string
	Date    string
)

// dev is the version reported when neither ldflags nor build info name one.
const dev = "dev"

// shortLen is the number of commit hash characters shown to users.
const shortLen = 7

// String returns the short version, e.g. "v1.2.3" or "dev+abcdef1-dirty".
func String() string {
	if v := strings.TrimSpace(Version); v != "" && v != dev {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return dev
	}
	// A build from a working tree carries a VCS stamp; the version the toolchain
	// derives from it is a pseudo-version or a "+dirty" tag, so name the commit
	// instead and reserve bare version numbers for released binaries.
	if revision, modified := vcs(info); revision != "" {
		v := dev + "+" + short(revision)
		if modified {
			v += "-dirty"
		}
		return v
	}
	// `go install module@version` records the released version here and stamps
	// no VCS information; a build without either reports "(devel)".
	if v := info.Main.Version; v != "" && v != "(devel)" && v != dev {
		return v
	}
	return dev
}

// Full returns the version with build details on a single line, for --version.
func Full() string {
	v := String()
	var details []string
	if c := commit(); c != "" && !strings.Contains(v, short(c)) {
		details = append(details, "commit "+short(c))
	}
	if d := date(); d != "" {
		details = append(details, "built "+d)
	}
	details = append(details, runtime.Version(), runtime.GOOS+"/"+runtime.GOARCH)
	return v + " (" + strings.Join(details, ", ") + ")"
}

// commit returns the injected commit hash, or the one stamped by the toolchain.
func commit() string {
	if c := strings.TrimSpace(Commit); c != "" {
		return c
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		revision, _ := vcs(info)
		return revision
	}
	return ""
}

// date returns the injected build date, or the stamped commit time.
func date() string {
	if d := strings.TrimSpace(Date); d != "" {
		return d
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return setting(info, "vcs.time")
	}
	return ""
}

// vcs extracts the revision and dirty state from the embedded VCS stamp.
func vcs(info *debug.BuildInfo) (revision string, modified bool) {
	return setting(info, "vcs.revision"), setting(info, "vcs.modified") == "true"
}

func setting(info *debug.BuildInfo, key string) string {
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

func short(hash string) string {
	if len(hash) <= shortLen {
		return hash
	}
	return hash[:shortLen]
}
