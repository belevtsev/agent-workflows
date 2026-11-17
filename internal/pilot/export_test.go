package pilot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type exportReceipt struct {
	Version              int    `json:"version"`
	RunID                string `json:"run_id"`
	SourceWorktree       string `json:"source_worktree"`
	PublicSnapshotSHA256 string `json:"public_snapshot_sha256"`
	BaselineTree         string `json:"baseline_tree"`
	CandidateTree        string `json:"candidate_tree"`
	TrustedRepository    string `json:"trusted_repository"`
	Mechanism            string `json:"mechanism"`
}

// copyCandidateFiles reads only regular files through a confined os.Root. It
// imports no solver Git configuration, objects, hooks, filters, or references.
// The solver namespace has already exited before this function is called.
func copyCandidateFiles(source, destination string) error {
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	return fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == ".git" {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == "." {
			return nil
		}
		if entry.Name() == ".git" || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("candidate contains a link or nested Git metadata: %s", path)
		}
		out := filepath.Join(destination, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.MkdirAll(out, 0o700)
		}
		info, err := root.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("candidate contains nonregular evidence: %s", path)
		}
		in, err := root.Open(path)
		if err != nil {
			return err
		}
		opened, err := in.Stat()
		if err != nil || !opened.Mode().IsRegular() {
			in.Close()
			return fmt.Errorf("candidate file changed type: %s", path)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			in.Close()
			return err
		}
		file, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600|opened.Mode().Perm()&0o111)
		if err != nil {
			in.Close()
			return err
		}
		_, copied := io.Copy(file, in)
		return errors.Join(copied, in.Close(), file.Close())
	})
}

func trustedGitEnv(home string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USERPROFILE=" + home,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_COUNT=0", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Kitsu Pilot", "GIT_COMMITTER_NAME=Kitsu Pilot", "GIT_AUTHOR_EMAIL=pilot@example.invalid", "GIT_COMMITTER_EMAIL=pilot@example.invalid",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
}

func exportCandidate(ctx context.Context, chosen pilotCase, selected cell, output, runDir, solverWorktree, baselineTree string) (trustedPath, candidateTree string, patch asset, attestation asset, returned error) {
	trustedRoot := filepath.Join(output, "trusted-candidates")
	trustedPath = filepath.Join(trustedRoot, selected.ID)
	if err := os.MkdirAll(trustedRoot, 0o700); err != nil {
		returned = err
		return
	}
	if err := os.Mkdir(trustedPath, 0o700); err != nil {
		returned = err
		return
	}
	if err := copySnapshot(chosen.Snapshot, trustedPath); err != nil {
		returned = err
		return
	}
	git, err := exec.LookPath("git")
	if err != nil {
		returned = err
		return
	}
	home := filepath.Join(runDir, "trusted-git-home")
	if err := os.Mkdir(home, 0o700); err != nil {
		returned = err
		return
	}
	env := trustedGitEnv(home)
	sequence := 0
	callGit := func(args ...string) (invocation, error) {
		sequence++
		prefix := filepath.Join(runDir, fmt.Sprintf("trusted-git-%02d", sequence))
		fixed := []string{git, "-c", "core.fsmonitor=false", "-c", "core.autocrlf=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.attributesFile=" + os.DevNull, "-c", "submodule.recurse=false"}
		call, err := invoke(ctx, trustedPath, prefix, "", append(fixed, args...), env, 60)
		if err != nil || call.ExitCode != 0 {
			return call, fmt.Errorf("trusted Git %v: %v; exit %d; see %s", args, err, call.ExitCode, call.Stderr.Path)
		}
		return call, nil
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "--force", "--all"}, {"commit", "-q", "-m", "Frozen public task baseline"}} {
		if _, err := callGit(args...); err != nil {
			returned = err
			return
		}
	}
	base, err := callGit("rev-parse", "HEAD^{tree}")
	if err != nil {
		returned = err
		return
	}
	data, err := os.ReadFile(base.Stdout.Path)
	if err != nil || strings.TrimSpace(string(data)) != baselineTree {
		returned = errors.New("trusted reconstruction differs from frozen baseline tree")
		return
	}
	// These host-owned attributes outrank candidate .gitattributes. Archive
	// must export the exact retained tree, never candidate export-ignore/subst.
	if err := os.WriteFile(filepath.Join(trustedPath, ".git", "info", "attributes"), []byte("* -filter !diff -export-ignore -export-subst\n"), 0o600); err != nil {
		returned = err
		return
	}
	entries, err := os.ReadDir(trustedPath)
	if err != nil {
		returned = err
		return
	}
	for _, entry := range entries {
		if entry.Name() != ".git" {
			if err := os.RemoveAll(filepath.Join(trustedPath, entry.Name())); err != nil {
				returned = err
				return
			}
		}
	}
	if err := copyCandidateFiles(solverWorktree, trustedPath); err != nil {
		returned = err
		return
	}
	if _, err := callGit("add", "--all"); err != nil {
		returned = err
		return
	}
	tree, err := callGit("write-tree")
	if err != nil {
		returned = err
		return
	}
	data, err = os.ReadFile(tree.Stdout.Path)
	if err != nil {
		returned = err
		return
	}
	candidateTree = strings.TrimSpace(string(data))
	diff, err := callGit("diff", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", baselineTree, candidateTree)
	if err != nil {
		returned = err
		return
	}
	patch = diff.Stdout
	receiptPath := filepath.Join(trustedRoot, selected.ID+".json")
	receipt := exportReceipt{1, selected.ID, solverWorktree, chosen.SnapshotSHA256, baselineTree, candidateTree, trustedPath,
		"os.Root regular-file export excluding solver .git; fresh host-owned Git DB; fixed no-global/no-system config and no external diff/textconv"}
	if err := writeJSON(receiptPath, receipt, true); err != nil {
		returned = err
		return
	}
	digest, err := hashFile(receiptPath)
	if err != nil {
		returned = err
		return
	}
	attestation = asset{receiptPath, digest}
	return
}
