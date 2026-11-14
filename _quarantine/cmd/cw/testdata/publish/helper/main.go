// Command helper is the stand-in program of cmd/cw/publish_e2e_test.go. The
// test builds it once and installs copies or hard links under several names;
// the base name of the running executable selects the role:
//
//   - git: replaces itself with the real git named by "exec" in <exe>.json,
//     so a test PATH can hold git without the directories around it. When
//     $PUBE2E_GIT_LOG names a file, it first appends one JSON line with its
//     arguments, working directory and GIT_* environment, and, when
//     $PUBE2E_TASK_DIR is set, the publication files present at that moment.
//   - true: exits 0 (the task check of the test plans).
//   - pubappend FILE TEXT and pubtouch FILE...: append to or create files,
//     for side effects the fake gh runs at a chosen point of a cw run.
//   - gh: a fake GitHub CLI that keeps pull-request state in the JSON file
//     named by $PUBE2E_GH_STATE and appends one JSON line per call to the file
//     named by $PUBE2E_GH_LOG.
//   - any other name: a Git hook configured by <exe>.json (see hookConf).
//
// It uses the standard library only and is never part of cw.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// prFields is the --json list of the fake's list and view commands.
const prFields = "number,url,state,headRefName,headRefOid,baseRefName,title,body,isCrossRepository"

// staleOID is the headRefOid the fake reports while a view is stale.
const staleOID = "1111111111111111111111111111111111111111"

// blockLimit bounds how long a blocking role waits for its release file.
const blockLimit = 3 * time.Minute

func main() {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	role := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	switch role {
	case "git":
		os.Exit(runGit(exe))
	case "true":
		os.Exit(0)
	case "gh":
		os.Exit(runGH())
	case "pubappend":
		// pubappend FILE TEXT appends TEXT to FILE.
		if len(os.Args) != 3 {
			os.Exit(fail("pubappend FILE TEXT"))
		}
		f, err := os.OpenFile(os.Args[1], os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			os.Exit(fail("pubappend: %v", err))
		}
		_, werr := f.WriteString(os.Args[2])
		if cerr := f.Close(); werr != nil || cerr != nil {
			os.Exit(fail("pubappend: %v %v", werr, cerr))
		}
		os.Exit(0)
	case "pubtouch":
		// pubtouch FILE... creates each FILE empty.
		for _, p := range os.Args[1:] {
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				os.Exit(fail("pubtouch: %v", err))
			}
		}
		os.Exit(0)
	default:
		os.Exit(runHook(exe, role))
	}
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "pube2e helper: "+format+"\n", args...)
	return 97
}

// repoVars are the variables a nested git command must not inherit from a
// hook or from cw, so it addresses the repository it names.
var repoVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE",
	"GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM", "GIT_PREFIX",
	"GIT_QUARANTINE_PATH", "GIT_PUSH_OPTION_COUNT", "GIT_REFLOG_ACTION",
}

func nestedEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(repoVars, name) || strings.HasPrefix(name, "GIT_PUSH_OPTION_") || name == "PUBE2E_GIT_LOG" || name == "PUBE2E_TASK_DIR" {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// hookEnv is the hook's environment without the test's logging variables,
// for run_hook_env: a git command run with it addresses what the hook's git
// addresses, such as the index git commit is about to commit
// (GIT_INDEX_FILE), as a formatting hook that re-stages files does.
func hookEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "PUBE2E_GIT_LOG" || name == "PUBE2E_TASK_DIR" {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func runGit(exe string) int {
	var conf struct {
		Exec string `json:"exec"`
	}
	data, err := os.ReadFile(exe + ".json")
	if err != nil {
		return fail("git wrapper: %v", err)
	}
	if err := json.Unmarshal(data, &conf); err != nil || conf.Exec == "" {
		return fail("git wrapper: bad config %q: %v", data, err)
	}
	if logPath := os.Getenv("PUBE2E_GIT_LOG"); logPath != "" {
		cwd, _ := os.Getwd()
		var env []string
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "GIT_") {
				env = append(env, kv)
			}
		}
		entry := callLog{Role: "git", Args: os.Args[1:], Cwd: cwd, Pid: os.Getpid(), Env: env}
		if dir := os.Getenv("PUBE2E_TASK_DIR"); dir != "" {
			entry.PubFiles = publicationFiles(dir)
		}
		if err := appendLog(logPath, entry); err != nil {
			return fail("git wrapper: log: %v", err)
		}
	}
	argv := append([]string{conf.Exec}, os.Args[1:]...)
	if err := syscall.Exec(conf.Exec, argv, os.Environ()); err != nil {
		return fail("git wrapper: exec %s: %v", conf.Exec, err)
	}
	return 0
}

// hookConf configures one hook. Every action is optional and runs in the
// order of the fields.
type hookConf struct {
	Log        string      `json:"log"`          // append one JSON line per call
	Probe      []string    `json:"probe"`        // paths whose existence the log line records
	Create     []string    `json:"create"`       // files to create
	Copy       [][2]string `json:"copy"`         // copy [0] over [1]
	Run        [][]string  `json:"run"`          // commands run with repository variables removed
	RunHookEnv [][]string  `json:"run_hook_env"` // commands run with the hook's own environment (see hookEnv)
	AppendArg1 string      `json:"append_arg1"`  // text appended to the file named by the first argument
	Started    string      `json:"started"`      // created, then the hook waits for Release
	Release    string      `json:"release"`
	Exit       int         `json:"exit"`
}

type callLog struct {
	Role   string          `json:"role"`
	Args   []string        `json:"args"`
	Stdin  string          `json:"stdin"`
	Cwd    string          `json:"cwd"`
	Pid    int             `json:"pid"`
	Env    []string        `json:"env"`
	Probe  map[string]bool `json:"probe,omitempty"`
	Result int             `json:"result"`
	// PubFiles lists the publication-*.json files of $PUBE2E_TASK_DIR when
	// the git wrapper ran, so a test sees which entries existed before each
	// git command.
	PubFiles []string `json:"pubfiles,omitempty"`
}

func publicationFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{"unreadable: " + err.Error()}
	}
	names := []string{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "publication-") {
			names = append(names, e.Name())
		}
	}
	return names
}

func appendLog(path string, entry callLog) error {
	if path == "" {
		return nil
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func waitRelease(started, release string) {
	if started != "" {
		_ = os.WriteFile(started, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
	}
	deadline := time.Now().Add(blockLimit)
	for time.Now().Before(deadline) {
		if release != "" {
			if _, err := os.Stat(release); err == nil {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runHook(exe, role string) int {
	data, err := os.ReadFile(exe + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		return fail("hook %s: %v", role, err)
	}
	var conf hookConf
	if err := json.Unmarshal(data, &conf); err != nil {
		return fail("hook %s: bad config: %v", role, err)
	}
	var stdin []byte
	switch role {
	case "pre-push", "pre-receive", "post-receive", "update", "reference-transaction":
		stdin, _ = io.ReadAll(os.Stdin)
	}
	cwd, _ := os.Getwd()
	entry := callLog{Role: role, Args: os.Args[1:], Stdin: string(stdin), Cwd: cwd, Pid: os.Getpid(), Env: os.Environ(), Result: conf.Exit}
	if len(conf.Probe) > 0 {
		entry.Probe = map[string]bool{}
		for _, p := range conf.Probe {
			_, err := os.Lstat(p)
			entry.Probe[p] = err == nil
		}
	}
	if err := appendLog(conf.Log, entry); err != nil {
		return fail("hook %s: log: %v", role, err)
	}
	for _, p := range conf.Create {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			return fail("hook %s: create %s: %v", role, p, err)
		}
	}
	for _, c := range conf.Copy {
		b, err := os.ReadFile(c[0])
		if err != nil {
			return fail("hook %s: copy: %v", role, err)
		}
		if err := os.WriteFile(c[1], b, 0o644); err != nil {
			return fail("hook %s: copy: %v", role, err)
		}
	}
	for _, argv := range conf.Run {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Env = nestedEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			return fail("hook %s: run %q: %v\n%s", role, argv, err, out)
		}
	}
	for _, argv := range conf.RunHookEnv {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Env = hookEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			return fail("hook %s: run_hook_env %q: %v\n%s", role, argv, err, out)
		}
	}
	if conf.AppendArg1 != "" && len(os.Args) > 1 {
		f, err := os.OpenFile(os.Args[1], os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return fail("hook %s: %v", role, err)
		}
		_, werr := f.WriteString(conf.AppendArg1)
		if cerr := f.Close(); werr != nil || cerr != nil {
			return fail("hook %s: append: %v %v", role, werr, cerr)
		}
	}
	if conf.Started != "" || conf.Release != "" {
		waitRelease(conf.Started, conf.Release)
	}
	return conf.Exit
}

// ghPR is one pull request of the fake. HeadRefOid empty means "read the head
// branch from the remote repository", as GitHub reports the pushed head.
type ghPR struct {
	Number            int    `json:"number"`
	URL               string `json:"url"`
	State             string `json:"state"`
	HeadRefName       string `json:"headRefName"`
	HeadRefOid        string `json:"headRefOid"`
	BaseRefName       string `json:"baseRefName"`
	Title             string `json:"title"`
	Body              string `json:"body"`
	IsCrossRepository bool   `json:"isCrossRepository"`
}

// ghState is the fake's whole state and behaviour, read and written on every
// call.
type ghState struct {
	Repo              string     `json:"repo"`           // OWNER/NAME the fake serves
	RepoURL           string     `json:"repo_url"`       // repo view url
	SSHURL            string     `json:"ssh_url"`        // repo view sshUrl
	RemoteGitDir      string     `json:"remote_git_dir"` // bare repository of the head branches
	NextNumber        int        `json:"next_number"`
	PRs               []ghPR     `json:"prs"`
	RepoMode          string     `json:"repo_mode"`   // "", "fail", "block"
	ListMode          string     `json:"list_mode"`   // "", "fail"
	CreateMode        string     `json:"create_mode"` // "", "fail-before", "fail-after", "block"
	EditMode          string     `json:"edit_mode"`   // "", "fail-before", "fail-after", "block"
	ViewMode          string     `json:"view_mode"`   // "", "fail"
	CreateTitleSuffix string     `json:"create_title_suffix"`
	BodyLF            bool       `json:"body_lf"`         // see storedBody
	BodyPrefix        string     `json:"body_prefix"`     // see storedBody
	BodySuffix        string     `json:"body_suffix"`     // see storedBody
	ViewSetsState     string     `json:"view_sets_state"` // stored as the viewed PR's state before a view reads it
	HideNewLists      int        `json:"hide_new_lists"`  // list calls after a create that omit the new PR
	HiddenNumber      int        `json:"hidden_number"`
	StaleViews        int        `json:"stale_views"` // views that report a stale headRefOid
	OnList            [][]string `json:"on_list"`     // run at the next list call, then cleared
	Started           string     `json:"started"`
	Release           string     `json:"release"`
}

func runGH() int {
	statePath := os.Getenv("PUBE2E_GH_STATE")
	stdin, _ := io.ReadAll(os.Stdin)
	cwd, _ := os.Getwd()
	entry := callLog{Role: "gh", Args: os.Args[1:], Stdin: string(stdin), Cwd: cwd, Pid: os.Getpid(), Env: os.Environ()}
	code := ghMain(statePath, os.Args[1:], string(stdin))
	entry.Result = code
	if err := appendLog(os.Getenv("PUBE2E_GH_LOG"), entry); err != nil {
		return fail("gh: log: %v", err)
	}
	return code
}

func loadState(path string) (*ghState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s ghState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func saveState(path string, s *ghState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func liveHead(s *ghState, pr ghPR) string {
	if pr.HeadRefOid != "" || pr.IsCrossRepository || s.RemoteGitDir == "" {
		return pr.HeadRefOid
	}
	cmd := exec.Command("git", "--git-dir="+s.RemoteGitDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+pr.HeadRefName+"^{commit}")
	cmd.Env = nestedEnv()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// storedBody is the body create and edit store for stdin: BodyPrefix, then
// stdin (with CRLF turned into LF when BodyLF is set), then BodySuffix, as
// GitHub or a bot rewriting the body after gh sent it. ViewSetsState models
// someone closing or merging the PR after the list that located it.
func storedBody(s *ghState, stdin string) string {
	body := stdin
	if s.BodyLF {
		body = strings.ReplaceAll(body, "\r\n", "\n")
	}
	return s.BodyPrefix + body + s.BodySuffix
}

func printJSON(v any) int {
	data, err := json.Marshal(v)
	if err != nil {
		return fail("gh: %v", err)
	}
	fmt.Println(string(data))
	return 0
}

func flagValue(arg, name string) (string, bool) {
	return strings.CutPrefix(arg, "--"+name+"=")
}

func ghMain(statePath string, args []string, stdin string) int {
	s, err := loadState(statePath)
	if err != nil {
		return fail("gh: state: %v", err)
	}
	bad := func() int { return fail("gh: unexpected argv %q", args) }
	if len(args) < 2 {
		return bad()
	}
	switch args[0] + " " + args[1] {
	case "repo view":
		if len(args) != 4 || args[2] != s.Repo || args[3] != "--json=url,sshUrl" || stdin != "" {
			return bad()
		}
		switch s.RepoMode {
		case "fail":
			return fail("gh: repo view failed")
		case "block":
			waitRelease(s.Started, s.Release)
			return fail("gh: repo view released")
		}
		return printJSON(map[string]string{"url": s.RepoURL, "sshUrl": s.SSHURL})
	case "pr list":
		if len(args) != 7 || args[2] != "--repo="+s.Repo || args[4] != "--state=open" || args[5] != "--limit=100" || args[6] != "--json="+prFields || stdin != "" {
			return bad()
		}
		head, ok := flagValue(args[3], "head")
		if !ok {
			return bad()
		}
		if len(s.OnList) > 0 {
			commands := s.OnList
			s.OnList = nil
			if err := saveState(statePath, s); err != nil {
				return fail("gh: %v", err)
			}
			for _, argv := range commands {
				cmd := exec.Command(argv[0], argv[1:]...)
				cmd.Env = nestedEnv()
				if out, err := cmd.CombinedOutput(); err != nil {
					return fail("gh: on_list %q: %v\n%s", argv, err, out)
				}
			}
			if s, err = loadState(statePath); err != nil {
				return fail("gh: state: %v", err)
			}
		}
		if s.ListMode == "fail" {
			return fail("gh: pr list failed")
		}
		out := []ghPR{}
		hide := 0
		if s.HiddenNumber != 0 && s.HideNewLists > 0 {
			hide = s.HiddenNumber
			s.HideNewLists--
			if err := saveState(statePath, s); err != nil {
				return fail("gh: %v", err)
			}
		}
		for _, pr := range s.PRs {
			if pr.HeadRefName != head || pr.State != "OPEN" || pr.Number == hide {
				continue
			}
			pr.HeadRefOid = liveHead(s, pr)
			out = append(out, pr)
		}
		return printJSON(out)
	case "pr create":
		if len(args) != 7 || args[2] != "--repo="+s.Repo || args[6] != "--body-file=-" {
			return bad()
		}
		base, ok1 := flagValue(args[3], "base")
		head, ok2 := flagValue(args[4], "head")
		title, ok3 := flagValue(args[5], "title")
		if !ok1 || !ok2 || !ok3 {
			return bad()
		}
		switch s.CreateMode {
		case "fail-before":
			return fail("gh: pr create failed")
		case "block":
			waitRelease(s.Started, s.Release)
			return fail("gh: pr create released")
		}
		if s.NextNumber == 0 {
			s.NextNumber = 1
		}
		n := s.NextNumber
		s.NextNumber++
		s.PRs = append(s.PRs, ghPR{
			Number: n, URL: fmt.Sprintf("https://github.example/%s/pull/%d", s.Repo, n), State: "OPEN",
			HeadRefName: head, BaseRefName: base, Title: title + s.CreateTitleSuffix, Body: storedBody(s, stdin),
		})
		if s.HideNewLists > 0 {
			s.HiddenNumber = n
		}
		if err := saveState(statePath, s); err != nil {
			return fail("gh: %v", err)
		}
		fmt.Printf("https://github.example/%s/pull/%d\n", s.Repo, n)
		if s.CreateMode == "fail-after" {
			return fail("gh: pr create failed after creating")
		}
		return 0
	case "pr edit":
		if len(args) != 7 || args[3] != "--repo="+s.Repo || args[6] != "--body-file=-" {
			return bad()
		}
		n, err := strconv.Atoi(args[2])
		if err != nil {
			return bad()
		}
		base, ok1 := flagValue(args[4], "base")
		title, ok2 := flagValue(args[5], "title")
		if !ok1 || !ok2 {
			return bad()
		}
		i := slices.IndexFunc(s.PRs, func(pr ghPR) bool { return pr.Number == n })
		if i < 0 {
			return fail("gh: no pull request %d", n)
		}
		switch s.EditMode {
		case "fail-before":
			return fail("gh: pr edit failed")
		case "block":
			waitRelease(s.Started, s.Release)
			return fail("gh: pr edit released")
		}
		s.PRs[i].BaseRefName, s.PRs[i].Title, s.PRs[i].Body = base, title, storedBody(s, stdin)
		if err := saveState(statePath, s); err != nil {
			return fail("gh: %v", err)
		}
		if s.EditMode == "fail-after" {
			return fail("gh: pr edit failed after editing")
		}
		return 0
	case "pr view":
		if len(args) != 5 || args[3] != "--repo="+s.Repo || args[4] != "--json="+prFields || stdin != "" {
			return bad()
		}
		n, err := strconv.Atoi(args[2])
		if err != nil {
			return bad()
		}
		if s.ViewMode == "fail" {
			return fail("gh: pr view failed")
		}
		i := slices.IndexFunc(s.PRs, func(pr ghPR) bool { return pr.Number == n })
		if i < 0 {
			return fail("gh: no pull request %d", n)
		}
		if s.ViewSetsState != "" && s.PRs[i].State != s.ViewSetsState {
			s.PRs[i].State = s.ViewSetsState
			if err := saveState(statePath, s); err != nil {
				return fail("gh: %v", err)
			}
		}
		pr := s.PRs[i]
		pr.HeadRefOid = liveHead(s, pr)
		if s.StaleViews > 0 {
			s.StaleViews--
			if err := saveState(statePath, s); err != nil {
				return fail("gh: %v", err)
			}
			pr.HeadRefOid = staleOID
		}
		return printJSON(pr)
	}
	return bad()
}
