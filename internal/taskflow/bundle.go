package taskflow

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
)

// A reviewer bundle orients an independent review. cw generates every part of
// it from the task record and the current source except the four reach facets
// of each row, which the coordinator fills. A review assignment carries the
// filled bundle's path and hash; assign regenerates the bundle and refuses any
// difference outside the facets, so the bundle a reviewer reads is bound to the
// record and source it was assigned at.

const (
	// bundleVersion 2 adds the reviewer's duty to answer each reach row in the
	// review result's reach_runs; version 1 bundles never asked for it.
	bundleVersion        = 2
	baselineManifestFile = "baseline-manifest.json"
	maxReferences        = 10
	maxReferenceText     = 240
)

var bundleFileName = regexp.MustCompile(`^bundle-[A-Za-z][A-Za-z0-9_-]{0,63}-[0-9a-f]{32}\.json$`)

// ManifestRef binds the private task artifact that lists every eligible file
// at task start (path, mode and SHA-256).
type ManifestRef struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Files  int    `json:"files"`
}

// BundleRef names the filled reviewer bundle a review assignment carries.
type BundleRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ChangedFile is one entry of a review's manifest of files changed since the
// task baseline. A deleted file has an empty mode and SHA-256.
type ChangedFile struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	SHA256  string `json:"sha256"`
	Deleted bool   `json:"deleted"`
}

type BundleRequirement struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type BundleFinding struct {
	AssignmentID string      `json:"assignment_id"`
	FindingID    string      `json:"finding_id"`
	Summary      string      `json:"summary"`
	Severity     string      `json:"severity"`
	Evidence     []SourceRef `json:"evidence"`
	Disposition  string      `json:"disposition"`
	Reason       string      `json:"reason"`
}

type BundleCheck struct {
	CheckID  string `json:"check_id"`
	RunID    string `json:"run_id"`
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code"`
	Current  bool   `json:"current"`
	Log      string `json:"log"`
}

type ReachHints struct {
	Shebang    string   `json:"shebang"`
	References []string `json:"references"`
}

// ReachRow asks how a changed file is reached. Each facet holds the
// coordinator's answer from source, or "n/a: <reason>".
type ReachRow struct {
	Path            string     `json:"path"`
	Deleted         bool       `json:"deleted"`
	Hints           ReachHints `json:"hints"`
	Callers         string     `json:"callers"`
	InvocationModes string     `json:"invocation_modes"`
	Platforms       string     `json:"platforms"`
	Consumers       string     `json:"consumers"`
}

type Bundle struct {
	Version            int                 `json:"version"`
	TaskID             string              `json:"task_id"`
	ReviewID           string              `json:"review_id"`
	Kind               string              `json:"kind"`
	PreviousReview     string              `json:"previous_review"`
	Snapshot           projectctx.Snapshot `json:"snapshot"`
	BaselineHead       string              `json:"baseline_head"`
	Requirements       []BundleRequirement `json:"requirements"`
	ChangedFiles       []ChangedFile       `json:"changed_files"`
	ManifestSHA256     string              `json:"manifest_sha256"`
	ChangedSinceReview []string            `json:"changed_since_review"`
	PriorFindings      []BundleFinding     `json:"prior_findings"`
	Checks             []BundleCheck       `json:"checks"`
	Reach              []ReachRow          `json:"reach"`
	Instructions       []string            `json:"instructions"`
}

// BuildBundle generates the bundle for a review about to be assigned, with
// every reach facet empty. It changes nothing.
func BuildBundle(ctx context.Context, taskPath, reviewID string) (Bundle, error) {
	root, _, err := openTask(taskPath)
	if err != nil {
		return Bundle{}, err
	}
	defer root.Close()
	record, err := Load(taskPath)
	if err != nil {
		return Bundle{}, err
	}
	if !identifier.MatchString(reviewID) {
		return Bundle{}, errors.New("review ID must be an identifier")
	}
	if findAssignment(record, reviewID) != nil {
		return Bundle{}, fmt.Errorf("review ID %s is already assigned; use a new ID", reviewID)
	}
	return buildBundle(ctx, root, record, reviewID, nil)
}

// EncodeBundle returns the file form: indented JSON ending in a newline.
func EncodeBundle(bundle Bundle) ([]byte, error) {
	data, err := json.Marshal(bundle, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// buildBundle generates the bundle at the current source. When want is set,
// the source must still be that snapshot.
func buildBundle(ctx context.Context, root *os.Root, record Record, reviewID string, want *projectctx.Snapshot) (Bundle, error) {
	if record.BaselineManifest == nil {
		return Bundle{}, errors.New("this task has no baseline manifest (it was started by an older cw); reviewer bundles need a task started by this cw")
	}
	baseline, err := readBaselineManifest(root, *record.BaselineManifest)
	if err != nil {
		return Bundle{}, err
	}
	snapshot, files, err := projectctx.CaptureManifest(ctx, record.Baseline.SelectedPath)
	if err != nil {
		return Bundle{}, err
	}
	if want != nil && !equalSnapshot(*want, snapshot) {
		return Bundle{}, errors.New("source changed while checking the reviewer bundle")
	}
	changed := changedSince(baseline, files)
	manifestHash, err := manifestDigest(changed)
	if err != nil {
		return Bundle{}, err
	}
	bundle := Bundle{
		Version: bundleVersion, TaskID: record.ID, ReviewID: reviewID, Kind: "full", Snapshot: snapshot,
		BaselineHead: record.Baseline.Head, Requirements: []BundleRequirement{}, ChangedFiles: changed,
		ManifestSHA256: manifestHash, PriorFindings: priorFindings(record), Checks: bundleChecks(record, snapshot),
	}
	for _, requirement := range record.Plan.Requirements {
		bundle.Requirements = append(bundle.Requirements, BundleRequirement{ID: requirement.ID, Description: requirement.Description})
	}
	bundle.ChangedSinceReview = []string{}
	for _, file := range changed {
		bundle.ChangedSinceReview = append(bundle.ChangedSinceReview, file.Path)
	}
	if previous := previousReviewed(record); previous != nil {
		earlier, err := readStoredBundle(root, *previous)
		if err != nil {
			return Bundle{}, fmt.Errorf("previous review %s: %w", previous.Brief.ID, err)
		}
		bundle.Kind, bundle.PreviousReview = "follow_up", previous.Brief.ID
		bundle.ChangedSinceReview = changedBetween(earlier.ChangedFiles, changed)
	}
	current := map[string]bool{}
	for _, file := range files {
		current[file.Path] = file.Mode != "120000"
	}
	bundle.Reach = []ReachRow{}
	for _, path := range bundle.ChangedSinceReview {
		regular, present := current[path]
		row := ReachRow{Path: path, Deleted: !present, Hints: ReachHints{References: []string{}}}
		// The row names the file by its manifest spelling; open and search
		// for it by its on-disk name.
		raw := projectctx.ManifestRawPath(path)
		if regular {
			row.Hints.Shebang = shebang(snapshot.Root, raw)
		}
		if row.Hints.References, err = references(ctx, snapshot.Root, path, raw); err != nil {
			return Bundle{}, fmt.Errorf("reach hints for %s: %w", path, err)
		}
		bundle.Reach = append(bundle.Reach, row)
	}
	bundle.Instructions = bundleInstructions(bundle, record.Baseline.Head != "" && !record.Baseline.Dirty)
	return bundle, nil
}

func readBaselineManifest(root *os.Root, ref ManifestRef) ([]projectctx.FileState, error) {
	data, err := privateRead(root, ref.File, MaxInputBytes)
	if err != nil {
		return nil, fmt.Errorf("read baseline manifest: %w", err)
	}
	if digest(data) != ref.SHA256 {
		return nil, errors.New("baseline manifest hash differs")
	}
	var files []projectctx.FileState
	if err := decodeBytes(data, &files); err != nil || len(files) != ref.Files {
		return nil, errors.New("baseline manifest is invalid")
	}
	return files, nil
}

// changedSince lists every path whose mode or bytes differ between the two
// sorted manifests, or that exists in only one, sorted by path.
func changedSince(baseline, current []projectctx.FileState) []ChangedFile {
	before := map[string]projectctx.FileState{}
	for _, file := range baseline {
		before[file.Path] = file
	}
	changed := []ChangedFile{}
	seen := map[string]bool{}
	for _, file := range current {
		seen[file.Path] = true
		if old, ok := before[file.Path]; ok && old.Mode == file.Mode && old.SHA256 == file.SHA256 {
			continue
		}
		changed = append(changed, ChangedFile{Path: file.Path, Mode: file.Mode, SHA256: file.SHA256})
	}
	for _, file := range baseline {
		if !seen[file.Path] {
			changed = append(changed, ChangedFile{Path: file.Path, Deleted: true})
		}
	}
	slices.SortFunc(changed, func(a, b ChangedFile) int { return strings.Compare(a.Path, b.Path) })
	return changed
}

func manifestDigest(changed []ChangedFile) (string, error) {
	data, err := encode(changed)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

// changedBetween lists, sorted, the paths whose entries differ between two
// review manifests, or that appear in only one. A file edited back to its
// baseline bytes leaves the newer manifest and so is listed.
func changedBetween(earlier, now []ChangedFile) []string {
	entries := map[string]ChangedFile{}
	for _, file := range earlier {
		entries[file.Path] = file
	}
	paths := []string{}
	for _, file := range now {
		if old, ok := entries[file.Path]; !ok || old != file {
			paths = append(paths, file.Path)
		}
		delete(entries, file.Path)
	}
	for path := range entries {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

// previousReviewed returns the latest review whose reviewer completed its
// handoff and whose latest acceptance, if any, is not a rejection. A review
// closed as unused after a fix still reviewed that source.
func previousReviewed(record Record) *WorkerAssignment {
	for i := len(record.Assignments) - 1; i >= 0; i-- {
		assigned := record.Assignments[i]
		if assigned.Brief.Role != "review" {
			continue
		}
		result := findWorkerResult(record, assigned.Brief.ID)
		if result == nil || result.Result.Status != "completed" {
			continue
		}
		if accepted := latestAcceptance(record, assigned.Brief.ID); accepted != nil && accepted.Acceptance.Status == "rejected" {
			continue
		}
		return &record.Assignments[i]
	}
	return nil
}

func readStoredBundle(root *os.Root, assigned WorkerAssignment) (Bundle, error) {
	if assigned.BundleFile == "" || assigned.Brief.Bundle == nil {
		return Bundle{}, errors.New("review has no stored bundle")
	}
	data, err := privateRead(root, assigned.BundleFile, MaxInputBytes)
	if err != nil {
		return Bundle{}, err
	}
	if digest(data) != assigned.Brief.Bundle.SHA256 {
		return Bundle{}, errors.New("stored reviewer bundle hash differs")
	}
	var bundle Bundle
	if err := decodeBytes(data, &bundle); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

func reviewBundleArtifact(root *os.Root, assigned WorkerAssignment) error {
	_, err := readStoredBundle(root, assigned)
	return err
}

func priorFindings(record Record) []BundleFinding {
	findings := []BundleFinding{}
	for _, bound := range record.WorkerResults {
		id := bound.Result.AssignmentID
		for _, finding := range bound.Result.Findings {
			entry := BundleFinding{AssignmentID: id, FindingID: finding.ID, Summary: finding.Summary, Severity: finding.severity(), Evidence: finding.Evidence, Disposition: "pending"}
			if entry.Evidence == nil {
				entry.Evidence = []SourceRef{}
			}
		latest:
			for i := len(record.WorkerAcceptances) - 1; i >= 0; i-- {
				acceptance := record.WorkerAcceptances[i].Acceptance
				if acceptance.AssignmentID != id {
					continue
				}
				for _, disposition := range acceptance.Findings {
					if disposition.FindingID == finding.ID {
						entry.Disposition, entry.Reason = disposition.Status, disposition.Reason
						break latest
					}
				}
			}
			findings = append(findings, entry)
		}
	}
	return findings
}

func bundleChecks(record Record, snapshot projectctx.Snapshot) []BundleCheck {
	checks := []BundleCheck{}
	for _, check := range record.Plan.Checks {
		entry := BundleCheck{CheckID: check.ID, Status: "not_run"}
		if run := latestRun(record, check.ID); run != nil {
			entry.RunID, entry.Status, entry.Log = run.ID, run.Status, filepath.Join(record.Directory, run.Log)
			if run.ExitCode != nil {
				code := *run.ExitCode
				entry.ExitCode = &code
			}
			entry.Current = run.After != nil && equalSnapshot(run.Before, *run.After) && equalSnapshot(snapshot, *run.After)
		}
		checks = append(checks, entry)
	}
	return checks
}

// shebang returns the first line of a regular file when it names an
// interpreter; the interpreter is the first hint for the platforms facet.
func shebang(sourceRoot, path string) string {
	source, err := os.OpenRoot(sourceRoot)
	if err != nil {
		return ""
	}
	defer source.Close()
	file, err := source.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	head := make([]byte, 4096)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return ""
	}
	line, _, _ := bytes.Cut(head[:n], []byte{'\n'})
	text := strings.ToValidUTF8(strings.TrimSpace(string(line)), "\uFFFD")
	if !strings.HasPrefix(text, "#!") {
		return ""
	}
	return cutText(text)
}

// references lists up to maxReferences documentation and script lines that
// name the file, invocation-like lines first.
func references(ctx context.Context, root, path, raw string) ([]string, error) {
	base := filepath.Base(filepath.FromSlash(raw))
	hits, _, err := projectctx.GrepReferences(ctx, root, base)
	if err != nil {
		return nil, err
	}
	// Hit text is sanitized to valid UTF-8; match it with the name sanitized
	// the same way (a regexp needs valid UTF-8, and git greps the raw bytes).
	invocation := invocationPattern(strings.ToValidUTF8(base, "\uFFFD"))
	type ranked struct {
		hit   projectctx.GrepHit
		group int
	}
	var candidates []ranked
	for _, hit := range hits {
		if hit.Path == path {
			continue
		}
		group := 1
		if invocation.MatchString(hit.Text) {
			group = 0
		}
		candidates = append(candidates, ranked{hit, group})
	}
	slices.SortStableFunc(candidates, func(a, b ranked) int {
		if a.group != b.group {
			return a.group - b.group
		}
		if c := strings.Compare(a.hit.Path, b.hit.Path); c != 0 {
			return c
		}
		return a.hit.Line - b.hit.Line
	})
	out := []string{}
	for _, candidate := range candidates {
		if len(out) == maxReferences {
			break
		}
		out = append(out, candidate.hit.Path+":"+strconv.Itoa(candidate.hit.Line)+": "+cutText(strings.TrimSpace(candidate.hit.Text)))
	}
	return out, nil
}

// invocationPattern matches a line that runs the file: a pipe into a shell,
// curl or wget, ./name, or a shell given an argument ending in the name.
func invocationPattern(base string) *regexp.Regexp {
	name := regexp.QuoteMeta(base)
	return regexp.MustCompile(`\|\s*(?:sudo\s+)?(?:sh|bash|zsh|dash|ksh)\b` +
		`|\b(?:curl|wget)\b` +
		`|\./` + name +
		`|\b(?:sh|bash|zsh|dash|ksh)\s+\S*` + name + `(?:$|[\s"'` + "`" + `;)|&])`)
}

func cutText(text string) string {
	if len(text) <= maxReferenceText {
		return text
	}
	end := maxReferenceText
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}

func bundleInstructions(bundle Bundle, diffable bool) []string {
	instructions := []string{
		"This bundle orients the review and never narrows it: read any source and run any experiment you judge necessary, such as focused tests or platform and shell probes.",
		"Reach facets: callers = who calls, sources or imports the file and its entry points; invocation_modes = every documented way it is run (piped, sourced, executed, flags, CI); platforms = every operating system, shell and version it must work on (the shebang names the interpreter); consumers = what reads the state, files, output or formats it writes.",
		"For each reach row, run the changed file in a scratch copy in every invocation mode and on every platform and shell the row names (for example piped as curl | sh under macOS /bin/sh, which is bash 3.2), check its callers and consumers, and report a defect on any of them. Reading the code, running under another shell, or running an extracted fragment or a stand-in is not running the file that way. Record each mode in the result's reach_runs: status ran with the command and what you observed, or not_run with why it cannot run here, such as no Windows host.",
	}
	if diffable {
		instructions = append(instructions, "Full diff since the task started: git diff "+bundle.BaselineHead+" in "+bundle.Snapshot.Root+", plus the untracked files in changed_files; the checks list gives each run's log.")
	} else {
		instructions = append(instructions, "changed_files is the change set since the task started: the start had uncommitted edits or no Git commit, so git diff would mix in earlier edits or has no base; compare each listed file with its current source. The checks list gives each run's log.")
	}
	if bundle.Kind == "follow_up" {
		instructions = append(instructions, "Follow-up of "+bundle.PreviousReview+": confirm each prior finding's disposition against the current source, review the changes to the changed_since_review files on every path in their reach rows, report anything else you notice, and state what you did not re-review.")
	}
	return instructions
}

// validFacet accepts nonblank text, or "n/a: <reason>" in any letter case.
func validFacet(value string) bool {
	text := strings.TrimSpace(value)
	if text == "" || len(value) > 16384 {
		return false
	}
	if len(text) >= 3 && strings.EqualFold(text[:3], "n/a") {
		rest, ok := strings.CutPrefix(text[3:], ":")
		return ok && strings.TrimSpace(rest) != ""
	}
	return true
}

// storeReviewBundle validates the brief's filled bundle against a bundle
// regenerated now and stores an immutable copy in the task directory.
func storeReviewBundle(ctx context.Context, root *os.Root, record Record, brief WorkerBrief, snapshot projectctx.Snapshot) (string, error) {
	if brief.Bundle == nil {
		return "", errors.New("a review assignment needs its reviewer bundle: run cw task bundle, fill every reach facet, and give its path and sha256 as the brief's bundle")
	}
	if record.BaselineManifest == nil {
		return "", errors.New("this task has no baseline manifest (it was started by an older cw), so its reviews take no bundle")
	}
	resolved := brief.Bundle.Path
	if parent, err := filepath.EvalSymlinks(filepath.Dir(resolved)); err == nil {
		resolved = filepath.Join(parent, filepath.Base(resolved))
	}
	if within(record.Baseline.Root, resolved) {
		return "", errors.New("the reviewer bundle must be outside the project source")
	}
	data, err := readBundleInput(brief.Bundle.Path)
	if err != nil {
		return "", err
	}
	if digest(data) != brief.Bundle.SHA256 {
		return "", errors.New("reviewer bundle sha256 differs from the file's bytes")
	}
	var filled Bundle
	if err := decodeBytes(data, &filled); err != nil {
		return "", fmt.Errorf("reviewer bundle: %w", err)
	}
	if filled.ReviewID != brief.ID {
		return "", fmt.Errorf("reviewer bundle is for review %q, not %q", filled.ReviewID, brief.ID)
	}
	expected, err := buildBundle(ctx, root, record, brief.ID, &snapshot)
	if err != nil {
		return "", err
	}
	rows := map[string]ReachRow{}
	for _, row := range filled.Reach {
		rows[row.Path] = row
	}
	for i, row := range expected.Reach {
		got, ok := rows[row.Path]
		if !ok {
			return "", fmt.Errorf("changed file %s has no reach row in the bundle; rebuild it with cw task bundle", row.Path)
		}
		for _, facet := range []struct{ name, value string }{{"callers", got.Callers}, {"invocation_modes", got.InvocationModes}, {"platforms", got.Platforms}, {"consumers", got.Consumers}} {
			if !validFacet(facet.value) {
				return "", fmt.Errorf("reach row %s: facet %s must be filled or 'n/a: <reason>'", row.Path, facet.name)
			}
		}
		expected.Reach[i].Callers, expected.Reach[i].InvocationModes, expected.Reach[i].Platforms, expected.Reach[i].Consumers = got.Callers, got.InvocationModes, got.Platforms, got.Consumers
		delete(rows, row.Path)
	}
	if len(rows) > 0 || len(filled.Reach) != len(expected.Reach) {
		extra := slices.Sorted(maps.Keys(rows))
		if len(extra) == 0 {
			return "", errors.New("reviewer bundle repeats a reach row")
		}
		return "", fmt.Errorf("reach row %s names no file changed since the previous reviewed source", extra[0])
	}
	if !sameEncoded(expected, filled) {
		return "", errors.New("reviewer bundle differs from what cw task bundle generates now (a later record, check run or source edit, or an edit outside the reach facets); rebuild it and refill the facets")
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	name := "bundle-" + brief.ID + "-" + id + ".json"
	if err := writeNew(root, name, data); err != nil {
		return "", err
	}
	return name, nil
}

func readBundleInput(path string) ([]byte, error) { return readInputFile("reviewer bundle", path) }

// readInputFile reads a coordinator-supplied regular file, refusing a symlink
// or a file replaced while opening, up to MaxInputBytes.
func readInputFile(label, path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file, not a symlink", label)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%s changed while opening", label)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxInputBytes {
		return nil, fmt.Errorf("%s exceeds the 8 MiB limit", label)
	}
	return data, nil
}
