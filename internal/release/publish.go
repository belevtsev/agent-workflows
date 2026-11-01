package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// PublishOptions pins all local and remote publication inputs. APIURL and Client
// allow offline HTTP fixtures; normal use contacts GitHub with the supplied token.
type PublishOptions struct {
	Source   string
	Version  string
	Revision string
	Dist     string
	Repo     string
	Token    string
	APIURL   string
	Client   *http.Client
}

type Publication struct {
	URL      string `json:"url"`
	Tag      string `json:"tag"`
	Revision string `json:"revision"`
	Changed  bool   `json:"changed"`
}

type remoteRelease struct {
	ID        int64  `json:"id"`
	Tag       string `json:"tag_name"`
	Draft     bool   `json:"draft"`
	UploadURL string `json:"upload_url"`
	URL       string `json:"html_url"`
}

type remoteAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type publisher struct {
	options  PublishOptions
	assets   *Assets
	client   *http.Client
	base     string
	verified map[string]remoteAsset
}

type httpFailure struct {
	status int
	method string
	path   string
}

type responseUnavailable struct {
	method string
	cause  error
}

func (failure *responseUnavailable) Error() string {
	return "GitHub " + failure.method + " response unavailable; reconcile before retrying"
}

func (failure *responseUnavailable) Unwrap() error { return failure.cause }

func (failure *httpFailure) Error() string {
	return fmt.Sprintf("GitHub %s %s returned HTTP %d", failure.method, failure.path, failure.status)
}

// Publish performs no destructive writes or automatic mutation retries. Every
// uncertain response is reconciled by reading the exact expected remote state.
func Publish(ctx context.Context, options PublishOptions) (*Publication, error) {
	if !versionPattern.MatchString(options.Version) || !revisionPattern.MatchString(options.Revision) {
		return nil, errors.New("a semantic release version and exact 40-character revision are required")
	}
	if options.Token == "" {
		return nil, errors.New("GH_TOKEN is required for release publication")
	}
	_, stamp, err := SourceIdentity(ctx, options.Source, options.Revision, true)
	if err != nil {
		return nil, err
	}
	assets, err := ValidateNativeAssets(options.Dist, options.Version, options.Revision, stamp)
	if err != nil {
		return nil, err
	}
	if options.Repo == "" {
		options.Repo = "belevtsev/agent-workflows"
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(options.Repo) {
		return nil, errors.New("invalid release repository")
	}
	api := strings.TrimRight(options.APIURL, "/")
	if api == "" {
		api = "https://api.github.com"
	}
	parsed, err := url.Parse(api)
	if err != nil || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1"))) || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("release API must use HTTPS or a loopback HTTP fixture")
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	p := &publisher{options: options, assets: assets, client: client, base: api + "/repos/" + options.Repo, verified: make(map[string]remoteAsset)}
	// Complete read preflight first, including every existing asset. A conflict
	// must not cause a partial upload before the publisher discovers it.
	tagExists, err := p.readTag(ctx)
	if err != nil {
		return nil, err
	}
	remote, err := p.readRelease(ctx)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]remoteAsset)
	if remote != nil {
		existing, err = p.verifyExisting(ctx, remote)
		if err != nil {
			return nil, err
		}
		if !remote.Draft && len(existing) != len(assets.Digests) {
			return nil, errors.New("published release is incomplete; refusing to modify it")
		}
	}
	changed := false
	if !tagExists {
		body := map[string]string{"ref": "refs/tags/" + options.Version, "sha": options.Revision}
		writeErr := p.jsonRequest(ctx, http.MethodPost, p.base+"/git/refs", body, nil)
		readErr := reconcile(ctx, "tag creation", func() (bool, error) { return p.readTag(ctx) })
		if readErr != nil {
			return nil, errors.Join(errors.New("tag creation was not verified; no retry attempted"), writeErr, readErr)
		}
		changed = true
	}
	if remote == nil {
		body := map[string]any{"tag_name": options.Version, "target_commitish": options.Revision, "draft": true,
			"name": "Agent workflows " + options.Version, "body": "Native cw for macOS/Linux on AMD64 and ARM64. Source revision: " + options.Revision + ". SHA256SUMS verifies each archive."}
		writeErr := p.jsonRequest(ctx, http.MethodPost, p.base+"/releases", body, nil)
		err = reconcile(ctx, "draft creation", func() (bool, error) {
			var readErr error
			remote, readErr = p.readRelease(ctx)
			return remote != nil, readErr
		})
		if err != nil {
			return nil, errors.Join(errors.New("draft creation was not verified; no retry attempted"), writeErr, err)
		}
		existing, err = p.verifyExisting(ctx, remote)
		if err != nil {
			return nil, err
		}
		changed = true
	}
	if !remote.Draft {
		if len(existing) != len(assets.Digests) {
			return nil, errors.New("published release is incomplete; refusing to modify it")
		}
		if exists, err := p.readTag(ctx); err != nil || !exists {
			return nil, errors.Join(errors.New("exact release tag is missing"), err)
		}
		return &Publication{URL: remote.URL, Tag: options.Version, Revision: options.Revision, Changed: changed}, nil
	}
	for _, name := range append(append([]string{}, ArchiveNames...), "SHA256SUMS") {
		if _, exists := existing[name]; exists {
			continue
		}
		data, err := regularBytes(filepath.Join(assets.Directory, name))
		if err != nil {
			return nil, err
		}
		// Read again immediately before upload to avoid publishing bytes changed
		// after local preflight.
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != assets.Digests[name] {
			return nil, fmt.Errorf("local asset changed before upload: %s", name)
		}
		uploadURL, _, _ := strings.Cut(remote.UploadURL, "{")
		parsed, err := url.Parse(uploadURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, errors.New("GitHub release has an invalid upload URL")
		}
		base, _ := url.Parse(p.base)
		if (base.Scheme == "https" && (parsed.Scheme != "https" || parsed.Hostname() != "uploads.github.com")) || (base.Scheme == "http" && parsed.Host != base.Host) {
			return nil, errors.New("GitHub release has an unexpected upload host")
		}
		query := parsed.Query()
		query.Set("name", name)
		parsed.RawQuery = query.Encode()
		writeErr := p.request(ctx, http.MethodPost, parsed.String(), data, "application/octet-stream", nil)
		var current map[string]remoteAsset
		readErr := reconcile(ctx, "asset upload "+name, func() (bool, error) {
			var readErr error
			current, readErr = p.verifyExisting(ctx, remote)
			if readErr != nil {
				return false, readErr
			}
			if _, complete := current[name]; !complete {
				return false, nil
			}
			// A temporarily incomplete listing must not erase previously
			// verified assets and cause a second upload in a later iteration.
			for previous := range existing {
				if _, visible := current[previous]; !visible {
					return false, nil
				}
			}
			return true, nil
		})
		if readErr != nil {
			return nil, errors.Join(writeErr, readErr)
		}
		existing = current
		changed = true
	}
	if len(existing) != len(assets.Digests) {
		return nil, errors.New("release is missing verified assets")
	}
	if exists, err := p.readTag(ctx); err != nil || !exists {
		return nil, errors.Join(errors.New("exact release tag is missing before publication"), err)
	}
	writeErr := p.jsonRequest(ctx, http.MethodPatch, fmt.Sprintf("%s/releases/%d", p.base, remote.ID), map[string]any{"draft": false, "make_latest": "legacy"}, nil)
	var verified *remoteRelease
	readErr := reconcile(ctx, "publication", func() (bool, error) {
		var readErr error
		verified, readErr = p.readRelease(ctx)
		if readErr != nil || verified == nil {
			return false, readErr
		}
		if verified.ID != remote.ID {
			return false, errors.New("release identity changed during publication")
		}
		return !verified.Draft, nil
	})
	if readErr != nil {
		return nil, errors.Join(errors.New("publication was not verified; no retry attempted"), writeErr, readErr)
	}
	if exists, err := p.readTag(ctx); err != nil || !exists {
		return nil, errors.Join(errors.New("exact release tag is missing after publication"), err)
	}
	if err := reconcile(ctx, "published release assets", func() (bool, error) {
		final, err := p.verifyExisting(ctx, verified)
		return len(final) == len(assets.Digests), err
	}); err != nil {
		return nil, errors.Join(errors.New("published release assets were not verified"), err)
	}
	return &Publication{URL: verified.URL, Tag: options.Version, Revision: options.Revision, Changed: true}, nil
}

func (p *publisher) readTag(ctx context.Context) (bool, error) {
	var ref struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	err := p.jsonRequest(ctx, http.MethodGet, p.base+"/git/ref/tags/"+p.options.Version, nil, &ref)
	if failure, ok := errors.AsType[*httpFailure](err); ok && failure.status == http.StatusNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ref.Object.Type != "commit" || ref.Object.SHA != p.options.Revision {
		return false, errors.New("existing release tag points to another revision or is not a lightweight commit tag")
	}
	return true, nil
}

func (p *publisher) readRelease(ctx context.Context) (*remoteRelease, error) {
	// The authenticated list includes drafts. The public release-by-tag
	// endpoint alone cannot establish whether a draft creation completed.
	var found *remoteRelease
	for page := 1; ; page++ {
		var releases []remoteRelease
		if err := p.jsonRequest(ctx, http.MethodGet, fmt.Sprintf("%s/releases?per_page=100&page=%d", p.base, page), nil, &releases); err != nil {
			return nil, err
		}
		for _, release := range releases {
			if release.Tag == p.options.Version {
				if found != nil || release.ID <= 0 {
					return nil, errors.New("GitHub returned an ambiguous release identity")
				}
				found = &release
			}
		}
		if len(releases) < 100 {
			return found, nil
		}
	}
}

// reconcile retries only reads after one mutation attempt. GitHub can return a
// successful write before its listings reflect it; semantic conflicts and
// authorization failures remain terminal, and cancellation interrupts backoff.
func reconcile(ctx context.Context, operation string, read func() (bool, error)) error {
	const attempts = 6
	var lastError error
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		ready, err := read()
		if contextError := ctx.Err(); contextError != nil {
			return contextError
		}
		if err == nil && ready {
			return nil
		}
		if err != nil && !transientRead(err) {
			return fmt.Errorf("%s readback failed: %w", operation, err)
		}
		lastError = err
		if attempt == attempts-1 {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond * time.Duration(1<<attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return errors.Join(fmt.Errorf("%s was not visible after %d reads; no mutation retry attempted", operation, attempts), lastError)
}

func transientRead(err error) bool {
	if failure, ok := errors.AsType[*httpFailure](err); ok {
		return failure.status == http.StatusNotFound || failure.status == http.StatusTooManyRequests || failure.status >= 500
	}
	_, unavailable := errors.AsType[*responseUnavailable](err)
	return unavailable
}

func (p *publisher) verifyExisting(ctx context.Context, remote *remoteRelease) (map[string]remoteAsset, error) {
	result := make(map[string]remoteAsset)
	for page := 1; ; page++ {
		var assets []remoteAsset
		if err := p.jsonRequest(ctx, http.MethodGet, fmt.Sprintf("%s/releases/%d/assets?per_page=100&page=%d", p.base, remote.ID, page), nil, &assets); err != nil {
			return nil, err
		}
		for _, asset := range assets {
			digest, expected := p.assets.Digests[asset.Name]
			if !expected {
				return nil, fmt.Errorf("unexpected remote release asset: %s", asset.Name)
			}
			if _, duplicate := result[asset.Name]; duplicate || asset.ID <= 0 {
				return nil, errors.New("duplicate or invalid remote release asset")
			}
			local, err := regularBytes(filepath.Join(p.assets.Directory, asset.Name))
			if err != nil {
				return nil, err
			}
			if asset.Size != int64(len(local)) {
				return nil, fmt.Errorf("remote release asset differs: %s", asset.Name)
			}
			localDigest := sha256.Sum256(local)
			if hex.EncodeToString(localDigest[:]) != digest {
				return nil, fmt.Errorf("local asset changed during publication: %s", asset.Name)
			}
			// GitHub replaces uploaded bytes by deleting the old asset and
			// creating a new ID. Rechecking identity/size avoids repeatedly
			// downloading every earlier verified archive after each upload.
			if cached, ok := p.verified[asset.Name]; ok && cached == asset {
				result[asset.Name] = asset
				continue
			}
			data, err := p.download(ctx, asset.ID, asset.Size)
			if err != nil {
				return nil, err
			}
			actual := sha256.Sum256(data)
			if hex.EncodeToString(actual[:]) != digest || !bytes.Equal(data, local) {
				return nil, fmt.Errorf("remote release asset differs: %s", asset.Name)
			}
			result[asset.Name] = asset
			p.verified[asset.Name] = asset
		}
		if len(assets) < 100 {
			return result, nil
		}
	}
}

func (p *publisher) jsonRequest(ctx context.Context, method, endpoint string, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	return p.request(ctx, method, endpoint, body, "application/json", output)
}

func (p *publisher) request(ctx context.Context, method, endpoint string, body []byte, contentType string, output any) error {
	request, err := p.newRequest(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", contentType)
	response, err := p.client.Do(request)
	if err != nil {
		return &responseUnavailable{method: method, cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &httpFailure{status: response.StatusCode, method: method, path: request.URL.Path}
	}
	if output != nil {
		return json.UnmarshalRead(io.LimitReader(response.Body, 4<<20), output)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<20))
	return err
}

func (p *publisher) download(ctx context.Context, id, size int64) ([]byte, error) {
	request, err := p.newRequest(ctx, http.MethodGet, fmt.Sprintf("%s/releases/assets/%d", p.base, id), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, &responseUnavailable{method: http.MethodGet, cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &httpFailure{status: response.StatusCode, method: http.MethodGet, path: request.URL.Path}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, size+1))
	if err != nil || int64(len(data)) != size {
		return nil, errors.Join(errors.New("remote asset download size differs"), err)
	}
	return data, nil
}

func (p *publisher) newRequest(ctx context.Context, method, endpoint string, body []byte) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+p.options.Token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "agent-workflows-cwdev")
	return request, nil
}
