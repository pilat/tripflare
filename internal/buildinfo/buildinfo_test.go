package buildinfo

import (
	"runtime/debug"
	"testing"
)

const (
	testCommit = "8e82b2ab27516a5ef2bbce171592a6162eaf8056"
	testDate   = "2026-11-07T21:44:34Z"
	biCommit   = "745be32305f0c1a7d9e4b6f2a8c3d5e7f9a1b2c4"
	biDate     = "2026-11-07T20:57:39Z"
	pseudo     = "v0.1.3-0.20261107205739-745be32305f0"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name                  string
		version, commit, date string
		bi                    *debug.BuildInfo
		want                  Info
	}{
		{
			name:    "ldflags win over dirty build info",
			version: "0.1.3", commit: testCommit, date: testDate,
			bi:   vcsBuild("v0.1.3+dirty", biCommit, biDate, "true"),
			want: Info{Version: "v0.1.3", Commit: testCommit, Date: testDate},
		},
		{
			name:    "ldflags version gets v prefix",
			version: "0.1.3", commit: testCommit, date: testDate,
			want: Info{Version: "v0.1.3", Commit: testCommit, Date: testDate},
		},
		{
			name:    "ldflags version already v-prefixed",
			version: "v0.1.3", commit: testCommit, date: testDate,
			want: Info{Version: "v0.1.3", Commit: testCommit, Date: testDate},
		},
		{
			name:    "build info tagged",
			version: "dev", commit: "none", date: "unknown",
			bi:   vcsBuild("v0.1.3", biCommit, biDate, "false"),
			want: Info{Version: "v0.1.3", Commit: biCommit, Date: biDate},
		},
		{
			name:    "build info pseudo-version",
			version: "dev", commit: "none", date: "unknown",
			bi:   vcsBuild(pseudo, biCommit, biDate, "false"),
			want: Info{Version: pseudo, Commit: biCommit, Date: biDate},
		},
		{
			name:    "build info dirty",
			version: "dev", commit: "none", date: "unknown",
			bi:   vcsBuild("v0.1.3+dirty", biCommit, biDate, "true"),
			want: Info{Version: "v0.1.3+dirty", Commit: biCommit, Date: biDate},
		},
		{
			name:    "build info dirty pseudo-version",
			version: "dev", commit: "none", date: "unknown",
			bi:   vcsBuild(pseudo+"+dirty", biCommit, biDate, "true"),
			want: Info{Version: pseudo + "+dirty", Commit: biCommit, Date: biDate},
		},
		{
			name:    "build info devel without vcs",
			version: "dev", commit: "none", date: "unknown",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want: Info{Version: "dev"},
		},
		{
			name:    "build info go install without vcs",
			version: "dev", commit: "none", date: "unknown",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "v0.1.3"}},
			want: Info{Version: "v0.1.3"},
		},
		{
			name:    "nil build info",
			version: "dev", commit: "none", date: "unknown",
			want: Info{Version: "dev"},
		},
		{
			name:    "ldflags version without commit and date",
			version: "0.1.3", commit: "none", date: "unknown",
			bi:   vcsBuild("v0.1.3+dirty", biCommit, biDate, "true"),
			want: Info{Version: "v0.1.3"},
		},
		{
			name:    "empty ldflags version falls back to build info",
			version: "", commit: testCommit, date: testDate,
			bi:   vcsBuild("v0.1.3", biCommit, biDate, "false"),
			want: Info{Version: "v0.1.3", Commit: biCommit, Date: biDate},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.version, tt.commit, tt.date, tt.bi); got != tt.want {
				t.Errorf("Resolve(%q, %q, %q) = %+v, want %+v", tt.version, tt.commit, tt.date, got, tt.want)
			}
		})
	}
}

func vcsBuild(version, revision, vcsTime, modified string) *debug.BuildInfo {
	return &debug.BuildInfo{
		Main: debug.Module{Version: version},
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: revision},
			{Key: "vcs.time", Value: vcsTime},
			{Key: "vcs.modified", Value: modified},
		},
	}
}
