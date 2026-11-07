package cli

import (
	"bufio"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/belevtsev/agent-workflows/internal/projectctx"
	"github.com/belevtsev/agent-workflows/internal/taskflow"
)

// The compact views below are one JSON line each. They keep the facts a
// coordinator acts on and drop what repeats on every call; the full output
// stays available without the flag.

// problemCheckStatuses are the doctor check statuses that need attention.
// Many missing checks are optional tools, which are still facts worth listing.
var problemCheckStatuses = []string{"missing", "degraded", "stale"}

// dirtyComparisonWarning is the fixed caveat that projectctx appends to the
// profile warnings of every Git snapshot, clean or dirty
// (internal/projectctx/capture.go:158). The summary omits it by exact text.
const dirtyComparisonWarning = "Dirty compares raw on-disk bytes and index/tree metadata without executing Git filters; filtered or normalized files may be conservatively marked dirty"

type doctorSnapshotSummary struct {
	Head        string `json:"head"`
	Branch      string `json:"branch"`
	Fingerprint string `json:"fingerprint"`
	Dirty       bool   `json:"dirty"`
	FileCount   int    `json:"file_count"`
}

type checkProblem struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorSummary struct {
	Status             string                  `json:"status"`
	Snapshot           doctorSnapshotSummary   `json:"snapshot"`
	Kinds              []string                `json:"kinds"`
	Contracts          []string                `json:"contracts"`
	Live               bool                    `json:"live,omitzero"`
	Problems           map[string]checkProblem `json:"problems"`
	Warnings           []string                `json:"warnings,omitempty"`
	OmittedFixed       int                     `json:"omitted_fixed_warnings,omitzero"`
	MissingCredentials []string                `json:"missing_credentials,omitempty"`
}

func summarizeDoctor(report projectctx.DoctorReport) doctorSummary {
	snapshot := report.Profile.Snapshot
	value := doctorSummary{
		Status:    "ok",
		Snapshot:  doctorSnapshotSummary{Head: snapshot.Head, Branch: snapshot.Branch, Fingerprint: snapshot.Fingerprint, Dirty: snapshot.Dirty, FileCount: snapshot.FileCount},
		Kinds:     report.Profile.Kinds,
		Contracts: []string{},
		Live:      report.Live,
		Problems:  map[string]checkProblem{},
	}
	if value.Kinds == nil {
		value.Kinds = []string{}
	}
	for _, contract := range report.Profile.Contracts {
		value.Contracts = append(value.Contracts, contract.Path)
	}
	for id, check := range report.Checks {
		if slices.Contains(problemCheckStatuses, check.Status) {
			value.Problems[id] = checkProblem{Status: check.Status, Detail: check.Detail}
		}
	}
	for _, warning := range slices.Concat(report.Warnings, report.Profile.Warnings) {
		if warning == dirtyComparisonWarning {
			value.OmittedFixed++
			continue
		}
		value.Warnings = append(value.Warnings, warning)
	}
	for _, name := range slices.Sorted(maps.Keys(report.Credentials.Effective)) {
		if !report.Credentials.Effective[name] {
			value.MissingCredentials = append(value.MissingCredentials, name)
		}
	}
	if len(value.Problems) > 0 || len(value.Warnings) > 0 {
		value.Status = "attention"
	}
	return value
}

type verifySummary struct {
	TaskID   string `json:"task_id"`
	Complete bool   `json:"complete"`
	// RejectedFindings holds the limitations that report a rejected blocker
	// or major finding, ahead of everything else; Limitations omits them.
	RejectedFindings        []string `json:"rejected_findings,omitempty"`
	Problems                []string `json:"problems"`
	Limitations             []string `json:"limitations"`
	OmittedFixedLimitations int      `json:"omitted_fixed_limitations"`
}

// summarizeVerification drops the fixed limitations, matched by exact text,
// moves rejected-finding limitations to RejectedFindings, and keeps every
// problem and every other task-specific limitation in order.
func summarizeVerification(value taskflow.Verification) verifySummary {
	result := verifySummary{TaskID: value.TaskID, Complete: value.Complete, Problems: value.Problems, Limitations: []string{}}
	if result.Problems == nil {
		result.Problems = []string{}
	}
	fixed := taskflow.FixedVerifyLimitations()
	for _, limitation := range value.Limitations {
		if index := slices.Index(fixed, limitation); index >= 0 {
			fixed = slices.Delete(fixed, index, index+1)
			result.OmittedFixedLimitations++
			continue
		}
		if taskflow.IsRejectedFindingLimitation(limitation) {
			result.RejectedFindings = append(result.RejectedFindings, limitation)
			continue
		}
		result.Limitations = append(result.Limitations, limitation)
	}
	return result
}

// fieldPathError is a well-formed --field path that names no value.
type fieldPathError struct {
	path, segment string
	available     []string
}

func (e *fieldPathError) Error() string {
	message := fmt.Sprintf("task record has no %q in --field %q", e.segment, e.path)
	if len(e.available) == 0 {
		return message + "; nothing to select there"
	}
	return message + "; available: " + strings.Join(e.available, ", ")
}

// maxListedKeys bounds the keys or IDs an unknown --field path lists.
const maxListedKeys = 20

// fieldIdentities are, in priority order, the members that identify an
// element of a record array: a check run by its run id and then the latest run
// of a check id, an assignment by its brief id, and worker results and
// acceptances by their assignment id.
var fieldIdentities = [][]string{{"id"}, {"brief", "id"}, {"result", "assignment_id"}, {"acceptance", "assignment_id"}, {"assignment_id"}, {"finding_id"}, {"check_id"}}

// validFieldPath reports whether path is a nonempty dotted path without empty
// segments.
func validFieldPath(path string) bool {
	if path == "" || strings.TrimSpace(path) != path {
		return false
	}
	return !slices.Contains(strings.Split(path, "."), "")
}

// selectFields returns each requested path of the record's JSON. A numeric
// segment indexes an array; any other segment selects the last array element
// whose identifying member equals it.
func selectFields(record taskflow.Record, paths []string) (map[string]jsontext.Value, error) {
	data, err := json.Marshal(record, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	result := map[string]jsontext.Value{}
	for _, path := range paths {
		value, err := selectField(jsontext.Value(data), path)
		if err != nil {
			return nil, err
		}
		result[path] = value
	}
	return result, nil
}

func selectField(current jsontext.Value, path string) (jsontext.Value, error) {
	for _, segment := range strings.Split(path, ".") {
		switch current.Kind() {
		case '{':
			var members map[string]jsontext.Value
			if err := json.Unmarshal(current, &members); err != nil {
				return nil, err
			}
			next, ok := members[segment]
			if !ok {
				return nil, &fieldPathError{path: path, segment: segment, available: firstKeys(slices.Sorted(maps.Keys(members)))}
			}
			current = next
		case '[':
			var elements []jsontext.Value
			if err := json.Unmarshal(current, &elements); err != nil {
				return nil, err
			}
			next, ok := selectElement(elements, segment)
			if !ok {
				return nil, &fieldPathError{path: path, segment: segment, available: elementKeys(elements)}
			}
			current = next
		default:
			return nil, &fieldPathError{path: path, segment: segment}
		}
	}
	return current, nil
}

func selectElement(elements []jsontext.Value, segment string) (jsontext.Value, bool) {
	if index, err := strconv.Atoi(segment); err == nil && index >= 0 && index < len(elements) && strconv.Itoa(index) == segment {
		return elements[index], true
	}
	for _, identity := range fieldIdentities {
		for i := len(elements) - 1; i >= 0; i-- {
			if id, ok := elementIdentity(elements[i], identity); ok && id == segment {
				return elements[i], true
			}
		}
	}
	return nil, false
}

func elementIdentity(element jsontext.Value, identity []string) (string, bool) {
	current := element
	for _, name := range identity {
		if current.Kind() != '{' {
			return "", false
		}
		var members map[string]jsontext.Value
		if json.Unmarshal(current, &members) != nil {
			return "", false
		}
		next, ok := members[name]
		if !ok {
			return "", false
		}
		current = next
	}
	var id string
	if json.Unmarshal(current, &id) != nil || id == "" {
		return "", false
	}
	return id, true
}

// elementKeys lists the distinct values of the identity that every element
// has and that has the fewest distinct values (check ids rather than run ids),
// or the indexes when no identity is shared.
func elementKeys(elements []jsontext.Value) []string {
	var keys []string
	for _, identity := range fieldIdentities {
		var values []string
		for _, element := range elements {
			id, ok := elementIdentity(element, identity)
			if !ok {
				values = nil
				break
			}
			if !slices.Contains(values, id) {
				values = append(values, id)
			}
		}
		if len(values) > 0 && (keys == nil || len(values) < len(keys)) {
			keys = values
		}
	}
	if keys == nil {
		keys = []string{}
		for i := range elements {
			keys = append(keys, strconv.Itoa(i))
		}
	}
	return firstKeys(keys)
}

func firstKeys(keys []string) []string {
	if len(keys) > maxListedKeys {
		return append(keys[:maxListedKeys:maxListedKeys], fmt.Sprintf("... %d more", len(keys)-maxListedKeys))
	}
	return keys
}

// Bounds of the excerpts that a failed check summary reads from its stored log.
const (
	maxFailingTests = 20
	maxErrorLines   = 20
	maxTailLines    = 10
	maxExcerptRunes = 240
	maxExcerptBytes = 4 << 10
)

var (
	failingTest = regexp.MustCompile(`--- FAIL: (\S+)`)
	errorShape  = regexp.MustCompile(`\.go:\d+(:\d+)?:|panic:|\bFAIL\b|\b(error|Error)\b`)
)

// logExcerpt is what a failed check summary quotes from its stored log.
type logExcerpt struct {
	FailingTests []string
	ErrorLines   []string
	Tail         []string
	Unavailable  string
}

// excerptLog reads at most the stored log's bounded size from inside the task
// directory. A missing or unreadable log yields only a short reason.
func excerptLog(task, name string) logExcerpt {
	file, err := os.OpenInRoot(task, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return logExcerpt{Unavailable: "log missing"}
		}
		return logExcerpt{Unavailable: "log unreadable"}
	}
	defer file.Close()
	var excerpt logExcerpt
	var tail []string
	total := 0
	scanner := bufio.NewScanner(io.LimitReader(file, taskflow.MaxLogBytes))
	// One more byte than the reader can return, so the final line always fits.
	scanner.Buffer(make([]byte, 0, 64<<10), taskflow.MaxLogBytes+1)
	for scanner.Scan() {
		line := excerptLine(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		tail = append(tail, line)
		if len(tail) > maxTailLines {
			tail = tail[1:]
		}
		if match := failingTest.FindStringSubmatch(line); match != nil && len(excerpt.FailingTests) < maxFailingTests && !slices.Contains(excerpt.FailingTests, match[1]) {
			excerpt.FailingTests = append(excerpt.FailingTests, match[1])
		}
		if len(excerpt.ErrorLines) < maxErrorLines && total+len(line) <= maxExcerptBytes && errorShape.MatchString(line) {
			excerpt.ErrorLines = append(excerpt.ErrorLines, line)
			total += len(line)
		}
	}
	if scanner.Err() != nil {
		return logExcerpt{Unavailable: "log unreadable"}
	}
	if len(excerpt.FailingTests) == 0 && len(excerpt.ErrorLines) == 0 {
		excerpt.Tail = tail
	}
	return excerpt
}

// excerptLine makes one log line valid UTF-8 without a trailing carriage
// return and cuts it to the excerpt line limit.
func excerptLine(line string) string {
	line = strings.ToValidUTF8(strings.TrimRight(line, "\r"), "�")
	if utf8.RuneCountInString(line) <= maxExcerptRunes {
		return line
	}
	count := 0
	for index := range line {
		if count == maxExcerptRunes {
			return line[:index]
		}
		count++
	}
	return line
}
