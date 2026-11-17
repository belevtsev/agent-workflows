package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
)

const maxWorkerReturn = 64 << 10
const workerReturnScope = "Untrusted worker claims only. Local observations bind current source, write bounds and existing check evidence; no model execution, skill loading, task completion, review or approval is certified. The interval remains active until task finish."

type workerFinding struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

type workerLoadedSkill struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

type workerReturnRequest struct {
	Version      int                `json:"version"`
	DispatchID   string             `json:"dispatch_id"`
	Task         string             `json:"task"`
	PacketSHA256 string             `json:"packet_sha256"`
	Worktree     string             `json:"worktree,omitempty"`
	Source       *contextpack.State `json:"source,omitempty"`
	ChangedPaths []string           `json:"changed_paths"`
	Checks       []stageCheck       `json:"checks"`
	Findings     []workerFinding    `json:"findings"`
	Blockers     []string           `json:"blockers"`
	Loaded       *struct {
		BriefSHA256 string              `json:"brief_sha256"`
		Skills      []workerLoadedSkill `json:"skills"`
	} `json:"loaded,omitempty"`
	Worker *struct {
		Host      string `json:"host"`
		ID        string `json:"id"`
		Completed bool   `json:"completed"`
	} `json:"worker,omitempty"`
}

type workerReturnReceipt struct {
	Version   int                 `json:"version"`
	ID        string              `json:"return_id"`
	Created   time.Time           `json:"created_at"`
	Authority string              `json:"authority"`
	Claims    workerReturnRequest `json:"claims"`
	Observed  stageReceipt        `json:"observed"`
	Scope     string              `json:"scope"`
}

func readWorkerReturn(input io.Reader, filename string) (workerReturnRequest, error) {
	var request workerReturnRequest
	if filename != "-" {
		f, err := os.Open(filename)
		if err != nil {
			return request, err
		}
		defer func() { _ = f.Close() }()
		input = f
	}
	if input == nil {
		return request, errors.New("worker return input is missing")
	}
	data, err := io.ReadAll(io.LimitReader(input, maxWorkerReturn+1))
	if err != nil || len(data) > maxWorkerReturn || !utf8.Valid(data) {
		return request, errors.New("worker return must be valid UTF-8 JSON at most 65536 bytes")
	}
	if err := uniqueReturnKeys(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return request, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return request, errors.New("worker return must contain exactly one JSON object")
	}
	if err := validateWorkerReturn(request); err != nil {
		return request, err
	}
	return request, nil
}

// Ambiguous duplicate keys must not let the sender and coordinator see different claims.
func uniqueReturnKeys(d *json.Decoder, depth int) error {
	if depth > 16 {
		return errors.New("worker return JSON is too deeply nested")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name := strings.ToLower(fmt.Sprint(key))
			if seen[name] {
				return errors.New("worker return contains duplicate JSON fields")
			}
			seen[name] = true
		}
		if err := uniqueReturnKeys(d, depth+1); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

func returnText(text string, maxBytes int) bool {
	return len(text) <= maxBytes && strings.TrimSpace(text) != "" && utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

func returnPath(name string) bool {
	return returnText(name, 4096) && filepath.IsLocal(name) && path.Clean(name) == name && name != "." && !strings.ContainsAny(name, "\\:") && name != ".git" && !strings.HasPrefix(name, ".git/")
}

func validateWorkerReturn(r workerReturnRequest) error {
	if r.Version != 1 || !stageValidID(r.DispatchID) || !stageValidID(r.PacketSHA256) || !taskName.MatchString(r.Task) || r.ChangedPaths == nil || r.Checks == nil || r.Findings == nil || r.Blockers == nil {
		return errors.New("worker return requires version 1, dispatch/task/packet identities and explicit changed_paths, checks, findings and blockers arrays")
	}
	if len(r.ChangedPaths) > 256 || len(r.Checks) > 128 || len(r.Findings) > 128 || len(r.Blockers) > 64 {
		return errors.New("worker return exceeds path, check, finding or blocker limits")
	}
	if err := validateReturnChanges(r); err != nil {
		return err
	}
	if err := validateReturnMessages(r); err != nil {
		return err
	}
	return validateReturnHostClaims(r)
}

func validateReturnChanges(r workerReturnRequest) error {
	seen := map[string]bool{}
	for _, name := range r.ChangedPaths {
		if !returnPath(name) || seen[name] {
			return errors.New("changed paths must be unique clean repository-relative source paths")
		}
		seen[name] = true
	}
	seen = map[string]bool{}
	for _, check := range r.Checks {
		if !returnText(check.ID, 256) || !returnText(check.State, 64) || len(check.RunID) > 256 || strings.ContainsRune(check.RunID, 0) || seen[check.ID] {
			return errors.New("invalid or duplicate worker check claim")
		}
		seen[check.ID] = true
	}
	return nil
}

func validateReturnMessages(r workerReturnRequest) error {
	for _, finding := range r.Findings {
		if finding.Severity != "blocker" && finding.Severity != "major" && finding.Severity != "minor" {
			return errors.New("findings require severity blocker, major or minor")
		}
		if !returnPath(finding.Path) || finding.Line < 1 || !returnText(finding.Message, 4096) {
			return errors.New("findings require a source path, positive line and bounded message")
		}
	}
	return nil
}

func validateReturnHostClaims(r workerReturnRequest) error {
	for _, blocker := range r.Blockers {
		if !returnText(blocker, 4096) {
			return errors.New("blockers must be nonempty bounded text")
		}
	}
	if r.Loaded != nil && (len(r.Loaded.Skills) > 64 || r.Loaded.BriefSHA256 != "" && !stageValidID(r.Loaded.BriefSHA256)) {
		return errors.New("invalid worker load claims")
	}
	if r.Worker != nil && (!returnText(r.Worker.ID, 256) || !uxValidHost(r.Worker.Host)) {
		return errors.New("worker identity requires a valid host and bounded id")
	}
	return nil
}

func runTaskReturn(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("task return")
	filename := flags.String("request", "", "")
	budget := flags.Int("max-bytes", contextpack.DefaultBytes, "")
	flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if p.dash || len(p.pos) != 1 || !stageValidID(p.pos[0]) || *filename == "" {
		return usagef("return requires a dispatch id and --request <file|->")
	}
	if err := compactBudget(*budget); err != nil {
		return err
	}
	request, err := readWorkerReturn(cfg.In, *filename)
	if err != nil {
		return usagef("invalid worker return: %v", err)
	}
	if request.DispatchID != p.pos[0] {
		return usagef("worker return dispatch id does not match its operand")
	}
	return ingestWorkerReturn(ctx, cfg, request, *budget)
}

func ingestWorkerReturn(ctx context.Context, cfg Config, request workerReturnRequest, budget int) error {
	s, e, err := compactEngine(ctx, cfg)
	if err != nil {
		return err
	}
	ledger, err := openStageLedger(ctx, s)
	if err != nil {
		return compactFailure(err)
	}
	defer ledger.close()
	record, err := ledger.record(request.DispatchID, s.repo.Root)
	if err != nil {
		return compactFailure(err)
	}
	active, err := ledger.active()
	if err != nil || active != request.DispatchID {
		return failf("worker return requires the active unfinished dispatch: %v", err)
	}
	if err := matchWorkerReturn(record, request); err != nil {
		return compactFailure(err)
	}
	observed, err := finishStageReceipt(ctx, s, e, record, stageFinishOptions{id: request.DispatchID, expect: "any"})
	if err != nil {
		return compactFailure(err)
	}
	if request.Source != nil && *request.Source != observed.State {
		return failf("worker return source assertion is stale")
	}
	if err := matchWorkerChecks(request.Checks, observed.Checks); err != nil {
		return compactFailure(err)
	}
	// Observations are not a finished-stage receipt: the active pointer is retained.
	observed.ID, observed.Status, observed.ExpectChecks = "", "observed", ""
	receipt := workerReturnReceipt{Version: 1, Created: time.Now().UTC(), Authority: "untrusted-worker-claims", Claims: request, Observed: observed, Scope: workerReturnScope}
	name := request.DispatchID + ".return.json"
	if err := reuseWorkerReturn(ledger, record, name, &receipt); err != nil {
		return err
	}
	receipt.ID = contextpack.Key(receipt)
	data, err := boundedStageJSON(receipt, budget)
	if err != nil {
		return err
	}
	if err := verifyStageBeforePublication(ctx, s, e, record, stageReceipt{Status: "finished", State: observed.State, EvidenceKey: observed.EvidenceKey, Verified: observed.Verified, Checks: observed.Checks, Warnings: observed.Warnings, EvidenceError: observed.EvidenceError}); err != nil {
		return compactFailure(err)
	}
	if err := ledger.write(name, receipt); err != nil {
		return compactFailure(err)
	}
	_, err = cfg.Out.Write(data)
	return err
}

func reuseWorkerReturn(ledger *stageLedger, record stageRecord, name string, receipt *workerReturnReceipt) error {
	var saved workerReturnReceipt
	if err := ledger.read(name, &saved); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return compactFailure(err)
	}
	id := saved.ID
	saved.ID = ""
	if id != contextpack.Key(saved) || saved.Version != 1 || saved.Authority != receipt.Authority || saved.Scope != receipt.Scope || saved.Created.Before(record.Created) || saved.Created.After(time.Now().Add(time.Minute)) || contextpack.Key(saved.Claims) != contextpack.Key(receipt.Claims) {
		return failf("saved worker return was tampered with or differs from this handback")
	}
	// Time and the synthetic observation id are not source or evidence identity.
	receipt.Created, receipt.Observed.Created = saved.Created, saved.Observed.Created
	if contextpack.Key(receipt) != contextpack.Key(saved) {
		return failf("saved worker return is stale: source or check evidence changed")
	}
	return nil
}

func matchWorkerReturn(record stageRecord, r workerReturnRequest) error {
	p := record.Prepared
	if r.Task != p.Brief.Task || r.PacketSHA256 != p.Dispatch.SHA256 || r.Worktree != "" && r.Worktree != record.Worktree || r.Worker != nil && r.Worker.Host != p.Brief.Host {
		return errors.New("worker return does not match the dispatch task, packet, host or worktree")
	}
	if r.Loaded != nil {
		if r.Loaded.BriefSHA256 != "" && r.Loaded.BriefSHA256 != p.Dispatch.SHA256 {
			return errors.New("worker loaded-packet claim does not match the original")
		}
		seen := map[string]bool{}
		for _, loaded := range r.Loaded.Skills {
			found := false
			for _, skill := range p.Brief.Skills {
				found = found || loaded.ID == skill.ID && loaded.SHA256 == skill.SHA256
			}
			if !found || seen[loaded.ID] {
				return errors.New("worker loaded-skill claim is not a unique original selected skill")
			}
			seen[loaded.ID] = true
		}
	}
	return nil
}

func matchWorkerChecks(claims, observed []stageCheck) error {
	for _, claim := range claims {
		found := false
		for _, check := range observed {
			found = found || claim == check
		}
		if !found {
			return fmt.Errorf("worker check %q does not match current declared check evidence", claim.ID)
		}
	}
	return nil
}
