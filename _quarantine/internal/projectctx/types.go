// Package projectctx identifies the selected project and its source bytes
// without modifying the repository. The commit gate and task records bind its
// snapshot; cw doctor's environment checks live in internal/doctor.
package projectctx

import (
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
	// WaitDelay is how long the probe's output may still arrive after the
	// process exits or is stopped, before the pipes are closed and the run
	// fails with exec.ErrWaitDelay. Zero selects defaultProbeWaitDelay.
	WaitDelay time.Duration
}

// CommandResult never contains more than the configured probe output limit.
type CommandResult struct {
	Stdout    []byte
	Truncated bool
}
