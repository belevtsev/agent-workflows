package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/internal/manageruntime"
)

const repository = "belevtsev/agent-workflows"
const maxArchiveBytes = 64 << 20

var archiveNames = []string{"cw_darwin_arm64.tar.gz", "cw_darwin_amd64.tar.gz", "cw_linux_arm64.tar.gz", "cw_linux_amd64.tar.gz"}

// Release is a pinned published release and checksum set. Preparation never
// consults latest a second time or combines assets from different tags.
type Release struct {
	ID              int64  `json:"id"`
	Version         string `json:"version"`
	Revision        string `json:"revision"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	ArchiveName     string `json:"archive_name"`
	ArchiveURL      string `json:"archive_url"`
	ArchiveSHA256   string `json:"archive_sha256"`
	ChecksumsSHA256 string `json:"checksums_sha256"`
}

// Client has no process/build fallback. The transport seams support isolated
// fixtures; production URLs and all production redirects require HTTPS.
type Client struct {
	HTTP         *http.Client
	APIBase      string
	DownloadBase string
	Inspect      func(context.Context, string) (manageruntime.Candidate, error)
}

func (client Client) bases() (string, string, error) {
	api, download := strings.TrimRight(client.APIBase, "/"), strings.TrimRight(client.DownloadBase, "/")
	if api == "" {
		api = "https://api.github.com/repos/" + repository
	}
	if download == "" {
		download = "https://github.com/" + repository + "/releases/download"
	}
	for _, address := range []string{api, download} {
		parsed, err := url.Parse(address)
		if err != nil || !safeURL(parsed) || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", "", errors.New("release endpoints require HTTPS or explicit loopback HTTP fixtures")
		}
	}
	return api, download, nil
}

func safeURL(address *url.URL) bool {
	return address != nil && address.Host != "" && address.User == nil && (address.Scheme == "https" || address.Scheme == "http" && (address.Hostname() == "127.0.0.1" || address.Hostname() == "localhost" || address.Hostname() == "::1"))
}

func (client Client) fetch(ctx context.Context, address string, limit int64) ([]byte, error) {
	parsed, err := url.Parse(address)
	if err != nil || !safeURL(parsed) || parsed.Fragment != "" {
		return nil, errors.New("unsafe release download URL")
	}
	transport := client.HTTP
	if transport == nil {
		transport = &http.Client{Timeout: 90 * time.Second}
	}
	bounded := *transport
	if bounded.Timeout <= 0 || bounded.Timeout > 90*time.Second {
		bounded.Timeout = 90 * time.Second
	}
	previousRedirect := bounded.CheckRedirect
	bounded.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if !safeURL(request.URL) || len(via) > 5 || parsed.Scheme == "https" && request.URL.Scheme != "https" || parsed.Scheme == "http" && request.URL.Host != parsed.Host {
			return errors.New("unsafe release download redirect")
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "agent-workflows-standalone-manager")
	response, err := bounded.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release request returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("release response exceeds input limit")
	}
	return data, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func parseChecksums(data []byte) (map[string]string, error) {
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, errors.New("release checksum manifest must end with a complete newline record")
	}
	checksums := make(map[string]string, 4)
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		hash, name, ok := strings.Cut(line, "  ")
		known := false
		for _, allowed := range archiveNames {
			known = known || name == allowed
		}
		if !ok || !known || !contentDigest.MatchString(hash) || checksums[name] != "" {
			return nil, errors.New("release checksum manifest must contain exactly four unique native archives")
		}
		checksums[name] = hash
	}
	if len(checksums) != 4 {
		return nil, errors.New("release checksum manifest must cover all four supported targets")
	}
	return checksums, nil
}

// Latest pins a single stable public release, its lightweight source tag and
// complete checksum set. It writes no cache or installation files.
func (client Client) Latest(ctx context.Context) (Release, error) {
	api, download, err := client.bases()
	if err != nil {
		return Release{}, err
	}
	data, err := client.fetch(ctx, api+"/releases/latest", 4<<20)
	if err != nil {
		return Release{}, err
	}
	var published struct {
		ID         int64  `json:"id"`
		Tag        string `json:"tag_name"`
		Target     string `json:"target_commitish"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Size  int64  `json:"size"`
			State string `json:"state"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &published); err != nil {
		return Release{}, err
	}
	if _, err := compareVersion(published.Tag, published.Tag); err != nil || published.ID <= 0 || published.Draft || published.Prerelease {
		return Release{}, errors.New("latest release is not a published stable native release")
	}
	seen := map[string]bool{}
	for _, asset := range published.Assets {
		known := asset.Name == "SHA256SUMS"
		for _, name := range archiveNames {
			known = known || asset.Name == name
		}
		if !known || seen[asset.Name] || asset.ID <= 0 || asset.Size <= 0 || asset.Size > maxArchiveBytes || asset.State != "uploaded" {
			return Release{}, errors.New("published release assets are incomplete, duplicated or invalid")
		}
		seen[asset.Name] = true
	}
	if len(seen) != 5 {
		return Release{}, errors.New("published release is missing a supported platform or checksum asset")
	}
	revision, err := client.tagRevision(ctx, published.Tag)
	if err != nil {
		return Release{}, err
	}
	if sourceRevision.MatchString(published.Target) && published.Target != revision {
		return Release{}, errors.New("published release tag differs from its source target")
	}
	base := download + "/" + url.PathEscape(published.Tag)
	sums, err := client.fetch(ctx, base+"/SHA256SUMS", 64<<10)
	if err != nil {
		return Release{}, err
	}
	checksums, err := parseChecksums(sums)
	if err != nil {
		return Release{}, err
	}
	asset := "cw_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if checksums[asset] == "" {
		return Release{}, errors.New("standalone release does not support this platform")
	}
	return Release{ID: published.ID, Version: published.Tag, Revision: revision, OS: runtime.GOOS, Arch: runtime.GOARCH, ArchiveName: asset, ArchiveURL: base + "/" + asset, ArchiveSHA256: checksums[asset], ChecksumsSHA256: digest(sums)}, nil
}

// VerifyTag binds an already checksummed standalone executable to the exact
// pinned source tag. It never re-reads latest or acquires a different release.
func (client Client) VerifyTag(ctx context.Context, version, revision string) error {
	if _, err := compareVersion(version, version); err != nil {
		return err
	}
	if !sourceRevision.MatchString(revision) {
		return errors.New("release verification requires an exact source revision")
	}
	actual, err := client.tagRevision(ctx, version)
	if err != nil {
		return err
	}
	if actual != revision {
		return errors.New("standalone source revision differs from pinned release tag")
	}
	return nil
}

func (client Client) tagRevision(ctx context.Context, version string) (string, error) {
	api, _, err := client.bases()
	if err != nil {
		return "", err
	}
	data, err := client.fetch(ctx, api+"/git/ref/tags/"+url.PathEscape(version), 1<<20)
	if err != nil {
		return "", err
	}
	var tag struct {
		Ref    string `json:"ref"`
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if err = json.Unmarshal(data, &tag); err != nil {
		return "", err
	}
	if tag.Ref != "refs/tags/"+version || tag.Object.Type != "commit" || !sourceRevision.MatchString(tag.Object.SHA) {
		return "", errors.New("published release tag does not identify one exact source commit")
	}
	return tag.Object.SHA, nil
}
