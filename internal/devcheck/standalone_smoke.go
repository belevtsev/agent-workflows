package devcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

type standaloneSmokeHome struct {
	root, home, codex, claude, state, config, work, tools string
	originalCodex, originalClaude                         []byte
	registrations                                         map[string]string
}
type standaloneSmokeCapture struct {
	mutex          sync.Mutex
	stdout, stderr []byte
	exceeded       bool
}
type standaloneSmokeStream struct {
	capture *standaloneSmokeCapture
	stderr  bool
}

func (stream standaloneSmokeStream) Write(data []byte) (int, error) {
	stream.capture.mutex.Lock()
	defer stream.capture.mutex.Unlock()
	if len(stream.capture.stdout)+len(stream.capture.stderr)+len(data) > 1<<20 {
		stream.capture.exceeded = true
		return 0, errors.New("output-limit")
	}
	if stream.stderr {
		stream.capture.stderr = append(stream.capture.stderr, data...)
	} else {
		stream.capture.stdout = append(stream.capture.stdout, data...)
	}
	return len(data), nil
}
func standaloneSmokeMap(value any) map[string]any { result, _ := value.(map[string]any); return result }
func standaloneSmokeWrite(path string, data []byte, mode os.FileMode) error {
	if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, data, mode) != nil {
		return errors.New("fixture-write")
	}
	return nil
}
func standaloneSmokeJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	var result map[string]any
	if err != nil || json.Unmarshal(data, &result) != nil || result == nil {
		return nil, errors.New("json-file")
	}
	return result, nil
}
func (home *standaloneSmokeHome) standaloneSmokeRun(ctx context.Context, binary string, arguments ...string) ([]byte, bool, error) {
	child, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	capture := &standaloneSmokeCapture{}
	command := exec.CommandContext(child, binary, arguments...)
	command.Dir = home.work
	command.Env = []string{"HOME=" + home.home, "CODEX_HOME=" + home.codex, "CLAUDE_CONFIG_DIR=" + home.claude, "XDG_CONFIG_HOME=" + filepath.Join(home.root, "xdg-config"), "XDG_DATA_HOME=" + filepath.Join(home.root, "xdg-data"), "XDG_STATE_HOME=" + filepath.Join(home.root, "xdg-state"), "XDG_CACHE_HOME=" + filepath.Join(home.root, "xdg-cache"), "PATH=" + home.tools + ":/usr/bin:/bin", "TERM=dumb", "LC_ALL=C", "SSL_CERT_FILE=" + filepath.Join(home.root, "no-network-ca.pem"), "TMPDIR=" + filepath.Join(home.root, "tmp"), "TMP=" + filepath.Join(home.root, "tmp"), "TEMP=" + filepath.Join(home.root, "tmp")}
	command.Stdin = strings.NewReader("")
	command.Stdout = standaloneSmokeStream{capture: capture}
	command.Stderr = standaloneSmokeStream{capture: capture, stderr: true}
	command.WaitDelay = time.Second
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
	runErr := command.Run()
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	if child.Err() != nil {
		return nil, false, errors.New("child-deadline")
	}
	if capture.exceeded {
		return nil, false, errors.New("output-limit")
	}
	if bytes.Contains(capture.stdout, []byte{27}) || bytes.Contains(capture.stderr, []byte{27}) {
		return nil, false, errors.New("unexpected-ui")
	}
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return nil, false, errors.New("child-start")
		}
		return capture.stdout, false, nil
	}
	return capture.stdout, true, nil
}
func (home *standaloneSmokeHome) standaloneSmokeArgs(action string, rooted bool, extra ...string) []string {
	arguments := []string{}
	if action != "" {
		arguments = append(arguments, action)
	}
	arguments = append(arguments, "--json", "--non-interactive")
	if rooted {
		arguments = append(arguments, "--home", home.home, "--codex-home", home.codex, "--claude-home", home.claude, "--state-dir", home.state, "--config-dir", home.config)
	}
	return append(arguments, extra...)
}
func (home *standaloneSmokeHome) standaloneSmokeAction(ctx context.Context, binary, action string, rooted bool, extra ...string) (map[string]any, error) {
	data, success, err := home.standaloneSmokeRun(ctx, binary, home.standaloneSmokeArgs(action, rooted, extra...)...)
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, errors.New("action-failed")
	}
	expected := action
	if expected == "" {
		expected = "setup"
	}
	var result map[string]any
	if json.Unmarshal(data, &result) != nil || result["action"] != expected {
		return nil, errors.New("action-json")
	}
	actionResult, resultObject := result["result"].(map[string]any)
	if !resultObject || actionResult == nil {
		return nil, errors.New("result-object")
	}
	status, statusObject := result["status"].(map[string]any)
	if !statusObject || status == nil {
		return nil, errors.New("status-object")
	}
	committed, present := actionResult["configuration_committed"]
	if _, valid := committed.(bool); !valid && (present || expected == "install" || expected == "setup" || expected == "update") {
		return nil, errors.New("commit-indicator")
	}
	if _, valid := status["installed"].(bool); !valid {
		return nil, errors.New("installed-indicator")
	}
	return result, nil
}
func (home *standaloneSmokeHome) standaloneSmokePreserved(original bool) error {
	for _, fixture := range []struct {
		path               string
		original, sentinel []byte
	}{{filepath.Join(home.codex, "config.toml"), home.originalCodex, []byte("unrelated = \"smoke  sentinel\"\n")}, {filepath.Join(home.claude, "settings.json"), home.originalClaude, []byte("  \"smoke_unrelated\": { \"spacing\" : \"keep\" }")}} {
		data, err := os.ReadFile(fixture.path)
		info, statErr := os.Stat(fixture.path)
		if err != nil || statErr != nil || info.Mode().Perm() != 0640 || !bytes.Contains(data, fixture.sentinel) || (original && !bytes.Equal(data, fixture.original)) {
			return errors.New("settings-preservation")
		}
	}
	for _, directory := range []string{filepath.Join(home.home, ".agents", "skills"), filepath.Join(home.claude, "skills")} {
		path := filepath.Join(directory, "unmanaged-smoke", "SKILL.md")
		data, err := os.ReadFile(path)
		info, statErr := os.Stat(path)
		directoryInfo, directoryErr := os.Stat(filepath.Dir(path))
		if err != nil || statErr != nil || directoryErr != nil || info.Mode().Perm() != 0600 || directoryInfo.Mode().Perm() != 0700 || string(data) != "unmanaged smoke\n" {
			return errors.New("unmanaged-preservation")
		}
	}
	data, err := os.ReadFile(filepath.Join(home.home, ".bashrc"))
	info, statErr := os.Stat(filepath.Join(home.home, ".bashrc"))
	if err != nil || statErr != nil || info.Mode().Perm() != 0600 || !bytes.Contains(data, []byte(": standalone-smoke-sentinel\n")) {
		return errors.New("shell-preservation")
	}
	if _, err := os.Stat(filepath.Join(home.root, "forbidden-tools")); !os.IsNotExist(err) {
		return errors.New("git-go-invocation")
	}
	return nil
}
func (home *standaloneSmokeHome) standaloneSmokeVerify() error {
	codexData, err := os.ReadFile(filepath.Join(home.codex, "config.toml"))
	var codex map[string]any
	if err != nil || toml.Unmarshal(codexData, &codex) != nil {
		return errors.New("codex-toml")
	}
	claude, err := standaloneSmokeJSON(filepath.Join(home.claude, "settings.json"))
	if err != nil {
		return err
	}
	if codex["model"] != "gpt-6.1-sol" || codex["model_reasoning_effort"] != "max" || codex["model_context_window"] != int64(512000) || claude["model"] != "claude-opus-5-5" || claude["effortLevel"] != "xhigh" {
		return errors.New("native-defaults")
	}
	memories := standaloneSmokeMap(codex["memories"])
	if standaloneSmokeMap(codex["features"])["memories"] != true || memories["generate_memories"] != true || memories["use_memories"] != true || memories["extract_model"] != "gpt-6.1-sol" || memories["consolidation_model"] != "gpt-6.1-sol" {
		return errors.New("memory-defaults")
	}
	state, err := standaloneSmokeJSON(filepath.Join(home.state, "state.json"))
	if err != nil {
		return err
	}
	if state["version"] != float64(4) || state["config_dir"] != home.config || standaloneSmokeMap(state["manager"])["version"] != float64(2) {
		return errors.New("state-contract")
	}
	current := filepath.Join(home.state, "current")
	info, err := os.Lstat(current)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return errors.New("active-symlink")
	}
	for _, host := range []struct{ file, model, effort, boundedModel, boundedEffort, effortKey string }{{"model-policy.yaml", "gpt-6.1-sol", "max", "gpt-6-luna", "high", "reasoning_effort"}, {"model-policy.claude.yaml", "claude-opus-5-5", "xhigh", "claude-sonnet-5-5", "medium", "effort"}} {
		data, readErr := os.ReadFile(filepath.Join(current, "skills", "task-orchestration", host.file))
		var policy map[string]any
		if readErr != nil || yaml.Unmarshal(data, &policy) != nil {
			return errors.New("policy-yaml")
		}
		roles, profiles := standaloneSmokeMap(policy["roles"]), standaloneSmokeMap(policy["profiles"])
		for _, role := range []string{"coordinator", "commit_execution", "pr_authoring_routine", "pr_submission"} {
			name, _ := roles[role].(string)
			profile := standaloneSmokeMap(profiles[name])
			model, effort := host.model, host.effort
			if role != "coordinator" {
				model, effort = host.boundedModel, host.boundedEffort
			}
			if profile["model"] != model || profile[host.effortKey] != effort {
				return errors.New("policy-role")
			}
		}
	}
	for _, directory := range []string{filepath.Join(home.home, ".agents", "skills"), filepath.Join(home.claude, "skills")} {
		for _, skill := range []string{"task-orchestration", "test-strategy"} {
			path := filepath.Join(directory, skill)
			link, linkErr := os.Lstat(path)
			resolved, resolveErr := filepath.EvalSymlinks(path)
			info, statErr := os.Stat(resolved)
			if linkErr != nil || resolveErr != nil || statErr != nil || link.Mode()&os.ModeSymlink == 0 || !info.IsDir() {
				return errors.New("critical-skill-link")
			}
		}
	}
	if err := home.standaloneSmokeInventory(); err != nil {
		return err
	}
	return home.standaloneSmokePreserved(false)
}
func (home *standaloneSmokeHome) standaloneSmokeSnapshot() (map[string]string, error) {
	result := map[string]string{}
	err := filepath.WalkDir(home.root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("snapshot-walk")
		}
		if entry.IsDir() {
			result[path] = "directory"
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return errors.New("snapshot-link")
			}
			result[path] = "link:" + target
			return nil
		}
		data, err := os.ReadFile(path)
		info, statErr := entry.Info()
		if err != nil || statErr != nil {
			return errors.New("snapshot-read")
		}
		result[path] = fmt.Sprint(info.Mode().Perm()) + ":" + string(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// standaloneSmokeRegistrations reads the subject inventory only when the coordinator executes this helper.
func standaloneSmokeRegistrations(source string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(source, "skills-manifest.json"))
	if err != nil {
		return nil, errors.New("skill-manifest-read")
	}
	var manifest struct {
		Version       int               `json:"version"`
		Registrations map[string]string `json:"registrations"`
	}
	if json.Unmarshal(data, &manifest) != nil || manifest.Version != 3 || len(manifest.Registrations) == 0 {
		return nil, errors.New("skill-manifest-schema")
	}
	for name, relative := range manifest.Registrations {
		local := filepath.FromSlash(relative)
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || !filepath.IsLocal(local) || local == "." || strings.Contains(relative, `\`) || filepath.ToSlash(filepath.Clean(local)) != relative {
			return nil, errors.New("skill-manifest-registration")
		}
	}
	if _, conflict := manifest.Registrations["unmanaged-smoke"]; conflict {
		return nil, errors.New("skill-manifest-fixture-collision")
	}
	return manifest.Registrations, nil
}
func standaloneSmokeWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func (home *standaloneSmokeHome) standaloneSmokeInventory() error {
	current := filepath.Join(home.state, "current")
	physicalCurrent, err := filepath.EvalSymlinks(current)
	if err != nil {
		return errors.New("skill-inventory-current")
	}
	for _, host := range []struct{ name, directory string }{{"codex", filepath.Join(home.home, ".agents", "skills")}, {"claude", filepath.Join(home.claude, "skills")}} {
		fail := func(category string) error { return fmt.Errorf("skill-inventory-%s-%s", host.name, category) }
		for name, relative := range home.registrations {
			path := filepath.Join(host.directory, name)
			link, linkErr := os.Lstat(path)
			if os.IsNotExist(linkErr) {
				return fail("missing-registration")
			}
			if linkErr != nil || link.Mode()&os.ModeSymlink == 0 {
				return fail("invalid-registration-link")
			}
			resolved, resolveErr := filepath.EvalSymlinks(path)
			info, statErr := os.Stat(resolved)
			if resolveErr != nil || statErr != nil || !info.IsDir() {
				return fail("broken-registration")
			}
			expected := filepath.Join(physicalCurrent, filepath.FromSlash(relative))
			if resolved != expected {
				return fail("registration-destination")
			}
		}
		entries, err := os.ReadDir(host.directory)
		if err != nil {
			return fail("read-directory")
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			path := filepath.Join(host.directory, entry.Name())
			resolved, resolveErr := filepath.EvalSymlinks(path)
			if resolveErr != nil {
				target, readErr := os.Readlink(path)
				if readErr != nil {
					return fail("read-link")
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(host.directory, target)
				}
				if standaloneSmokeWithin(current, target) || standaloneSmokeWithin(physicalCurrent, target) {
					return fail("broken-managed-registration")
				}
				continue
			}
			if standaloneSmokeWithin(physicalCurrent, resolved) {
				if _, registered := home.registrations[entry.Name()]; !registered {
					return fail("extra-managed-registration")
				}
			}
		}
	}
	return nil
}

func StandaloneNativeSmoke(ctx context.Context, source, binary string, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	absolute, err := filepath.Abs(binary)
	if err != nil {
		return errors.New("R-N1/binary-path")
	}
	binary = absolute
	root, err := os.MkdirTemp("", "standalone smoke ")
	if err != nil {
		return errors.New("R-N1/workspace")
	}
	defer func() { _ = os.RemoveAll(root) }()
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return errors.New("R-N1/workspace-physical")
	}
	root = physicalRoot
	registrations, err := standaloneSmokeRegistrations(source)
	if err != nil {
		return fmt.Errorf("R-N1/%s", err.Error())
	}
	home := &standaloneSmokeHome{registrations: registrations, root: root, home: filepath.Join(root, "home with spaces"), codex: filepath.Join(root, "codex with spaces"), claude: filepath.Join(root, "claude with spaces"), state: filepath.Join(root, "state with spaces"), config: filepath.Join(root, "config with spaces"), work: filepath.Join(root, "unrelated cwd"), tools: filepath.Join(root, "tools")}
	home.originalCodex = []byte("unrelated = \"smoke  sentinel\"\nmodel = \"original-smoke\"\nmodel_reasoning_effort = \"low\"\n")
	home.originalClaude = []byte("{\n  \"model\": \"original-smoke\",\n  \"effortLevel\": \"low\",\n  \"smoke_unrelated\": { \"spacing\" : \"keep\" }\n}\n")
	for _, directory := range []string{home.home, home.codex, home.claude, home.work, home.tools, filepath.Join(root, "tmp")} {
		if os.MkdirAll(directory, 0700) != nil {
			return errors.New("R-N1/fixture-directory")
		}
	}
	for _, fixture := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{{filepath.Join(home.codex, "config.toml"), home.originalCodex, 0640}, {filepath.Join(home.claude, "settings.json"), home.originalClaude, 0640}, {filepath.Join(home.home, ".bashrc"), []byte(": standalone-smoke-sentinel\n"), 0600}, {filepath.Join(home.home, ".agents", "skills", "unmanaged-smoke", "SKILL.md"), []byte("unmanaged smoke\n"), 0600}, {filepath.Join(home.claude, "skills", "unmanaged-smoke", "SKILL.md"), []byte("unmanaged smoke\n"), 0600}, {filepath.Join(root, "no-network-ca.pem"), []byte("synthetic empty trust store: network not permitted\n"), 0600}} {
		if err := standaloneSmokeWrite(fixture.path, fixture.data, fixture.mode); err != nil {
			return errors.New("R-N1/fixture-write")
		}
	}
	marker := "'" + strings.ReplaceAll(filepath.Join(root, "forbidden-tools"), "'", "'\\''") + "'"
	for _, tool := range []string{"git", "go"} {
		if err := standaloneSmokeWrite(filepath.Join(home.tools, tool), []byte("#!/bin/sh\nprintf 'called\\n' >> "+marker+"\nexit 93\n"), 0700); err != nil {
			return errors.New("R-N1/tool-wrapper")
		}
	}
	installed := filepath.Join(home.home, ".local", "bin", "cw")
	versionPattern := regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	bash, zsh := "", "missing"
	for _, path := range []string{"/bin/bash", "/usr/bin/bash"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			bash = path
			break
		}
	}
	for _, path := range []string{"/bin/zsh", "/usr/bin/zsh"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			zsh = "available-not-exercised"
			break
		}
	}
	cases := []struct {
		id  string
		run func() error
	}{
		{"R-N1/version", func() error {
			data, success, err := home.standaloneSmokeRun(ctx, binary, "version")
			var version map[string]any
			if err != nil {
				return err
			}
			if !success || json.Unmarshal(data, &version) != nil || !versionPattern.MatchString(fmt.Sprint(version["version"])) {
				return errors.New("stable-version")
			}
			return nil
		}},
		{"R-N1/dry-run", func() error {
			before, err := home.standaloneSmokeSnapshot()
			if err != nil {
				return err
			}
			result, err := home.standaloneSmokeAction(ctx, binary, "install", true, "--host", "all", "--preset", "recommended", "--shell", "bash", "--dry-run")
			if err != nil {
				return err
			}
			if standaloneSmokeMap(result["result"])["configuration_committed"] != false || standaloneSmokeMap(result["status"])["installed"] != false {
				return errors.New("dry-run-outcome")
			}
			after, err := home.standaloneSmokeSnapshot()
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(before, after) {
				return errors.New("dry-run-persistent-write")
			}
			return nil
		}},
		{"R-N1/install-repeat", func() error {
			for _, action := range []string{"install", ""} {
				result, err := home.standaloneSmokeAction(ctx, binary, action, true, "--host", "all", "--preset", "recommended", "--shell", "bash")
				if err != nil {
					return err
				}
				if standaloneSmokeMap(result["status"])["installed"] != true {
					return errors.New("install-outcome")
				}
				if err := home.standaloneSmokeVerify(); err != nil {
					return err
				}
			}
			return nil
		}},
		{"R-N1/status-locator-bash", func() error {
			for _, fixture := range []struct {
				binary string
				rooted bool
			}{{binary, true}, {installed, false}} {
				result, err := home.standaloneSmokeAction(ctx, fixture.binary, "status", fixture.rooted)
				if err != nil {
					return err
				}
				if standaloneSmokeMap(result["status"])["installed"] != true {
					return errors.New("locator-status")
				}
			}
			if bash == "" {
				return errors.New("bash-unavailable")
			}
			data, success, err := home.standaloneSmokeRun(ctx, bash, "--noprofile", "--rcfile", filepath.Join(home.home, ".bashrc"), "-ic", "command -v cw")
			if err != nil {
				return err
			}
			if !success || strings.TrimSpace(string(data)) != installed {
				return errors.New("bash-startup-path")
			}
			return home.standaloneSmokePreserved(false)
		}},
		{"R-N1/drift", func() error {
			path := filepath.Join(home.codex, "config.toml")
			prior, err := os.ReadFile(path)
			if err != nil {
				return errors.New("drift-read")
			}
			expression := regexp.MustCompile(`(?m)^model[ \t]*=[^\r\n]*`)
			if len(expression.FindAll(prior, -1)) != 1 {
				return errors.New("drift-fixture-shape")
			}
			if err := standaloneSmokeWrite(path, expression.ReplaceAll(prior, []byte(`model = "smoke-user-change"`)), 0640); err != nil {
				return err
			}
			before, err := home.standaloneSmokeSnapshot()
			if err != nil {
				return err
			}
			_, success, runErr := home.standaloneSmokeRun(ctx, binary, home.standaloneSmokeArgs("install", true, "--host", "all", "--shell", "bash")...)
			after, snapshotErr := home.standaloneSmokeSnapshot()
			restoreErr := standaloneSmokeWrite(path, prior, 0640)
			if runErr != nil {
				return runErr
			}
			if snapshotErr != nil {
				return snapshotErr
			}
			if success || !reflect.DeepEqual(before, after) {
				return errors.New("drift-refusal-preservation")
			}
			return restoreErr
		}},
		{"R-N1/recover-no-pending", func() error {
			result, err := home.standaloneSmokeAction(ctx, installed, "recover", false)
			if err != nil {
				return err
			}
			if standaloneSmokeMap(result["status"])["installed"] != true {
				return errors.New("recover-no-pending-outcome")
			}
			return home.standaloneSmokeVerify()
		}},
		{"R-N1/uninstall", func() error {
			result, err := home.standaloneSmokeAction(ctx, installed, "uninstall", false)
			if err != nil {
				return err
			}
			if standaloneSmokeMap(result["status"])["installed"] != false {
				return errors.New("uninstall-outcome")
			}
			if err := home.standaloneSmokePreserved(true); err != nil {
				return err
			}
			if _, err := os.Lstat(installed); !os.IsNotExist(err) {
				return errors.New("locator-remains")
			}
			for _, directory := range []string{filepath.Join(home.home, ".agents", "skills"), filepath.Join(home.claude, "skills")} {
				for _, skill := range []string{"task-orchestration", "test-strategy"} {
					if _, err := os.Lstat(filepath.Join(directory, skill)); !os.IsNotExist(err) {
						return errors.New("managed-skill-remains")
					}
				}
				for skill := range home.registrations {
					if _, err := os.Lstat(filepath.Join(directory, skill)); !os.IsNotExist(err) {
						return errors.New("registered-skill-remains")
					}
				}
			}
			return nil
		}},
	}
	for _, testCase := range cases {
		if ctx.Err() != nil {
			return fmt.Errorf("%s/smoke-deadline", testCase.id)
		}
		if err := testCase.run(); err != nil {
			return fmt.Errorf("%s/%s", testCase.id, err.Error())
		}
		if output != nil {
			if _, err := fmt.Fprintf(output, "%s success\n", testCase.id); err != nil {
				return errors.New("R-N1/report-write")
			}
		}
	}
	if output != nil {
		if _, err := fmt.Fprintf(output, "R-N1 success; bash=exercised; zsh=%s; full_inventory=verified; update_rollback_migration=not-claimed\n", zsh); err != nil {
			return errors.New("R-N1/report-write")
		}
	}
	return nil
}
