package taskflow

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	json "encoding/json/v2"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
)

// MaxOpenAge bounds how long a substantial record without a current passing
// verification keeps gating publication. Older records are treated as
// abandoned so a forgotten task cannot block a repository indefinitely.
const MaxOpenAge = 14 * 24 * time.Hour

// SourceIdentifier returns the content identity of a selected path now, as
// SourceDigest computes it. It is called only for a record whose stored
// verification passed, so the caller keeps the capture out of the common
// no-record path.
type SourceIdentifier func(ctx context.Context, selectedPath string) (string, error)

// SourceDigest identifies the eligible source bytes under path: the SHA-256
// of the encoded file manifest (path, mode and content hash per file) that
// Verify stores as source_sha256. Unlike the snapshot fingerprint it ignores
// HEAD, the branch, the index and the dirty flag, so staging or committing
// the verified bytes leaves it unchanged. It equals a record's
// baseline_manifest.sha256 while the source is as it was at start.
func SourceDigest(ctx context.Context, path string) (string, error) {
	_, files, err := projectctx.CaptureManifest(ctx, path)
	if err != nil {
		return "", err
	}
	manifest, err := encode(files)
	if err != nil {
		return "", err
	}
	return digest(manifest), nil
}

// FindOpen returns the record that gates publication for a repository whose
// worktree roots are roots: the newest substantial record in store that was
// created within MaxOpenAge of now and whose baseline root is one of roots,
// when that record is open. It returns nil when no such record exists or the
// newest one is closed; older records are superseded by the newest.
//
// A record is closed when its verification passed on the source bytes its
// selected path holds now: Verification.Complete with a source_sha256 equal
// to what identify reports. A record without a complete verification, one
// verified before source digests were recorded, or one whose verified bytes
// have since changed is open.
//
// A store that does not exist is empty. A task directory whose latest record
// cannot be read, or whose record chain does not load, is skipped and the
// next newest candidate decides. A current identity that cannot be captured
// also skips its record: the hook that calls this fails open and must never
// block on a damaged or unreadable record.
func FindOpen(ctx context.Context, store string, roots []string, now time.Time, identify SourceIdentifier) (*Record, error) {
	if identify == nil {
		return nil, errors.New("a source identifier is required")
	}
	entries, err := os.ReadDir(store)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Peek at the latest revision's identifying members first; loading a
	// whole revision chain is only worth it for the newest candidate.
	type candidate struct {
		directory string
		createdAt time.Time
	}
	var candidates []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(store, entry.Name())
		head, ok := peekLatest(directory)
		if !ok || head.Plan.Kind != "substantial" || !slices.Contains(roots, head.Baseline.Root) {
			continue
		}
		if age := now.Sub(head.CreatedAt); age < 0 || age > MaxOpenAge {
			continue
		}
		candidates = append(candidates, candidate{directory, head.CreatedAt})
	}
	slices.SortStableFunc(candidates, func(a, b candidate) int { return b.createdAt.Compare(a.createdAt) })
	for _, next := range candidates {
		record, err := Load(next.directory)
		if err != nil || record.Plan.Kind != "substantial" || !slices.Contains(roots, record.Baseline.Root) {
			continue
		}
		if record.Verification != nil && record.Verification.Complete && record.Verification.SourceSHA256 != "" {
			current, err := identify(ctx, record.Baseline.SelectedPath)
			if err != nil {
				continue
			}
			if current == record.Verification.SourceSHA256 {
				return nil, nil
			}
		}
		return &record, nil
	}
	return nil, nil
}

// peekHead is the part of a record that selects candidates.
type peekHead struct {
	CreatedAt time.Time `json:"created_at"`
	Plan      struct {
		Kind string `json:"kind"`
	} `json:"plan"`
	Baseline struct {
		Root string `json:"root"`
	} `json:"baseline"`
}

// peekLatest decodes the selecting members of the highest revision file in
// directory without validating the chain.
func peekLatest(directory string) (peekHead, bool) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return peekHead{}, false
	}
	latest := ""
	for _, entry := range entries {
		if name := entry.Name(); revisionName.MatchString(name) && strings.Compare(name, latest) > 0 {
			latest = name
		}
	}
	if latest == "" {
		return peekHead{}, false
	}
	file, err := os.Open(filepath.Join(directory, latest))
	if err != nil {
		return peekHead{}, false
	}
	defer file.Close()
	var head peekHead
	if err := json.UnmarshalRead(io.LimitReader(file, MaxInputBytes), &head); err != nil {
		return peekHead{}, false
	}
	return head, true
}
