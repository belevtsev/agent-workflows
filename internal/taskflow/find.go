package taskflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	json "encoding/json/v2"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// MaxOpenAge bounds how long a substantial record keeps governing
// publication after its latest revision. A record nobody has touched for
// longer is treated as forgotten, so it cannot block a repository forever;
// `cw task abandon` closes one explicitly.
const MaxOpenAge = 14 * 24 * time.Hour

// SourceIdentifier returns the content identity of a path now, as
// SourceDigest computes it. It is called only for a record whose stored
// verification passed, so the caller keeps the capture out of the common
// unverified path.
type SourceIdentifier func(ctx context.Context, selectedPath string) (string, error)

// SourceDigest identifies the eligible source bytes under path: the SHA-256
// of the encoded file manifest (path, mode and content hash per file) that
// Verify stores as source_sha256. Unlike the snapshot fingerprint it ignores
// HEAD, the branch, the index and the dirty flag, so staging or committing
// the verified bytes leaves it unchanged. It equals a record's
// baseline_manifest.sha256 while the source is as it was at start. In a Git
// repository the manifest covers the whole worktree, whichever directory
// inside it path names.
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

// Repository locates a publication: where the command runs and which
// records govern it.
type Repository struct {
	// Worktree is the canonical Git toplevel of the directory the command
	// runs in.
	Worktree string
	// Roots are the baseline roots whose records govern the command:
	// Worktree and, for a linked worktree, the main worktree's root.
	Roots []string
}

// GitOutput runs git in dir and returns its output without trailing line
// ends; empty output is an error.
type GitOutput func(ctx context.Context, dir string, args ...string) (string, error)

// ResolveRepository returns where a command in dir publishes from: the
// canonical root of the worktree that contains dir and, for a linked
// worktree, the main worktree's root as well, so a record started in either
// location gates both. gitOutput runs git.
func ResolveRepository(ctx context.Context, dir string, gitOutput GitOutput) (Repository, error) {
	toplevel, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repository{}, err
	}
	roots := []string{}
	add := func(path string) {
		if canonical, err := filepath.EvalSymlinks(path); err == nil && !slices.Contains(roots, canonical) {
			roots = append(roots, canonical)
		}
	}
	add(toplevel)
	if len(roots) == 0 {
		return Repository{}, errors.New("worktree root is unavailable")
	}
	common, err := gitOutput(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		// Git before 2.31 has no --path-format; its plain form is relative to dir.
		common, err = gitOutput(ctx, dir, "rev-parse", "--git-common-dir")
	}
	if err == nil {
		if !filepath.IsAbs(common) {
			common = filepath.Join(dir, common)
		}
		if filepath.Base(common) == ".git" {
			add(filepath.Dir(common))
		}
	}
	return Repository{Worktree: roots[0], Roots: roots}, nil
}

// OpenRecord is a record that still governs publication for a repository.
type OpenRecord struct {
	Record Record
	// Problem is empty when the record's passing verification matches the
	// source the command would publish; otherwise it says why the record
	// gates.
	Problem string
}

// Gates reports whether the record blocks publication.
func (o OpenRecord) Gates() bool { return o.Problem != "" }

// FindOpen returns every record in store that governs publication from
// repo, newest latest revision first: each substantial, unabandoned,
// unfinished record whose baseline root is one of repo.Roots and whose
// latest revision was written within MaxOpenAge of now (a revision dated
// after now is skipped). Starting a new record does not hide an older one:
// the older one stays open until it is verified, finished, abandoned or
// ages out.
//
// A returned record gates unless its verification passed on the source the
// command would publish: Verification.Complete with a source_sha256 equal
// to what identify reports for repo.Worktree when that differs from the
// record's baseline root (a linked worktree of the record's checkout), else
// for the record's selected path. identify runs at most once per worktree,
// since a Git source digest covers the whole worktree.
//
// A record whose verification passed on bytes that another governing record
// (substantial, unabandoned, within the age window), started after that
// verification, started from (its baseline_manifest.sha256) is finished,
// and is not returned however the source changed since: the later record
// gates those changes itself. A record started over changed, unverified
// source therefore never finishes an older one; neither does a small,
// abandoned or stale record, and two records started on the same bytes
// before either was verified do not finish each other.
//
// A store that does not exist is empty. A task directory whose record does
// not load, and a verified record whose current source identity cannot be
// captured, are skipped: the hook that calls this fails open and must never
// block on a damaged or unreadable record.
func FindOpen(ctx context.Context, store string, repo Repository, now time.Time, identify SourceIdentifier) ([]OpenRecord, error) {
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
	type capture struct {
		digest string
		ok     bool
	}
	captured := map[string]capture{}
	digestOf := func(worktree, path string) (string, bool) {
		if previous, ok := captured[worktree]; ok {
			return previous.digest, previous.ok
		}
		value, err := identify(ctx, path)
		captured[worktree] = capture{value, err == nil}
		return value, err == nil
	}
	// Peek at every latest revision first; loading and checking a whole
	// revision chain is only worth it for a candidate or a successor.
	heads := map[string]head{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(store, entry.Name())
		if peeked, ok := peekLatest(directory); ok && slices.Contains(repo.Roots, peeked.root) {
			heads[directory] = peeked
		}
	}
	finished := func(record Record) bool {
		verified := record.Verification.SourceSHA256
		for directory, peeked := range heads {
			if peeked.baseline != verified || !peeked.governs(repo.Roots, now) {
				continue
			}
			successor, err := Load(directory)
			if err == nil && successor.ID != record.ID && headOf(successor).governs(repo.Roots, now) &&
				successor.BaselineManifest != nil && successor.BaselineManifest.SHA256 == verified &&
				successor.CreatedAt.After(record.Verification.CheckedAt) {
				return true
			}
		}
		return false
	}
	var open []OpenRecord
	for directory, peeked := range heads {
		if !peeked.governs(repo.Roots, now) {
			continue
		}
		record, err := Load(directory)
		if err != nil || !headOf(record).governs(repo.Roots, now) {
			continue
		}
		problem, ok := verificationProblem(record, repo.Worktree, digestOf)
		if !ok {
			continue
		}
		if problem != "" && verifiedDigest(record) && finished(record) {
			continue
		}
		open = append(open, OpenRecord{Record: record, Problem: problem})
	}
	slices.SortStableFunc(open, func(a, b OpenRecord) int {
		if order := b.Record.UpdatedAt.Compare(a.Record.UpdatedAt); order != 0 {
			return order
		}
		return strings.Compare(a.Record.ID, b.Record.ID)
	})
	return open, nil
}

// verifiedDigest reports whether record's verification passed and recorded
// the digest of the source it passed on.
func verifiedDigest(record Record) bool {
	return record.Verification != nil && record.Verification.Complete && record.Verification.SourceSHA256 != ""
}

// verificationProblem says why record gates a publication from worktree,
// or "" when its passing verification covers the source there. It reports
// false when the current source identity cannot be captured.
func verificationProblem(record Record, worktree string, digestOf func(worktree, path string) (string, bool)) (string, bool) {
	verification := record.Verification
	switch {
	case verification == nil || !verification.Complete:
		return "it has no passing cw task verify", true
	case verification.SourceSHA256 == "":
		return "it was verified by a cw that did not record source digests; verify it again", true
	}
	key, path := record.Baseline.Root, record.Baseline.SelectedPath
	linked := worktree != "" && worktree != record.Baseline.Root
	if linked {
		key, path = worktree, worktree
	}
	current, ok := digestOf(key, path)
	switch {
	case !ok:
		return "", false
	case current == verification.SourceSHA256:
		return "", true
	case linked:
		return fmt.Sprintf("it was verified on the source in %s, but this command runs in the linked worktree %s, whose source differs", record.Baseline.Root, worktree), true
	default:
		return "its source changed after the last passing cw task verify", true
	}
}

// head is the part of a record that selects candidates and successors.
type head struct {
	updatedAt time.Time
	kind      string
	root      string
	abandoned bool
	// baseline is baseline_manifest.sha256, the digest of the source the
	// record started from ("" for records without a manifest).
	baseline string
}

func headOf(record Record) head {
	value := head{updatedAt: record.UpdatedAt, kind: record.Plan.Kind, root: record.Baseline.Root, abandoned: record.Abandonment != nil}
	if record.BaselineManifest != nil {
		value.baseline = record.BaselineManifest.SHA256
	}
	return value
}

// governs reports whether a record with this head governs publication for
// roots at now.
func (h head) governs(roots []string, now time.Time) bool {
	age := now.Sub(h.updatedAt)
	return h.kind == "substantial" && !h.abandoned && slices.Contains(roots, h.root) && age >= 0 && age <= MaxOpenAge
}

// storedHead is the JSON shape of head inside a revision file.
type storedHead struct {
	UpdatedAt time.Time `json:"updated_at"`
	Plan      struct {
		Kind string `json:"kind"`
	} `json:"plan"`
	Baseline struct {
		Root string `json:"root"`
	} `json:"baseline"`
	BaselineManifest *struct {
		SHA256 string `json:"sha256"`
	} `json:"baseline_manifest"`
	Abandonment *struct{} `json:"abandonment"`
}

// peekLatest decodes the selecting members of the highest revision file in
// directory without validating the chain.
func peekLatest(directory string) (head, bool) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return head{}, false
	}
	latest := ""
	for _, entry := range entries {
		if name := entry.Name(); revisionName.MatchString(name) && strings.Compare(name, latest) > 0 {
			latest = name
		}
	}
	if latest == "" {
		return head{}, false
	}
	file, err := os.Open(filepath.Join(directory, latest))
	if err != nil {
		return head{}, false
	}
	defer file.Close()
	var stored storedHead
	if err := json.UnmarshalRead(io.LimitReader(file, MaxInputBytes), &stored); err != nil {
		return head{}, false
	}
	value := head{updatedAt: stored.UpdatedAt, kind: stored.Plan.Kind, root: stored.Baseline.Root, abandoned: stored.Abandonment != nil}
	if stored.BaselineManifest != nil {
		value.baseline = stored.BaselineManifest.SHA256
	}
	return value, true
}
