package taskflow

import (
	"context"
	"os"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// Why credited credits a run or withholds credit. Each reason that withholds
// credit names one invalidator that docs/operations.md lists.
const (
	reasonSnapshot      = "passed at the current source snapshot"
	reasonReused        = "passed at the same source digest, go command, GOFLAGS and environment key"
	reasonSuperseded    = "a later run of the check did not pass"
	reasonNotPassed     = "the run did not pass, or the source changed while it ran"
	reasonOtherSnapshot = "the run passed at another source snapshot"
	reasonUnkeyed       = "the run was recorded without a source digest or environment key"
	reasonSourceChanged = "the source digest differs"
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
	// go command, GOFLAGS and env_key equal the current ones.
	reuse     bool
	artifacts Artifacts
	// digest returns SourceDigest of the source at snapshot, or "" when it
	// has none.
	digest func() string
	// identity returns what check would run under now.
	identity func(check Check) (runIdentity, error)
}

// exactCredit credits only a passed run at snapshot, as worker results and
// acceptances bind their runs.
func exactCredit(snapshot projectctx.Snapshot) *creditSource {
	return &creditSource{snapshot: snapshot, exact: true}
}

// credit returns the creditSource of record at snapshot. files is the
// manifest read with snapshot, or nil to read it when a digest is needed.
// The digest and each check's identity are read once, and only for a run
// that could be reused; reading an identity runs a go check's go command.
func (env Env) credit(ctx context.Context, artifacts Artifacts, record Record, snapshot projectctx.Snapshot, files []projectctx.FileState) *creditSource {
	source := &creditSource{snapshot: snapshot, exact: true, reuse: !reuseOff(), artifacts: artifacts}
	sum, read := "", false
	source.digest = func() string {
		if !read {
			read = true
			current := snapshot
			if files == nil {
				var err error
				if current, files, err = env.source().Manifest(ctx, snapshot.SelectedPath); err != nil {
					return ""
				}
			}
			if _, manifestSum, err := sourceManifest(files); err == nil && equalSnapshot(current, snapshot) {
				sum = manifestSum
			}
		}
		return sum
	}
	type probed struct {
		identity runIdentity
		err      error
	}
	cache := map[string]probed{}
	source.identity = func(check Check) (runIdentity, error) {
		if found, ok := cache[check.ID]; ok {
			return found.identity, found.err
		}
		_, tool, _, config, err := checkSpec(ctx, check, record.Baseline.SelectedPath)
		identity := runIdentity{}
		if err == nil {
			identity = currentIdentity(check, tool)
			identity.envKey, err = runKey(artifacts, config, false)
		}
		cache[check.ID] = probed{identity, err}
		return identity, err
	}
	return source
}

// currentIdentity is the identity of a run of check by tool, without its
// key.
func currentIdentity(check Check, tool *langpack.GoTool) runIdentity {
	return runIdentity{tool: tool, goflags: langpack.RedactGOFLAGS(environmentValue(checkEnvironment(check), "GOFLAGS"))}
}

// credited reports whether run earns credit at source, and why or why not.
// It does when run is the latest run of its check, passed with exit status 0
// without the source changing while it ran, and either ran at the current
// snapshot (when source.exact) or is reused: its source digest, go command,
// redacted GOFLAGS and env_key equal what the check would run under now, and
// its log is whole and matches its hash. The check's argv cannot differ:
// plans are immutable and add-checks only adds IDs.
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
	case run.SourceSHA256 == "" || run.EnvKey == "":
		return false, reasonUnkeyed
	case run.SourceSHA256 != source.digest():
		return false, reasonSourceChanged
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
