package buildinfo

import (
	"runtime/debug"
	"strings"
)

const (
	devVersion   = "dev"
	develVersion = "(devel)"
	unsetCommit  = "none"
	unsetDate    = "unknown"
)

// Info is the running binary's build identity. Date is the commit time (RFC3339 in
// practice), not the build time. Commit and Date are empty when unknown.
type Info struct {
	Version string
	Commit  string
	Date    string
}

// Resolve returns the -ldflags identity, v-prefixed, when version is set (not "dev" or
// empty); otherwise the identity from Go's build info bi, which may be nil.
func Resolve(version, commit, date string, bi *debug.BuildInfo) Info {
	// Release identity must not depend on CI checkout hygiene: any untracked file marks build info dirty.
	if version != "" && version != devVersion {
		return fromLdflags(version, commit, date)
	}

	return fromBuildInfo(bi)
}

func fromLdflags(version, commit, date string) Info {
	info := Info{
		Version: "v" + strings.TrimPrefix(version, "v"),
		Commit:  commit,
		Date:    date,
	}

	if info.Commit == unsetCommit {
		info.Commit = ""
	}

	if info.Date == unsetDate {
		info.Date = ""
	}

	return info
}

func fromBuildInfo(bi *debug.BuildInfo) Info {
	if bi == nil {
		return Info{Version: devVersion}
	}

	info := Info{Version: bi.Main.Version}
	if info.Version == "" || info.Version == develVersion {
		info.Version = devVersion
	}

	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Commit = s.Value
		case "vcs.time":
			info.Date = s.Value
		}
	}

	return info
}
