package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/fslock"
)

// Stage state belongs to a worktree's Git directory, independently of the
// evictable context cache and the shared check store.
type stageLedger struct {
	root    *os.Root
	gitRoot *os.Root
	info    os.FileInfo
	lock    *fslock.Lock
}

type stageRecord struct {
	Version  int                 `json:"version"`
	Worktree string              `json:"worktree"`
	Nonce    string              `json:"nonce"`
	Created  time.Time           `json:"created_at"`
	Checks   string              `json:"check_policy_key"`
	Sources  map[string]string   `json:"raw_sources"`
	Prepared preparedWorkerBrief `json:"prepared"`
	Prompt   string              `json:"native_prompt,omitempty"`
	Managed  *managedStage       `json:"configured_stage,omitempty"`
}

// ledgerWait is how long a command of a task waits for another kitsu command
// that holds the ledger. A longer holder is running an assertion.
const ledgerWait = 10 * time.Second

func openStageLedger(ctx context.Context, s *session) (*stageLedger, error) {
	return openStageLedgerWait(ctx, s, fslock.NoWait)
}

func openStageLedgerWait(ctx context.Context, s *session, wait time.Duration) (*stageLedger, error) {
	gitRoot, err := os.OpenRoot(s.repo.GitDir)
	if err != nil {
		return nil, err
	}
	lock, err := privateStageLock(ctx, gitRoot, s.repo.GitDir, "kitsu-stages.lock", wait)
	if err != nil {
		_ = gitRoot.Close()
		if errors.Is(err, fslock.ErrBusy) {
			return nil, failf("the task ledger of this checkout is busy: another kitsu command, most likely a running assertion, still holds it. Run this command again when it ends")
		}
		return nil, fmt.Errorf("stage ledger unsafe: %w", err)
	}
	root, info, err := stageLedgerRoot(gitRoot)
	if err != nil {
		_ = lock.Close()
		_ = gitRoot.Close()
		return nil, err
	}
	return &stageLedger{root: root, gitRoot: gitRoot, info: info, lock: lock}, nil
}

func (l *stageLedger) close() {
	if l.root == nil {
		return
	}
	_ = l.root.Close()
	l.root = nil
	if l.lock != nil {
		_ = l.lock.Close()
	}
	_ = l.gitRoot.Close()
}

func privateStageLock(ctx context.Context, gitRoot *os.Root, gitDir, name string, wait time.Duration) (*fslock.Lock, error) {
	f, err := gitRoot.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		err = f.Close()
	}
	if err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	check := func(f *os.File) error {
		info, err := gitRoot.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("unsafe stage ledger lock")
		}
		opened, err := f.Stat()
		if err != nil || !os.SameFile(info, opened) {
			return errors.New("stage ledger lock is outside the pinned Git directory")
		}
		return nil
	}
	// Create only through the pinned root. Absolute-path acquisition must never
	// create a lock in a directory substituted by a concurrent rename/symlink.
	return fslock.Acquire(ctx, filepath.Join(gitDir, name), fslock.Options{Wait: wait, NoCreate: true, Check: check})
}

func stageLedgerRoot(gitRoot *os.Root) (*os.Root, os.FileInfo, error) {
	const name = "kitsu-stages"
	if err := gitRoot.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, nil, err
	}
	info, err := gitRoot.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("stage ledger must be a real private directory")
	}
	root, err := gitRoot.OpenRoot(name)
	if err != nil {
		return nil, nil, err
	}
	opened, statErr := root.Stat(".")
	named, namedErr := gitRoot.Lstat(name)
	if statErr != nil || namedErr != nil || !os.SameFile(info, opened) || !os.SameFile(info, named) {
		_ = root.Close()
		return nil, nil, errors.New("stage ledger directory was replaced")
	}
	if err := root.Chmod(".", 0o700); err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	return root, info, nil
}

func (l *stageLedger) verify() error {
	if l.lock != nil {
		if err := l.lock.Verify(); err != nil {
			return err
		}
	}
	info, err := l.gitRoot.Lstat("kitsu-stages")
	if err != nil || !info.IsDir() || !os.SameFile(l.info, info) {
		return errors.New("stage ledger directory was replaced")
	}
	return nil
}

func stageValidID(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (l *stageLedger) read(name string, value any) error {
	if err := l.verify(); err != nil {
		return err
	}
	info, err := l.root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > contextpack.MaxArtifact {
		return errors.New("stage record is unsafe or oversized")
	}
	f, err := l.root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("stage record was replaced while opening")
	}
	decoder := json.NewDecoder(io.LimitReader(f, contextpack.MaxArtifact+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("stage record contains trailing data")
	}
	return nil
}

func (l *stageLedger) write(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return l.writeBytes(name, data)
}

func (l *stageLedger) writeBytes(name string, data []byte) error {
	if err := l.verify(); err != nil {
		return err
	}
	if len(data) > contextpack.MaxArtifact {
		return errors.New("stage record exceeds its private size limit")
	}
	tmp := ".stage-" + rand.Text()
	f, err := l.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = l.root.Remove(tmp) }()
	_, err = io.Copy(f, bytes.NewReader(data))
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := l.verify(); err != nil {
		return err
	}
	return l.root.Rename(tmp, name)
}

func (l *stageLedger) active() (string, error) {
	var id string
	err := l.read("active.json", &id)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !stageValidID(id) {
		return "", errors.New("invalid active stage record")
	}
	return id, nil
}

func (l *stageLedger) record(id, worktree string) (stageRecord, error) {
	var record stageRecord
	if !stageValidID(id) {
		return record, errors.New("invalid dispatch id")
	}
	if err := l.read(id+".json", &record); err != nil {
		return record, err
	}
	if record.Version != 1 || contextpack.Key(record) != id || record.Worktree != worktree {
		return record, errors.New("stage record was tampered with or belongs to another worktree")
	}
	if record.Created.After(time.Now().Add(time.Minute)) {
		return record, errors.New("stage record is dated in the future")
	}
	return record, nil
}
