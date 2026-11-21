package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
)

const coordinatorScope = "Local recorded state only. References and worker claims do not certify execution, skill loading, semantic TDD, independent review, approval or task completion. Expand original sources; the gate and hooked Git workflow remain authoritative."
const coordinatorSnapshotLimit = 64

type coordinatorSnapshot struct {
	Version  int                        `json:"version"`
	ID       string                     `json:"snapshot_id"`
	Task     string                     `json:"task"`
	Worktree string                     `json:"worktree"`
	Created  time.Time                  `json:"created_at"`
	Sections map[string]json.RawMessage `json:"sections"`
}

type coordinatorView struct {
	coordinatorSnapshot
	Kind  string `json:"kind"`
	Since string `json:"since,omitempty"`
	Scope string `json:"scope"`
}

func runCoordinatorTaskShow(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("task show")
	compact := flags.Bool("compact", false, "")
	review := flags.String("review-ticket", "", "")
	reviewBrief := flags.Bool("review-brief", false, "")
	since := flags.String("since", "", "")
	budget := flags.Int("max-bytes", contextpack.DefaultBytes, "")
	flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if p.dash || len(p.pos) != 1 || !taskName.MatchString(p.pos[0]) {
		return usagef("show requires one valid task name")
	}
	if uxFlagPresent(args, "review-ticket") {
		if !stageValidID(*review) || uxFlagPresent(args, "compact") || uxFlagPresent(args, "since") || uxFlagPresent(args, "max-bytes") {
			return usagef("--review-ticket requires a reservation ticket and a separate view from --compact, --since or --max-bytes")
		}
		return printReservedReview(ctx, cfg, p.pos[0], *review, *reviewBrief)
	}
	if uxFlagPresent(args, "review-brief") {
		return usagef("--review-brief requires --review-ticket")
	}
	if !*compact && !uxFlagPresent(args, "since") {
		if uxFlagPresent(args, "max-bytes") {
			return usagef("--max-bytes requires --compact or --since")
		}
		legacy := []string{p.pos[0]}
		if flags.Lookup("json").Value.String() == "true" {
			legacy = append(legacy, "--json")
		}
		return uxPrintTasks(ctx, cfg, legacy, true)
	}
	if uxFlagPresent(args, "since") && (!stageValidID(*since) || strings.ToLower(*since) != *since) {
		return usagef("--since requires a snapshot id; request a fresh snapshot with --compact")
	}
	if err := compactBudget(*budget); err != nil {
		return err
	}
	return printCoordinatorState(ctx, cfg, p.pos[0], *since, *budget)
}

func printCoordinatorState(ctx context.Context, cfg Config, task, since string, budget int) error {
	s, e, err := compactEngine(ctx, cfg)
	if err != nil {
		return err
	}
	l, err := openStageLedger(ctx, s)
	if err != nil {
		return compactFailure(err)
	}
	defer l.close()
	var base coordinatorSnapshot
	if since != "" {
		base, err = loadCoordinatorSnapshot(l, since, task, s.repo.Root)
		if err != nil {
			return failf("snapshot unavailable: %v; request a fresh snapshot with --compact", err)
		}
	}
	// Compare complete observations, including time-dependent check verdicts and
	// external selected skills, before exposing a reusable baseline.
	sections, err := coordinatorSections(ctx, s, e, l, task)
	if err != nil {
		return compactFailure(err)
	}
	confirmed, err := coordinatorSections(ctx, s, e, l, task)
	if err != nil || !sameCoordinatorSections(sections, confirmed) {
		return failf("coordinator state changed while reading; retry a full snapshot: %v", err)
	}
	snapshot := coordinatorSnapshot{Version: 1, Task: task, Worktree: s.repo.Root, Created: time.Now().UTC(), Sections: sections}
	snapshot.ID = contextpack.Key(snapshot)
	view := coordinatorView{coordinatorSnapshot: snapshot, Kind: "snapshot", Scope: coordinatorScope}
	if since != "" {
		view.Kind, view.Since = "delta", since
		view.Sections = coordinatorDelta(base.Sections, snapshot.Sections)
	}
	data, err := boundedStageJSON(view, budget)
	if err != nil {
		return usagef("budget cannot hold complete coordinator sections; increase --max-bytes (nothing was omitted or saved)")
	}
	retained, err := json.Marshal(snapshot)
	if err != nil || len(retained) > contextpack.MaxArtifact {
		return failf("complete snapshot exceeds the private storage limit")
	}
	if err := pruneCoordinatorSnapshots(l); err != nil {
		return compactFailure(err)
	}
	if err := l.write("snapshot-"+snapshot.ID+".json", snapshot); err != nil {
		return compactFailure(err)
	}
	_, err = cfg.Out.Write(data)
	return err
}

func sameCoordinatorSections(a, b map[string]json.RawMessage) bool {
	return contextpack.Key(a) == contextpack.Key(b)
}

// A section is replaced atomically. Empty collections and explicit none states
// clear prior facts; unchanged sections are absent, never implicitly cleared.
func coordinatorDelta(before, after map[string]json.RawMessage) map[string]json.RawMessage {
	changes := map[string]json.RawMessage{}
	for key, value := range after {
		if !bytes.Equal(value, before[key]) {
			changes[key] = value
		}
	}
	for key := range before {
		if _, exists := after[key]; !exists {
			changes[key] = json.RawMessage("null")
		}
	}
	return changes
}

func loadCoordinatorSnapshot(l *stageLedger, id, task, worktree string) (coordinatorSnapshot, error) {
	var saved coordinatorSnapshot
	if !stageValidID(id) || strings.ToLower(id) != id {
		return saved, errors.New("invalid snapshot id")
	}
	if err := l.read("snapshot-"+id+".json", &saved); err != nil {
		return saved, err
	}
	savedID := saved.ID
	saved.ID = ""
	if savedID != id || contextpack.Key(saved) != id || saved.Version != 1 || saved.Task != task || saved.Worktree != worktree || len(saved.Sections) != len(coordinatorSectionNames) {
		return saved, errors.New("snapshot identity, version or sections mismatch")
	}
	for _, name := range coordinatorSectionNames {
		if data := bytes.TrimSpace(saved.Sections[name]); len(data) == 0 || data[0] != '{' {
			return saved, errors.New("snapshot is not a complete baseline")
		}
	}
	if saved.Created.IsZero() || time.Since(saved.Created) > contextpack.Lifetime || saved.Created.After(time.Now().Add(time.Minute)) {
		return saved, errors.New("snapshot expired or future dated")
	}
	saved.ID = id
	return saved, nil
}

// Bound only this evictable view cache; never remove intervals or receipts.
func pruneCoordinatorSnapshots(l *stageLedger) error {
	entries, err := fs.ReadDir(l.root.FS(), ".")
	if err != nil {
		return err
	}
	type entry struct {
		name string
		info fs.FileInfo
	}
	var retained []entry
	var total int64
	for _, item := range entries {
		if !strings.HasPrefix(item.Name(), "snapshot-") || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		info, err := l.root.Lstat(item.Name())
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe snapshot cache entry: %s", item.Name())
		}
		if time.Since(info.ModTime()) > contextpack.Lifetime {
			if err := l.root.Remove(item.Name()); err != nil {
				return err
			}
			continue
		}
		retained = append(retained, entry{item.Name(), info})
		total += info.Size()
	}
	slices.SortFunc(retained, func(a, b entry) int { return a.info.ModTime().Compare(b.info.ModTime()) })
	for _, item := range retained {
		if len(retained) < coordinatorSnapshotLimit && total <= contextpack.CacheBytes-contextpack.MaxArtifact {
			break
		}
		if err := l.verify(); err != nil {
			return err
		}
		if err := l.root.Remove(item.name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		total -= item.info.Size()
		retained = retained[1:]
	}
	return nil
}
