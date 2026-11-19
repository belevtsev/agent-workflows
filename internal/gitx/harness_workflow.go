package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

// StageWorkflowCandidate installs an already checked tree into the index only
// if the original staging and branch are still current. Its caller authorizes
// the full tree (including any pre-existing edits) before making this call.
func (r *Repo) StageWorkflowCandidate(ctx context.Context, tmp, expectedTarget, head, expectedIndex, tree string) error {
	original, err := readIndexBytes(r.indexPath())
	if err != nil {
		return err
	}
	index, cleanup, err := r.copyIndex(tmp)
	if err != nil {
		return err
	}
	defer cleanup()
	env := []string{"GIT_INDEX_FILE=" + index}
	before, err := r.git(ctx, env, "write-tree")
	if err != nil {
		return err
	}
	if before != expectedIndex {
		return fmt.Errorf("index changed before staging the workflow candidate")
	}
	if _, err := r.git(ctx, env, "read-tree", tree); err != nil {
		return err
	}
	info, err := os.Stat(index)
	if err != nil {
		return err
	}
	replacement, err := readIndexBytes(index)
	if err != nil {
		return err
	}
	target, err := r.CommitTarget(ctx)
	if err != nil {
		return err
	}
	if target != expectedTarget {
		return fmt.Errorf("checkout changed before staging the workflow candidate")
	}
	return r.replaceCommittedIndex(ctx, original, replacement, info.ModTime(), target, head)
}

type WorkflowFile struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Data []byte `json:"data,omitempty"`
}

func (r *Repo) WorkflowPathIgnored(ctx context.Context, name string) (bool, error) {
	_, err := r.git(ctx, nil, "check-ignore", "--no-index", "--", name)
	var gitErr *Error
	if errors.As(err, &gitErr) && gitErr.Code == 1 {
		return false, nil
	}
	return err == nil, err
}

// ApplyWorkflowFiles imports the checked regular-file delta as one binary
// patch. Git's default application refuses the entire patch on a mismatch.
func (r *Repo) ApplyWorkflowFiles(ctx context.Context, before, after []WorkflowFile) error {
	tmp, err := os.MkdirTemp("", "kitsu-runner-import-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	dir, err := filepath.Abs(tmp)
	if err != nil {
		return err
	}
	env := []string{"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_ATTR_NOSYSTEM=1"}
	scratch := call{dir: dir, foreign: true, env: env}
	if _, err := run(ctx, scratch, "init", "--quiet", "--template="); err != nil {
		return err
	}
	// Git converts files while applying patches. Give it a private repository
	// whose highest-priority attributes preserve the runner's approved bytes.
	gitDir := filepath.Join(dir, ".git")
	info := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(info, 0o700); err != nil {
		return err
	}
	attributes := "* -text -crlf !eol !filter -ident !working-tree-encoding\n"
	if err := os.WriteFile(filepath.Join(info, "attributes"), []byte(attributes), 0o600); err != nil {
		return err
	}
	for _, set := range []struct {
		name  string
		files []WorkflowFile
	}{{"before", before}, {"after", after}} {
		root := filepath.Join(dir, set.name)
		if err := os.Mkdir(root, 0o700); err != nil {
			return err
		}
		for _, file := range set.files {
			if file.Mode == "" {
				continue
			}
			name := filepath.Join(root, filepath.FromSlash(file.Path))
			if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if file.Mode == "100755" {
				mode = 0o755
			}
			if err := os.WriteFile(name, file.Data, mode); err != nil {
				return err
			}
		}
	}
	patch, err := run(ctx, scratch,
		"-c", "core.autocrlf=false", "diff", "--no-index", "--no-prefix", "--no-renames", "--binary",
		"--no-ext-diff", "--no-textconv", "--", "before", "after")
	var result *Error
	if err == nil {
		return nil
	}
	if !errors.As(err, &result) || result.Code != 1 {
		return err
	}
	name := filepath.Join(dir, "changes.patch")
	if err := os.WriteFile(name, patch, 0o600); err != nil {
		return err
	}
	_, err = run(ctx, call{dir: r.dir, foreign: true, env: env},
		"--git-dir="+gitDir, "--work-tree="+r.Root, "-c", "core.autocrlf=false", "-c", "apply.ignoreWhitespace=none",
		"apply", "-p1", "--binary", "--whitespace=nowarn", "--", name)
	return err
}

// ReadWorkflowBlobs streams exact staged blob bytes without worktree filters.
func (r *Repo) ReadWorkflowBlobs(ctx context.Context, ids []string, limit int) ([][]byte, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	input := strings.NewReader(strings.Join(ids, "\n") + "\n")
	result := proc.Run(ctx, proc.Spec{Path: "git", Args: []string{"cat-file", "--batch"},
		Dir: r.dir, Env: environment(false), Stdin: input, StdoutLimit: limit + 128*len(ids), StderrLimit: 4096})
	if result.Err != nil || result.Code != 0 || result.Overflow {
		return nil, errors.Join(result.Err, errors.New("could not read staged runner inputs within the bundle limit"))
	}
	data := result.Stdout
	blobs := make([][]byte, 0, len(ids))
	for _, id := range ids {
		header, rest, ok := bytes.Cut(data, []byte{'\n'})
		parts := strings.Fields(string(header))
		if !ok || len(parts) != 3 || parts[0] != id || parts[1] != "blob" {
			return nil, errors.New("invalid staged Git blob header")
		}
		size, err := strconv.Atoi(parts[2])
		if err != nil || size < 0 || size > limit || size >= len(rest) || rest[size] != '\n' {
			return nil, errors.New("staged Git blobs exceed the runner bundle limit or are incomplete")
		}
		limit -= size
		blobs, data = append(blobs, rest[:size:size]), rest[size+1:]
	}
	if len(data) > 0 {
		return nil, errors.New("unexpected data after staged Git blobs")
	}
	return blobs, nil
}
