package langpack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

// GoFactsTimeout is the budget of a LoadGoFacts call whose ctx has no
// deadline.
const GoFactsTimeout = 5 * time.Second

// LoadGoFacts gathers GoFacts for the module at dir. It verifies the go
// command that env's PATH names with ResolveGo, runs the go command it
// verified, offline and with GoTool.Env, and reads the headers of source's
// changed .go files. When no go command verifies, GraphError says why and
// NoRace stays "", so the graph facts are unavailable and -race stays
// required. Its budget ends at ctx's deadline when ctx has one, and
// GoFactsTimeout after the call starts otherwise; it returns within one
// second after the budget ends. It never writes under dir, and it reports
// failures in GraphError and NoRace instead of returning an error. A nil env
// means the process environment.
func LoadGoFacts(ctx context.Context, dir string, env []string, source Source) GoFacts {
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); ok {
		ctx, cancel = context.WithCancel(ctx)
	} else {
		ctx, cancel = context.WithTimeout(ctx, GoFactsTimeout)
	}
	defer cancel()
	if env == nil {
		env = os.Environ()
	}
	var facts GoFacts
	if !scanHeaders(ctx, dir, source.Changed, &facts) {
		facts.GraphError = "reading the changed .go files did not finish within the facts budget"
		return facts
	}
	// A shim first on PATH could otherwise answer the -race probe with "not
	// supported" and waive -race.
	tool, err := resolveGo(ctx, "go", dir, slices.Concat(env, []string{"GOPROXY=off"}))
	if err != nil {
		facts.GraphError = err.Error()
		if ctx.Err() != nil {
			facts.GraphError = "verifying the go command did not finish within the facts budget"
		}
		return facts
	}
	mod := "readonly"
	if slices.Contains(source.Current, "vendor/modules.txt") {
		mod = "vendor"
	}
	var probe sync.WaitGroup
	probe.Go(func() { facts.NoRace = raceUnavailable(ctx, tool, dir, env, mod) })
	tested, embedded, excluded, graphError := packageGraph(ctx, tool, dir, env, mod)
	probe.Wait()
	if graphError != "" {
		facts.GraphError = graphError
		return facts
	}
	facts.Tested, facts.Embedded, facts.Excluded = tested, embedded, excluded
	return facts
}

// findGo returns the first usable go file in env's search list (findOnPath),
// or "" when there is none.
func findGo(env []string) string {
	name := "go"
	if runtime.GOOS == "windows" {
		name = "go.exe"
	}
	return findOnPath(env, name)
}

// findOnPath returns the first usable file named name in env's search list
// (its last PATH entry, else the process PATH), or "" when there is none.
// Empty and relative entries are skipped, as cmd/go's lookups skip them.
func findOnPath(env []string, name string) string {
	list, found := "", false
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			list, found = value, true
		}
	}
	if !found {
		list = os.Getenv("PATH")
	}
	for _, entry := range filepath.SplitList(list) {
		if entry == "" || !filepath.IsAbs(entry) {
			continue
		}
		candidate := filepath.Join(entry, name)
		info, err := os.Stat(candidate) //nolint:gosec // G703: a PATH lookup stats PATH entries by design.
		if err == nil && info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0) {
			return candidate
		}
	}
	return ""
}

// resolveGo is ResolveGo; a test replaces it with a resolver it controls.
var resolveGo = ResolveGo

// runGo runs tool with args in dir, under ctx, with env followed by extra and
// then tool.Env, and with standard error kept in stderr; stdout, when not nil,
// receives the output. go and its descendants run in a process group of their
// own, which is killed when the run ends.
func runGo(ctx context.Context, tool GoTool, dir string, env []string, stdout io.Writer, stderr *proc.HeadBuffer, args []string, extra ...string) proc.Result {
	return proc.Run(ctx, proc.Spec{Path: tool.Path, Args: args, Dir: dir, Env: slices.Concat(env, extra, tool.Env()), Stdout: stdout, Stderr: stderr,
		WaitDelay: 500 * time.Millisecond})
}

// stderrLimit is how much of go's standard error a run keeps.
const stderrLimit = 64 << 10

// raceUnavailable runs the race probe and returns the go command's reason
// that -race cannot run for cw's own target, or "" when it can or when the
// probe failed in another way.
func raceUnavailable(ctx context.Context, tool GoTool, dir string, env []string, mod string) string {
	if ctx.Err() != nil {
		return ""
	}
	stderr := &proc.HeadBuffer{Limit: stderrLimit}
	got := runGo(ctx, tool, dir, env, nil, stderr, []string{"list", "-mod=" + mod, "-race", "runtime"},
		"GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH, "GOFLAGS=-mod="+mod, "GOPROXY=off")
	if got.Code != 2 {
		return ""
	}
	line := firstLine(stderr.Bytes())
	if strings.HasPrefix(line, "-race is not supported on ") || strings.HasPrefix(line, "go: -race requires cgo") {
		return line
	}
	return ""
}

// listEntry is one package that go list -json prints.
type listEntry struct {
	ImportPath, Dir, ForTest                    string
	Module                                      *struct{ Dir string }
	Deps                                        []string
	Incomplete                                  bool
	EmbedFiles, TestEmbedFiles, XTestEmbedFiles []string
	IgnoredGoFiles, IgnoredOtherFiles           []string
}

const goListLimit = 64 << 20

var errGoListLimit = errors.New("go list output exceeds 64 MiB")

// packageGraph runs go list on the module at dir and returns its facts, or
// why they could not be computed.
func packageGraph(ctx context.Context, tool GoTool, dir string, env []string, mod string) (tested map[string][]string, embedded, excluded []string, graphError string) {
	const unfinished = "go list did not finish within the facts budget"
	if ctx.Err() != nil {
		return nil, nil, nil, unfinished
	}
	commandCtx, stop := context.WithCancel(ctx)
	defer stop()
	stderr := &proc.HeadBuffer{Limit: stderrLimit}
	// stdout is not an *os.File, so os/exec copies into it from its own
	// goroutine, which Wait joins, and WaitDelay closes that copy's pipe.
	stdout := &cappedBuffer{stop: stop}
	got := runGo(commandCtx, tool, dir, env, stdout, stderr, []string{"list", "-e=false", "-mod=" + mod, "-deps", "-test",
		"-json=ImportPath,Dir,Module,ForTest,Deps,Incomplete,EmbedFiles,TestEmbedFiles,XTestEmbedFiles,IgnoredGoFiles,IgnoredOtherFiles", "./..."},
		"GOPROXY=off")
	err := got.Err
	switch {
	case stdout.over:
		return nil, nil, nil, errGoListLimit.Error()
	// A run stopped before it started has no error of its own.
	case got.Stopped, err != nil && ctx.Err() != nil:
		return nil, nil, nil, unfinished
	}
	var entries []listEntry
	if err == nil {
		entries, err = decodeEntries(stdout.data)
	}
	if err != nil {
		if line := firstLine(stderr.Bytes()); line != "" {
			return nil, nil, nil, line
		}
		return nil, nil, nil, err.Error()
	}
	return graphFacts(dir, entries)
}

// decodeEntries decodes the stream of go list -json entries in data.
func decodeEntries(data []byte) ([]listEntry, error) {
	var entries []listEntry
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var entry listEntry
		if err := decoder.Decode(&entry); err == io.EOF {
			return entries, nil
		} else if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
}

// graphFacts assembles the facts of the module at dir from go list entries.
func graphFacts(dir string, entries []listEntry) (tested map[string][]string, embedded, excluded []string, graphError string) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, nil, nil, err.Error()
	}
	resolved := map[string]bool{} // module directory -> is the module at dir
	byPath := map[string]listEntry{}
	forTest := map[string]bool{}
	for _, entry := range entries {
		if entry.Incomplete {
			return nil, nil, nil, fmt.Sprintf("go list reported package %s as incomplete", entry.ImportPath)
		}
		byPath[entry.ImportPath] = entry
		if entry.ForTest != "" {
			forTest[entry.ForTest] = true
		}
		if entry.Module != nil {
			if _, ok := resolved[entry.Module.Dir]; !ok {
				moduleDir, err := filepath.EvalSymlinks(entry.Module.Dir)
				resolved[entry.Module.Dir] = err == nil && moduleDir == root
			}
		}
	}
	// local returns the slash-separated directory of a package of the module
	// at dir, relative to dir.
	local := func(entry listEntry) (string, bool) {
		if entry.Module == nil || !resolved[entry.Module.Dir] {
			return "", false
		}
		relative, err := filepath.Rel(entry.Module.Dir, entry.Dir)
		if err != nil {
			return "", false
		}
		return filepath.ToSlash(relative), true
	}
	tested = map[string][]string{}
	for _, entry := range entries {
		// A test binary is F.test for a package F that some entry is built for.
		name, binary := strings.CutSuffix(entry.ImportPath, ".test")
		if !binary || !forTest[name] {
			continue
		}
		key, ok := local(byPath[name])
		if !ok {
			continue
		}
		linked := []string{key}
		for _, dep := range entry.Deps {
			if dir, ok := local(byPath[dep]); ok {
				linked = append(linked, dir)
			}
		}
		slices.Sort(linked)
		tested[key] = slices.Compact(linked)
	}
	for _, entry := range entries {
		if pkg, ok := local(entry); ok {
			for _, files := range [][]string{entry.EmbedFiles, entry.TestEmbedFiles, entry.XTestEmbedFiles} {
				for _, name := range files {
					embedded = append(embedded, path.Join(pkg, filepath.ToSlash(name)))
				}
			}
			for _, files := range [][]string{entry.IgnoredGoFiles, entry.IgnoredOtherFiles} {
				for _, name := range files {
					excluded = append(excluded, path.Join(pkg, filepath.ToSlash(name)))
				}
			}
		}
	}
	slices.Sort(embedded)
	slices.Sort(excluded)
	return tested, slices.Compact(embedded), slices.Compact(excluded), ""
}

// scanHeaders fills facts.Constrained and facts.Generated from the changed
// .go files outside testdata that exist under dir. It reports false when the
// budget ended before every file was read.
func scanHeaders(ctx context.Context, dir string, changed []string, facts *GoFacts) bool {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return true // no file can be read, so none is listed
	}
	defer root.Close()
	for _, name := range slices.Compact(slices.Sorted(slices.Values(changed))) {
		if !strings.HasSuffix(name, ".go") || inTestdata(name) {
			continue
		}
		if ctx.Err() != nil {
			return false
		}
		file := parseHeader(root, name)
		if file == nil {
			continue
		}
		if slices.ContainsFunc(file.Comments, func(group *ast.CommentGroup) bool {
			return slices.ContainsFunc(group.List, func(comment *ast.Comment) bool {
				return comment.Pos() < file.Package && (constraint.IsGoBuild(comment.Text) || constraint.IsPlusBuild(comment.Text))
			})
		}) {
			facts.Constrained = append(facts.Constrained, name)
		}
		if ast.IsGenerated(file) {
			facts.Generated = append(facts.Generated, name)
		}
	}
	return true
}

// parseHeader parses a regular file's comments and package clause through
// root; nil when it is missing, not a regular file or does not parse.
func parseHeader(root *os.Root, name string) *ast.File {
	info, err := root.Stat(filepath.FromSlash(name))
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	handle, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return nil
	}
	defer handle.Close()
	file, err := parser.ParseFile(token.NewFileSet(), name, handle, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		return nil
	}
	return file
}

// firstLine returns the first line of kept, without surrounding white space.
func firstLine(kept []byte) string {
	line, _, _ := bytes.Cut(kept, []byte("\n"))
	return string(bytes.TrimSpace(line))
}

// cappedBuffer keeps a command's output while it stays within goListLimit
// bytes. The first write past the limit marks it over, stops the command and
// fails, which ends os/exec's copy. It has no ReadFrom, so io.Copy goes
// through Write.
type cappedBuffer struct {
	data []byte
	over bool
	stop context.CancelFunc
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	if len(buffer.data)+len(data) > goListLimit {
		buffer.over = true
		buffer.stop()
		return 0, errGoListLimit
	}
	buffer.data = append(buffer.data, data...)
	return len(data), nil
}
