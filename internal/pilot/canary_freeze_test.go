package pilot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

var canaryFreezeRoot = flag.String("pilot-canary-freeze-root", "", "new private canary root; snapshots/builds/freezes assets only, never invokes a model")
var canaryPriorOutput = flag.String("pilot-canary-prior-output", "", "read prior rollout timestamps offline to assess aggregate counter-silence watchdog")

func TestPilotCanaryDraftPlan(t *testing.T) {
	if *canaryFreezeRoot == "" {
		t.Skip("explicit canary input root required; no model invocation")
	}
	var m manifest
	if err := readJSON(filepath.Join(*canaryFreezeRoot, "canary-input.json"), &m); err != nil {
		t.Fatal(err)
	}
	if !validCanaryProfile(m) {
		t.Fatal("invalid draft canary profile")
	}
	for _, run := range schedule(m, "").Runs {
		t.Logf("order=%d run=%s", run.Order, run.ID)
	}
}

func TestPilotCanaryPriorCounterGaps(t *testing.T) {
	if *canaryPriorOutput == "" {
		t.Skip("explicit prior output required; no model invocation")
	}
	entries, err := os.ReadDir(filepath.Join(*canaryPriorOutput, "runs"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Run        string  `json:"run"`
		Events     int     `json:"changed_counter_events"`
		MaximumGap float64 `json:"maximum_aggregate_gap_seconds"`
		FirstGap   float64 `json:"startup_gap_seconds"`
		LastGap    float64 `json:"tail_gap_seconds"`
	}
	var rows []row
	for _, entry := range entries {
		var result runResult
		if err := readJSON(filepath.Join(*canaryPriorOutput, "runs", entry.Name(), "result.json"), &result); err != nil {
			t.Fatal(err)
		}
		if result.Execution == nil || !result.Measured {
			continue
		}
		var times []time.Time
		for _, trace := range result.Usage.Threads {
			f, err := os.Open(trace.File.Path)
			if err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 64<<10), 64<<20)
			previous := int64(-1)
			for scanner.Scan() {
				var e struct {
					Timestamp time.Time `json:"timestamp"`
					Type      string    `json:"type"`
					Payload   struct {
						Type string `json:"type"`
						Info *struct {
							Total struct {
								Input  int64 `json:"input_tokens"`
								Output int64 `json:"output_tokens"`
							} `json:"total_token_usage"`
						} `json:"info"`
					} `json:"payload"`
				}
				if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
					t.Fatal(err)
				}
				if e.Type != "event_msg" || e.Payload.Type != "token_count" || e.Payload.Info == nil || e.Timestamp.IsZero() {
					continue
				}
				current := e.Payload.Info.Total.Input + e.Payload.Info.Total.Output
				if current == previous {
					continue
				}
				previous = current
				if !e.Timestamp.Before(result.Execution.StartedAt) && !e.Timestamp.After(result.Execution.EndedAt) {
					times = append(times, e.Timestamp)
				}
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}
		slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
		r := row{Run: entry.Name(), Events: len(times)}
		previous := result.Execution.StartedAt
		for i, at := range append(times, result.Execution.EndedAt) {
			gap := at.Sub(previous).Seconds()
			r.MaximumGap = max(r.MaximumGap, gap)
			if i == 0 {
				r.FirstGap = gap
			}
			r.LastGap = gap
			previous = at
		}
		rows = append(rows, r)
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(data))
	if *pilotOutput != "" {
		if err := writeJSON(filepath.Join(*pilotOutput, "prior-counter-gaps.json"), rows, true); err != nil {
			t.Fatal(err)
		}
	}
}

// Explicit offline replacement for ad hoc freeze scripts. The measured binary
// is built from retained source bytes, and neither prior pilots nor an existing
// frozen canary may be replaced by this helper.
func TestPilotFreezeCanaryAssets(t *testing.T) {
	if *canaryFreezeRoot == "" {
		t.Skip("explicit new private canary root required; no model invocation")
	}
	root, err := filepath.Abs(*canaryFreezeRoot)
	if err != nil {
		t.Fatal(err)
	}
	repoBytes, err := exec.CommandContext(t.Context(), "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	repo := strings.TrimSpace(string(repoBytes))
	if inside(repo, root) || inside(root, repo) {
		t.Fatal("private freeze root must be outside the source repository")
	}
	if err := freezeCanaryAssets(t.Context(), repo, root); err != nil {
		t.Fatal(err)
	}
	var p plan
	if err := readJSON(filepath.Join(root, "trial", "plan.json"), &p); err != nil {
		t.Fatal(err)
	}
	for _, run := range p.Runs {
		t.Logf("frozen order=%d run=%s", run.Order, run.ID)
	}
	t.Log("offline freeze complete; paid_calls=0; no proxy or model launched")
}

// supportSources gives each file of the frozen support folder its place in
// the repository. The folder keeps the paths that earlier runs recorded.
var supportSources = map[string]string{
	"skills/task/SKILL.md":              "skills/task/SKILL.md",
	"skills/task/references/compact.md": "plugins/kitsu-team/skills/managed/references/compact.md",
}

func canarySourceIndex(ctx context.Context, repo string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = repo
	data, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	index := map[string]string{}
	for _, raw := range bytes.Split(data, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		rel := string(raw)
		if !filepath.IsLocal(rel) {
			return nil, errors.New("nonlocal source path")
		}
		path := filepath.Join(repo, rel)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("source freeze requires regular files: %s", rel)
		}
		index[rel], err = hashFile(path)
		if err != nil {
			return nil, err
		}
	}
	return index, nil
}

func TestCanaryFreezeRefusesExistingEvidence(t *testing.T) {
	for _, name := range []string{"trial", "frozen-source"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			evidence := filepath.Join(root, name)
			if err := os.Mkdir(evidence, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(evidence, "retained.txt")
			if err := os.WriteFile(path, []byte("original frozen evidence"), 0o400); err != nil {
				t.Fatal(err)
			}
			if err := freezeCanaryAssets(t.Context(), t.TempDir(), root); err == nil {
				t.Fatal("existing evidence replaced")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "original frozen evidence" {
				t.Fatal("existing evidence mutated", err)
			}
		})
	}
}

func TestCanarySourceIndexCapturesUntrackedFilesAndDetectsDrift(t *testing.T) {
	repo := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "init", "-q")
	cmd.Dir = repo
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(data), err)
	}
	path := filepath.Join(repo, "source.go")
	if err := os.WriteFile(path, []byte("first source"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := canarySourceIndex(t.Context(), repo)
	if err != nil || len(first) != 1 {
		t.Fatal(first, err)
	}
	if err := os.WriteFile(path, []byte("changed source"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := canarySourceIndex(t.Context(), repo)
	if err != nil || reflect.DeepEqual(first, second) {
		t.Fatal("source drift lost", second, err)
	}
	if err := os.Symlink(path, filepath.Join(repo, "alias.go")); err != nil {
		t.Skip("symlink unavailable on this host")
	}
	if _, err := canarySourceIndex(t.Context(), repo); err == nil {
		t.Fatal("linked source accepted without explicit capture semantics")
	}
}

func freezeCanaryAssets(ctx context.Context, repo, root string) error {
	a := filepath.Join(root, "canary-assets")
	trial := filepath.Join(root, "trial")
	if _, err := os.Stat(trial); !errors.Is(err, os.ErrNotExist) {
		return errors.New("trial already exists or is unreadable; never replace a frozen or partial canary")
	}
	if _, err := os.Stat(filepath.Join(root, "frozen-source")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("source freeze already claimed; inspect retained artifacts before explicit recovery")
	}
	var m manifest
	input := filepath.Join(root, "canary-input.json")
	if err := readJSON(input, &m); err != nil {
		return err
	}
	if !validCanaryProfile(m) {
		return errors.New("freeze helper accepts only the explicit four-run canary profile")
	}
	index, err := canarySourceIndex(ctx, repo)
	if err != nil {
		return err
	}
	source := filepath.Join(root, "frozen-source")
	if err := os.Mkdir(source, 0o700); err != nil {
		return err
	}
	paths := make([]string, 0, len(index))
	for rel := range index {
		paths = append(paths, rel)
	}
	slices.Sort(paths)
	for _, rel := range paths {
		path := filepath.Join(repo, rel)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		out := filepath.Join(source, rel)
		if err := copyRegular(path, out, 0o400|info.Mode().Perm()&0o111); err != nil {
			return err
		}
		if digest, err := hashFile(out); err != nil || digest != index[rel] {
			return fmt.Errorf("source changed during capture: %s", rel)
		}
	}
	if err := writeJSON(filepath.Join(root, "source-index.json"), index, true); err != nil {
		return err
	}
	for _, item := range []struct {
		name string
		args []string
	}{
		{"source-base.txt", []string{"rev-parse", "HEAD"}},
		{"source.diff", []string{"diff", "--binary", "HEAD"}},
	} {
		cmd := exec.CommandContext(ctx, "git", item.args...)
		cmd.Dir = repo
		data, err := cmd.Output()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, item.name), data, 0o400); err != nil {
			return err
		}
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		return err
	}
	buildEnv := append(os.Environ(), "GOWORK=off")
	for i, args := range [][]string{{"build", "-o", filepath.Join(a, "bin", "kitsu"), "./cmd/kitsu"}, {"test", "-c", "-o", filepath.Join(a, "bin", "pilot.test"), "./internal/pilot"}} {
		call, err := invoke(ctx, source, filepath.Join(root, fmt.Sprintf("freeze-build-%d", i)), "", append([]string{goBinary}, args...), buildEnv, 180)
		if err != nil || call.ExitCode != 0 || call.Stopped || call.Cleanup != "" {
			return fmt.Errorf("offline build failed: %v; exit %d; evidence %s", err, call.ExitCode, call.Stderr.Path)
		}
	}
	after, err := canarySourceIndex(ctx, repo)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(index, after) {
		return errors.New("repository source changed during freeze; built evidence retained, no launch permitted")
	}
	for rel, from := range supportSources {
		out := filepath.Join(a, "support", rel)
		if err := os.Remove(out); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := copyRegular(filepath.Join(source, from), out, 0o400); err != nil {
			return err
		}
	}
	for _, path := range []string{filepath.Join(a, "bin", "kitsu"), filepath.Join(a, "bin", "pilot.test"), filepath.Join(a, "support", "skills/task/SKILL.md"), filepath.Join(a, "support", "skills/task/references/compact.md")} {
		m.Host.Assets = slices.DeleteFunc(m.Host.Assets, func(a asset) bool { return a.Path == path })
		m.Host.Assets = append(m.Host.Assets, asset{Path: path})
	}
	if err := writeJSON(input, m, false); err != nil {
		return err
	}
	if err := freeze(ctx, input, trial); err != nil {
		return err
	}
	// Archive with the freshly built harness, preserving its explicit read-only
	// archive entry point and exclusion of every credential home file.
	call, err := invoke(ctx, source, filepath.Join(root, "freeze-archive"), "", []string{filepath.Join(a, "bin", "pilot.test"), "-test.run=^TestPilotArchiveAssets$", "-pilot-output=" + trial, "-pilot-archive-assets"}, buildEnv, 180)
	if err != nil || call.ExitCode != 0 || call.Stopped || call.Cleanup != "" {
		return fmt.Errorf("asset archive failed: %v; exit %d", err, call.ExitCode)
	}
	for _, name := range []string{"kitsu", "pilot.test"} {
		if err := os.Chmod(filepath.Join(a, "bin", name), 0o500); err != nil {
			return err
		}
	}
	return nil
}
