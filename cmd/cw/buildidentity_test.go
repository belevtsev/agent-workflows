package main

// Tests for test contract TC-1 revision 5, BI-1..BI-5: buildIdentity(v, r, b, info).
//
// Requirement mapping (subtests of TestBuildIdentityFromBuildInfoTC1):
//   BI-1 "nil info ..." cases
//   BI-2 "defaults with pseudo-version ..." case
//   BI-3 "(devel) ..." and "empty Main.Version ..." cases
//   BI-4 "linker values win ..." cases
//   BI-5 "no vcs settings ..." cases

import (
	"runtime/debug"
	"testing"
)

func TestBuildIdentityFromBuildInfoTC1(t *testing.T) {
	const (
		pseudo  = "v0.0.0-20261107120000-abcdef123456"
		infoRev = "abcdef1234567890abcdef1234567890abcdef12"
		infoAt  = "2026-11-07T12:00:00Z"
	)
	vcs := []debug.BuildSetting{
		{Key: "-compiler", Value: "gc"},
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: infoRev},
		{Key: "vcs.time", Value: infoAt},
		{Key: "vcs.modified", Value: "false"},
		{Key: "GOOS", Value: "linux"},
	}
	noVCS := []debug.BuildSetting{
		{Key: "-compiler", Value: "gc"},
		{Key: "GOOS", Value: "linux"},
		{Key: "GOARCH", Value: "amd64"},
	}
	info := func(version string, settings []debug.BuildSetting) *debug.BuildInfo {
		return &debug.BuildInfo{
			GoVersion: "go1.27",
			Path:      "github.com/belevtsev/agent-workflows/cmd/cw",
			Main:      debug.Module{Path: "github.com/belevtsev/agent-workflows", Version: version},
			Settings:  settings,
		}
	}
	type triple struct{ version, revision, builtAt string }
	defaults := triple{"dev", "unknown", "unknown"}
	release := triple{"v3.0.0", "abc123", "2026-11-01T00:00:00Z"}

	cases := []struct {
		name string
		in   triple
		info *debug.BuildInfo
		want triple
	}{
		// BI-1
		{"nil info keeps defaults", defaults, nil, defaults},
		{"nil info keeps linker values", release, nil, release},
		{"nil info keeps arbitrary values", triple{"x", "y", "z"}, nil, triple{"x", "y", "z"}},
		// BI-2
		{"defaults with pseudo-version take info", defaults, info(pseudo, vcs), triple{pseudo, infoRev, infoAt}},
		// BI-3
		{"(devel) keeps dev version, settings still apply", defaults, info("(devel)", vcs), triple{"dev", infoRev, infoAt}},
		{"empty Main.Version keeps dev version, settings still apply", defaults, info("", vcs), triple{"dev", infoRev, infoAt}},
		// BI-4
		{"linker values win over info", release, info(pseudo, vcs), release},
		{"linker values win over (devel) info", release, info("(devel)", vcs), release},
		// BI-5
		{"no vcs settings keep default revision and time", defaults, info(pseudo, noVCS), triple{pseudo, "unknown", "unknown"}},
		{"no vcs settings keep (devel) defaults", defaults, info("(devel)", noVCS), defaults},
		{"no settings at all", defaults, info(pseudo, nil), triple{pseudo, "unknown", "unknown"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var before *debug.BuildInfo
			if tc.info != nil {
				cp := *tc.info
				cp.Settings = append([]debug.BuildSetting(nil), tc.info.Settings...)
				before = &cp
			}
			v, r, b := buildIdentity(tc.in.version, tc.in.revision, tc.in.builtAt, tc.info)
			if got := (triple{v, r, b}); got != tc.want {
				t.Errorf("buildIdentity(%q, %q, %q, info) = %+v, want %+v", tc.in.version, tc.in.revision, tc.in.builtAt, got, tc.want)
			}
			if tc.info != nil && (tc.info.Main != before.Main || len(tc.info.Settings) != len(before.Settings)) {
				t.Errorf("buildIdentity modified its info argument")
			}
		})
	}
}
