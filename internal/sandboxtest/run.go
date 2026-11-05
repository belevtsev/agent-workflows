package sandboxtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1" // Git blob identities use SHA-1; this matches their native format.
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/belevtsev/agent-workflows/internal/release"
)

const subprocessOutputLimit = 1 << 20

type commandRunner struct {
	binary string
	env    []string
	dir    string
}

type boundedOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	limit  int
	cancel context.CancelFunc
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(data) > b.limit-b.buffer.Len() {
		b.cancel()
		return 0, errors.New("subprocess output limit exceeded")
	}
	return b.buffer.Write(data)
}

func (r commandRunner) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, r.binary, args...)
	command.Env, command.Dir = r.env, r.dir
	command.WaitDelay = time.Second
	out := &boundedOutput{limit: subprocessOutputLimit, cancel: cancel}
	stderr := &boundedOutput{limit: 64 << 10, cancel: cancel}
	command.Stdout, command.Stderr = out, stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return bytes.Clone(out.buffer.Bytes()), ctx.Err()
		}
		return bytes.Clone(out.buffer.Bytes()), errors.New("bounded subprocess failed")
	}
	return bytes.Clone(out.buffer.Bytes()), nil
}

// Run builds two exact-source managers outside the test environment, then
// exposes only executables to the explicitly selected isolation backend.
func Run(ctx context.Context, options Options) (report Report, err error) {
	start := time.Now()
	report = Report{Backend: options.Backend, Status: "failed", OS: "linux", Artifacts: []Artifact{}, Resources: []Resource{}, Limitations: []string{}, CleanupErrors: []string{}}
	if options.Output != "" {
		report.Output, err = filepath.Abs(options.Output)
		if err != nil {
			return report, err
		}
	}
	defer func() {
		report.DurationMS = time.Since(start).Milliseconds()
		if err == nil && len(report.CleanupErrors) == 0 {
			report.Status = "success"
		}
		if len(report.CleanupErrors) > 0 {
			report.Status = "failed"
			err = errors.Join(err, errors.New("owned resource cleanup failed"))
		}
		if err != nil {
			report.Limitations = append(report.Limitations, err.Error())
		}
		if report.Output != "" {
			if saveErr := writeReport(report.Output, report); saveErr != nil {
				err = errors.Join(err, errors.New("cannot save sandbox report"))
				report.Status = "failed"
			}
		}
	}()
	if options.Backend != "docker" && options.Backend != "sbx" {
		return report, errors.New("sandbox backend must be docker or sbx")
	}
	backendBinary, lookupErr := exec.LookPath(options.Backend)
	if lookupErr != nil {
		report.Status = "unavailable"
		report.Limitations = append(report.Limitations, options.Backend+" executable is unavailable")
		return report, errors.New("requested sandbox backend is unavailable")
	}
	private, err := os.MkdirTemp("", "cw-artifact-sandbox-")
	if err != nil {
		return report, err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(private); cleanupErr != nil {
			report.CleanupErrors = append(report.CleanupErrors, "private artifact directory cleanup failed")
		}
	}()
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return report, err
	}
	name := "cw-artifact-" + hex.EncodeToString(nonce[:])
	backend := backendConfig{binary: backendBinary, private: private, name: name}
	if options.Backend == "docker" {
		if err = backend.prepareDocker(ctx, &report); err != nil {
			return report, err
		}
	} else {
		report.Arch, report.Available = runtime.GOARCH, true
		report.Limitations = append(report.Limitations, "SBX outer shell may contain developer tools; the independent probe must restrict the cw child PATH and verify its dependency boundary")
	}
	if report.Arch != "amd64" && report.Arch != "arm64" {
		report.Status = "unavailable"
		report.Available = false
		return report, errors.New("sandbox backend architecture is unsupported")
	}
	for _, tool := range []string{"git", "go"} {
		if _, lookupErr := exec.LookPath(tool); lookupErr != nil {
			report.Status, report.Available = "unavailable", false
			return report, fmt.Errorf("%s is required only for host-side artifact preparation", tool)
		}
	}
	source, err := filepath.Abs(options.Source)
	if err != nil || options.Source == "" {
		return report, errors.New("sandbox preparation requires an explicit source checkout")
	}
	revision, stamp, err := release.SourceIdentity(ctx, source, "", true)
	if err != nil {
		return report, errors.New("sandbox preparation requires a clean exact source checkout")
	}
	report.SourceRevision = revision
	artifacts := filepath.Join(private, "artifacts")
	if err = os.Mkdir(artifacts, 0755); err != nil {
		return report, err
	}
	prepareCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	for index, version := range []string{"v0.0.101", "v0.0.102"} {
		filename := "first-cw"
		if index == 1 {
			filename = "second-cw"
		}
		if err = release.BuildBinary(prepareCtx, source, filepath.Join(artifacts, filename), version, revision, stamp, "linux", report.Arch); err != nil {
			return report, errors.New("exact-source standalone artifact build failed")
		}
	}
	probeSource := filepath.Join(private, "probe-source")
	if err = exportProbeSource(prepareCtx, source, revision, probeSource); err != nil {
		return report, err
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		report.Status = "unavailable"
		return report, errors.New("Go is required only for host-side artifact preparation")
	}
	goEnv := append(os.Environ(), "GOWORK=off", "GOENV=off", "GOTOOLCHAIN=go1.27.1", "GOFLAGS=-mod=readonly", "GOEXPERIMENT=", "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+report.Arch, "GOAMD64=v1", "GOARM64=v8.0")
	build := commandRunner{binary: goBinary, env: goEnv, dir: probeSource}
	if _, err = build.run(prepareCtx, 5*time.Minute, "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", filepath.Join(artifacts, "cwdev"), "./cmd/cwdev"); err != nil {
		return report, errors.New("exact-source acceptance executable build failed; independent Probe must be present")
	}
	if current, _, e := release.SourceIdentity(ctx, source, revision, true); e != nil || current != revision {
		return report, errors.New("source changed during artifact preparation")
	}
	for _, filename := range []string{"first-cw", "second-cw", "cwdev"} {
		if err = os.Chmod(filepath.Join(artifacts, filename), 0755); err != nil {
			return report, err
		}
		data, readErr := os.ReadFile(filepath.Join(artifacts, filename))
		if readErr != nil {
			return report, readErr
		}
		digest := sha256.Sum256(data)
		report.Artifacts = append(report.Artifacts, Artifact{Name: filename, SHA256: hex.EncodeToString(digest[:]), OS: "linux", Arch: report.Arch})
	}
	if options.Backend == "docker" {
		err = backend.runDocker(ctx, probeSource, artifacts, &report)
	} else {
		err = backend.runSBX(ctx, probeSource, artifacts, &report)
	}
	return report, err
}

func exportProbeSource(ctx context.Context, source, revision, destination string) error {
	git, err := exec.LookPath("git")
	if err != nil {
		return errors.New("Git is required only for host-side exact source preparation")
	}
	runner := commandRunner{binary: git, env: append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1"), dir: source}
	manifest, err := runner.run(ctx, time.Minute, "ls-tree", "-rz", "--full-tree", revision)
	if err != nil {
		return errors.New("cannot list exact acceptance source")
	}
	if len(manifest) == 0 || manifest[len(manifest)-1] != 0 {
		return errors.New("invalid exact acceptance source manifest")
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	var total int64
	for count, entry := range bytes.Split(manifest[:len(manifest)-1], []byte{0}) {
		meta, filename, ok := bytes.Cut(entry, []byte{'\t'})
		fields, name := strings.Fields(string(meta)), string(filename)
		if !ok || count >= 10000 || len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") || !filepath.IsLocal(name) || filepath.Clean(name) != name || strings.ContainsAny(name, "\\\x00\r\n") {
			return errors.New("unsafe exact acceptance source manifest")
		}
		path := filepath.Join(source, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 32<<20 || info.Size() > (128<<20)-total {
			return errors.New("acceptance source is modified or oversized")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		total += int64(len(data))
		digest := sha1.New()
		fmt.Fprintf(digest, "blob %d\x00", len(data))
		digest.Write(data)
		if hex.EncodeToString(digest.Sum(nil)) != fields[2] {
			return errors.New("acceptance source bytes differ from the exact Git tree")
		}
		target := filepath.Join(destination, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if fields[0] == "100755" {
			mode = 0755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return err
		}
	}
	return nil
}

func writeReport(path string, report Report) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("report destination must be a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(parent, ".cw-sandbox-report-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	data, err := json.Marshal(report, json.Deterministic(true))
	if err == nil {
		_, err = file.Write(append(data, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func decodeProbe(data []byte) (map[string]any, error) {
	var result map[string]any
	if len(data) == 0 || len(data) > subprocessOutputLimit {
		return nil, errors.New("acceptance result is empty or exceeds output limit")
	}
	if err := json.Unmarshal(data, &result); err != nil || result == nil {
		return nil, errors.New("acceptance executable did not return one JSON object")
	}
	return result, nil
}

func safeStage(stage string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", stage, err)
	}
	return errors.New(stage + " failed; backend output is withheld")
}
