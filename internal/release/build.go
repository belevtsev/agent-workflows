package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

// BuildOptions identifies the exact source and destination for a native release.
type BuildOptions struct {
	Source   string
	Version  string
	Revision string
	Dist     string
}

var stableReleaseVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func validStableReleaseVersion(version string) bool {
	return len(version) <= 128 && stableReleaseVersion.MatchString(version)
}

// SourceIdentity requires a clean checkout at the requested exact Git revision.
func SourceIdentity(ctx context.Context, source, revision string, clean bool) (string, time.Time, error) {
	head, err := gitOutput(ctx, source, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", time.Time{}, err
	}
	if !revisionPattern.MatchString(head) || (revision != "" && revision != head) {
		return "", time.Time{}, errors.New("release revision must equal source HEAD")
	}
	if clean {
		dirty, err := gitOutput(ctx, source, "status", "--porcelain", "--untracked-files=all")
		if err != nil {
			return "", time.Time{}, err
		}
		if dirty != "" {
			return "", time.Time{}, errors.New("release builds require a clean source checkout")
		}
	}
	stamp, err := gitOutput(ctx, source, "show", "-s", "--format=%ct", head)
	if err != nil {
		return "", time.Time{}, err
	}
	epoch, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return "", time.Time{}, errors.New("invalid source commit timestamp")
	}
	return head, time.Unix(epoch, 0).UTC(), nil
}

// Build creates reproducible archives with Go alone. Existing different assets
// are never replaced, and the complete directory becomes visible atomically.
func Build(ctx context.Context, options BuildOptions) (*Assets, error) {
	if !validStableReleaseVersion(options.Version) || !revisionPattern.MatchString(options.Revision) {
		return nil, errors.New("a semantic release version and exact 40-character revision are required")
	}
	revision, stamp, err := SourceIdentity(ctx, options.Source, options.Revision, true)
	if err != nil {
		return nil, err
	}
	dist, err := filepath.Abs(options.Dist)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dist), 0o755); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(dist); err == nil && !info.IsDir() {
		return nil, errors.New("release destination is occupied or symlinked")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	temporary, err := os.MkdirTemp(filepath.Dir(dist), ".cw-release-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temporary)
	source := filepath.Join(temporary, "source")
	if err := exportSource(ctx, options.Source, revision, source); err != nil {
		return nil, err
	}
	if err := embedResources(source, options.Version, revision); err != nil {
		return nil, err
	}
	output := filepath.Join(temporary, "assets")
	if err := os.Mkdir(output, 0o755); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	failures := make(chan error, len(ArchiveNames))
	for _, name := range ArchiveNames {
		workers.Go(func() {
			parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, "cw_"), ".tar.gz"), "_")
			stage := filepath.Join(temporary, parts[0]+"_"+parts[1])
			if err := os.Mkdir(stage, 0o755); err != nil {
				failures <- err
				cancel()
				return
			}
			if err := buildBinary(ctx, source, filepath.Join(stage, "cw"), options.Version, revision, stamp, parts[0], parts[1], "readonly"); err != nil {
				failures <- err
				cancel()
				return
			}
			if err := archive(filepath.Join(output, name), stage, source, stamp); err != nil {
				failures <- err
				cancel()
			}
		})
	}
	workers.Wait()
	close(failures)
	for failure := range failures {
		err = errors.Join(err, failure)
	}
	if err != nil {
		return nil, err
	}
	var manifest strings.Builder
	for _, name := range ArchiveNames {
		digest, err := fileDigest(filepath.Join(output, name))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&manifest, "%s  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(output, "SHA256SUMS"), []byte(manifest.String()), 0o644); err != nil {
		return nil, err
	}
	assets, err := ValidateNativeAssets(output, options.Version, revision, stamp)
	if err != nil {
		return nil, err
	}
	if err := validateSourceContents(source, output, options.Version, revision); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(dist); err == nil {
		existing, err := ValidateAssets(dist)
		if err != nil {
			return nil, fmt.Errorf("existing release directory is not reusable: %w", err)
		}
		for name, digest := range assets.Digests {
			if existing.Digests[name] != digest {
				return nil, fmt.Errorf("existing release asset differs: %s", name)
			}
		}
		return existing, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := os.Rename(output, dist); err != nil {
		return nil, err
	}
	assets.Directory = dist
	return assets, nil
}

// BuildBinary builds one pinned, independent cw executable for a target platform.
func BuildBinary(ctx context.Context, source, destination, version, revision string, stamp time.Time, targetOS, targetArch string) error {
	// Developer preparation may request an older exact commit without moving
	// the checkout. Release Build still requires revision == selected HEAD.
	if _, _, err := SourceIdentity(ctx, source, "", true); err != nil {
		return err
	}
	if !revisionPattern.MatchString(revision) {
		return errors.New("binary build requires an exact source revision")
	}
	actual, err := gitOutput(ctx, source, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return err
	}
	if actual != revision {
		return errors.New("binary build source is not the requested exact commit")
	}
	timestamp, err := gitOutput(ctx, source, "show", "-s", "--format=%ct", actual)
	if err != nil {
		return err
	}
	epoch, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("invalid binary source timestamp")
	}
	actualStamp := time.Unix(epoch, 0).UTC()
	if !stamp.Equal(actualStamp) {
		return errors.New("binary build timestamp differs from exact source commit")
	}
	temporary, err := os.MkdirTemp("", "cw-binary-source-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	exported := filepath.Join(temporary, "source")
	if err := exportSource(ctx, source, actual, exported); err != nil {
		return err
	}
	if err := embedResources(exported, version, actual); err != nil {
		return err
	}
	return buildBinary(ctx, exported, destination, version, actual, stamp, targetOS, targetArch, "mod")
}

// embedResources writes generated inputs only inside a disposable exact export.
// In particular, cwdev checks never generate payloads in the source worktree.
func embedResources(source, version, revision string) error {
	pack, err := bundled.Build(source, version, revision)
	if err != nil {
		return fmt.Errorf("build embedded resources: %w", err)
	}
	directory := filepath.Join(source, "internal", "bundled")
	if info, err := os.Lstat(directory); err != nil || !info.IsDir() {
		return errors.New("source does not support standalone embedded resources")
	}
	generated := []struct {
		name string
		data []byte
	}{
		{"payload_generated.bin", pack},
		{"embedded_generated.go", []byte("// Code generated by cwdev in an exact disposable source export. DO NOT EDIT.\npackage bundled\n\nimport _ \"embed\"\n\n//go:embed payload_generated.bin\nvar generatedPayload []byte\n\nfunc init() { embeddedRaw = generatedPayload }\n")},
	}
	for _, file := range generated {
		output, err := os.OpenFile(filepath.Join(directory, file.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("reserved generated bundle path %s: %w", file.name, err)
		}
		_, writeErr := output.Write(file.data)
		if err := errors.Join(writeErr, output.Close()); err != nil {
			return err
		}
	}
	return nil
}

// validateSourceContents binds both carried resources and external notices to
// the exact export, rather than accepting four mutually consistent markers.
func validateSourceContents(source, directory, version, revision string) error {
	pack, err := bundled.Build(source, version, revision)
	if err != nil {
		return err
	}
	base, err := bundled.Open(pack)
	if err != nil {
		return err
	}
	notices := make(map[string][]byte)
	for _, name := range []string{"LICENSE", "THIRD_PARTY.md", "licenses"} {
		target := filepath.Join(source, name)
		if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := filepath.WalkDir(target, func(filename string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(source, filename)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(relative)
			if entry.IsDir() {
				notices[name] = nil
				return nil
			}
			if !entry.Type().IsRegular() {
				return errors.New("source notice is not a regular file")
			}
			data, err := os.ReadFile(filename)
			if err != nil {
				return err
			}
			notices[name] = data
			return nil
		}); err != nil {
			return err
		}
	}
	for _, name := range ArchiveNames {
		contents, err := archiveContents(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		// The retained marker binds metadata, not the embedded gzip bytes.
		// Require the exact generated pack once; this is a payload check,
		// not an attestation of the entire executable's build provenance.
		if bytes.Count(contents["cw"], pack) != 1 {
			return fmt.Errorf("release embedded payload differs from exact source export: %s", name)
		}
		identity, err := bundled.InspectBinary(contents["cw"])
		if err != nil || identity.Digest != base.Identity().Digest {
			return fmt.Errorf("release bundle differs from exact source export: %s", name)
		}
		delete(contents, "cw")
		if len(contents) != len(notices) {
			return fmt.Errorf("release notices differ from exact source export: %s", name)
		}
		for path, expected := range notices {
			actual, present := contents[path]
			if !present || (actual == nil) != (expected == nil) || !bytes.Equal(actual, expected) {
				return fmt.Errorf("release notice differs from exact source export: %s/%s", name, path)
			}
		}
	}
	return nil
}

func validatePublishedSource(ctx context.Context, source, directory, version, revision string) error {
	temporary, err := os.MkdirTemp("", "cw-publication-source-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	exported := filepath.Join(temporary, "source")
	if err := exportSource(ctx, source, revision, exported); err != nil {
		return err
	}
	return validateSourceContents(exported, directory, version, revision)
}

func buildBinary(ctx context.Context, source, destination, version, revision string, stamp time.Time, targetOS, targetArch, moduleMode string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	flags := fmt.Sprintf("-s -w -X main.version=%s -X main.revision=%s -X main.builtAt=%s", version, revision, stamp.UTC().Format(time.RFC3339))
	flags += " -X main.releaseIdentity=" + releaseMarker(version, revision, stamp, targetOS, targetArch)
	pack, err := os.ReadFile(filepath.Join(source, "internal", "bundled", "payload_generated.bin"))
	if err != nil {
		return fmt.Errorf("standalone build requires generated resources: %w", err)
	}
	base, err := bundled.Open(pack)
	if err != nil {
		return err
	}
	identity := base.Identity()
	identity.OS, identity.Arch = targetOS, targetArch
	marker, err := bundled.BinaryMarker(identity)
	if err != nil || identity.Version != version || identity.Revision != revision {
		return errors.New("generated bundle differs from binary build identity")
	}
	flags += " -X github.com/belevtsev/agent-workflows/internal/bundled.embeddedMarker=" + marker
	moduleFlag := "-mod=" + moduleMode
	command := exec.CommandContext(ctx, "go", "build", moduleFlag, "-trimpath", "-buildvcs=false", "-ldflags", flags, "-o", destination, "./cmd/cw")
	command.Dir = source
	command.Env = environment(map[string]string{"GOWORK": "off", "GOTOOLCHAIN": "go1.27.1", "GOENV": "off", "GOFLAGS": moduleFlag, "GOEXPERIMENT": "", "CGO_ENABLED": "0", "GOOS": targetOS, "GOARCH": targetArch, "GOAMD64": "v1", "GOARM64": "v8.0"})
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("build %s/%s: %w\n%s", targetOS, targetArch, err, output)
	}
	return os.Chmod(destination, 0o755)
}

func archive(destination, stage, source string, stamp time.Time) (err error) {
	paths := map[string]string{"cw": filepath.Join(stage, "cw")}
	for _, notice := range []string{"LICENSE", "THIRD_PARTY.md"} {
		path := filepath.Join(source, notice)
		if _, statErr := os.Lstat(path); statErr == nil {
			paths[notice] = path
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return statErr
		}
	}
	licenses := filepath.Join(source, "licenses")
	if _, statErr := os.Lstat(licenses); statErr == nil {
		if err := filepath.WalkDir(licenses, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(source, path)
			if err == nil {
				paths[filepath.ToSlash(relative)] = path
			}
			return err
		}); err != nil {
			return err
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	zipped := gzip.NewWriter(file)
	zipped.Header.ModTime = time.Time{}
	zipped.Header.OS = 255
	defer func() { err = errors.Join(err, zipped.Close()) }()
	writer := tar.NewWriter(zipped)
	defer func() { err = errors.Join(err, writer.Close()) }()
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		info, err := os.Lstat(paths[name])
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("release archive input is not a regular file or directory: %s", name)
		}
		header := &tar.Header{Name: name, Mode: 0o644, Size: info.Size(), ModTime: stamp, Format: tar.FormatUSTAR, Typeflag: tar.TypeReg}
		if name == "cw" {
			header.Mode = 0o755
		}
		if info.IsDir() {
			header.Typeflag, header.Mode, header.Size = tar.TypeDir, 0o755, 0
			header.Name += "/"
		}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			continue
		}
		input, err := os.Open(paths[name])
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, input)
		if err := errors.Join(copyErr, input.Close()); err != nil {
			return err
		}
	}
	return nil
}

func gitOutput(ctx context.Context, source string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", source}, arguments...)...)
	command.Env = environment(map[string]string{"GIT_OPTIONAL_LOCKS": "0", "GIT_TERMINAL_PROMPT": "0", "GIT_NO_REPLACE_OBJECTS": "1"})
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", arguments[0], err)
	}
	return strings.TrimSpace(string(output)), nil
}

func environment(overrides map[string]string) []string {
	result := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[name]; !replaced {
			result = append(result, entry)
		}
	}
	for name, value := range overrides {
		result = append(result, name+"="+value)
	}
	return result
}
