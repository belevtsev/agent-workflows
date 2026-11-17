package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNoHistory means neither portable history location exists. An incomplete
// or corrupt history is a different error and must never be treated as absent.
var ErrNoHistory = errors.New("no verification history")

// ErrPromotionPending means a complete, valid small history has moved to the
// full location but its binding still selects a small attempt. Load returns
// that usable prior History alongside this error. It is never current-ready;
// an explicit full publication may finish the interrupted promotion.
var ErrPromotionPending = errors.New("verification promotion is incomplete")

// SelectionRef identifies a portable receipt without exposing local skill paths.
type SelectionRef struct {
	ID   string `json:"id"`
	Host string `json:"host"`
	Role string `json:"role"`
}

// Result is a historical summary, never authority to skip or complete a check.
// Logs, commands, environment, and free-form diagnostics remain private.
type Result struct {
	ID         string `json:"id"`
	Expect     string `json:"expect"`
	State      string `json:"state"`
	RunID      string `json:"run_id"`
	ContentKey string `json:"content_key"`
	Tree       string `json:"tree"`
	LogSHA256  string `json:"log_sha256"`
	ExitStatus *int   `json:"exit_status"`
}

// Attempt records portable claims about one verification attempt. All fields
// are required in JSON, including empty evidence and a nullable exit status.
type Attempt struct {
	Version    int            `json:"version"`
	Task       string         `json:"task"`
	Workflow   string         `json:"workflow"`
	BaseTree   string         `json:"base_tree"`
	SourceTree string         `json:"source_tree"`
	PolicyKey  string         `json:"policy_key"`
	SkillsKey  string         `json:"skills_key"`
	Selections []SelectionRef `json:"selections"`
	Results    []Result       `json:"results"`
}

// History holds the currently bound attempt and its SHA256 content identity.
type History struct {
	ID      string
	Attempt Attempt
}

type historyBinding struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
}

func historyPaths(task string) (string, string) {
	return "openspec/changes/" + task + "/verification", "openspec/changes/.verification/" + task
}

// Load validates all saved attempts and reads the current binding offline.
// Only ErrPromotionPending returns a usable History together with an error.
func Load(root, task string) (History, error) {
	r, err := openHistory(root)
	if err != nil {
		return History{}, err
	}
	defer func() { _ = r.Close() }()
	rel, err := r.location(task)
	if err != nil {
		return History{}, err
	}
	return r.history(rel, task, false)
}

// Publish appends immutable content and atomically advances the current binding.
// The caller chooses the base; previous attempts retain their original bases.
// A full publication promotes the entire small history without merging it.
func Publish(root, task, workflow string, attempt Attempt) (History, error) {
	if attempt.Task != task || attempt.Workflow != workflow {
		return History{}, errors.New("verification attempt must match its task and workflow")
	}
	if err := validateAttempt(attempt); err != nil {
		return History{}, err
	}
	data, err := historyJSON(attempt)
	if err != nil {
		return History{}, err
	}
	next := History{ID: historyDigest(data), Attempt: attempt}
	r, err := openHistory(root)
	if err != nil {
		return History{}, err
	}
	defer func() { _ = r.Close() }()
	lock, err := historyLock(root, task)
	if err != nil {
		return History{}, err
	}
	defer func() { _ = lock.Close() }()
	rel, err := r.destination(task, workflow)
	if err != nil {
		return History{}, err
	}
	return r.publishAttempt(rel, next, data)
}

func (r *historyFS) publishAttempt(rel string, next History, data []byte) (History, error) {
	exists, err := r.directory(rel, false)
	if err != nil {
		return History{}, err
	}
	if exists {
		previous, err := r.history(rel, next.Attempt.Task, true)
		if err != nil {
			return History{}, err
		}
		if previous.ID == next.ID {
			return previous, nil
		}
	}
	if err := r.write(path.Join(rel, "attempts", next.ID+".json"), data, false); err != nil {
		return History{}, err
	}
	pointer, err := historyJSON(historyBinding{Version: 1, ID: next.ID})
	if err == nil {
		err = r.write(path.Join(rel, "binding.json"), pointer, true)
	}
	return next, err
}

func (r *historyFS) location(task string) (string, error) {
	if !historyTask(task) {
		return "", fmt.Errorf("invalid verification task %q", task)
	}
	full, small := historyPaths(task)
	f, err := r.directory(full, false)
	if err != nil {
		return "", err
	}
	s, err := r.directory(small, false)
	if err != nil {
		return "", err
	}
	switch {
	case f && s:
		return "", errors.New("task has competing full and small verification histories")
	case f:
		return full, nil
	case s:
		return small, nil
	default:
		return "", ErrNoHistory
	}
}

func (r *historyFS) destination(task, workflow string) (string, error) {
	full, small := historyPaths(task)
	current, err := r.location(task)
	if err != nil && !errors.Is(err, ErrNoHistory) {
		return "", err
	}
	if workflow == "small" {
		if current == full {
			return "", errors.New("full verification history already exists; use full workflow")
		}
		return small, nil
	}
	if exists, err := r.directory(path.Dir(full), false); err != nil || !exists {
		return "", fmt.Errorf("full verification requires an existing task folder: %w", errors.Join(err, fs.ErrNotExist))
	}
	if current == small {
		if _, err := r.history(small, task, false); err != nil {
			return "", err
		}
		if _, err := r.Lstat(full); !errors.Is(err, fs.ErrNotExist) {
			return "", errors.New("full verification destination already exists or cannot be inspected")
		}
		if err := r.Rename(small, full); err != nil {
			return "", err
		}
	}
	return full, nil
}

func validateAttempt(a Attempt) error {
	if a.Version != 1 || !historyTask(a.Task) || (a.Workflow != "small" && a.Workflow != "full") {
		return errors.New("verification attempt requires version 1, a task, and small or full workflow")
	}
	if !historyTree(a.BaseTree) || !historyTree(a.SourceTree) || !historyHash(a.PolicyKey) || !historyHash(a.SkillsKey) {
		return errors.New("verification attempt requires full tree ids and SHA256 policy and skills keys")
	}
	if a.Selections == nil || a.Results == nil || len(a.Selections) > 128 || len(a.Results) > 256 {
		return errors.New("verification selections and results must be bounded, non-null arrays")
	}
	if err := validateHistorySelections(a.Selections); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, result := range a.Results {
		if err := validateHistoryResult(result); err != nil {
			return err
		}
		if seen[result.ID] {
			return fmt.Errorf("duplicate verification check %q", result.ID)
		}
		seen[result.ID] = true
	}
	return nil
}

func validateHistorySelections(selections []SelectionRef) error {
	seen := map[string]bool{}
	roles := []string{"coordinator", "lookup", "spec-writer", "architect", "quality-engineer", "developer", "reviewer", "git"}
	for _, s := range selections {
		if !historyHash(s.ID) || (s.Host != "codex" && s.Host != "claude") || !slices.Contains(roles, s.Role) || seen[s.ID] {
			return errors.New("verification selections require unique receipt SHA256 ids and valid host and role")
		}
		seen[s.ID] = true
	}
	return nil
}

func validateHistoryResult(r Result) error {
	if !historyName(r.ID) || len(r.Expect) > 4096 || !utf8.ValidString(r.Expect) || strings.ContainsRune(r.Expect, 0) {
		return errors.New("verification result requires a check id and bounded UTF-8 expectation")
	}
	switch r.State {
	case "pending", "unavailable":
		if r.RunID != "" || r.ContentKey != "" || r.Tree != "" || r.LogSHA256 != "" || r.ExitStatus != nil {
			return errors.New("pending or unavailable verification must not claim run evidence")
		}
		return nil
	case "passed", "failed", "timed_out", "interrupted", "source_changed":
	default:
		return errors.New("unknown verification result state")
	}
	return validateHistoryEvidence(r)
}

func validateHistoryEvidence(r Result) error {
	if !historyRunID(r.RunID) || !historyTree(r.ContentKey) || !historyTree(r.Tree) || !historyHash(r.LogSHA256) {
		return errors.New("verification run evidence requires a run id, content key, tree, and log SHA256")
	}
	if r.State == "passed" && (r.ExitStatus == nil || *r.ExitStatus != 0) {
		return errors.New("passed verification requires a known zero exit status")
	}
	if r.State == "failed" && (r.ExitStatus == nil || *r.ExitStatus == 0) {
		return errors.New("failed verification requires a known nonzero exit status")
	}
	return nil
}

func historyHash(value string) bool {
	_, err := hex.DecodeString(value)
	return len(value) == 64 && value == strings.ToLower(value) && err == nil
}

func historyTree(value string) bool {
	_, err := hex.DecodeString(value)
	return (len(value) == 40 || len(value) == 64) && value == strings.ToLower(value) && err == nil
}

func historyDigest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func historyTask(task string) bool {
	if len(task) == 0 || len(task) > 128 {
		return false
	}
	for _, word := range strings.Split(task, "-") {
		if word == "" || strings.ContainsFunc(word, func(c rune) bool {
			return !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9')
		}) {
			return false
		}
	}
	return true
}

func historyName(name string) bool {
	if name == "" || len(name) > 128 || name[0] == '.' || strings.HasSuffix(name, ".") {
		return false
	}
	return !strings.ContainsFunc(name, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.')
	})
}

func historyRunID(id string) bool {
	if len(id) != 25 || id[16] != '-' {
		return false
	}
	_, err := time.Parse("20060102T150405Z", id[:16])
	_, hexErr := hex.DecodeString(id[17:])
	return err == nil && hexErr == nil && strings.ToLower(id[17:]) == id[17:]
}

// Tasks discovers full tasks and named small verification histories without
// requiring any installed skills, adviser bindings, or private run records.
func Tasks(root string) ([]string, error) {
	r, err := openHistory(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	set := map[string]bool{}
	for _, dir := range []string{"openspec/changes", "openspec/changes/.verification"} {
		exists, err := r.directory(dir, false)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		entries, err := r.entries(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if dir == "openspec/changes" && (entry.Name() == "archive" || strings.HasPrefix(entry.Name(), ".")) {
				continue
			}
			if dir == "openspec/changes" && !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			if !historyTask(entry.Name()) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("%s/%s is not a real verification task directory", dir, entry.Name())
			}
			set[entry.Name()] = true
		}
	}
	tasks := make([]string, 0, len(set))
	for task := range set {
		tasks = append(tasks, task)
	}
	slices.Sort(tasks)
	return tasks, nil
}
