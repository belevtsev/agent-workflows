package pilot

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type gradeRequest struct {
	Version             int    `json:"version"`
	CaseID              string `json:"case_id"`
	CandidateWorktree   string `json:"candidate_worktree"`
	OutputPath          string `json:"output_path"`
	BaselineTree        string `json:"baseline_tree"`
	CandidateTree       string `json:"candidate_tree"`
	VisiblePromptSHA256 string `json:"visible_prompt_sha256"`
}

type gradeOverlay struct {
	Source      asset  `json:"source"`
	Destination string `json:"destination"`
	Mode        uint32 `json:"mode"`
}

type gradeAcceptance struct {
	Commands []command `json:"commands"`
	Kind     string    `json:"kind"`
	Expected []string  `json:"expected"`
}

type gradeCase struct {
	ID                string          `json:"id"`
	Prompt            asset           `json:"prompt"`
	Eligibility       json.RawMessage `json:"eligibility"`
	Original          []gradeOverlay  `json:"original_overlays"`
	AcceptanceSources []gradeOverlay  `json:"acceptance_sources"`
	RemoveControls    []string        `json:"remove_controls"`
	Setup             []command       `json:"setup"`
	Acceptance        gradeAcceptance `json:"acceptance"`
	Regressions       []command       `json:"regressions"`
	GoModCache        string          `json:"go_mod_cache"`
	NodeModules       string          `json:"node_modules"`
}

type gradeManifest struct {
	Version              int         `json:"version"`
	WorkRoot             string      `json:"work_root"`
	TrustedCandidateRoot string      `json:"trusted_candidate_root"`
	GoRoot               string      `json:"go_root"`
	NodeRoot             string      `json:"node_root"`
	GoCache              string      `json:"go_cache"`
	Cases                []gradeCase `json:"cases"`
}

type gradeCandidateAttestation struct {
	Version              int    `json:"version"`
	RunID                string `json:"run_id"`
	SourceWorktree       string `json:"source_worktree"`
	PublicSnapshotSHA256 string `json:"public_snapshot_sha256"`
	BaselineTree         string `json:"baseline_tree"`
	CandidateTree        string `json:"candidate_tree"`
	TrustedRepository    string `json:"trusted_repository"`
	Mechanism            string `json:"mechanism"`
}

type gradeCriterion struct {
	ID            string `json:"id"`
	RequirementID string `json:"requirement_id"`
	Passed        bool   `json:"passed"`
	EvidencePath  string `json:"evidence_path"`
}

type gradeCheckResult struct {
	Passed         bool             `json:"passed"`
	Criteria       []gradeCriterion `json:"criteria"`
	CommandResults []invocation     `json:"command_results"`
}

type gradeIntegrity struct {
	Passed   bool     `json:"passed"`
	Findings []string `json:"findings"`
}

type gradeResult struct {
	Version              int              `json:"version"`
	CaseID               string           `json:"case_id"`
	CandidateID          string           `json:"candidate_id"`
	InfrastructureStatus string           `json:"infrastructure_status"`
	Eligibility          json.RawMessage  `json:"eligibility"`
	Acceptance           gradeCheckResult `json:"acceptance"`
	Regressions          gradeCheckResult `json:"regressions"`
	PatchIntegrity       gradeIntegrity   `json:"patch_integrity"`
	Setup                []invocation     `json:"setup"`
	Error                string           `json:"error,omitempty"`
}

// TestPilotGradeRequest is host-only. Nothing runs in ordinary test invocations.
// The caller must finish the solver namespace before exposing private tests here.
//
//	pilot.test -test.run=^TestPilotGradeRequest$ -- /private/grade-request.json
func TestPilotGradeRequest(t *testing.T) {
	requestPath := ""
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			requestPath = os.Args[i+1]
			break
		}
	}
	if requestPath == "" {
		t.Skip("host-only grade request required")
	}
	var request gradeRequest
	if err := readJSON(requestPath, &request); err != nil {
		t.Fatal(err)
	}
	result := gradeResult{Version: 1, CaseID: request.CaseID, CandidateID: request.CandidateTree,
		InfrastructureStatus: "invalid_input", Eligibility: json.RawMessage(`{"passed":false}`),
		PatchIntegrity: gradeIntegrity{Findings: []string{}}, Setup: []invocation{}}
	if err := executeGrade(t.Context(), request, &result); err != nil {
		result.Error = err.Error()
	}
	if !filepath.IsAbs(request.OutputPath) {
		t.Fatal("grader output must be absolute")
	}
	if err := writeJSON(request.OutputPath, result, true); err != nil {
		t.Fatal(err)
	}
}

func executeGrade(ctx context.Context, request gradeRequest, result *gradeResult) error {
	if request.Version != 1 || !slug.MatchString(request.CaseID) || !filepath.IsAbs(request.CandidateWorktree) ||
		request.BaselineTree == "" || request.CandidateTree == "" {
		return errors.New("invalid grader request")
	}
	manifestPath := os.Getenv("KITSU_PILOT_GRADING_MANIFEST")
	if !filepath.IsAbs(manifestPath) {
		return errors.New("KITSU_PILOT_GRADING_MANIFEST must name a private frozen manifest")
	}
	var m gradeManifest
	if err := readJSON(manifestPath, &m); err != nil {
		return err
	}
	if m.Version != 1 || !filepath.IsAbs(m.WorkRoot) || !filepath.IsAbs(m.GoRoot) {
		return errors.New("invalid private grading manifest")
	}
	index := slices.IndexFunc(m.Cases, func(c gradeCase) bool { return c.ID == request.CaseID })
	if index < 0 {
		return errors.New("case absent from frozen grading manifest")
	}
	if err := trustedGradeCandidate(m.TrustedCandidateRoot, request); err != nil {
		return err
	}
	c := m.Cases[index]
	if err := verifyGradeAssets(c); err != nil {
		return err
	}
	result.Eligibility = c.Eligibility
	var eligibility struct {
		Passed bool `json:"passed"`
	}
	if json.Unmarshal(c.Eligibility, &eligibility) != nil || !eligibility.Passed {
		return errors.New("case did not pass frozen eligibility gates")
	}
	if err := os.MkdirAll(m.WorkRoot, 0o700); err != nil {
		return err
	}
	gradeDir, err := os.MkdirTemp(m.WorkRoot, "candidate-")
	if err != nil {
		return err
	}
	work := filepath.Join(gradeDir, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		return err
	}
	result.InfrastructureStatus = "failed"
	if err := archiveGradeTree(ctx, request.CandidateWorktree, request.CandidateTree, work); err != nil {
		result.PatchIntegrity.Findings = append(result.PatchIntegrity.Findings, err.Error())
		return err
	}
	if err := restoreGradeOracle(work, c); err != nil {
		return err
	}
	result.PatchIntegrity.Passed = true
	if err := os.MkdirAll(filepath.Join(gradeDir, "home"), 0o700); err != nil {
		return err
	}
	if !filepath.IsAbs(m.GoCache) {
		return errors.New("grader requires a private absolute Go build cache")
	}
	if err := os.MkdirAll(m.GoCache, 0o700); err != nil {
		return err
	}
	if err := initGradeRepository(ctx, work, filepath.Join(gradeDir, "home")); err != nil {
		return err
	}
	vars := map[string]string{"worktree": "/workspace", "home": "/home/grade"}
	if c.NodeModules != "" {
		if err := os.Symlink("/opt/deps/node_modules", filepath.Join(work, "node_modules")); err != nil {
			return err
		}
	}
	step := 0
	run := func(cmd command) (invocation, error) {
		step++
		args := gradeSandboxArgs(m, c, gradeDir, expandedArgv(cmd.Argv, vars))
		return invoke(ctx, gradeDir, filepath.Join(gradeDir, fmt.Sprintf("step-%02d", step)), "", args,
			[]string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(gradeDir, "home")}, cmd.TimeoutSeconds)
	}
	for _, cmd := range c.Setup {
		invoked, err := run(cmd)
		result.Setup = append(result.Setup, invoked)
		if err != nil || invoked.ExitCode != 0 || invoked.Stopped || invoked.Cleanup != "" {
			if invoked.Stopped {
				result.InfrastructureStatus = "timeout"
			}
			return fmt.Errorf("grader setup failed: exit %d: %v", invoked.ExitCode, err)
		}
	}
	result.InfrastructureStatus = "ok"
	result.Acceptance, err = runGradeChecks(c.Acceptance.Commands, run)
	if err != nil {
		return err
	}
	criteria, complete, err := gradeEvidence(c.Acceptance, result.Acceptance.CommandResults)
	if err != nil {
		return err
	}
	result.Acceptance.Criteria = criteria
	result.Acceptance.Passed = result.Acceptance.Passed && complete
	result.Regressions, err = runGradeChecks(c.Regressions, run)
	return err
}

func verifyGradeAssets(c gradeCase) error {
	assets := []asset{c.Prompt}
	for _, overlay := range append(slices.Clone(c.Original), c.AcceptanceSources...) {
		if !gradeLocalPath(overlay.Destination) {
			return fmt.Errorf("invalid private overlay destination: %s", overlay.Destination)
		}
		assets = append(assets, overlay.Source)
	}
	for _, a := range assets {
		got, err := hashFile(a.Path)
		if err != nil || got != a.SHA256 || a.SHA256 == "" {
			return fmt.Errorf("private grading asset changed: %s: %v", a.Path, err)
		}
	}
	return nil
}

func trustedGradeCandidate(root string, request gradeRequest) error {
	if !filepath.IsAbs(root) {
		return errors.New("private trusted candidate root is required")
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	canonicalCandidate, err := filepath.EvalSymlinks(request.CandidateWorktree)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(canonicalRoot, canonicalCandidate)
	if err != nil || !filepath.IsLocal(relative) || relative == "." || strings.ContainsAny(relative, `/\`) {
		return errors.New("grader refuses Git metadata outside its private trusted candidate root")
	}
	var attestation gradeCandidateAttestation
	if err := readJSON(filepath.Join(canonicalRoot, relative+".json"), &attestation); err != nil {
		return err
	}
	if !filepath.IsAbs(attestation.TrustedRepository) {
		return errors.New("trusted candidate attestation requires an absolute repository path")
	}
	attestedCandidate, err := filepath.EvalSymlinks(attestation.TrustedRepository)
	if err != nil {
		return err
	}
	if attestation.Version != 1 || attestation.RunID != relative || attestedCandidate != canonicalCandidate ||
		attestation.BaselineTree != request.BaselineTree || attestation.CandidateTree != request.CandidateTree || attestation.Mechanism == "" {
		return errors.New("trusted candidate attestation does not match grading request")
	}
	return nil
}

func gradeLocalPath(name string) bool {
	return filepath.IsLocal(name) && name != "." && filepath.ToSlash(filepath.Clean(name)) == name &&
		name != ".git" && !strings.HasPrefix(name, ".git/")
}

func archiveGradeTree(ctx context.Context, repository, tree, destination string) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/git", "-C", repository, "archive", "--format=tar", tree)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	extractErr := extractGradeArchive(pipe, destination, 512<<20)
	if extractErr != nil {
		_ = cmd.Process.Kill()
	}
	return errors.Join(extractErr, cmd.Wait())
}

func extractGradeArchive(pipe io.Reader, destination string, maxBytes int64) error {
	archive := &io.LimitedReader{R: pipe, N: maxBytes + 1}
	reader := tar.NewReader(archive)
	var extractErr error
	for count := 0; ; count++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		name := strings.TrimSuffix(headerName(header), "/")
		if err != nil || count > 100_000 || !gradeLocalPath(name) {
			extractErr = fmt.Errorf("invalid candidate archive entry %q: %v", name, err)
			break
		}
		path := filepath.Join(destination, filepath.FromSlash(name))
		if header.Typeflag == tar.TypeDir {
			extractErr = os.MkdirAll(path, 0o700)
		} else if header.Typeflag == tar.TypeReg {
			if header.Size < 0 || header.Size > maxBytes {
				extractErr = errors.New("candidate archive file exceeds size limit")
				break
			}
			if extractErr = os.MkdirAll(filepath.Dir(path), 0o700); extractErr == nil {
				var f *os.File
				f, extractErr = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600|fs.FileMode(header.Mode)&0o111)
				if extractErr == nil {
					_, copyErr := io.CopyN(f, reader, header.Size)
					extractErr = errors.Join(copyErr, f.Close())
				}
			}
		} else {
			extractErr = fmt.Errorf("candidate contains a link or special file: %s", name)
		}
		if extractErr != nil {
			break
		}
	}
	if extractErr == nil {
		// tar EOF can precede Git's block padding. Consume the bounded tail
		// before waiting, otherwise Git can block writing a full stdout pipe.
		_, extractErr = io.Copy(io.Discard, archive)
	}
	if archive.N == 0 {
		extractErr = errors.Join(extractErr, errors.New("candidate archive exceeds size limit"))
	}
	return extractErr
}

func TestGradeArchiveDrainsBoundedPaddingBeforeProducerWait(t *testing.T) {
	for _, test := range []struct {
		name    string
		padding int
		wantErr bool
	}{
		{name: "large-padding", padding: 32 << 10},
		{name: "oversized-padding", padding: 128 << 10, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, writer := io.Pipe()
			t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
			produced := make(chan error, 1)
			go func() {
				archive := tar.NewWriter(writer)
				err := archive.WriteHeader(&tar.Header{Name: "source.txt", Mode: 0o600, Size: 1})
				if err == nil {
					_, err = archive.Write([]byte("x"))
				}
				if err == nil {
					err = archive.Close()
				}
				if err == nil {
					_, err = io.Copy(writer, bytes.NewReader(make([]byte, test.padding)))
				}
				_ = writer.CloseWithError(err)
				produced <- err
			}()
			err := extractGradeArchive(reader, t.TempDir(), 64<<10)
			if (err != nil) != test.wantErr {
				t.Fatalf("extract: %v; want error %t", err, test.wantErr)
			}
			if test.wantErr {
				_ = reader.Close() // mirrors killing an oversized archive producer
			}
			select {
			case err := <-produced:
				if err != nil && !test.wantErr {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("archive producer remained blocked after extraction")
			}
		})
	}
}

func headerName(header *tar.Header) string {
	if header == nil {
		return ""
	}
	return header.Name
}

func initGradeRepository(ctx context.Context, work, home string) error {
	env := []string{"PATH=/usr/bin:/bin", "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=Independent Pilot Grader", "GIT_COMMITTER_NAME=Independent Pilot Grader",
		"GIT_AUTHOR_EMAIL=grader@example.invalid", "GIT_COMMITTER_EMAIL=grader@example.invalid",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "core.hooksPath=/dev/null", "commit", "-qm", "Frozen private grading baseline"}} {
		cmd := exec.CommandContext(ctx, "/usr/bin/git", args...)
		cmd.Dir, cmd.Env = work, env
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("private grading baseline: %v: %s", err, output)
		}
	}
	return nil
}

func restoreGradeOracle(work string, c gradeCase) error {
	// Candidate-authored tests are reviewed separately. They cannot define the
	// independent oracle or replace a TestMain, npm runner, or test configuration.
	if err := filepath.WalkDir(work, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_test.go") {
			return os.Remove(path)
		}
		return nil
	}); err != nil {
		return err
	}
	for _, name := range c.RemoveControls {
		if !gradeLocalPath(name) {
			return errors.New("invalid frozen control path")
		}
		if err := os.RemoveAll(filepath.Join(work, filepath.FromSlash(name))); err != nil {
			return err
		}
	}
	for _, overlay := range append(slices.Clone(c.Original), c.AcceptanceSources...) {
		destination := filepath.Join(work, filepath.FromSlash(overlay.Destination))
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
		if err := copyRegular(overlay.Source.Path, destination, 0o600|fs.FileMode(overlay.Mode)&0o111); err != nil {
			return err
		}
	}
	return nil
}

func gradeSandboxArgs(m gradeManifest, c gradeCase, directory string, command []string) []string {
	args := []string{"/usr/bin/bwrap", "--unshare-all", "--die-with-parent", "--new-session", "--clearenv",
		"--ro-bind", "/usr", "/usr", "--ro-bind", "/lib", "/lib", "--ro-bind", "/lib64", "/lib64",
		"--symlink", "usr/bin", "/bin", "--symlink", "usr/sbin", "/sbin", "--proc", "/proc", "--dev", "/dev",
		"--tmpfs", "/tmp", "--dir", "/etc", "--ro-bind", "/etc/alternatives", "/etc/alternatives",
		"--ro-bind", "/etc/hosts", "/etc/hosts", "--ro-bind", "/etc/nsswitch.conf", "/etc/nsswitch.conf",
		"--dir", "/home", "--dir", "/opt",
		"--ro-bind", m.GoRoot, "/opt/go", "--bind", filepath.Join(directory, "work"), "/workspace",
		"--bind", filepath.Join(directory, "home"), "/home/grade", "--chdir", "/workspace"}
	if c.GoModCache != "" {
		args = append(args, "--ro-bind", c.GoModCache, "/opt/gomod")
	}
	if m.NodeRoot != "" {
		args = append(args, "--ro-bind", m.NodeRoot, "/opt/node")
	}
	if c.NodeModules != "" {
		args = append(args, "--dir", "/opt/deps", "--ro-bind", c.NodeModules, "/opt/deps/node_modules")
	}
	if m.GoCache != "" {
		args = append(args, "--bind", m.GoCache, "/opt/grade-go-cache")
	}
	for _, pair := range [][2]string{{"HOME", "/home/grade"}, {"PATH", "/opt/go/bin:/opt/node/bin:/usr/bin:/bin"},
		{"LANG", "C.UTF-8"}, {"LC_ALL", "C.UTF-8"}, {"TZ", "UTC"}, {"GOTOOLCHAIN", "local"},
		{"GOMODCACHE", "/opt/gomod"}, {"GOCACHE", "/opt/grade-go-cache"}, {"GOPROXY", "off"}, {"GOSUMDB", "off"},
		{"GOFLAGS", "-mod=readonly -p=2"}, {"GOMAXPROCS", "2"}, {"GOWORK", "off"}, {"GIT_CONFIG_NOSYSTEM", "1"},
		{"GIT_CONFIG_GLOBAL", "/dev/null"}, {"GORACE", "atexit_sleep_ms=0"}} {
		args = append(args, "--setenv", pair[0], pair[1])
	}
	return append(append(args, "--"), command...)
}

func runGradeChecks(commands []command, run func(command) (invocation, error)) (gradeCheckResult, error) {
	result := gradeCheckResult{Passed: len(commands) > 0, Criteria: []gradeCriterion{}, CommandResults: []invocation{}}
	for _, cmd := range commands {
		invoked, err := run(cmd)
		result.CommandResults = append(result.CommandResults, invoked)
		if err != nil {
			return result, err
		}
		result.Passed = result.Passed && invoked.ExitCode == 0 && !invoked.Stopped && invoked.Cleanup == ""
	}
	return result, nil
}

func gradeEvidence(spec gradeAcceptance, commands []invocation) ([]gradeCriterion, bool, error) {
	seen := map[string]bool{}
	paths := map[string]string{}
	for _, cmd := range commands {
		f, err := os.Open(cmd.Stdout.Path)
		if err != nil {
			return nil, false, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64<<10), 16<<20)
		for scanner.Scan() {
			line := scanner.Text()
			switch spec.Kind {
			case "go-json":
				var event struct{ Action, Test string }
				if json.Unmarshal([]byte(line), &event) == nil && event.Test != "" && (event.Action == "pass" || event.Action == "fail") {
					seen[event.Test], paths[event.Test] = event.Action == "pass", cmd.Stdout.Path
				}
			case "json-lines":
				var event struct {
					ID     string `json:"id"`
					Passed bool   `json:"passed"`
				}
				if json.Unmarshal([]byte(line), &event) == nil && event.ID != "" {
					seen[event.ID], paths[event.ID] = event.Passed, cmd.Stdout.Path
				}
			case "marker":
				for _, expected := range spec.Expected {
					if strings.Contains(line, expected) {
						seen[expected], paths[expected] = true, cmd.Stdout.Path
					}
				}
			}
		}
		err = errors.Join(scanner.Err(), f.Close())
		if err != nil {
			return nil, false, err
		}
	}
	criteria, passed := []gradeCriterion{}, len(spec.Expected) > 0
	for _, expected := range spec.Expected {
		criteria = append(criteria, gradeCriterion{expected, expected, seen[expected], paths[expected]})
		passed = passed && seen[expected]
	}
	return criteria, passed, nil
}

func TestGradeArchivePathRejectsEscapesAndHistory(t *testing.T) {
	for _, path := range []string{"../gold", "/private/grader", ".git", ".git/config", "src/../gold", "."} {
		if gradeLocalPath(path) {
			t.Errorf("accepted unsafe archive path %q", path)
		}
	}
	if !gradeLocalPath("src/new.go") {
		t.Fatal("valid source path rejected")
	}
}

func TestGradeEvidenceRequiresEveryBehaviorToRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.jsonl")
	if err := os.WriteFile(path, []byte("{\"Action\":\"pass\",\"Package\":\"example\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := gradeAcceptance{Kind: "go-json", Expected: []string{"TestPilotBehavior"}}
	_, passed, err := gradeEvidence(spec, []invocation{{Stdout: asset{Path: path}}})
	if err != nil || passed {
		t.Fatalf("package pass without actual acceptance execution accepted: %t %v", passed, err)
	}
	if err := os.WriteFile(path, []byte("{\"Action\":\"pass\",\"Test\":\"TestPilotBehavior\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, passed, err = gradeEvidence(spec, []invocation{{Stdout: asset{Path: path}}})
	if err != nil || !passed {
		t.Fatalf("executed behavior was not accepted: %t %v", passed, err)
	}
}

func TestGradeOracleIgnoresCandidateTestAndBuildControlChanges(t *testing.T) {
	work, private := t.TempDir(), t.TempDir()
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(work, "candidate_test.go"), "package subject\nfunc TestMain() { /* skip everything */ }\n")
	write(filepath.Join(work, "original_test.go"), "candidate removed the assertions")
	write(filepath.Join(work, "package.json"), `{"scripts":{"test":"true"}}`)
	write(filepath.Join(work, ".taprc"), "files: []")
	write(filepath.Join(private, "original"), "original independent assertions")
	write(filepath.Join(private, "package"), `{"scripts":{"test":"original-runner"}}`)
	write(filepath.Join(private, "acceptance"), "private acceptance assertions")
	c := gradeCase{RemoveControls: []string{"package.json", ".taprc"},
		Original: []gradeOverlay{
			{Source: asset{Path: filepath.Join(private, "original")}, Destination: "original_test.go"},
			{Source: asset{Path: filepath.Join(private, "package")}, Destination: "package.json"}},
		AcceptanceSources: []gradeOverlay{{Source: asset{Path: filepath.Join(private, "acceptance")}, Destination: "oracle_test.go"}}}
	if err := restoreGradeOracle(work, c); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"candidate_test.go", ".taprc"} {
		if _, err := os.Stat(filepath.Join(work, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("candidate test control survived: %s: %v", name, err)
		}
	}
	for name, expected := range map[string]string{"original_test.go": "original independent assertions",
		"package.json": `{"scripts":{"test":"original-runner"}}`, "oracle_test.go": "private acceptance assertions"} {
		data, err := os.ReadFile(filepath.Join(work, name))
		if err != nil || string(data) != expected {
			t.Fatalf("frozen oracle not restored: %s: %q: %v", name, data, err)
		}
	}
}

func TestGradeCandidateRequiresPrivateAttestationBeforeGit(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	candidate := filepath.Join(root, "run-one")
	if err := os.Mkdir(candidate, 0o700); err != nil {
		t.Fatal(err)
	}
	request := gradeRequest{CandidateWorktree: outside, BaselineTree: "base", CandidateTree: "candidate"}
	if err := trustedGradeCandidate(root, request); err == nil {
		t.Fatal("solver-owned repository outside trusted root accepted")
	}
	request.CandidateWorktree = candidate
	if err := trustedGradeCandidate(root, request); err == nil {
		t.Fatal("repository without a coordinator attestation accepted")
	}
	attestation := gradeCandidateAttestation{Version: 1, RunID: "run-one", TrustedRepository: candidate,
		BaselineTree: "base", CandidateTree: "candidate", Mechanism: "trusted raw reconstruction"}
	if err := writeJSON(filepath.Join(root, "run-one.json"), attestation, true); err != nil {
		t.Fatal(err)
	}
	if err := trustedGradeCandidate(root, request); err != nil {
		t.Fatal(err)
	}
	request.CandidateTree = "another-candidate"
	if err := trustedGradeCandidate(root, request); err == nil {
		t.Fatal("attestation for another candidate accepted")
	}
}

func TestGradeCandidateResolvesAttestedRepositoryAliases(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "trusted")
	candidate := filepath.Join(root, "run-one")
	if err := os.MkdirAll(candidate, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "trusted-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	request := gradeRequest{CandidateWorktree: candidate, BaselineTree: "base", CandidateTree: "candidate"}
	attestation := gradeCandidateAttestation{Version: 1, RunID: "run-one", TrustedRepository: filepath.Join(alias, "run-one"),
		BaselineTree: "base", CandidateTree: "candidate", Mechanism: "trusted raw reconstruction"}
	receipt := filepath.Join(root, "run-one.json")
	if err := writeJSON(receipt, attestation, false); err != nil {
		t.Fatal(err)
	}
	if err := trustedGradeCandidate(alias, request); err != nil {
		t.Fatal("attestation for the same repository through a directory alias rejected", err)
	}
	outside := t.TempDir()
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "another repository", path: outside},
		{name: "relative repository", path: "run-one"},
		{name: "missing repository", path: filepath.Join(root, "missing")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attestation.TrustedRepository = tc.path
			if err := writeJSON(receipt, attestation, false); err != nil {
				t.Fatal(err)
			}
			if err := trustedGradeCandidate(root, request); err == nil {
				t.Fatalf("attestation for %s accepted", tc.name)
			}
		})
	}
	attestation.TrustedRepository = filepath.Join(alias, "run-one")
	if err := writeJSON(receipt, attestation, false); err != nil {
		t.Fatal(err)
	}
	request.CandidateWorktree = outside
	if err := trustedGradeCandidate(alias, request); err == nil {
		t.Fatal("directory alias allowed a candidate outside the trusted root")
	}
}
