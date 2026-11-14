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
	_, files, err := projectSource{}.Manifest(ctx, path)
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
	// Legacy reports a record that Load refuses with ErrLegacyRecord. Its
	// Record holds only what its newest decodable revision shows
	// (peekedRecord), and neither verify nor abandon can close it: Problem
	// names the way out.
	Legacy bool
	// Unreadable is the text of the error Load returns for a governing
	// record it cannot read for any reason other than ErrLegacyRecord, and
	// empty for every other record. Whether such a record gates is unknown,
	// so it never gates; it needs confirmation. Its Record holds only what
	// its newest decodable revision shows (peekedRecord).
	Unreadable string
	// Newer reports that Load refused the record with ErrNewerRecord: a
	// newer cw wrote it, and upgrading cw reads it.
	Newer bool
}

// Gates reports whether the record blocks publication.
func (o OpenRecord) Gates() bool { return o.Problem != "" }

// NeedsConfirmation reports whether the record could not be read, so that
// whether it gates is unknown: the hook asks before publishing, and
// cw publish refuses.
func (o OpenRecord) NeedsConfirmation() bool { return o.Unreadable != "" }

// FindOpen returns every record in store that governs publication from
// repo, newest latest revision first: each substantial, unabandoned,
// unfinished record whose baseline root is one of repo.Roots and whose
// latest revision was written within MaxOpenAge of now. A revision dated
// after now counts as written now: monotonic stamps (Env.stamp) run ahead of
// a clock that stepped back, and the task being worked on must still gate.
// Starting a new record does not hide an older one:
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
// A record that Load refuses with ErrLegacyRecord is judged the same way
// from its newest decodable revision, unvalidated, and returned with Legacy
// set, so a record written by cw v2.0.66 to v2.0.96 keeps gating as it did under
// v3.0.0-rc.1 until its directory is moved out of the store. One whose
// verification passed without a source_sha256, which those releases never
// recorded, is finished.
//
// A record that Load refuses for any other reason, such as a revision that
// is not JSON or not private to its owner, a broken chain or ErrNewerRecord,
// is returned with Unreadable set when its newest revision that decodes
// governs by the rules above: a garbage newest revision does not hide the
// record, and the caller asks rather than denies on a record it cannot judge.
//
// A store that does not exist is empty. A task directory with no revision
// that decodes, and a verified record whose current source identity cannot be
// captured, are skipped.
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
	// Peek at every newest decodable revision first; loading and checking a
	// whole revision chain is only worth it for a candidate or a successor.
	heads := map[string]head{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(store, entry.Name())
		if peeked, ok := peekNewestReadable(directory); ok && slices.Contains(repo.Roots, peeked.root) {
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
				successor.BaselineManifest.SHA256 == verified &&
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
		legacy := errors.Is(err, ErrLegacyRecord)
		if err != nil {
			// Name the directory of a refused record as Load would: in
			// canonical form, so a store reached through a symlink (macOS
			// /var) matches.
			if canonical, err := filepath.EvalSymlinks(directory); err == nil {
				directory = canonical
			}
		}
		switch {
		case legacy:
			record = peeked.peekedRecord(directory)
			if v := record.Verification; v != nil && v.Complete && v.SourceSHA256 == "" {
				// cw v2.0.66 to v2.0.96 recorded no source digest, and
				// a passing verification was the end of its task.
				continue
			}
		case err != nil:
			open = append(open, OpenRecord{Record: peeked.peekedRecord(directory), Unreadable: err.Error(), Newer: errors.Is(err, ErrNewerRecord)})
			continue
		case !headOf(record).governs(repo.Roots, now):
			continue
		}
		problem, ok := verificationProblem(record, repo.Worktree, digestOf)
		if !ok {
			continue
		}
		if problem != "" && verifiedDigest(record) && finished(record) {
			continue
		}
		if legacy && problem != "" {
			problem = legacyProblem(directory)
		}
		open = append(open, OpenRecord{Record: record, Problem: problem, Legacy: legacy})
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
	// title, selectedPath and verification are read only by peekRevision,
	// for a record this cw refuses to load (peekedRecord).
	title        string
	selectedPath string
	verification *Verification
}

func headOf(record Record) head {
	return head{updatedAt: record.UpdatedAt, kind: record.Plan.Kind, root: record.Baseline.Root, abandoned: record.Abandonment != nil, baseline: record.BaselineManifest.SHA256}
}

// peekedRecord is what FindOpen knows of a record in directory that Load
// refuses: the members of its newest decodable revision that select and gate
// it, unvalidated. Its ID is the directory name, as Load requires of every
// record.
func (h head) peekedRecord(directory string) Record {
	return Record{ID: filepath.Base(directory), Directory: directory, UpdatedAt: h.updatedAt,
		Plan:     Plan{Title: h.title, Kind: h.kind},
		Baseline: projectctx.Snapshot{Root: h.root, SelectedPath: h.selectedPath}, Verification: h.verification}
}

// legacyProblem is the gating problem of a record this cw cannot read: it
// names the way out, since verify and abandon cannot load the record either.
func legacyProblem(directory string) string {
	return "this cw cannot read its record, which cw v2.0.66 to v2.0.96 or a v3 build before v3.0.0-rc.1 wrote; move " + directory + " out of the task store, then start a new task with `cw task start`"
}

// governs reports whether a record with this head governs publication for
// roots at now. A head dated after now has a negative age and governs, as
// one written now does.
func (h head) governs(roots []string, now time.Time) bool {
	return h.kind == "substantial" && !h.abandoned && slices.Contains(roots, h.root) && now.Sub(h.updatedAt) <= MaxOpenAge
}

// storedHead is the JSON shape of head inside a revision file.
type storedHead struct {
	UpdatedAt time.Time `json:"updated_at"`
	Plan      struct {
		Title string `json:"title"`
		Kind  string `json:"kind"`
	} `json:"plan"`
	Baseline struct {
		Root         string `json:"root"`
		SelectedPath string `json:"selected_path"`
	} `json:"baseline"`
	BaselineManifest *struct {
		SHA256 string `json:"sha256"`
	} `json:"baseline_manifest"`
	Verification *struct {
		Complete     bool      `json:"complete"`
		SourceSHA256 string    `json:"source_sha256"`
		CheckedAt    time.Time `json:"checked_at"`
	} `json:"verification"`
	Abandonment *struct{} `json:"abandonment"`
}

// peekNewestReadable returns the head of the highest revision file in
// directory that peekRevision decodes, walking down from the newest, so a
// newest revision that is garbage, unreadable or empty still attributes the
// directory to its repository. It reports false when no revision decodes.
func peekNewestReadable(directory string) (head, bool) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return head{}, false
	}
	var names []string
	for _, entry := range entries {
		if revisionName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	for _, name := range slices.Backward(names) {
		if value, ok := peekRevision(filepath.Join(directory, name)); ok {
			return value, true
		}
	}
	return head{}, false
}

// peekRevision decodes the selecting members of one revision file without
// validating it. A file without a plan kind, baseline root or update time,
// which every revision cw writes has, does not decode.
func peekRevision(path string) (head, bool) {
	file, err := os.Open(path)
	if err != nil {
		return head{}, false
	}
	defer file.Close()
	var stored storedHead
	if err := json.UnmarshalRead(io.LimitReader(file, MaxInputBytes), &stored); err != nil {
		return head{}, false
	}
	if stored.Plan.Kind == "" || stored.Baseline.Root == "" || stored.UpdatedAt.IsZero() {
		return head{}, false
	}
	value := head{updatedAt: stored.UpdatedAt, kind: stored.Plan.Kind, root: stored.Baseline.Root, abandoned: stored.Abandonment != nil,
		title: stored.Plan.Title, selectedPath: stored.Baseline.SelectedPath}
	if stored.BaselineManifest != nil {
		value.baseline = stored.BaselineManifest.SHA256
	}
	if v := stored.Verification; v != nil {
		value.verification = &Verification{Complete: v.Complete, SourceSHA256: v.SourceSHA256, CheckedAt: v.CheckedAt}
	}
	return value, true
}
