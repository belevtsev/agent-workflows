package projectctx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// maxReferenceOutput bounds one reference search. Output past it is cut at the
// last complete line, so the same source always yields the same hits.
const maxReferenceOutput = 1 << 20

// ReferencePathspecs selects documentation, scripts and CI or build
// configuration: the files that document how a source file is invoked.
var ReferencePathspecs = []string{
	"*.md", "*.markdown", "*.mdx", "*.txt", "*.rst", "*.adoc",
	"*.sh", "*.bash", "*.zsh", "*.fish", "*.ps1", "*.bat", "*.cmd",
	"*.mk", "*Makefile", "*makefile", "*Dockerfile*", "*Justfile", "*justfile",
	"*.yml", "*.yaml", "*.toml", "*scripts/*",
}

// GrepHit is one line that mentions a searched term. Path is spelled as in a
// manifest and Text is valid UTF-8, with U+FFFD for invalid bytes.
type GrepHit struct {
	Path string
	Line int
	Text string
}

// GrepReferences lists the lines of tracked and untracked, not ignored files
// under root, limited to ReferencePathspecs, that contain term as a
// case-sensitive fixed string. Hits are ordered by path, then line. cut reports
// that output reached the search bound and later hits were dropped. A
// directory that is not a Git work tree is searched with the standard ignore
// rules instead.
func GrepReferences(ctx context.Context, root, term string) (hits []GrepHit, cut bool, err error) {
	if term == "" {
		return nil, false, errors.New("reference search needs a term")
	}
	output, cut, err := runGrep(ctx, root, term, "--untracked")
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 128 {
		output, cut, err = runGrep(ctx, root, term, "--no-index", "--exclude-standard")
	}
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return []GrepHit{}, false, nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, false, errors.New("reference search failed")
	}
	// With -z each hit is "path NUL line NUL text LF". A path may itself
	// contain a newline, so records are read field by field; text never does.
	// Past the output bound, the trailing partial record is dropped.
	hits = []GrepHit{}
	for len(output) > 0 {
		path, rest, okPath := bytes.Cut(output, []byte{0})
		number, rest, okLine := bytes.Cut(rest, []byte{0})
		text, rest, okText := bytes.Cut(rest, []byte{'\n'})
		if !okPath || !okLine || !okText {
			if cut {
				break
			}
			return nil, false, errors.New("reference search returned an unexpected record")
		}
		line, convErr := strconv.Atoi(string(number))
		if convErr != nil || line < 1 {
			return nil, false, errors.New("reference search returned an invalid line number")
		}
		// Bundles are JSON, which holds only valid UTF-8: paths use the
		// manifest spelling and text replaces invalid bytes, deterministically.
		hits = append(hits, GrepHit{Path: manifestPath(string(path)), Line: line, Text: strings.ToValidUTF8(string(text), "�")})
		output = rest
	}
	return hits, cut, nil
}

func runGrep(ctx context.Context, root, term string, mode ...string) ([]byte, bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// Configuration that changes the output shape or invokes hooks is pinned.
	args := []string{"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.quotePath=false",
		"-c", "grep.column=false", "-c", "grep.fullName=false", "-c", "grep.fallbackToNoIndex=false", "-C", root, "grep"}
	args = append(args, mode...)
	args = append(args, "--no-color", "-z", "-n", "-I", "-F", "-e", term, "--")
	args = append(args, ReferencePathspecs...)
	result, err := executeBounded(probeCtx, Command{Executable: "git", Args: args, Dir: root, Env: minimalEnvironment(os.Getenv)}, maxReferenceOutput)
	return result.Stdout, result.Truncated, err
}
