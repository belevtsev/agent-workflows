// Package projectctx identifies the selected project and inspects optional local
// development capabilities without modifying the repository.
package projectctx

import (
	"context"
	"time"
)

// Snapshot identifies both the selected directory and its current source bytes.
// Fingerprint is an opaque value; callers must not infer completeness from it.
type Snapshot struct {
	Root         string `json:"root"`
	SelectedPath string `json:"selected_path"`
	Head         string `json:"head"`
	Branch       string `json:"branch"`
	Fingerprint  string `json:"fingerprint"`
	// Dirty conservatively compares raw tracked bytes with the Git index and
	// HEAD tree, without executing repository-configured content filters.
	Dirty     bool `json:"dirty"`
	FileCount int  `json:"file_count"`
}

// Marker is a detected, repository-relative capability marker.
type Marker struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// Contract records instruction bytes as data. Instructions are never executed.
type Contract struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Profile describes bounded inspection coverage, including nested projects.
type Profile struct {
	Snapshot  Snapshot   `json:"snapshot"`
	Kinds     []string   `json:"kinds"`
	Markers   []Marker   `json:"markers"`
	Contracts []Contract `json:"contracts"`
	Warnings  []string   `json:"warnings"`
}

// Command is a fixed, bounded read-only probe. Env is an explicit allowlist.
type Command struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
	Stdin      string
	// StopAfterJSONID closes a one-request protocol probe after a complete
	// response; it does not leave a language server running in the background.
	StopAfterJSONID int
}

// CommandResult never contains more than the configured probe output limit.
type CommandResult struct {
	Stdout    []byte
	Truncated bool
}

// Runner permits deterministic testing without starting optional tools.
type Runner func(context.Context, Command) (CommandResult, error)

// DoctorOptions selects the target independently of the workflow installation.
// Timeout bounds the complete inspection, including all optional live probes.
type DoctorOptions struct {
	Repo, Home, CodexHome, ClaudeHome, SkillsHome string
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
	Profile     Profile            `json:"profile"`
	Live        bool               `json:"live"`
	Checks      map[string]Check   `json:"checks"`
	Credentials CredentialPresence `json:"credentials"`
	Warnings    []string           `json:"warnings"`
}

// CredentialPresence reports booleans only. Launcher means literal Codex MCP
// environment entries, independently of this doctor's process environment.
type CredentialPresence struct {
	Environment map[string]bool `json:"environment"`
	Launcher    map[string]bool `json:"launcher"`
	CodexShell  map[string]bool `json:"codex_shell"`
}
