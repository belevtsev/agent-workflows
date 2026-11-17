// Package contextpack collects bounded, inspectable context without running agent code.
package contextpack

import (
	"encoding/json"
	"time"
)

const (
	DefaultBytes = 16 << 10
	MaxBytes     = 1 << 20
	MaxFileBytes = 1 << 20
	MaxArtifact  = 32 << 20
	CacheBytes   = 64 << 20
	Lifetime     = 24 * time.Hour
)

// Request is the versioned, deliberately non-executable collection language.
type Request struct {
	Version    int         `json:"version"`
	Operations []Operation `json:"operations"`
}

// Operation names a read, literal/Go-regexp search, or Git-state query.
// Search paths are exact files or directory prefixes; an empty path searches all sources.
type Operation struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Start   int    `json:"start,omitempty"`
	End     int    `json:"end,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	Regex   bool   `json:"regex,omitempty"`
}

// Line preserves an original line number. Offset is a byte offset within long lines.
type Line struct {
	Path   string `json:"path,omitempty"`
	Number int    `json:"line"`
	Offset int    `json:"offset,omitempty"`
	Text   string `json:"text"`
}

// Item holds one operation's complete retained results. Errors and skip counts are mandatory context.
type Item struct {
	ID        string `json:"id"`
	Path      string `json:"path,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Skipped   int    `json:"skipped_files,omitempty"`
	Lines     []Line `json:"lines,omitempty"`
	LogRunID  string `json:"log_run_id,omitempty"`
	LogChunks int    `json:"log_chunks,omitempty"`
}

// State identifies the worktree content and HEAD independently.
type State struct {
	Tree         string `json:"tree"`
	Head         string `json:"head"`
	SourcePolicy string `json:"source_policy"`
	Raw          string `json:"raw_content"`
}

// Artifact is private evidence, never approval, check evidence, or proof of skill loading.
// Metadata is indivisible: budgets never discard role constraints or check outcomes.
type Artifact struct {
	Version  int             `json:"version"`
	ID       string          `json:"artifact_id"`
	Kind     string          `json:"kind"`
	Created  time.Time       `json:"created_at"`
	State    State           `json:"state"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	Items    []Item          `json:"items"`
}

// PageItem describes all omissions and a cursor for the next bounded page.
type PageItem struct {
	Item
	Total   int    `json:"total"`
	Omitted int    `json:"omitted"`
	Next    string `json:"next_cursor,omitempty"`
	Preview bool   `json:"preview,omitempty"`
}

// Page is the only model-visible representation of an artifact.
type Page struct {
	Version  int             `json:"version"`
	ID       string          `json:"artifact_id"`
	Kind     string          `json:"kind"`
	Created  time.Time       `json:"created_at"`
	State    State           `json:"state"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	CacheHit bool            `json:"cache_hit"`
	Items    []PageItem      `json:"items"`
}
