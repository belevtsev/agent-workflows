package taskflow

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// The publication record of cw publish: an append-only, hash-chained log of
// publication-NNNNNN.json files in the task directory, written beside the
// task revisions and never mixed with them. Load ignores these files, so a
// task with publications still loads, verifies and abandons as before.

const (
	publicationVersion  = 1
	maxPublications     = 4096
	maxPublicationBytes = 1 << 20
	publishLockName     = ".publish-lock"
)

var publicationName = regexp.MustCompile(`^publication-([0-9]{6})\.json$`)

// ErrPublicationLocked reports that another cw publish holds the task's
// .publish-lock directory, or that an interrupted one left it behind.
var ErrPublicationLocked = errors.New("another cw publish holds this task's .publish-lock; inspect an interrupted run before removing it")

// ErrSourceCapture wraps a failure to capture the current source manifest.
var ErrSourceCapture = errors.New("capture the current source")

// PublicationSteps is the outcome of each publication step. The Report of
// cw publish and the finished publication entry share it.
type PublicationSteps struct {
	Commit CommitStep `json:"commit"`
	Push   PushStep   `json:"push"`
	PR     PRStep     `json:"pr"`
}

// CommitStep is the commit step's outcome. Status is one of done,
// already-present, not-needed, skipped, refused, failed, unknown, mismatch.
type CommitStep struct {
	Status string `json:"status"`
	Commit string `json:"commit"`
	Detail string `json:"detail"`
}

// PushStep is the push step's outcome; RemoteRef is the last observed remote
// branch: a commit, "absent", or "" when unread.
type PushStep struct {
	Status    string `json:"status"`
	RemoteRef string `json:"remote_ref"`
	Detail    string `json:"detail"`
}

// PRStep is the pull-request step's outcome.
type PRStep struct {
	Status     string `json:"status"`
	Number     int    `json:"number"`
	URL        string `json:"url"`
	HeadRefOID string `json:"head_ref_oid"`
	Base       string `json:"base"`
	State      string `json:"state"`
	Detail     string `json:"detail"`
}

// PublicationEntry is one publication-NNNNNN.json file. A run writes a
// started entry before its first state-changing command and a finished entry
// with the same run_id when it exits.
type PublicationEntry struct {
	Version        int                 `json:"version"`
	TaskID         string              `json:"task_id"`
	Sequence       int                 `json:"sequence"`
	PreviousSHA256 string              `json:"previous_sha256"`
	RunID          string              `json:"run_id"`
	Kind           string              `json:"kind"`
	At             time.Time           `json:"at"`
	Request        *PublicationRequest `json:"request,omitempty"`
	Status         string              `json:"status,omitempty"`
	Reason         string              `json:"reason,omitempty"`
	ExitCode       *int                `json:"exit_code,omitempty"`
	Steps          *PublicationSteps   `json:"steps,omitempty"`
}

// PublicationRequest is what a started entry records about the run's request.
type PublicationRequest struct {
	SourceSHA256   string        `json:"source_sha256"`
	Paths          []string      `json:"paths"`
	CommittedPaths []string      `json:"committed_paths"`
	MessageSHA256  string        `json:"message_sha256"`
	ExpectHead     string        `json:"expect_head"`
	Branch         string        `json:"branch"`
	Remote         string        `json:"remote"`
	ExpectRemote   string        `json:"expect_remote"`
	PR             PublicationPR `json:"pr"`
}

// PublicationPR is the pull-request part of a PublicationRequest.
type PublicationPR struct {
	Mode       string `json:"mode"`
	Repo       string `json:"repo"`
	Base       string `json:"base"`
	Title      string `json:"title"`
	BodySHA256 string `json:"body_sha256"`
}

// PublicationLog summarises a validated publication log.
type PublicationLog struct {
	// Entries is the number of publication files.
	Entries int
	// Last is the newest entry, nil when there is none.
	Last *PublicationEntry
}

// PreviousOutcome is none without entries, unfinished when the newest entry
// is a started entry, else the status of the newest finished entry.
func (l PublicationLog) PreviousOutcome() string {
	switch {
	case l.Last == nil:
		return "none"
	case l.Last.Kind == "started":
		return "unfinished"
	default:
		return l.Last.Status
	}
}

// PublicationSource is the task's source as cw publish sees it now.
type PublicationSource struct {
	// Files is the current manifest, sorted by path.
	Files []projectctx.FileState
	// Digest is SourceDigest of Files.
	Digest string
	// Changed is the change set since the baseline manifest, sorted by path.
	Changed []ChangedFile
}

// PublicationTask is an opened task directory for one cw publish run. It
// holds the directory open through os.Root, so every read and write stays
// inside it.
type PublicationTask struct {
	root      *os.Root
	directory string
	locked    bool
	read      bool
	next      int
	previous  string
}

// OpenPublicationTask opens a private task directory. It takes no lock.
func OpenPublicationTask(path string) (*PublicationTask, error) {
	root, directory, err := openTask(path)
	if err != nil {
		if root != nil {
			root.Close()
		}
		return nil, err
	}
	return &PublicationTask{root: root, directory: directory}, nil
}

// Directory is the canonical task directory.
func (t *PublicationTask) Directory() string { return t.directory }

// Lock creates .publish-lock. An existing one is ErrPublicationLocked and
// stays in place.
func (t *PublicationTask) Lock() error {
	if err := t.root.Mkdir(publishLockName, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrPublicationLocked
		}
		return err
	}
	t.locked = true
	return nil
}

// Close removes the lock this task took and closes the directory.
func (t *PublicationTask) Close() error {
	var err error
	if t.locked {
		err = t.root.Remove(publishLockName)
		t.locked = false
	}
	return errors.Join(err, t.root.Close())
}

// Load reads and checks the task's revision chain, as Load does.
func (t *PublicationTask) Load() (Record, error) {
	record, _, err := loadRoot(t.root, t.directory)
	return record, err
}

// ReadPublications validates the publication log of task taskID: names from
// publication-000001.json without gaps, at most 4096 private regular files,
// each a version 1 entry of this task whose sequence equals its number and
// whose previous_sha256 is the SHA-256 of the previous file, with started
// and finished entries shaped as their kind requires and every finished
// entry directly after the started entry of its run.
func (t *PublicationTask) ReadPublications(taskID string) (PublicationLog, error) {
	directory, err := t.root.Open(".")
	if err != nil {
		return PublicationLog{}, err
	}
	entries, err := directory.ReadDir(-1)
	directory.Close()
	if err != nil {
		return PublicationLog{}, err
	}
	var names []string
	for _, entry := range entries {
		if publicationName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	if len(names) > maxPublications {
		return PublicationLog{}, errors.New("publication log exceeds 4096 entries")
	}
	slices.Sort(names)
	previous := ""
	var last *PublicationEntry
	for i, name := range names {
		number, _ := strconv.Atoi(publicationName.FindStringSubmatch(name)[1])
		if number != i+1 {
			return PublicationLog{}, fmt.Errorf("publication log has a gap before %s", name)
		}
		data, err := privateRead(t.root, name, maxPublicationBytes)
		if err != nil {
			return PublicationLog{}, fmt.Errorf("%s: %w", name, err)
		}
		var stored struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(data, &stored); err != nil {
			return PublicationLog{}, fmt.Errorf("%s: invalid JSON: %w", name, err)
		}
		if stored.Version != publicationVersion {
			return PublicationLog{}, fmt.Errorf("%s: publication entry version %d is not supported", name, stored.Version)
		}
		var entry PublicationEntry
		if err := decodeBytes(data, &entry); err != nil {
			return PublicationLog{}, fmt.Errorf("%s: %w", name, err)
		}
		if entry.TaskID != taskID || entry.Sequence != number || entry.PreviousSHA256 != previous {
			return PublicationLog{}, fmt.Errorf("%s: publication entry belongs to another task or breaks the chain", name)
		}
		if err := validatePublicationEntry(entry, last); err != nil {
			return PublicationLog{}, fmt.Errorf("%s: %w", name, err)
		}
		last = &entry
		previous = digest(data)
	}
	t.read, t.next, t.previous = true, len(names)+1, previous
	return PublicationLog{Entries: len(names), Last: last}, nil
}

func validatePublicationEntry(entry PublicationEntry, previous *PublicationEntry) error {
	if len(entry.RunID) != 32 || !lowerHex(entry.RunID) || entry.At.IsZero() {
		return errors.New("publication entry needs a run_id and a time")
	}
	switch entry.Kind {
	case "started":
		if entry.Request == nil || entry.Status != "" || entry.Reason != "" || entry.ExitCode != nil || entry.Steps != nil {
			return errors.New("a started publication entry carries only its request")
		}
	case "finished":
		if entry.Request != nil || entry.Status == "" || entry.ExitCode == nil || entry.Steps == nil {
			return errors.New("a finished publication entry needs a status, an exit code and steps")
		}
		if previous == nil || previous.Kind != "started" || previous.RunID != entry.RunID {
			return errors.New("a finished publication entry must follow the started entry of its run")
		}
	default:
		return errors.New("publication entry kind must be started or finished")
	}
	return nil
}

func lowerHex(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// Append writes entry as the next publication file, filling its version,
// sequence and previous_sha256, and returns the file's absolute path. A
// started entry needs room for its finished entry too. ReadPublications must
// have run first.
func (t *PublicationTask) Append(entry PublicationEntry) (string, error) {
	if !t.read {
		return "", errors.New("the publication log was not read")
	}
	room := 1
	if entry.Kind == "started" {
		room = 2
	}
	if t.next-1+room > maxPublications {
		return "", errors.New("publication log limit of 4096 entries reached; start a new task")
	}
	entry.Version, entry.Sequence, entry.PreviousSHA256 = publicationVersion, t.next, t.previous
	data, err := encode(entry)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("publication-%06d.json", t.next)
	if err := writeNew(t.root, name, data); err != nil {
		return "", err
	}
	t.next++
	t.previous = digest(data)
	return filepath.Join(t.directory, name), nil
}

// Source reads the baseline manifest of record and captures the current
// manifest at its selected path. A capture failure wraps ErrSourceCapture;
// any other error concerns the baseline manifest.
func (t *PublicationTask) Source(ctx context.Context, record Record) (PublicationSource, error) {
	if record.BaselineManifest == nil {
		return PublicationSource{}, errors.New("task record has no baseline manifest")
	}
	baseline, err := readBaselineManifest(t.root, *record.BaselineManifest)
	if err != nil {
		return PublicationSource{}, err
	}
	_, files, err := projectctx.CaptureManifest(ctx, record.Baseline.SelectedPath)
	if err != nil {
		return PublicationSource{}, fmt.Errorf("%w: %w", ErrSourceCapture, err)
	}
	manifest, err := encode(files)
	if err != nil {
		return PublicationSource{}, fmt.Errorf("%w: %w", ErrSourceCapture, err)
	}
	return PublicationSource{Files: files, Digest: digest(manifest), Changed: changedSince(baseline, files)}, nil
}
