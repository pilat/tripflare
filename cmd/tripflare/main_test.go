package main

import (
	"testing"

	"github.com/pilat/tripflare/internal/buildinfo"
)

func TestVersionLine(t *testing.T) {
	const (
		commit = "8e82b2ab27516a5ef2bbce171592a6162eaf8056"
		date   = "2026-11-07T21:44:34Z"
	)

	tests := []struct {
		name  string
		build buildinfo.Info
		want  string
	}{
		{
			"commit and date",
			buildinfo.Info{Version: "v0.1.3", Commit: commit, Date: date},
			"tripflare v0.1.3 (commit " + commit + ", committed " + date + ")",
		},
		{"commit only", buildinfo.Info{Version: "v0.1.3", Commit: commit}, "tripflare v0.1.3 (commit " + commit + ")"},
		{"date only", buildinfo.Info{Version: "v0.1.3", Date: date}, "tripflare v0.1.3 (committed " + date + ")"},
		{"neither", buildinfo.Info{Version: "dev"}, "tripflare dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionLine(tt.build); got != tt.want {
				t.Errorf("versionLine(%+v) = %q, want %q", tt.build, got, tt.want)
			}
		})
	}
}
