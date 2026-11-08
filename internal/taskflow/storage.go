package taskflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
)

const maxTaskRevisions = 4096

var revisionName = regexp.MustCompile(`^record-([0-9]{6})\.json$`)
var errTaskLocked = errors.New("task is locked; inspect an interrupted writer before removing its .lock directory")
var errTaskRevisionLimit = errors.New("task revision limit reached; start a new task")

func randomID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func within(base, path string) bool {
	relative, err := filepath.Rel(base, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

// privateStore creates only missing components and never traverses symlinks
// supplied inside the chosen storage path. Existing platform aliases (such as
// macOS /var) are canonicalized before any task files are created.
func privateStore(path, repository string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	missing := []string{}
	ancestor := filepath.Clean(abs)
	for {
		info, statErr := os.Lstat(ancestor)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("task storage must not be a symlink")
			}
			if !info.IsDir() {
				return "", errors.New("task storage ancestor is not a directory")
			}
			break
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		missing = append(missing, filepath.Base(ancestor))
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", statErr
		}
		ancestor = parent
	}
	canonical, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	canonicalAncestor := canonical
	for i := len(missing) - 1; i >= 0; i-- {
		canonical = filepath.Join(canonical, missing[i])
	}
	if within(repository, canonical) {
		return "", errors.New("task records must be stored outside the target repository")
	}
	for _, segment := range strings.Split(filepath.ToSlash(canonical), "/") {
		if segment == "releases" || segment == ".workflows-releases" {
			return "", errors.New("task records must be outside managed releases")
		}
	}
	if len(missing) > 0 {
		if err := createStoreDirectories(canonicalAncestor, missing); err != nil {
			return "", err
		}
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("task storage must be a private directory (0700)")
	}
	return canonical, nil
}

// createStoreDirectories binds every new component to its opened parent. Each
// new directory and its parent are synced before descending, including the
// previously existing ancestor. Existing directories are never chmodded.
func createStoreDirectories(ancestor string, missing []string) (err error) {
	currentPath := ancestor
	defer func() {
		if err != nil {
			err = fmt.Errorf("task storage creation at %s may be incomplete or not durable; inspect and reload this path before retrying: %w", currentPath, err)
		}
	}()
	current, err := os.OpenRoot(ancestor)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, current.Close()) }()
	for i := len(missing) - 1; i >= 0; i-- {
		name := missing[i]
		currentPath = filepath.Join(currentPath, name)
		if mkdirErr := current.Mkdir(name, 0700); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return mkdirErr
		}
		info, err := current.Lstat(name)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return errors.New("new task storage component must be a private directory and must not be a symlink")
		}
		child, err := current.OpenRoot(name)
		if err != nil {
			return err
		}
		opened, err := child.Stat(".")
		if err != nil {
			return errors.Join(err, child.Close())
		}
		if !os.SameFile(info, opened) {
			return errors.Join(errors.New("task storage component changed while opening"), child.Close())
		}
		if err := errors.Join(syncTaskDirectory(child), syncTaskDirectory(current)); err != nil {
			return errors.Join(err, child.Close())
		}
		parent := current
		current = child
		if err := parent.Close(); err != nil {
			return err
		}
	}
	return nil
}

func openTask(path string) (*os.Root, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, "", errors.New("task directory must be private and must not be a symlink")
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(canonical)
	return root, canonical, err
}

func privateRead(root *os.Root, name string, limit int64) ([]byte, error) {
	if filepath.Base(name) != name || name == "." {
		return nil, errors.New("invalid task artifact name")
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("task artifact must be a private regular file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("task artifact changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("task artifact exceeds its size limit")
	}
	return data, nil
}

// writeNew publishes a fully written file with a no-replace hard link. A crash
// before publication leaves only an ignored .write-* file; old revisions stay
// intact. A successful publication also syncs its descriptor-bound directory.
// os.Root prevents path and symlink escapes during every operation.
func writeNew(root *os.Root, name string, data []byte) error {
	return writeNewWithSync(root, name, data, syncTaskDirectory)
}

func syncTaskDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func writeNewWithSync(root *os.Root, name string, data []byte, syncDir func(*os.Root) error) error {
	path, err := filepath.Abs(filepath.Join(root.Name(), name))
	if err != nil {
		return err
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	temporary := ".write-" + id
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := root.Link(temporary, name); err != nil {
		return fmt.Errorf("publish new task artifact: %w", err)
	}
	if err := syncDir(root); err != nil {
		return fmt.Errorf("task artifact %s was published but durability is uncertain; reload the task before retrying: %w", path, err)
	}
	return nil
}

func loadRoot(root *os.Root, directory string) (Record, string, error) {
	file, err := root.Open(".")
	if err != nil {
		return Record{}, "", err
	}
	entries, err := file.ReadDir(-1)
	file.Close()
	if err != nil {
		return Record{}, "", err
	}
	var names []string
	for _, entry := range entries {
		if revisionName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 || len(names) > maxTaskRevisions {
		return Record{}, "", errors.New("missing task revisions or revision limit exceeded")
	}
	slices.Sort(names)
	previous := ""
	var record Record
	for i, name := range names {
		match := revisionName.FindStringSubmatch(name)
		number, _ := strconv.Atoi(match[1])
		if number != i+1 {
			return Record{}, "", errors.New("task revision sequence has a gap")
		}
		data, err := privateRead(root, name, MaxInputBytes)
		if err != nil {
			return Record{}, "", err
		}
		// Read the version alone first, so a record in another format is
		// refused by its version rather than by its unknown or missing fields.
		var stored struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(data, &stored); err != nil {
			return Record{}, "", fmt.Errorf("invalid task JSON: %w", err)
		}
		if stored.Version != Version {
			return Record{}, "", fmt.Errorf("task record version %d is not supported; this cw reads version %d records; start a new task with 'cw task start'", stored.Version, Version)
		}
		var current Record
		if err := decodeBytes(data, &current); err != nil {
			return Record{}, "", err
		}
		if current.Revision != number || current.PreviousSHA256 != previous || current.Directory != directory || current.ID != filepath.Base(directory) || current.Baseline.Fingerprint == "" {
			return Record{}, "", errors.New("invalid task record identity or revision chain")
		}
		if i > 0 && (current.ID != record.ID || current.CreatedAt != record.CreatedAt || current.Baseline != record.Baseline || !sameEncoded(current.BaselineManifest, record.BaselineManifest)) {
			return Record{}, "", errors.New("immutable task baseline changed")
		}
		if i > 0 && !maps.Equal(current.Advisers, record.Advisers) {
			return Record{}, "", errors.New("immutable task adviser binding changed")
		}
		if i > 0 && record.Abandonment != nil {
			return Record{}, "", errors.New("a task revision follows the abandoned revision")
		}
		if err := validateRecord(current); err != nil {
			return Record{}, "", err
		}
		if i > 0 {
			if err := validateWorkerTransition(record, current); err != nil {
				return Record{}, "", err
			}
			a, _ := encode(current.Plan)
			b, _ := encode(record.Plan)
			if string(a) != string(b) {
				return Record{}, "", errors.New("immutable task plan changed")
			}
		}
		record = current
		previous = digest(data)
	}
	if within(record.Baseline.Root, directory) {
		return Record{}, "", errors.New("task storage is inside the target repository")
	}
	return record, previous, nil
}

func validateRecord(record Record) error {
	if err := validateRecordedPlan(record.Plan); err != nil {
		return err
	}
	if err := validateWorkerRecord(record); err != nil {
		return err
	}
	if err := validateAbandonment(record); err != nil {
		return err
	}
	if len(record.ID) != 32 || record.EvidenceRevision < 0 || record.CreatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt) || record.Advisers == nil || record.Contexts == nil || record.Assessments == nil || record.Consultations == nil || record.Checks == nil || !filepath.IsAbs(record.Baseline.Root) || !filepath.IsAbs(record.Baseline.SelectedPath) {
		return errors.New("task record is missing required identity or collection fields")
	}
	if _, err := hex.DecodeString(record.ID); err != nil {
		return errors.New("invalid task ID")
	}
	if manifest := record.BaselineManifest; manifest != nil && (manifest.File != baselineManifestFile || !validSHA256(manifest.SHA256) || manifest.Files < 0) {
		return errors.New("invalid stored baseline manifest reference")
	}
	if err := consult.ValidateBinding(record.Advisers); err != nil {
		return err
	}
	baseline, ok := record.Contexts["before"]
	if !ok || baseline.TaskID != record.ID || baseline.Phase != "before" || baseline.EvidenceSHA256 != "" || !equalSnapshot(baseline.Snapshot, record.Baseline) {
		return errors.New("immutable before context is missing or changed")
	}
	for phase, context := range record.Contexts {
		if !phaseValid(phase) || context.Phase != phase || context.TaskID != record.ID || context.Snapshot.Fingerprint == "" || phase == "final" && !validSHA256(context.EvidenceSHA256) || !maps.Equal(context.Advisers, record.Advisers) {
			return errors.New("invalid stored task context")
		}
	}
	checks := map[string]Check{}
	for _, check := range record.Plan.Checks {
		checks[check.ID] = check
	}
	runs := map[string]bool{}
	for _, run := range record.Checks {
		planned, ok := checks[run.CheckID]
		if !ok || runs[run.ID] || len(run.ID) != 32 || !slices.Equal(planned.Argv, run.Argv) || run.StartedAt.IsZero() || !slices.Contains([]string{"running", "passed", "failed", "cancelled", "timed_out", "source_changed"}, run.Status) {
			return errors.New("invalid stored planned check run")
		}
		runs[run.ID] = true
		if run.Status != "running" && run.FinishedAt == nil {
			return errors.New("terminal check is missing its completion time")
		}
		if (run.TestResults != "" || run.TestResultsSHA256 != "") && (run.TestResults != testResultsName(run.ID) || !validSHA256(run.TestResultsSHA256) || run.Status == "running") {
			return errors.New("invalid stored check run test results binding")
		}
	}
	requirements := map[string]bool{}
	for _, requirement := range record.Plan.Requirements {
		requirements[requirement.ID] = true
	}
	for id, bound := range record.Assessments {
		if !requirements[id] || bound.Assessment.ID != id || !slices.Contains([]string{"pending", "satisfied"}, bound.Assessment.Status) {
			return errors.New("invalid stored requirement assessment")
		}
	}
	for phase, attachment := range record.Consultations {
		if !phaseValid(phase) || attachment.Phase != phase || attachment.Context.TaskID != record.ID || attachment.Context.Phase != phase || !maps.Equal(attachment.Context.Advisers, record.Advisers) || !sameKeys(attachment.ProviderStatuses, slices.Collect(maps.Keys(record.Advisers))) {
			return errors.New("invalid stored consultation attachment")
		}
	}
	if record.Decision != nil && !slices.Contains([]string{"resolved", "unresolved"}, record.Decision.Decision.Status) {
		return errors.New("invalid stored Coordinator decision")
	}
	if record.Verification != nil && record.Verification.TaskID != record.ID {
		return errors.New("verification belongs to a different task")
	}
	return nil
}

// Load reads and checks the complete immutable revision chain.
func Load(path string) (Record, error) {
	root, directory, err := openTask(path)
	if err != nil {
		return Record{}, err
	}
	defer root.Close()
	record, _, err := loadRoot(root, directory)
	return record, err
}

func update(path string, change func(*os.Root, *Record) error) (Record, error) {
	return updateWithCapacity(path, 1, "", change)
}

// Every running check owns one future terminal revision. Count reservations
// from the stored state under the writer lock, including historical records;
// only the terminal update for that exact run may spend its reserved slot.
func requireRevisionCapacity(record Record, additional int, completingRunID string) error {
	reserved := 0
	found := completingRunID == ""
	for _, run := range record.Checks {
		if run.Status != "running" {
			continue
		}
		if run.ID == completingRunID {
			found = true
		} else {
			reserved++
		}
	}
	if !found {
		return errors.New("starting check record is missing or already terminal")
	}
	if record.Revision+additional+reserved > maxTaskRevisions {
		return errTaskRevisionLimit
	}
	return nil
}

func updateWithCapacity(path string, additional int, completingRunID string, change func(*os.Root, *Record) error) (Record, error) {
	return updateDiscarding(path, additional, completingRunID, change, nil)
}

// updateDiscarding is updateWithCapacity with one addition: when change
// succeeded and the update then fails (revision capacity, validation,
// encoding, the size limit or the record write), it calls discard, still
// under the task lock, so change can remove the artefacts it stored. discard
// is not called once the revision name exists or its absence cannot be
// confirmed after a failed write, even when this run published nothing (the
// name already existed). A nil discard is updateWithCapacity.
func updateDiscarding(path string, additional int, completingRunID string, change func(*os.Root, *Record) error, discard func(*os.Root)) (_ Record, err error) {
	root, directory, err := openTask(path)
	if err != nil {
		return Record{}, err
	}
	defer root.Close()
	if err := root.Mkdir(".lock", 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Record{}, errTaskLocked
		}
		return Record{}, err
	}
	defer root.Remove(".lock")
	record, previous, err := loadRoot(root, directory)
	if err != nil {
		return Record{}, err
	}
	if record.Abandonment != nil {
		return Record{}, errAbandoned
	}
	if err := requireRevisionCapacity(record, additional, completingRunID); err != nil {
		return Record{}, err
	}
	beforeBytes, err := encode(record)
	if err != nil {
		return Record{}, err
	}
	if err := change(root, &record); err != nil {
		return Record{}, err
	}
	published := false
	defer func() {
		if err != nil && !published && discard != nil {
			discard(root)
		}
	}()
	record.Revision++
	record.PreviousSHA256 = previous
	record.UpdatedAt = time.Now().UTC()
	if err := requireRevisionCapacity(record, 0, ""); err != nil {
		return Record{}, err
	}
	if err := validateRecord(record); err != nil {
		return Record{}, err
	}
	var before Record
	if err := decodeBytes(beforeBytes, &before); err != nil {
		return Record{}, err
	}
	if err := validateWorkerTransition(before, record); err != nil {
		return Record{}, err
	}
	data, err := encode(record)
	if err != nil {
		return Record{}, err
	}
	if len(data) > MaxInputBytes {
		return Record{}, errors.New("task revision exceeds size limit")
	}
	name := fmt.Sprintf("record-%06d.json", record.Revision)
	if err := writeNew(root, name, data); err != nil {
		// Only a revision file that is certainly absent was not published.
		_, statErr := root.Lstat(name)
		published = !errors.Is(statErr, fs.ErrNotExist)
		return Record{}, err
	}
	return record, nil
}

// Terminal results may race a short assessment/context writer. Preserve the
// running revision while waiting for that writer; never overwrite or break a
// lock, and never manufacture success if an abandoned lock outlives the bound.
func updateWhenUnlocked(ctx context.Context, path, completingRunID string, change func(*os.Root, *Record) error) (Record, error) {
	for {
		record, err := updateWithCapacity(path, 1, completingRunID, change)
		if !errors.Is(err, errTaskLocked) {
			return record, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Record{}, fmt.Errorf("terminal check record could not acquire task lock: %w", ctx.Err())
		case <-timer.C:
		}
	}
}
