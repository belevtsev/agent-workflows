// Package doctor inspects the selected project and the optional local
// development capabilities around it, for cw doctor and the session-start
// summary, without modifying the repository. It reads the credential store,
// the Codex and Claude Code configuration and Graphify's pinned version; the
// commit gate imports projectctx for the source snapshot, never this package.
package doctor

import (
	"context"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// Runner permits deterministic testing without starting optional tools.
type Runner func(context.Context, projectctx.Command) (projectctx.CommandResult, error)

// DoctorOptions selects the target independently of the workflow installation.
// Timeout bounds the complete inspection, including all optional live probes.
type DoctorOptions struct {
	Repo, Home, CodexHome, ClaudeHome, SkillsHome string
	ConfigDir                                     string
	Live                                          bool
	Timeout                                       time.Duration
	Run                                           Runner
	LookPath                                      func(string) (string, error)
	Getenv                                        func(string) string
}

// Check uses a stable status vocabulary: missing, detected, configured,
// verified, stale, degraded, unchecked, or not_applicable. Source identifies
// evidence provenance; it never contains configuration values or raw output.
type Check struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
	Source string `json:"source"`
}

// DoctorReport keeps metadata detection separate from live verification.
type DoctorReport struct {
	Profile     projectctx.Profile `json:"profile"`
	Live        bool               `json:"live"`
	Checks      map[string]Check   `json:"checks"`
	Credentials CredentialPresence `json:"credentials"`
	Warnings    []string           `json:"warnings"`
}

// CredentialPresence reports booleans only. Launcher means literal Codex MCP
// environment entries, independently of this doctor's process environment.
type CredentialPresence struct {
	Environment map[string]bool   `json:"environment"`
	Launcher    map[string]bool   `json:"launcher"`
	CodexShell  map[string]bool   `json:"codex_shell"`
	Stored      map[string]bool   `json:"stored,omitempty"`
	Effective   map[string]bool   `json:"effective,omitempty"`
	Sources     map[string]string `json:"sources,omitempty"`
}
