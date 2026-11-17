package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

const maxWorkerAssignment = 1 << 20

// workerDispatch is a reference for a native host, never authority to spawn it.
type workerDispatch struct {
	Version   int         `json:"version"`
	Label     string      `json:"label"`
	Task      string      `json:"task"`
	Workflow  string      `json:"workflow"`
	Role      string      `json:"role"`
	Stage     string      `json:"stage"`
	Round     int         `json:"round"`
	Host      string      `json:"host"`
	Path      string      `json:"path"`
	SHA256    string      `json:"sha256"`
	Model     roles.Model `json:"model"`
	Baseline  string      `json:"baseline"`
	Selection string      `json:"selection_id"`
}

type preparedWorkerBrief struct {
	Dispatch workerDispatch    `json:"dispatch"`
	Brief    workerBrief       `json:"brief"`
	State    contextpack.State `json:"state"`
}

func readWorkerAssignment(input io.Reader, path string) (string, error) {
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return "", failf("read worker assignment: %v", err)
		}
		defer func() { _ = f.Close() }()
		input = f
	}
	if input == nil {
		return "", usagef("worker assignment input is missing")
	}
	data, err := io.ReadAll(io.LimitReader(input, maxWorkerAssignment+1))
	if err != nil {
		return "", failf("read worker assignment: %v", err)
	}
	if err := validateWorkerAssignment(string(data)); err != nil {
		return "", err
	}
	return string(data), nil
}

func validateWorkerAssignment(assignment string) error {
	if len(assignment) > maxWorkerAssignment || strings.TrimSpace(assignment) == "" || !utf8.ValidString(assignment) || strings.ContainsRune(assignment, 0) {
		return usagef("worker assignment must contain nonempty UTF-8 text without NUL, at most %d bytes", maxWorkerAssignment)
	}
	return nil
}

// prepareWorkerBrief preserves the original assignment and role, without model calls.
func prepareWorkerBrief(ctx context.Context, cfg Config, b workerBrief, assignment string) (preparedWorkerBrief, error) {
	var p preparedWorkerBrief
	if !validBriefArgs(parsed{pos: []string{b.Task}}, b) {
		return p, usagef("invalid worker brief task, role, stage, round, host or workflow")
	}
	if err := validateWorkerAssignment(assignment); err != nil {
		return p, err
	}
	b = workerBrief{Task: b.Task, Workflow: b.Workflow, Role: b.Role, Stage: b.Stage, Round: b.Round, Host: b.Host}
	b, state, s, e, err := prepareBriefInputs(ctx, cfg, b, 0)
	if err != nil {
		return p, err
	}
	canonical, err := canonicalWorkerBrief(b, assignment)
	if err != nil {
		return p, err
	}
	root, dir, err := workerBriefRoot(s.repo.GitDir)
	if err != nil {
		return p, compactFailure(err)
	}
	defer func() { _ = root.Close() }()
	hash := workerBriefHash(canonical)
	name := hash + ".txt"
	if err := saveWorkerBrief(root, name, canonical); err != nil {
		return p, compactFailure(err)
	}
	p = preparedWorkerBrief{Brief: b, State: state, Dispatch: workerBriefDispatch(b, filepath.Join(dir, name), hash)}
	if err := verifyWorkerBriefPolicy(s, p); err != nil {
		return preparedWorkerBrief{}, compactFailure(err)
	}
	if current, err := e.Snapshot(ctx); err != nil || current != state {
		return preparedWorkerBrief{}, failf("repository changed while saving worker brief: %v", err)
	}
	return p, nil
}

func workerBriefDispatch(b workerBrief, path, hash string) workerDispatch {
	return workerDispatch{
		Version: 1, Label: b.Label, Task: b.Task, Workflow: b.Workflow, Role: b.Role,
		Stage: b.Stage, Round: b.Round, Host: b.Host, Path: path,
		SHA256: hash, Model: b.Model, Baseline: b.Baseline, Selection: b.Selection,
	}
}

func canonicalWorkerBrief(b workerBrief, assignment string) ([]byte, error) {
	instructions := b.Instructions
	b.Instructions = ""
	metadata, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "%s\n\nWorker brief metadata:\n%s\n", b.Label, metadata)
	if instructions != "" {
		out.WriteString("\nOriginal role instructions:\n")
		out.WriteString(instructions)
		out.WriteByte('\n')
	}
	out.WriteString("\nOriginal assignment:\n")
	out.WriteString(assignment)
	return out.Bytes(), nil
}

func workerBriefHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func workerBriefRoot(gitDir string) (*os.Root, string, error) {
	root, err := os.OpenRoot(gitDir)
	if err != nil {
		return nil, "", err
	}
	for _, dir := range []string{"kitsu-context", "kitsu-context/briefs"} {
		if err := root.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			_ = root.Close()
			return nil, "", err
		}
		info, err := root.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			_ = root.Close()
			return nil, "", errors.New("worker brief store must be a real private directory")
		}
	}
	briefs, err := root.OpenRoot("kitsu-context/briefs")
	_ = root.Close()
	if err != nil {
		return nil, "", err
	}
	if err := briefs.Chmod(".", 0o700); err != nil {
		_ = briefs.Close()
		return nil, "", err
	}
	return briefs, filepath.Join(gitDir, "kitsu-context", "briefs"), nil
}

func saveWorkerBrief(root *os.Root, name string, data []byte) error {
	if _, err := root.Lstat(name); err == nil {
		return matchingWorkerBrief(root, name, data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := stageWorkerBrief(root, bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(tmp) }()
	if err := root.Link(tmp, name); errors.Is(err, os.ErrExist) {
		return matchingWorkerBrief(root, name, data)
	} else if err != nil {
		return err
	}
	return nil
}

func matchingWorkerBrief(root *os.Root, name string, data []byte) error {
	retained, err := readSavedWorkerBrief(root, name)
	if err != nil || !bytes.Equal(retained, data) {
		return errors.New("immutable worker brief exists with unsafe or changed contents")
	}
	return nil
}

// stageWorkerBrief never exposes a partial write under a canonical hash name.
func stageWorkerBrief(root *os.Root, input io.Reader) (string, error) {
	tmp := ".brief-" + rand.Text()
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, input)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = root.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

func readSavedWorkerBrief(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > contextpack.MaxArtifact || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("worker brief must be a bounded regular file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("worker brief changed while opening")
	}
	return io.ReadAll(io.LimitReader(f, contextpack.MaxArtifact+1))
}

// verifyWorkerBriefPolicy allows source edits made by the worker, but no policy drift.
func verifyWorkerBriefPolicy(s *session, p preparedWorkerBrief) error {
	d := p.Dispatch
	if len(d.SHA256) != 64 || strings.ToLower(d.SHA256) != d.SHA256 {
		return errors.New("invalid saved worker brief identity")
	}
	if _, err := hex.DecodeString(d.SHA256); err != nil {
		return errors.New("invalid saved worker brief hash")
	}
	root, dir, err := workerBriefRoot(s.repo.GitDir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := d.SHA256 + ".txt"
	if d != workerBriefDispatch(p.Brief, filepath.Join(dir, name), d.SHA256) || d.Baseline != p.State.Tree {
		return errors.New("saved worker brief identity or private path does not match its dispatch")
	}
	data, err := readSavedWorkerBrief(root, name)
	if err != nil || workerBriefHash(data) != d.SHA256 {
		return fmt.Errorf("saved worker brief is missing or changed: %v", err)
	}
	prefix, err := canonicalWorkerBrief(p.Brief, "")
	if err != nil || !bytes.HasPrefix(data, prefix) {
		return errors.New("saved worker brief metadata does not match its dispatch")
	}
	current, _, err := briefInputs(s, p.Brief)
	if err != nil {
		return err
	}
	if current.Policy != p.Brief.Policy {
		return errors.New("saved worker brief is stale: role, skills or adviser policy changed")
	}
	return nil
}

func verifySavedWorkerBrief(ctx context.Context, cfg Config, p preparedWorkerBrief) error {
	s, e, err := compactEngine(ctx, cfg)
	if err != nil {
		return err
	}
	if err := verifyWorkerBriefPolicy(s, p); err != nil {
		return err
	}
	state, err := e.Snapshot(ctx)
	if err != nil {
		return err
	}
	if state != p.State {
		return errors.New("saved worker brief is stale: repository content or HEAD changed")
	}
	return nil
}

// checkWorkerDispatchBudget includes extra stage fields before writing a receipt.
func checkWorkerDispatchBudget(s *session, b workerBrief, budget, reserve int) error {
	b, _, err := briefInputs(s, b)
	if err != nil {
		return compactFailure(err)
	}
	b.Label = fmt.Sprintf("kitsu-task: %s %s %d", b.Task, b.Stage, b.Round)
	hash := strings.Repeat("0", 64)
	b.Baseline, b.Selection = hash, hash
	path := filepath.Join(s.repo.GitDir, "kitsu-context", "briefs", hash+".txt")
	data, err := json.Marshal(workerBriefDispatch(b, path, hash))
	if err != nil {
		return err
	}
	if len(data)+1+reserve > budget {
		return usagef("budget cannot hold mandatory worker dispatch; increase --max-bytes")
	}
	return nil
}

func printWorkerDispatch(cfg Config, d workerDispatch, budget int) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(data)+1 > budget {
		return usagef("budget cannot hold worker dispatch; increase --max-bytes")
	}
	data = append(data, '\n')
	_, err = cfg.Out.Write(data)
	return err
}
