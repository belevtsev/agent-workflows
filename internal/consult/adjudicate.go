package consult

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/belevtsev/agent-workflows/internal/modelpolicy"
)

// investigationConfidence is the answer confidence at or above which a
// dissenting adviser's judgment must be investigated, not only rejected.
const investigationConfidence = 0.7

var (
	revisionPattern = regexp.MustCompile(`^(?:[0-9a-f]{7,64}|sha256:[0-9a-f]{64})$`)
	linePattern     = regexp.MustCompile(`^[1-9][0-9]*$`)
	// adviserName matches, case-insensitively and on whole words, any name that
	// would make a check or observation cite adviser output.
	adviserName = adviserNamePattern()
)

// adviserNamePattern builds the whole-word, case-insensitive adviser name
// rule: fixed names and words, every known provider ID and every single
// pinned model ID, both taken from the policy pin rules. Jev model IDs are
// covered by the word jev. A word boundary is the start or end of the text or
// any character other than a letter or digit, so an underscore, hyphen or dot
// separates words (jev_answer names jev) while letters and digits join them
// (snapshot does not name snap).
func adviserNamePattern() *regexp.Regexp {
	names := []string{"jev", "snap", "subq", `openai\s+decisions`, "typesafe", "advisers?", "advisors?"}
	for _, provider := range providerOrder {
		names = append(names, regexp.QuoteMeta(provider))
		if model, pinned := modelpolicy.PinnedModel(provider); pinned {
			names = append(names, regexp.QuoteMeta(model))
		}
	}
	return regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:` + strings.Join(names, "|") + `)(?:$|[^\p{L}\p{N}])`)
}

var errAdviserReference = errors.New("evidence and verification must reference source, checks or observations, not adviser output")

func noSpace(value string) bool {
	return value != "" && !strings.ContainsFunc(value, unicode.IsSpace) && !strings.ContainsFunc(value, unicode.IsControl)
}

// checkSource validates source:<path>:<start>[-<end>]@<revision>.
func checkSource(reference string) error {
	at := strings.LastIndexByte(reference, '@')
	if at < 0 {
		return errors.New("source reference needs @<revision>")
	}
	location, revision := reference[:at], reference[at+1:]
	if !revisionPattern.MatchString(revision) {
		return errors.New("source revision must be 7 to 64 lowercase hex digits or sha256: and 64 hex digits")
	}
	colon := strings.LastIndexByte(location, ':')
	if colon < 0 {
		return errors.New("source reference needs :<start>[-<end>] lines")
	}
	file, lines := location[:colon], location[colon+1:]
	if file == "" || path.IsAbs(file) || path.Clean(file) != file || file == "." || file == ".." || strings.HasPrefix(file, "../") ||
		strings.ContainsFunc(file, unicode.IsControl) || strings.ContainsRune(file, '\\') {
		return errors.New("source path must be a clean relative path")
	}
	start, end, ranged := strings.Cut(lines, "-")
	if !ranged {
		end = start
	}
	if !linePattern.MatchString(start) || !linePattern.MatchString(end) {
		return errors.New("source lines must be positive integers")
	}
	first, err1 := strconv.Atoi(start)
	last, err2 := strconv.Atoi(end)
	if err1 != nil || err2 != nil || last < first {
		return errors.New("source line range must end at or after its start")
	}
	return nil
}

// checkReference validates one evidence or verification entry.
func checkReference(value any) error {
	reference, ok := text(value)
	if !ok {
		return errAdviserReference
	}
	switch {
	case strings.HasPrefix(reference, "source:"):
		return checkSource(strings.TrimPrefix(reference, "source:"))
	case strings.HasPrefix(reference, "check:"):
		id, run, hasRun := strings.Cut(strings.TrimPrefix(reference, "check:"), "@")
		if !noSpace(id) || hasRun && (!noSpace(run) || strings.ContainsRune(run, '@')) {
			return errors.New("check reference needs nonblank check and run IDs without whitespace")
		}
		if adviserName.MatchString(id) || hasRun && adviserName.MatchString(run) {
			return fmt.Errorf("check and run IDs must not name an adviser: %w", errAdviserReference)
		}
		return nil
	case strings.HasPrefix(reference, "observation:"):
		observation := strings.TrimPrefix(reference, "observation:")
		if blank(observation) {
			return errors.New("observation must not be blank")
		}
		if adviserName.MatchString(observation) {
			return fmt.Errorf("observation must not name an adviser: %w", errAdviserReference)
		}
		return nil
	}
	return errAdviserReference
}

func checkReferences(value any, field string) ([]any, error) {
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list", field)
	}
	for index, entry := range entries {
		if err := checkReference(entry); err != nil {
			if _, isText := entry.(string); !isText {
				return nil, fmt.Errorf("%s entry %d: %w", field, index, err)
			}
			return nil, fmt.Errorf("%s entry %d %q: %w", field, index, entry, err)
		}
	}
	return entries, nil
}

func confidenceText(value any) string {
	switch item := value.(type) {
	case integer:
		return string(item)
	case float64:
		return strconv.FormatFloat(item, 'g', -1, 64)
	}
	return "unknown"
}

// checkDecisionItem applies the D4 rules to one question's decision and
// reports whether it is resolved. Errors name the question and, where one is
// at fault, the provider.
func (c *checkedRecord) checkDecisionItem(value any, id string) (bool, error) {
	fail := func(format string, args ...any) (bool, error) {
		return false, fmt.Errorf("question %s: %s", id, fmt.Sprintf(format, args...))
	}
	item, ok := mapping(value)
	if !ok || !item.exactly("status", "choice", "reason", "evidence", "verification", "rejected_judgments", "investigations", "agreement_not_independent") {
		return fail("decision item must have exactly status, choice, reason, evidence, verification, rejected_judgments, investigations and agreement_not_independent")
	}
	status := item.get("status")
	if status != "resolved" && status != "unresolved" {
		return fail("decision status must be resolved or unresolved")
	}
	if !textValue(item.get("reason")) {
		return fail("decision needs a nonblank reason")
	}
	independent, ok := item.get("agreement_not_independent").(bool)
	if !ok {
		return fail("agreement_not_independent must be a boolean")
	}
	evidence, err := checkReferences(item.get("evidence"), "evidence")
	if err != nil {
		return fail("%v", err)
	}
	verification, err := checkReferences(item.get("verification"), "verification")
	if err != nil {
		return fail("%v", err)
	}
	rejected, ok := mapping(item.get("rejected_judgments"))
	if !ok {
		return fail("rejected_judgments must map providers to reasons")
	}
	investigations, ok := mapping(item.get("investigations"))
	if !ok {
		return fail("investigations must map providers to notes")
	}
	if status == "unresolved" {
		switch {
		case item.get("choice") != nil:
			return fail("an unresolved decision must have a null choice")
		case rejected.size() > 0 || investigations.size() > 0:
			return fail("an unresolved decision must not reject or investigate adviser judgments")
		case independent:
			return fail("an unresolved decision must set agreement_not_independent to false")
		}
		return false, nil
	}
	criteria := c.shared.get("questions").(*object).get(id).(*object).get("criteria").(*object)
	choice, ok := text(item.get("choice"))
	if !ok || !criteria.has(choice) {
		return fail("decision choice is not an allowed option")
	}
	if choice == "insufficient_evidence" {
		return fail("insufficient_evidence cannot be a resolved choice; record the decision as unresolved")
	}
	if len(evidence) == 0 || len(verification) == 0 {
		return fail("a resolved decision needs at least one evidence and one verification entry")
	}
	compared := c.compared.get(id).(*object)
	choices := compared.get("choices").(*object)
	var dissent []string
	for _, name := range choices.names {
		if choices.get(name) != choice {
			dissent = append(dissent, name)
		}
	}
	for _, name := range dissent {
		if !rejected.has(name) {
			return fail("rejected_judgments must give the reason for rejecting %s, which chose %v", name, choices.get(name))
		}
		if !textValue(rejected.get(name)) {
			return fail("rejected_judgments reason for %s must be nonblank", name)
		}
	}
	for _, name := range rejected.names {
		if !choices.has(name) || choices.get(name) == choice {
			return fail("rejected_judgments names %s, which did not choose a different option", name)
		}
	}
	for _, name := range investigations.names {
		if !choices.has(name) || choices.get(name) == choice {
			return fail("investigations names %s, which did not choose a different option", name)
		}
		if !textValue(investigations.get(name)) {
			return fail("investigations note for %s must be nonblank", name)
		}
	}
	for _, name := range dissent {
		answer := c.providers.get(name).(*object).get("result").(*object).get("answers").(*object).get(id).(*object)
		confidence := answer.get("confidence")
		if floatOf(confidence) >= investigationConfidence && !investigations.has(name) {
			return fail("investigations must record a note for %s, which chose %v with confidence %s", name, choices.get(name), confidenceText(confidence))
		}
	}
	if hasUntrusted(c.shared.get("state")) && compared.get("status") == "agreement" && !independent {
		return fail("agreement_not_independent must be true: advisers agreed on a request that carries state.untrusted text")
	}
	return true, nil
}

// checkDecision validates a final_decision value against the record and
// returns its status.
func (c *checkedRecord) checkDecision(value any) (string, error) {
	decision, ok := mapping(value)
	if !ok || !decision.exactly("status", "questions") {
		return "", errors.New("final_decision must have exactly status and questions")
	}
	questions, ok := mapping(decision.get("questions"))
	if !ok {
		return "", errors.New("final_decision questions must be a mapping")
	}
	if decision.get("status") == "pending" {
		if questions.size() > 0 {
			return "", errors.New("a pending decision must not hold question decisions")
		}
		return "pending", nil
	}
	status, err := c.questionsStatus(questions)
	if err != nil {
		return "", err
	}
	if decision.get("status") != status {
		return "", fmt.Errorf("final_decision status must be %s", status)
	}
	return status, nil
}

// questionsStatus validates every question decision: the result is resolved
// iff every item is resolved.
func (c *checkedRecord) questionsStatus(questions *object) (string, error) {
	asked := c.shared.get("questions").(*object)
	for _, id := range asked.names {
		if !questions.has(id) {
			return "", fmt.Errorf("decision must cover question %s", id)
		}
	}
	for _, id := range questions.names {
		if !asked.has(id) {
			return "", fmt.Errorf("decision names unknown question %s", id)
		}
	}
	status := "resolved"
	for _, id := range asked.names {
		resolved, err := c.checkDecisionItem(questions.get(id), id)
		if err != nil {
			return "", err
		}
		if !resolved {
			status = "unresolved"
		}
	}
	return status, nil
}

// adjudicate validates the coordinator's recorded disposition without hosted
// requests, the current policy, or voting. The record must first prove itself
// unchanged and still pending.
func adjudicate(recordValue, decisionValue any) (*object, error) {
	checked, err := checkRecord(recordValue)
	if err != nil {
		return nil, err
	}
	if !equal(checked.record.get("final_decision"), pendingDecision()) {
		return nil, errors.New("consultation already has a decision")
	}
	decision, ok := mapping(decisionValue)
	if !ok || !decision.exactly("questions") {
		return nil, errors.New("decision must hold only questions")
	}
	questions, ok := mapping(decision.get("questions"))
	if !ok {
		return nil, errors.New("decision questions must be a mapping")
	}
	status, err := checked.questionsStatus(questions)
	if err != nil {
		return nil, err
	}
	return checked.record.with("final_decision", newObject("status", status, "questions", questions)), nil
}
