package outcomes

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

type entry struct {
	observation Observation
	file        string
	index       int
	problems    []string
	cost        *float64
	derivedCost bool
}

type wireDocument struct {
	SchemaVersion int              `json:"schema_version"`
	Observations  []jsontext.Value `json:"observations"`
}

// Summarize reads only regular files beneath InputRoot. It returns a useful
// partial report alongside ErrInvalidInput when other files or samples fail;
// invalid and failed samples never contribute to successful metric totals.
func Summarize(ctx context.Context, options Options) (Report, error) {
	report := Report{
		SchemaVersion: SchemaVersion, Status: "ok", Limits: limits(),
		Counts:      Counts{FilesRequested: len(options.Files)},
		Comparisons: Comparisons{Baseline: options.Baseline, Candidate: options.Candidate},
		Interpretation: []string{
			"Durations and deltas describe supplied observations only; synthetic acceptance and real-project diagnostics do not establish productivity.",
			"Metric coverage counts successful valid observations; failed and invalid observations are reported separately. Missing measurements remain unknown.",
			"Comparisons require the same project, case, workload, scope, source and environment, with exactly one successful observation per selected variant.",
			"Candidate-minus-baseline deltas do not imply causation or extrapolated savings. A zero baseline or nonfinite ratio leaves relative change unknown.",
			"Costs use supplied billing evidence or complete actual usage and explicit rates. Evidence references and rate reliability are not independently verified.",
		},
	}
	if err := ctx.Err(); err != nil {
		report.Status = "cancelled"
		return report, err
	}
	if len(options.Files) < 1 || len(options.Files) > MaxFiles {
		return inputError(report, fmt.Sprintf("supply between 1 and %d files", MaxFiles))
	}
	if !validText(options.Baseline, 256) || !validText(options.Candidate, 256) || options.Baseline == options.Candidate {
		return inputError(report, "baseline and candidate must be distinct nonempty variant names")
	}
	rootPath, err := filepath.Abs(options.InputRoot)
	if err != nil || options.InputRoot == "" {
		return inputError(report, "input-root must name a directory")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return inputError(report, fmt.Sprintf("cannot open input-root: %s", boundedError(err)))
	}
	defer root.Close()
	var entries []entry
	files := make(map[string]bool)
	totalBytes := 0
	for _, file := range options.Files {
		if err := ctx.Err(); err != nil {
			report.Status = "cancelled"
			return report, err
		}
		relative, err := inputPath(rootPath, file)
		if err != nil {
			report.FileErrors = append(report.FileErrors, FileIssue{file, err.Error()})
			continue
		}
		if files[relative] {
			report.FileErrors = append(report.FileErrors, FileIssue{file, "duplicate input path"})
			continue
		}
		files[relative] = true
		data, err := readFile(ctx, root, relative, MaxTotalBytes-totalBytes)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				report.Status = "cancelled"
				return report, err
			}
			report.FileErrors = append(report.FileErrors, FileIssue{file, boundedError(err)})
			continue
		}
		totalBytes += len(data)
		report.Counts.FilesRead++
		var document wireDocument
		if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil {
			report.FileErrors = append(report.FileErrors, FileIssue{file, "invalid observation document: " + boundedError(err)})
			continue
		}
		if document.SchemaVersion != SchemaVersion {
			report.FileErrors = append(report.FileErrors, FileIssue{file, "unsupported or missing schema_version; expected 1"})
			continue
		}
		if len(document.Observations) == 0 {
			report.FileErrors = append(report.FileErrors, FileIssue{file, "observations must contain at least one observation"})
			continue
		}
		if len(document.Observations) > MaxObservations-report.Counts.Seen {
			report.FileErrors = append(report.FileErrors, FileIssue{file, "observation count exceeds the total read limit"})
			continue
		}
		report.Counts.Seen += len(document.Observations)
		for index, raw := range document.Observations {
			e := entry{file: file, index: index + 1}
			if err := json.Unmarshal(raw, &e.observation, json.RejectUnknownMembers(true)); err != nil {
				// Keep a safe ID hint without accepting any fields from a failed decode.
				var hint struct {
					ObservationID string `json:"observation_id"`
				}
				_ = json.Unmarshal(raw, &hint)
				e.observation = Observation{ObservationID: hint.ObservationID}
				e.problems = []string{"invalid observation JSON: " + boundedError(err)}
			} else {
				e.validate()
			}
			entries = append(entries, e)
		}
	}
	validateRepeated(entries)
	var accepted []entry
	for _, e := range entries {
		if len(e.problems) > 0 {
			report.Counts.Invalid++
			id := e.observation.ObservationID
			if !validText(id, 256) {
				id = ""
			}
			report.InvalidSamples = append(report.InvalidSamples, SampleIssue{e.file, e.index, id, e.problems})
			continue
		}
		report.Counts.Accepted++
		if e.observation.Status == "succeeded" {
			report.Counts.Succeeded++
		} else {
			report.Counts.Failed++
		}
		accepted = append(accepted, e)
	}
	if err := ctx.Err(); err != nil {
		report.Status = "cancelled"
		return report, err
	}
	report.Summaries = summaries(accepted)
	report.Comparisons = comparisons(accepted, options.Baseline, options.Candidate)
	if len(report.FileErrors) > 0 || report.Counts.Invalid > 0 {
		report.Status = "invalid_input"
		return report, ErrInvalidInput
	}
	return report, nil
}

func inputError(report Report, problem string) (Report, error) {
	report.Status = "invalid_input"
	report.FileErrors = append(report.FileErrors, FileIssue{Problem: problem})
	return report, ErrInvalidInput
}

func inputPath(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" || strings.IndexByte(path, 0) >= 0 {
		return "", errors.New("input path is empty or contains NUL")
	}
	if filepath.IsAbs(path) {
		var err error
		path, err = filepath.Rel(root, path)
		if err != nil {
			return "", errors.New("input path cannot be made relative to input-root")
		}
	}
	// Reject traversal syntax even if cleaning would bring it back into the root.
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return "", errors.New("input path must not traverse parent directories")
		}
	}
	path = filepath.Clean(path)
	if !filepath.IsLocal(path) || path == "." {
		return "", errors.New("input path must name a file beneath input-root")
	}
	return path, nil
}

func readFile(ctx context.Context, root *os.Root, path string, budget int) ([]byte, error) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for index := range parts {
		info, err := root.Lstat(filepath.FromSlash(strings.Join(parts[:index+1], "/")))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symlink input paths and directories are not permitted")
		}
		if index+1 == len(parts) && !info.Mode().IsRegular() {
			return nil, errors.New("input must be a regular file")
		}
	}
	// os.Root also prevents path escape if a checked component changes before Open.
	file, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	limit := min(MaxFileBytes, budget)
	if limit < 1 || info.Size() > int64(limit) {
		return nil, errors.New("input exceeds per-file or total byte read limit")
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, file}, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("input exceeds per-file or total byte read limit")
	}
	return data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}

func boundedError(err error) string {
	message := err.Error()
	if len(message) > 512 {
		return message[:512] + "..."
	}
	return message
}

func validText(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

func validNumber(number float64) bool {
	return !math.IsNaN(number) && !math.IsInf(number, 0) && number >= 0 && number <= MaxMetric
}

func (e *entry) validate() {
	o := &e.observation
	for _, field := range []struct {
		name, value string
		limit       int
	}{
		{"observation_id", o.ObservationID, 256}, {"case_id", o.CaseID, 256}, {"task_id", o.TaskID, 256},
		{"project.identity", o.Project.Identity, 256}, {"variant", o.Variant, 256}, {"workload", o.Workload, 256},
		{"source.fingerprint", o.Source.Fingerprint, 512}, {"environment", o.Environment, 512},
	} {
		if !validText(field.value, field.limit) {
			e.problems = append(e.problems, field.name+" must be nonempty bounded text without control characters or surrounding whitespace")
		}
	}
	if o.Source.Revision != "" && !validText(o.Source.Revision, 256) {
		e.problems = append(e.problems, "source.revision must be bounded text when known; omit it when the project has no known revision")
	}
	if !slices.Contains([]string{"go", "terraform", "roblox", "node", "python", "other"}, o.Project.Kind) {
		e.problems = append(e.problems, "project.kind must be go, terraform, roblox, node, python or other")
	}
	if !slices.Contains([]string{"synthetic_acceptance", "real_project_diagnostic", "real_project_productivity"}, o.Scope) {
		e.problems = append(e.problems, "scope must explicitly distinguish synthetic acceptance, real-project diagnostics or real-project productivity")
	}
	if o.Status != "succeeded" && o.Status != "failed" {
		e.problems = append(e.problems, "status must be succeeded or failed")
	}
	if o.Status == "succeeded" && o.DurationMS == nil {
		e.problems = append(e.problems, "duration_ms is required for a successful observation")
	}
	if o.DurationMS != nil && !validNumber(*o.DurationMS) {
		e.problems = append(e.problems, "duration_ms must be finite, nonnegative and within the metric limit")
	}
	if len(o.Evidence) < 1 || len(o.Evidence) > 32 {
		e.problems = append(e.problems, "evidence must contain between 1 and 32 references")
	}
	for _, ref := range o.Evidence {
		if !validText(ref, 1024) {
			e.problems = append(e.problems, "evidence references must be nonempty bounded text")
			break
		}
	}
	for name, value := range counters(*o) {
		if value != nil && (*value < 0 || *value > int64(MaxMetric)) {
			e.problems = append(e.problems, name+" must be a nonnegative integer within the metric limit")
		}
	}
	if q := o.Quality; q != nil && q.CheckedReferences != nil && q.IncorrectReferences != nil && *q.IncorrectReferences > *q.CheckedReferences {
		e.problems = append(e.problems, "incorrect_references cannot exceed checked_references")
	}
	if u := o.Usage; u != nil {
		if u.InputTokens != nil {
			// Known breakdown counters can never exceed total input; all four
			// known must equal it exactly.
			known, sum := 0, int64(0)
			for _, part := range []*int64{u.UncachedInputTokens, u.CacheWrite5mTokens, u.CacheWrite1hTokens, u.CacheReadTokens} {
				if part != nil {
					known++
					sum += *part
				}
			}
			switch {
			case known == 4 && sum != *u.InputTokens:
				e.problems = append(e.problems, "uncached_input_tokens, cache_write_5m_tokens, cache_write_1h_tokens and cache_read_tokens must sum to input_tokens when all are known")
			case known > 0 && known < 4 && sum > *u.InputTokens:
				e.problems = append(e.problems, "the known input-breakdown counters (uncached_input_tokens, cache_write_5m_tokens, cache_write_1h_tokens, cache_read_tokens) must not sum to more than input_tokens")
			}
		}
		if u.ThinkingTokens != nil && u.OutputTokens != nil && *u.ThinkingTokens > *u.OutputTokens {
			e.problems = append(e.problems, "thinking_tokens cannot exceed output_tokens")
		}
	}
	e.validateCost()
	slices.Sort(e.problems)
}

func (e *entry) validateCost() {
	o := e.observation
	if o.Cost == nil {
		return
	}
	c := o.Cost
	if len(c.Currency) != 3 || strings.ContainsFunc(c.Currency, func(r rune) bool { return r < 'A' || r > 'Z' }) {
		e.problems = append(e.problems, "cost.currency must be a three-letter uppercase currency code")
	}
	if (c.Amount == nil) == (c.Rates == nil) {
		e.problems = append(e.problems, "cost must specify exactly one of an actual amount or complete rates")
		return
	}
	if c.Amount != nil {
		if !validNumber(*c.Amount) || !validText(c.Evidence, 1024) {
			e.problems = append(e.problems, "actual cost requires a finite nonnegative bounded amount and billing evidence")
			return
		}
		e.cost = c.Amount
		return
	}
	rates := c.Rates
	if c.Evidence != "" || !validText(rates.Evidence, 1024) {
		e.problems = append(e.problems, "derived cost requires rates.evidence only, identifying the reliable supplied rate source")
		return
	}
	if rates.CacheWrite5mPerMillion != nil || rates.CacheWrite1hPerMillion != nil || rates.CacheReadPerMillion != nil {
		e.validateCacheRatedCost()
		return
	}
	if o.Usage == nil || o.Usage.InputTokens == nil || o.Usage.OutputTokens == nil || o.Usage.APIRequests == nil || rates.InputPerMillion == nil || rates.OutputPerMillion == nil || rates.APIRequest == nil {
		e.problems = append(e.problems, "derived cost requires all three actual usage counters and all three explicit rates")
		return
	}
	for _, rate := range []*float64{rates.InputPerMillion, rates.OutputPerMillion, rates.APIRequest} {
		if !validNumber(*rate) {
			e.problems = append(e.problems, "cost rates must be finite, nonnegative and within the metric limit")
			return
		}
	}
	amount := float64(*o.Usage.InputTokens)/1e6**rates.InputPerMillion + float64(*o.Usage.OutputTokens)/1e6**rates.OutputPerMillion + float64(*o.Usage.APIRequests)**rates.APIRequest
	if !validNumber(amount) {
		e.problems = append(e.problems, "derived cost exceeds the finite nonnegative metric limit")
		return
	}
	e.cost, e.derivedCost = &amount, true
}

// validateCacheRatedCost prices uncached input, both cache-write classes and
// cache reads separately. Every rate and its usage counter must be explicit.
func (e *entry) validateCacheRatedCost() {
	u, rates := e.observation.Usage, e.observation.Cost.Rates
	if u == nil || u.UncachedInputTokens == nil || u.CacheWrite5mTokens == nil || u.CacheWrite1hTokens == nil || u.CacheReadTokens == nil || u.OutputTokens == nil || u.APIRequests == nil {
		e.problems = append(e.problems, "derived cost with cache rates requires uncached_input_tokens, cache_write_5m_tokens, cache_write_1h_tokens, cache_read_tokens, output_tokens and api_requests")
		return
	}
	all := []*float64{rates.InputPerMillion, rates.OutputPerMillion, rates.APIRequest, rates.CacheWrite5mPerMillion, rates.CacheWrite1hPerMillion, rates.CacheReadPerMillion}
	for _, rate := range all {
		if rate == nil {
			e.problems = append(e.problems, "derived cost with any cache rate requires all six explicit rates")
			return
		}
	}
	for _, rate := range all {
		if !validNumber(*rate) {
			e.problems = append(e.problems, "cost rates must be finite, nonnegative and within the metric limit")
			return
		}
	}
	amount := float64(*u.UncachedInputTokens)/1e6**rates.InputPerMillion + float64(*u.CacheWrite5mTokens)/1e6**rates.CacheWrite5mPerMillion +
		float64(*u.CacheWrite1hTokens)/1e6**rates.CacheWrite1hPerMillion + float64(*u.CacheReadTokens)/1e6**rates.CacheReadPerMillion +
		float64(*u.OutputTokens)/1e6**rates.OutputPerMillion + float64(*u.APIRequests)**rates.APIRequest
	if !validNumber(amount) {
		e.problems = append(e.problems, "derived cost exceeds the finite nonnegative metric limit")
		return
	}
	e.cost, e.derivedCost = &amount, true
}

func validateRepeated(entries []entry) {
	ids, tasks := make(map[string][]int), make(map[string][]int)
	for index, e := range entries {
		o := e.observation
		if o.ObservationID != "" {
			ids[o.ObservationID] = append(ids[o.ObservationID], index)
		}
		if o.TaskID != "" {
			tasks[o.TaskID] = append(tasks[o.TaskID], index)
		}
	}
	for _, group := range []map[string][]int{ids, tasks} {
		for _, indices := range group {
			if len(indices) > 1 {
				for _, index := range indices {
					entries[index].problems = append(entries[index].problems, "duplicate observation_id or task_id; every conflicting sample is excluded")
				}
			}
		}
	}
	for index := range entries {
		slices.Sort(entries[index].problems)
		entries[index].problems = slices.Compact(entries[index].problems)
	}
}
