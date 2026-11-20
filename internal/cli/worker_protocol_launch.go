package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
)

const smallTaskBriefWarning = "Small-task history does not establish a resumable workflow stage; use the explicitly supplied stage and round."

type nativeLaunchView struct {
	Version      int                `json:"version"`
	View         string             `json:"view"`
	Label        string             `json:"label"`
	Task         string             `json:"task"`
	DispatchID   string             `json:"dispatch_id"`
	Path         string             `json:"path"`
	SHA256       string             `json:"sha256"`
	Worktree     string             `json:"worktree"`
	Native       nativeWorkerLaunch `json:"native"`
	PromptSHA256 string             `json:"native_prompt_sha256"`
	Warnings     []string           `json:"warnings"`
	FullDispatch string             `json:"full_dispatch"`
}

func nativeDispatchView(d nativeDispatch, warnings []string, view string) any {
	if view != "launch" {
		return d
	}
	return nativeLaunchView{
		Version: d.Version, View: "launch", Label: d.Label, Task: d.Task, DispatchID: d.DispatchID,
		Path: d.Path, SHA256: d.SHA256, Worktree: d.Worktree, Native: d.Native,
		PromptSHA256: d.PromptSHA256, Warnings: warnings, FullDispatch: "kitsu task dispatch " + d.DispatchID,
	}
}

// Bind the original rendered prompt before the record gets its content-addressed
// ID. Only the handback schema's empty ID slot is filled after hashing.
func nativeDispatchRecord(record stageRecord, id string) nativeDispatch {
	d := stageDispatch{workerDispatch: record.Prepared.Dispatch, DispatchID: id}
	full := nativeWorkerDispatch(d, record.Prepared.State, record.Worktree)
	full.Native.Prompt = strings.Replace(record.Prompt, `"dispatch_id":""`, `"dispatch_id":"`+id+`"`, 1)
	full.PromptSHA256 = workerBriefHash([]byte(full.Native.Prompt))
	return full
}

func runTaskDispatch(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("task dispatch")
	budget := flags.Int("max-bytes", contextpack.DefaultBytes, "")
	run := flags.Bool("run", false, "")
	flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if p.dash || len(p.pos) != 1 || !stageValidID(p.pos[0]) {
		return usagef("dispatch requires an immutable dispatch id from this worktree")
	}
	if err := compactBudget(*budget); err != nil {
		return err
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	if *run {
		return failf("configured launch moved to kitsu task run, which uses supervised workers and checks")
	}
	ledger, err := readOnlyStageLedger(s.repo.GitDir)
	if err != nil {
		return compactFailure(err)
	}
	defer ledger.close()
	record, err := ledger.record(p.pos[0], s.repo.Root)
	if err != nil {
		return compactFailure(err)
	}
	if err := verifyDispatchReference(ctx, s, ledger, record); err != nil {
		return compactFailure(err)
	}
	data, err := boundedStageJSON(nativeDispatchRecord(record, p.pos[0]), *budget)
	if err != nil {
		return err
	}
	_, err = cfg.Out.Write(data)
	return err
}

// Archival reads pin existing private directories and never acquire a writer's
// lock, create a cache, take a new baseline or observe/run check evidence.
func readOnlyStageLedger(gitDir string) (*stageLedger, error) {
	gitRoot, err := os.OpenRoot(gitDir)
	if err != nil {
		return nil, err
	}
	root, info, err := existingPrivateRoot(gitRoot, "kitsu-stages")
	if err != nil {
		_ = gitRoot.Close()
		return nil, err
	}
	return &stageLedger{root: root, gitRoot: gitRoot, info: info}, nil
}

func existingPrivateRoot(parent *os.Root, name string) (*os.Root, os.FileInfo, error) {
	path := ""
	var info os.FileInfo
	for _, component := range strings.Split(name, "/") {
		path = filepath.Join(path, component)
		var err error
		info, err = parent.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errors.New("reference requires an existing real private directory")
		}
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, nil, err
	}
	opened, err := root.Stat(".")
	named, namedErr := parent.Lstat(name)
	if err != nil || namedErr != nil || !os.SameFile(info, opened) || !os.SameFile(info, named) {
		_ = root.Close()
		return nil, nil, errors.New("reference directory was replaced")
	}
	return root, info, nil
}

func verifyDispatchReference(ctx context.Context, s *session, ledger *stageLedger, record stageRecord) error {
	p := record.Prepared
	d := p.Dispatch
	if strings.Count(record.Prompt, `"dispatch_id":""`) != 1 {
		return errors.New("original native dispatch is unavailable; prepare a fresh worker with --dispatch")
	}
	root, _, err := existingPrivateRoot(ledger.gitRoot, "kitsu-context/briefs")
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := d.SHA256 + ".txt"
	path := filepath.Join(s.repo.GitDir, "kitsu-context", "briefs", name)
	if !stageValidID(d.SHA256) || d != workerBriefDispatch(p.Brief, path, d.SHA256) || d.Baseline != p.State.Tree {
		return errors.New("saved worker brief identity does not match its dispatch")
	}
	data, err := readSavedWorkerBrief(root, name)
	if err != nil || workerBriefHash(data) != d.SHA256 {
		return errors.New("saved worker brief is missing or changed")
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
	if err := verifyDispatchCheckPolicy(s, record); err != nil {
		return err
	}
	return errors.Join(ctx.Err(), ledger.verify())
}

func verifyDispatchCheckPolicy(s *session, record stageRecord) error {
	root, err := os.OpenRoot(s.repo.Root)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	want, found := record.Sources["kitsu.toml"]
	if !found {
		if _, err := root.Lstat("kitsu.toml"); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	} else if got, err := stageRawSource(root, "kitsu.toml"); err == nil && got == want {
		return nil
	}
	return errors.New("saved worker dispatch is stale: declared check policy changed")
}
