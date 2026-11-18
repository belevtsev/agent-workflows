// Package onboarding inspects local agent setup without changing it or running an agent.
package onboarding

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gate"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

// Options identifies the host, working directory and running Kitsu version.
type Options struct {
	Host    string `json:"host"`
	Dir     string `json:"dir"`
	Version string `json:"version"`
}

// Report describes configuration evidence, not authenticated or tested orchestration.
// Ready means no known blocking failure; warnings can still require host review.
type Report struct {
	Host           string     `json:"host"`
	Ready          bool       `json:"ready"`
	HostExecutable string     `json:"host_executable"`
	Checks         []Check    `json:"checks"`
	Repository     *gitx.Repo `json:"-"` // The inspected checkout for interactive startup.
}

// Check is one read-only observation and the actions a user can take next.
type Check struct {
	ID      string   `json:"id"`
	Status  string   `json:"status"`
	Summary string   `json:"summary"`
	Detail  string   `json:"detail"`
	Actions []string `json:"actions"`
}

// Inspect reads bounded local configuration and executable paths. Only read-only
// Git queries run: no host, hook, provider, authentication or model is invoked.
func Inspect(ctx context.Context, options Options) (Report, error) {
	return inspect(ctx, options, systemDir(options.Host))
}

func inspect(ctx context.Context, options Options, system string) (Report, error) {
	if options.Host != "codex" && options.Host != "claude" {
		return Report{}, errors.New("onboarding requires host codex or claude")
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if options.Dir == "" {
		options.Dir = "."
	}
	dir, err := filepath.Abs(options.Dir)
	if err != nil {
		return Report{}, errors.New("cannot locate onboarding directory")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Report{}, errors.New("onboarding directory must be an existing directory")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Report{}, errors.New("cannot locate host configuration home")
	}
	report := Report{Host: options.Host, Ready: true, Checks: []Check{}}
	report.add(cliCheck(options.Version))
	var native bool
	report.HostExecutable, native = HostExecutable(options.Host)
	if report.HostExecutable == "" {
		report.add(Check{ID: "host_cli", Status: "fail", Summary: "Host CLI is missing from absolute PATH entries", Actions: []string{"Install the " + options.Host + " CLI and add its directory to PATH."}})
	} else if !native {
		report.add(Check{ID: "host_cli", Status: "fail", Summary: "Host CLI resolves to a shell wrapper", Detail: "Task launch needs a native executable for direct argument passing.", Actions: []string{"Install a native host .exe, or use kitsu task start --print to copy the prompt into your host."}})
		report.HostExecutable = ""
	} else {
		report.add(Check{ID: "host_cli", Status: "pass", Summary: "Host CLI executable found", Detail: report.HostExecutable + "; availability only, version and authentication were not checked."})
	}
	repo, repoErr := gitx.Open(ctx, dir)
	root := dir
	switch {
	case repoErr == nil && repo.Root != "":
		root, report.Repository = repo.Root, repo
	case repoErr == nil:
		report.add(Check{ID: "checkout", Status: "fail", Summary: "This directory has no working tree", Actions: []string{"Use git worktree list and enter the intended checkout."}})
	case !errors.Is(repoErr, gitx.ErrNotRepo):
		report.add(Check{ID: "checkout", Status: "fail", Summary: "Cannot inspect the Git checkout", Detail: repoErr.Error(), Actions: []string{"Run git status here and resolve the reported error."}})
	}
	state := readSettings(options.Host, home, root, dir, system)
	if state.problem != "" {
		report.add(Check{ID: "host_settings", Status: "fail", Summary: "Host settings cannot be verified", Detail: state.problem, Actions: []string{"Repair the indicated host settings; no settings contents were printed."}})
	}
	plugin, pluginCheck := findPlugin(state, root)
	report.add(pluginCheck)
	report.add(taskCheck(plugin, root, options.Host, state))
	report.add(hooksCheck(plugin, options.Host, state))
	detail := "Hook execution, authentication, model access and live orchestration were not tested. Session flags and remote or device-managed policy can change this result."
	actions := []string{"Open /hooks in a fresh " + options.Host + " session and verify Kitsu's four hook events are loaded."}
	if options.Host == "codex" {
		detail = "Codex hook trust is unknown: enabled plugin hooks still require review and trust of their exact definition hashes. " + detail
		actions = append(actions, "Review each Kitsu hook in /hooks and trust only the definitions you approve; installation alone does not grant trust.")
	}
	report.add(Check{ID: "hook_runtime", Status: "warn", Summary: "Host runtime verification is still required", Detail: detail, Actions: actions})
	report.add(gitCheck(ctx, repo, repoErr))
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func (r *Report) add(check Check) {
	check.Summary, check.Detail = decl.Plain(check.Summary), decl.Plain(check.Detail)
	if check.Actions == nil {
		check.Actions = []string{}
	}
	for i := range check.Actions {
		check.Actions[i] = decl.Plain(check.Actions[i])
	}
	r.Checks = append(r.Checks, check)
	if check.Status == "fail" {
		r.Ready = false
	}
}

func cliCheck(version string) Check {
	check := Check{ID: "kitsu_cli", Status: "pass", Summary: "Kitsu is available on absolute PATH", Detail: "Running Kitsu " + version + "; hooks resolve kitsu through PATH."}
	path := absoluteExecutable("kitsu")
	if path == "" {
		check.Status, check.Summary = "fail", "Kitsu is missing from absolute PATH entries"
		check.Actions = []string{"Install Kitsu or add its installed directory to PATH; a checkout or go run alone does not make hooks usable."}
		return check
	}
	check.Detail += " Found " + path + "."
	running, err := os.Executable()
	current, currentErr := os.Stat(running)
	installed, installedErr := os.Stat(path)
	if err != nil || currentErr != nil || installedErr != nil || !os.SameFile(current, installed) {
		check.Status, check.Summary = "warn", "Hooks may use a different Kitsu executable"
		check.Detail += " It differs from this running executable; its version was not executed or verified."
		check.Actions = []string{"Run the installed kitsu agent check command to check the same executable that hooks will use."}
	}
	return check
}

func absoluteExecutable(name string) string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.IsAbs(dir) {
			if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil && filepath.IsAbs(path) {
				return path
			}
		}
	}
	return ""
}

// HostExecutable resolves a host from absolute PATH entries and identifies
// whether it can be used for direct argument passing.
func HostExecutable(host string) (path string, native bool) {
	path = absoluteExecutable(host)
	return path, path != "" && (runtime.GOOS != "windows" || strings.EqualFold(filepath.Ext(path), ".exe"))
}

func gitCheck(ctx context.Context, repo *gitx.Repo, err error) Check {
	check := Check{ID: "git_gate", Status: "warn", Summary: "Optional Git gate is not verified as enforcing", Detail: "The agent workflow does not require the Git gate.", Actions: []string{"For optional Git commit and push protection, declare checks in kitsu.toml and run kitsu init in the repository."}}
	if err != nil || repo.Root == "" {
		check.Summary = "Optional Git gate is unavailable here"
		check.Detail += " No working Git repository was detected; project-local task records need a working project directory."
		return check
	}
	_, declarationErr := readFile(filepath.Join(repo.Root, "kitsu.toml"))
	if declarationErr == nil && configuredGate(ctx, repo) {
		check.Status, check.Summary = "pass", "Optional Git gate files are installed in enforce mode"
		check.Detail += " Declared checks and their evidence were not run or verified."
		check.Actions = []string{"Use kitsu status to inspect evidence separately."}
	}
	return check
}

func configuredGate(ctx context.Context, repo *gitx.Repo) bool {
	mode, err := readFile(filepath.Join(repo.CommonDir, "kitsu", "mode"))
	if err != nil && !missing(err) || strings.TrimSuffix(string(mode), "\n") == "observe" {
		return false
	}
	dir, _, err := repo.HooksDir(ctx)
	if err != nil {
		return false
	}
	for _, name := range gate.Hooks {
		path := filepath.Join(dir, name)
		data, err := readFile(path)
		info, statErr := os.Stat(path)
		if err != nil || statErr != nil || string(data) != gate.Script(name) || runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			return false
		}
	}
	return true
}

func readFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("file must be regular and at most 1 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if len(data) > 1<<20 {
		err = errors.New("file exceeds 1 MiB")
	}
	return data, err
}

func missing(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func expand(path, home, base string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path)
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func systemDir(host string) string {
	if host == "codex" {
		return "/etc/codex"
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	default:
		return "/etc/claude-code"
	}
}

func issue(path, summary string) string { return fmt.Sprintf("%s: %s", path, summary) }
