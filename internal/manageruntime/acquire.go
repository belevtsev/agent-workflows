package manageruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	releasebuild "github.com/belevtsev/agent-workflows/internal/release"
)

const repository = "belevtsev/agent-workflows"
const maxArchive = 128 << 20

var errReleaseUnavailable = errors.New("no exact-revision native release is available")

type release struct {
	Tag    string `json:"tag_name"`
	Target string `json:"target_commitish"`
	Draft  bool   `json:"draft"`
}

// Acquirer holds replaceable transport and process dependencies for acquisition.
// Defaults permit only HTTPS and use bounded requests and subprocesses.
type Acquirer struct {
	Client       *http.Client
	APIBase      string
	DownloadBase string
	Inspect      func(context.Context, string) (Candidate, error)
	Build        func(context.Context, string, string, string) error
}

// Prepare acquires a candidate without activating it. Cached candidates must
// identify the requested SHA; unreleased clean source is built natively with Go.
func Prepare(ctx context.Context, source, sha, root string) (Candidate, error) {
	return (Acquirer{}).Prepare(ctx, source, sha, root)
}

// SourceHead identifies a clean local source without fetching or changing refs.
func SourceHead(ctx context.Context, source string) (string, error) {
	dirty, err := gitOutput(ctx, source, 1<<20, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	if dirty != "" {
		return "", errors.New("source checkout is dirty; preserve or commit changes before preparing manager")
	}
	head, err := gitOutput(ctx, source, 128, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	if !commitSHA.MatchString(head) {
		return "", errors.New("invalid source HEAD revision")
	}
	return head, nil
}

func (a Acquirer) Prepare(ctx context.Context, source, sha, root string) (Candidate, error) {
	if !commitSHA.MatchString(sha) {
		return Candidate{}, errors.New("invalid manager source revision")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return Candidate{}, errors.New("manager preparation requires an absolute clean state root")
	}
	if err := ctx.Err(); err != nil {
		return Candidate{}, err
	}
	if a.Inspect == nil {
		a.Inspect = Inspect
	}
	if a.Build == nil {
		a.Build = buildSource
	}
	candidateRoot := filepath.Join(root, "runtime", "candidates")
	destination := filepath.Join(candidateRoot, sha, "cw")
	if _, err := os.Lstat(destination); err == nil {
		if err = secureDirectories(filepath.Dir(destination), false); err != nil {
			return Candidate{}, err
		}
		identity, err := a.Inspect(ctx, destination)
		if err != nil {
			return Candidate{}, err
		}
		if identity.Revision != sha {
			return Candidate{}, errors.New("cached manager revision differs from requested source")
		}
		return identity, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Candidate{}, err
	}
	if err := secureDirectories(candidateRoot, true); err != nil {
		return Candidate{}, err
	}
	temporary, err := os.MkdirTemp(candidateRoot, ".prepare-")
	if err != nil {
		return Candidate{}, err
	}
	defer os.RemoveAll(temporary)
	path := filepath.Join(temporary, "cw")
	err = a.download(ctx, source, sha, path)
	if errors.Is(err, errReleaseUnavailable) {
		err = a.Build(ctx, source, sha, path)
	}
	if err != nil {
		return Candidate{}, err
	}
	if err = os.Chmod(path, 0755); err != nil {
		return Candidate{}, err
	}
	identity, err := a.Inspect(ctx, path)
	if err != nil {
		return Candidate{}, err
	}
	if identity.Revision != sha {
		return Candidate{}, errors.New("prepared manager revision differs from requested source")
	}
	if err = ctx.Err(); err != nil {
		return Candidate{}, err
	}
	if err = secureDirectories(filepath.Dir(destination), true); err != nil {
		return Candidate{}, err
	}
	// A concurrent preparer may have won. Never overwrite unidentified content.
	if _, err = os.Lstat(destination); err == nil {
		existing, err := a.Inspect(ctx, destination)
		if err != nil {
			return Candidate{}, err
		}
		if existing.Revision != sha {
			return Candidate{}, errors.New("manager candidate occupied by another revision")
		}
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Candidate{}, err
	}
	// Hard-link gives atomic no-replace publication on the same filesystem.
	if err = os.Link(path, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, inspectErr := a.Inspect(ctx, destination)
			if inspectErr != nil {
				return Candidate{}, inspectErr
			}
			if existing.Revision != sha {
				return Candidate{}, errors.New("concurrent manager candidate differs from requested revision")
			}
			return existing, nil
		}
		return Candidate{}, err
	}
	identity.Path = destination
	return identity, nil
}

func secureDirectories(path string, create bool) error {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err == nil {
			if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe manager directory: %s", p)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if create {
		return os.MkdirAll(path, 0700)
	}
	return nil
}

func (a Acquirer) fetch(ctx context.Context, address string, limit int64) ([]byte, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("manager downloads require HTTPS URLs without credentials")
	}
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	clone := *client
	if clone.Timeout <= 0 || clone.Timeout > 90*time.Second {
		clone.Timeout = 90 * time.Second
	}
	prior := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.User != nil || len(via) > 5 {
			return errors.New("unsafe manager download redirect")
		}
		if prior != nil {
			return prior(req, via)
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "agent-workflows-native-manager")
	response, err := clone.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, errReleaseUnavailable
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manager download HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("manager download exceeded size limit")
	}
	return data, nil
}

func (a Acquirer) download(ctx context.Context, source, sha, path string) error {
	api := a.APIBase
	if api == "" {
		api = "https://api.github.com/repos/" + repository
	}
	download := a.DownloadBase
	if download == "" {
		download = "https://github.com/" + repository + "/releases/download"
	}
	var chosen release
	// Exact local tags avoid matching an unrelated latest release. Git performs no
	// network access here; the selected executable still proves its revision.
	tags, err := gitOutput(ctx, source, 8192, "tag", "--points-at", sha, "--sort=-version:refname")
	if err == nil {
		for tag := range strings.SplitSeq(strings.TrimSpace(tags), "\n") {
			if !strings.HasPrefix(tag, "v") || !versionToken.MatchString(tag) {
				continue
			}
			data, err := a.fetch(ctx, api+"/releases/tags/"+url.PathEscape(tag), 1<<20)
			if errors.Is(err, errReleaseUnavailable) {
				continue
			}
			if err != nil {
				return err
			}
			if err = json.Unmarshal(data, &chosen); err != nil {
				return err
			}
			if !chosen.Draft && chosen.Tag == tag {
				break
			}
			chosen = release{}
		}
	}
	if chosen.Tag == "" {
		data, err := a.fetch(ctx, api+"/releases?per_page=100", 4<<20)
		if err != nil {
			return err
		}
		var releases []release
		if err = json.Unmarshal(data, &releases); err != nil {
			return err
		}
		for _, entry := range releases {
			if !entry.Draft && entry.Target == sha && strings.HasPrefix(entry.Tag, "v") && versionToken.MatchString(entry.Tag) {
				chosen = entry
				break
			}
		}
	}
	if chosen.Tag == "" {
		return errReleaseUnavailable
	}
	asset := "cw_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	base := download + "/" + url.PathEscape(chosen.Tag)
	sums, err := a.fetch(ctx, base+"/SHA256SUMS", 64<<10)
	if err != nil {
		return err
	}
	expected := ""
	for line := range strings.SplitSeq(string(sums), "\n") {
		digest, name, ok := strings.Cut(line, "  ")
		if !ok || name != asset {
			continue
		}
		if expected != "" || len(digest) != 64 {
			return errors.New("missing or duplicated manager asset checksum")
		}
		if _, err = hex.DecodeString(digest); err != nil {
			return errors.New("invalid manager checksum")
		}
		expected = digest
	}
	if expected == "" {
		return errors.New("manager checksum manifest does not include requested platform")
	}
	archive, err := a.fetch(ctx, base+"/"+asset, maxArchive)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != expected {
		return errors.New("manager archive checksum mismatch")
	}
	return extractBinary(archive, path)
}

func extractBinary(archive []byte, path string) error {
	zipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer zipped.Close()
	reader := tar.NewReader(io.LimitReader(zipped, maxArchive+1))
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name != "cw" {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxArchive {
			return errors.New("manager archive contains invalid or duplicate cw")
		}
		binary, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(binary, reader, header.Size)
		closeErr := binary.Close()
		if err = errors.Join(copyErr, closeErr); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return errors.New("manager archive contains no regular cw executable")
	}
	return nil
}

func gitOutput(ctx context.Context, source string, limit int, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-C", source}, args...)...)
	command.Env = environment("GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	var output boundedBuffer
	output.limit = limit
	command.Stdout = &output
	command.Stderr = io.Discard
	err := command.Run()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("inspect manager source: %w", err)
	}
	return strings.TrimSpace(output.String()), nil
}

func environment(values ...string) []string {
	out := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		replaced := false
		for _, value := range values {
			name, _, _ := strings.Cut(value, "=")
			if key == name {
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, entry)
		}
	}
	return append(out, values...)
}

func buildSource(ctx context.Context, source, sha, path string) error {
	dirty, err := gitOutput(ctx, source, 1<<20, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if dirty != "" {
		return errors.New("source checkout is dirty; preserve or commit changes before preparing manager")
	}
	head, err := gitOutput(ctx, source, 128, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	actual, err := gitOutput(ctx, source, 128, "rev-parse", "--verify", sha+"^{commit}")
	if err != nil || actual != sha {
		return errors.New("requested manager revision is not a source commit")
	}
	builtAt, err := gitOutput(ctx, source, 128, "show", "-s", "--format=%cI", sha)
	if err != nil {
		return err
	}
	stamp, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return errors.New("invalid exact source commit timestamp")
	}
	if err := releasebuild.BuildBinary(ctx, source, path, "dev", sha, stamp, runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}
	current, err := gitOutput(ctx, source, 128, "rev-parse", "--verify", "HEAD")
	if err != nil || current != head {
		return errors.New("source HEAD changed during manager preparation")
	}
	dirty, err = gitOutput(ctx, source, 1<<20, "status", "--porcelain", "--untracked-files=all")
	if err != nil || dirty != "" {
		return errors.New("source changed during manager preparation")
	}
	return nil
}
