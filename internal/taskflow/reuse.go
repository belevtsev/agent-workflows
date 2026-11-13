package taskflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// Why credited credits a run or withholds credit. Each reason that withholds
// credit names one invalidator that docs/operations.md lists.
const (
	reasonSnapshot      = "passed at the current source snapshot"
	reasonReused        = "passed at the same source digest, permission bits, go command, GOFLAGS and environment key"
	reasonSuperseded    = "a later run of the check did not pass"
	reasonNotPassed     = "the run did not pass, or the source changed while it ran"
	reasonOtherSnapshot = "the run passed at another source snapshot"
	reasonGit           = "a git check reads HEAD, the index or history, which the source digest does not cover"
	reasonUnkeyed       = "the run was recorded without a source digest, permission digest or environment key"
	reasonSourceChanged = "the source digest differs"
	reasonModes         = "a file's permission bits differ"
	reasonLog           = "the run's log is truncated, missing or differs from its hash"
	reasonUnreadable    = "the configuration the check would run under now cannot be read"
	reasonTool          = "the go command differs"
	reasonGOFLAGS       = "GOFLAGS differs"
	reasonEnvKey        = "the environment key differs"
)

// reuseOff reports whether CW_NO_REUSE=1 turns reuse off: then only a run at
// the current snapshot earns credit, and cw task run always runs.
func reuseOff() bool { return os.Getenv("CW_NO_REUSE") == "1" }

// runIdentity is what a run of a check is reused by beyond its source: the go
// command it runs, its redacted GOFLAGS and its env_key.
type runIdentity struct {
	tool    *langpack.GoTool
	goflags string
	envKey  string
}

// creditSource is the current source a check run is credited against.
type creditSource struct {
	snapshot projectctx.Snapshot
	// exact credits a passed run at snapshot without comparing the rest; cw
	// task run clears it, so a run at the same snapshot under another
	// configuration executes.
	exact bool
	// reuse credits a passed run at another snapshot whose source digest,
	// permission bits, go command, GOFLAGS and env_key equal the current ones.
	reuse     bool
	artifacts Artifacts
	// digest returns SourceDigest of the source at snapshot, and modes its
	// modesDigest; each is "" when the source has none.
	digest, modes func() string
	// identity returns what check would run under now.
	identity func(check Check) (runIdentity, error)
	// recorded replays a stored binding (recordedCredit): the comparisons
	// past the source digest were made when it was bound.
	recorded bool
}

// recordedCredit replays, when a stored record loads, a binding made at
// snapshot over the source whose digest is sourceSHA256: it credits a run at
// snapshot, or a reuse candidate (reuseCandidate) with that digest. The
// permission bits, log and configuration the run was reused by were compared
// when the binding was made and cannot be read again; CW_NO_REUSE does not
// make a stored record invalid.
func recordedCredit(snapshot projectctx.Snapshot, sourceSHA256 string) *creditSource {
	return &creditSource{snapshot: snapshot, exact: true, reuse: sourceSHA256 != "", recorded: true, digest: func() string { return sourceSHA256 }}
}

// probedIdentity is what probeReuse read for one check.
type probedIdentity struct {
	identity runIdentity
	err      error
}

// errNotProbed withholds reuse from a check that probeReuse did not read,
// such as one whose latest run changed after it read the record.
var errNotProbed = errors.New("cw did not read it before taking the task lock; run the command again")

// probeReuse reads, before the task lock is taken, what each check whose
// latest run could be reused at the current snapshot would run under now.
// Resolving a go command and reading its go env run it, which must not happen
// under the lock (RunCheck). It reads nothing when reuse is off or the record
// cannot be read, and credit then reuses nothing.
func (env Env) probeReuse(ctx context.Context, path string) map[string]probedIdentity {
	probes := map[string]probedIdentity{}
	if reuseOff() {
		return probes
	}
	record, err := Load(path)
	if err != nil {
		return probes
	}
	snapshot, err := env.source().Snapshot(ctx, record.Baseline.SelectedPath)
	if err != nil {
		return probes
	}
	root, _, err := openTask(path)
	if err != nil {
		return probes
	}
	defer root.Close()
	for _, check := range allChecks(record) {
		run := latestRun(record, check.ID)
		if run == nil || !reuseCandidate(*run) || equalSnapshot(snapshot, *run.After) {
			continue
		}
		_, tool, _, config, err := checkSpec(ctx, check, record.Baseline.SelectedPath)
		identity := runIdentity{}
		if err == nil {
			identity = currentIdentity(check, tool)
			identity.envKey, err = runKey(taskDirectory{root}, config, false)
		}
		probes[check.ID] = probedIdentity{identity, err}
	}
	return probes
}

// reuseCandidate reports whether credited could reuse run at another
// snapshot: it passed cleanly, is keyed and is not a git check.
func reuseCandidate(run CheckRun) bool {
	return run.Status == "passed" && run.ExitCode != nil && *run.ExitCode == 0 && run.After != nil && equalSnapshot(run.Before, *run.After) &&
		!gitCheck(run.Argv) && run.SourceSHA256 != "" && run.ModesSHA256 != "" && run.EnvKey != ""
}

// gitCheck reports whether argv runs git, whose answer can depend on HEAD,
// the index or history, which the source digest does not cover.
func gitCheck(argv []string) bool {
	return len(argv) > 0 && strings.TrimSuffix(filepath.Base(argv[0]), ".exe") == "git"
}

// exactCredit credits only a passed run at snapshot.
func exactCredit(snapshot projectctx.Snapshot) *creditSource {
	return &creditSource{snapshot: snapshot, exact: true}
}

// credit returns the creditSource of record at snapshot. files is the
// manifest read with snapshot, or nil to read it when a digest is needed.
// probes is what probeReuse read before the task lock; a check it did not
// read is not reused.
func (env Env) credit(ctx context.Context, artifacts Artifacts, snapshot projectctx.Snapshot, files []projectctx.FileState, probes map[string]probedIdentity) *creditSource {
	source := &creditSource{snapshot: snapshot, exact: true, reuse: !reuseOff(), artifacts: artifacts}
	sum, modes, read := "", "", false
	readSource := func() {
		if read {
			return
		}
		read = true
		current := snapshot
		if files == nil {
			var err error
			if current, files, err = env.source().Manifest(ctx, snapshot.SelectedPath); err != nil {
				return
			}
		}
		if _, manifestSum, err := sourceManifest(files); err == nil && manifestSum != "" && equalSnapshot(current, snapshot) {
			sum, modes = manifestSum, modesDigest(snapshot.Root, files)
		}
	}
	source.digest = func() string { readSource(); return sum }
	source.modes = func() string { readSource(); return modes }
	source.identity = func(check Check) (runIdentity, error) {
		found, ok := probes[check.ID]
		if !ok {
			return runIdentity{}, errNotProbed
		}
		return found.identity, found.err
	}
	return source
}

// modesDigest returns the SHA-256 of each file's path and full permission
// bits, read with lstat under root, or "" when one cannot be read. The source
// digest keeps only the owner-executable bit.
func modesDigest(root string, files []projectctx.FileState) string {
	sum := sha256.New()
	for _, file := range files {
		perm := "symlink"
		if file.Mode != "120000" {
			info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(projectctx.ManifestRawPath(file.Path))))
			if err != nil {
				return ""
			}
			perm = info.Mode().Perm().String()
		}
		fmt.Fprintf(sum, "%d:%s%s\n", len(file.Path), file.Path, perm)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// currentIdentity is the identity of a run of check by tool, without its
// key.
func currentIdentity(check Check, tool *langpack.GoTool) runIdentity {
	return runIdentity{tool: tool, goflags: langpack.RedactGOFLAGS(environmentValue(checkEnvironment(check), "GOFLAGS"))}
}

// credited reports whether run earns credit at source, and why or why not.
// It does when run is the latest run of its check, passed with exit status 0
// without the source changing while it ran, and either ran at the current
// snapshot (when source.exact) or is reused: it is not a git check, its
// source digest, permission bits, go command, redacted GOFLAGS and env_key
// equal what the check would run under now, and its log is whole and matches
// its hash. The check's argv cannot differ: plans are immutable and
// add-checks only adds IDs.
func credited(record Record, run CheckRun, source *creditSource) (bool, string) {
	if latest := latestRun(record, run.CheckID); latest == nil || latest.ID != run.ID {
		return false, reasonSuperseded
	}
	if run.Status != "passed" || run.ExitCode == nil || *run.ExitCode != 0 || run.After == nil || !equalSnapshot(run.Before, *run.After) {
		return false, reasonNotPassed
	}
	if source.exact && equalSnapshot(source.snapshot, *run.After) {
		return true, reasonSnapshot
	}
	switch {
	case !source.reuse:
		return false, reasonOtherSnapshot
	case gitCheck(run.Argv):
		return false, reasonGit
	case run.SourceSHA256 == "" || run.ModesSHA256 == "" || run.EnvKey == "":
		return false, reasonUnkeyed
	case run.SourceSHA256 != source.digest():
		return false, reasonSourceChanged
	case source.recorded:
		return true, reasonReused
	case run.ModesSHA256 != source.modes():
		return false, reasonModes
	}
	if data, err := source.artifacts.Read(run.Log, MaxLogBytes); run.Truncated || err != nil || digest(data) != run.LogSHA256 {
		return false, reasonLog
	}
	check, found := findCheck(record, run.CheckID)
	if !found {
		return false, reasonUnreadable
	}
	identity, err := source.identity(check)
	switch {
	case err != nil:
		return false, reasonUnreadable + ": " + err.Error()
	case (identity.tool == nil) != (run.Tool == nil) || identity.tool != nil && *identity.tool != *run.Tool:
		return false, reasonTool
	case identity.goflags != run.GOFLAGS:
		return false, reasonGOFLAGS
	case identity.envKey != run.EnvKey:
		return false, reasonEnvKey
	}
	return true, reasonReused
}

// creditedRun returns the latest run of check when it earns credit at source.
func creditedRun(record Record, check string, source *creditSource) *CheckRun {
	if run := latestRun(record, check); run != nil {
		if ok, _ := credited(record, *run, source); ok {
			return run
		}
	}
	return nil
}

// reusedLimitations names, in check order, each check whose credited run
// ran at another snapshot and is reused.
func reusedLimitations(record Record, source *creditSource) []string {
	var limitations []string
	for _, check := range allChecks(record) {
		if run := creditedRun(record, check.ID, source); run != nil && !equalSnapshot(source.snapshot, *run.After) {
			limitations = append(limitations, "check "+check.ID+" reuses run "+run.ID+", which "+reasonReused+" at commit "+shortHead(run.After.Head)+"; it was not run again")
		}
	}
	return limitations
}

func shortHead(head string) string {
	if len(head) > 12 {
		return head[:12]
	}
	if head == "" {
		return "(none)"
	}
	return head
}
