// Package manageruntime acquires and identifies native skill manager executables.
// Acquisition never activates a release or changes installation ownership.
package manageruntime

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/internal/workflow"
)

var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var versionToken = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)

// ErrLocatorMissing identifies an installed layout whose roots cannot be safely
// inferred. Callers must supply explicit roots rather than falling back to cwd.
var ErrLocatorMissing = errors.New("installed manager has no locator; provide --source, --home, --codex-home and --state-dir explicitly")

// Candidate identifies a regular executable prepared for an exact revision.
type Candidate struct {
	Path     string `json:"-"`
	Revision string `json:"revision"`
	Version  string `json:"version"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	BuiltAt  string `json:"built_at"`
}

// Locator binds an installed manager to its installation independently of cwd.
type Locator struct {
	Version     int    `json:"version"`
	Source      string `json:"source"`
	Home        string `json:"home"`
	State       string `json:"state"`
	CommandPath string `json:"command_path"`
	Integrity   string `json:"integrity_sha256,omitempty"`
}

// boundedBuffer refuses unlimited output from a subprocess.
type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("subprocess output exceeded limit")
	}
	return b.Buffer.Write(p)
}

// Inspect runs the version protocol with a bounded deadline and output size.
func Inspect(ctx context.Context, path string) (Candidate, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Candidate{}, errors.New("manager candidate must have an absolute clean path")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return Candidate{}, err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
		return Candidate{}, errors.New("manager candidate is not a regular executable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "version")
	var out boundedBuffer
	out.limit = 4096
	command.Stdout = &out
	command.Stderr = io.Discard
	if err = command.Run(); err != nil {
		if ctx.Err() != nil {
			return Candidate{}, ctx.Err()
		}
		return Candidate{}, fmt.Errorf("identify manager candidate: %w", err)
	}
	var identity Candidate
	if err = json.Unmarshal(out.Bytes(), &identity, json.RejectUnknownMembers(true)); err != nil {
		return Candidate{}, fmt.Errorf("invalid manager version protocol: %w", err)
	}
	if !commitSHA.MatchString(identity.Revision) || !versionToken.MatchString(identity.Version) || identity.BuiltAt == "" || identity.OS != runtime.GOOS || identity.Arch != runtime.GOARCH {
		return Candidate{}, errors.New("manager identity does not match this platform or a real source revision")
	}
	identity.Path = path
	return identity, nil
}

// Resolve finds the locator next to the runtime release tree of an executable.
// Missing locators indicate an uninstalled developer/bootstrap executable.
func Resolve(executable string) (Locator, error) {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return Locator{}, err
	}
	parent := filepath.Dir(resolved)
	// Runtime layout is runtime/releases/<revision>/cw. Do not search arbitrary
	// ancestors for a locator belonging to another checkout or installation.
	if filepath.Base(filepath.Dir(parent)) != "releases" || filepath.Base(filepath.Dir(filepath.Dir(parent))) != "runtime" {
		return Locator{}, os.ErrNotExist
	}
	runtimeRoot := filepath.Dir(filepath.Dir(parent))
	path := filepath.Join(runtimeRoot, "locator.json")
	st, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		// The immutable release descriptor remains available when an interrupted
		// uninstall has removed the active locator and command registration.
		path = filepath.Join(parent, "locator.json")
		st, err = os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return Locator{}, ErrLocatorMissing
		}
	}
	if err != nil {
		return Locator{}, err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > 16384 {
		return Locator{}, errors.New("unsafe installed manager locator")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Locator{}, err
	}
	var locator Locator
	if err = json.Unmarshal(data, &locator, json.RejectUnknownMembers(true)); err != nil {
		return Locator{}, fmt.Errorf("invalid installed manager locator: %w", err)
	}
	for _, p := range []string{locator.Source, locator.Home, locator.State} {
		if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.ContainsAny(p, "\x00\r\n") {
			return Locator{}, errors.New("unsafe installed manager locator paths")
		}
	}
	if locator.Version != 2 {
		return Locator{}, errors.New("unsupported installed manager locator version")
	}
	if err = workflow.VerifyLocalSeal(data); err != nil {
		return Locator{}, fmt.Errorf("invalid installed manager locator: %w", err)
	}
	if locator.CommandPath != filepath.Join(locator.Home, ".local", "bin", "cw") {
		return Locator{}, errors.New("unsafe installed manager command path")
	}
	stateRoot := filepath.Dir(runtimeRoot)
	if locator.State != stateRoot {
		return Locator{}, errors.New("manager locator belongs to another runtime root")
	}
	if !commitSHA.MatchString(filepath.Base(parent)) {
		return Locator{}, errors.New("invalid installed manager release path")
	}
	return locator, nil
}
