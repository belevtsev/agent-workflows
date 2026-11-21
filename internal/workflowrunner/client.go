package workflowrunner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

type Registration struct {
	Worktree          string `json:"worktree"`
	Project           string `json:"project"`
	Endpoint          string `json:"endpoint"`
	RootCA            string `json:"root_ca,omitempty"`
	ClientCertificate string `json:"client_certificate,omitempty"`
	ClientKey         string `json:"client_key,omitempty"`
	ReceiptKey        []byte `json:"receipt_key"`
}

// Runner starts jobs and returns their signed results. The client of a registered service is one runner.
type Runner interface {
	Run(ctx context.Context, job Job) (Signed, Receipt, error)
	Resume(ctx context.Context, attempt, binding, job string) (Signed, Receipt, error)
	Status(ctx context.Context, attempt, binding string) (Signed, Receipt, error)
	Verify(signed Signed, attempt, binding string) (Receipt, error)
	Authority() string
	Offer() Capabilities // the platform, profile and reviewed policy it runs jobs under
}

type Client struct {
	cfg  Registration
	http *http.Client
	Capabilities
}

func (c *Client) Offer() Capabilities { return c.Capabilities }

func Open(ctx context.Context, root string) (*Client, error) {
	c, err := Load(root)
	if err != nil {
		return nil, err
	}
	if err := c.probe(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Load binds cached evidence to the machine registration without contacting
// the service. Only starting or resuming jobs needs a live probe.
func Load(root string) (*Client, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	data, err := readRegistration()
	if err != nil {
		return nil, err
	}
	var registrations []Registration
	if err := decode(data, &registrations); err != nil {
		return nil, fmt.Errorf("invalid machine runner registration: %w", err)
	}
	var match *Registration
	for _, entry := range registrations {
		if !filepath.IsAbs(entry.Worktree) {
			return nil, errors.New("runner registration requires an absolute worktree path")
		}
		worktree, err := filepath.EvalSymlinks(entry.Worktree)
		if err == nil && samePath(worktree, root) {
			if match != nil {
				return nil, errors.New("multiple trusted runner registrations for this worktree")
			}
			match = &entry
		}
	}
	if match == nil {
		return nil, errors.New("no trusted workflow runner registered for this worktree")
	}
	return newClient(*match)
}

func (c *Client) Authority() string {
	return Digest(c.cfg)
}

func samePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	return left == right || runtime.GOOS == "windows" && strings.EqualFold(left, right)
}

func Connect(ctx context.Context, cfg Registration) (*Client, error) {
	c, err := newClient(cfg)
	if err != nil {
		return nil, err
	}
	if err := c.probe(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func newClient(cfg Registration) (*Client, error) {
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || len(cfg.ReceiptKey) != ed25519.PublicKeySize || cfg.Project == "" {
		return nil, errors.New("runner registration needs an HTTPS endpoint, project, and Ed25519 receipt key")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	if cfg.RootCA != "" {
		tlsConfig.RootCAs = x509.NewCertPool()
		if !tlsConfig.RootCAs.AppendCertsFromPEM([]byte(cfg.RootCA)) {
			return nil, errors.New("invalid registered runner CA")
		}
	}
	if cfg.ClientCertificate != "" || cfg.ClientKey != "" {
		identity, err := tls.X509KeyPair([]byte(cfg.ClientCertificate), []byte(cfg.ClientKey))
		if err != nil {
			return nil, errors.New("invalid registered workflow client certificate or key")
		}
		tlsConfig.Certificates = []tls.Certificate{identity}
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig, DisableKeepAlives: true}
	return &Client{cfg: cfg, http: &http.Client{
		Transport: transport, Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) probe(ctx context.Context) error {
	_, receipt, err := c.exchange(ctx, Request{Action: "probe"})
	if err != nil {
		return err
	}
	if receipt.Capabilities == nil || receipt.Status != "ready" {
		return errors.New("trusted runner has no ready execution profile")
	}
	c.Capabilities = *receipt.Capabilities
	if c.Platform != runtime.GOOS+"/"+runtime.GOARCH || c.Profile == "" || c.Policy.Declaration == "" ||
		receipt.Platform != c.Platform || receipt.Profile != c.Profile || receipt.Policy != Digest(c.Policy) {
		return errors.New("trusted runner lacks the native platform or reviewed policy for this worktree")
	}
	for _, os := range []string{"linux", "darwin", "windows"} {
		if !slices.Contains(c.NativeTest, os) {
			return fmt.Errorf("trusted runner lacks native acceptance for %s", os)
		}
	}
	return nil
}

// Run returns a signed terminal receipt. The runner renews its lease on
// status requests and tears the isolated job down when the lease expires.
func (c *Client) Run(ctx context.Context, job Job) (Signed, Receipt, error) {
	if job.Profile != c.Profile || job.Policy != Digest(c.Policy) {
		return Signed{}, Receipt{}, errors.New("runner job was built for a different profile or policy")
	}
	request := Request{Action: "start", Attempt: job.Attempt, Binding: job.Binding, Job: &job}
	signed, got, err := c.exchange(ctx, request)
	if err != nil {
		return signed, got, c.cancelOnInterrupt(ctx, job.Attempt, job.Binding, err)
	}
	return c.wait(ctx, job.Attempt, job.Binding, Digest(job), signed, got)
}

func (c *Client) Resume(ctx context.Context, id, binding, jobKey string) (Signed, Receipt, error) {
	signed, got, err := c.Status(ctx, id, binding)
	if err != nil {
		return signed, got, c.cancelOnInterrupt(ctx, id, binding, err)
	}
	return c.wait(ctx, id, binding, jobKey, signed, got)
}

func (c *Client) wait(ctx context.Context, id, binding, jobKey string, signed Signed, got Receipt) (Signed, Receipt, error) {
	for {
		if got.Job != jobKey {
			return signed, got, errors.New("runner receipt names different job inputs")
		}
		switch got.Status {
		case "returned", "failed", "stopped":
			if got.Stopped {
				return signed, got, nil
			}
			return signed, got, errors.New("runner could not confirm job termination")
		case "running":
		default:
			return signed, got, errors.New("runner returned an unknown job state")
		}
		select {
		case <-ctx.Done():
			return Signed{}, Receipt{}, c.cancelOnInterrupt(ctx, id, binding, ctx.Err())
		case <-time.After(time.Second):
		}
		var err error
		signed, got, err = c.Status(ctx, id, binding)
		if err != nil {
			return signed, got, c.cancelOnInterrupt(ctx, id, binding, err)
		}
	}
}

func (c *Client) cancelOnInterrupt(ctx context.Context, id, binding string, err error) error {
	if ctx.Err() == nil {
		return err
	}
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, _, stopErr := c.exchange(stopCtx, Request{Action: "cancel", Attempt: id, Binding: binding})
	return errors.Join(err, stopErr)
}

func (c *Client) Status(ctx context.Context, id, binding string) (Signed, Receipt, error) {
	return c.exchange(ctx, Request{Action: "status", Attempt: id, Binding: binding})
}

func (c *Client) Verify(signed Signed, attempt, binding string) (Receipt, error) {
	var got Receipt
	if !ed25519.Verify(c.cfg.ReceiptKey, signed.Receipt, signed.Signature) {
		return got, errors.New("untrusted runner receipt")
	}
	if err := decode(signed.Receipt, &got); err != nil {
		return got, err
	}
	if got.Project != c.cfg.Project || got.Attempt != attempt || got.Binding != binding ||
		c.Profile != "" && (got.Platform != c.Platform || got.Profile != c.Profile || got.Policy != Digest(c.Policy)) {
		return got, errors.New("runner receipt does not match the saved reservation and policy")
	}
	return got, nil
}

func (c *Client) exchange(ctx context.Context, request Request) (Signed, Receipt, error) {
	var signed Signed
	var got Receipt
	request.Version, request.Nonce, request.Project = Version, rand.Text(), c.cfg.Project
	data, err := json.Marshal(request)
	if err != nil || len(data) > maxMessage {
		return signed, got, errors.New("runner request exceeds its encoding limit")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.Endpoint, "/")+"/v1/workflow", bytes.NewReader(data))
	if err != nil {
		return signed, got, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return signed, got, fmt.Errorf("contact trusted runner: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return signed, got, fmt.Errorf("trusted runner returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxMessage+1))
	if err != nil || len(raw) > maxMessage {
		return signed, got, errors.New("runner response exceeds its read limit or is incomplete")
	}
	if err := decode(raw, &signed); err != nil {
		return signed, got, err
	}
	got, err = c.Verify(signed, request.Attempt, request.Binding)
	if err == nil && got.Request != BytesDigest(data) {
		err = errors.New("runner response does not identify this request")
	}
	return signed, got, err
}

func decode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("runner record must contain exactly one JSON value")
	}
	return nil
}
