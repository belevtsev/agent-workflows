package taskflow

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// FindingCode names the rule, and within it the kind of entry, behind one
// verify problem or limitation. A code is stable: a caller selects entries by
// code, never by their text, so rewording a message changes no caller.
type FindingCode string

// The verify finding codes. Each names one kind of entry; one rule may report
// several codes.
const (
	// CodeFixedLimitation marks the limitations that open every verification
	// (FixedVerifyLimitations).
	CodeFixedLimitation FindingCode = "fixed-limitation"
	// CodeLocalEvidence marks a problem in the requirement, check, decision
	// or worker evidence that final-context preparation also requires.
	CodeLocalEvidence FindingCode = "local-evidence"
	// CodeGoTool names the go command that ran passed checks.
	CodeGoTool FindingCode = "go-tool"
	// CodeEnvPassthrough names the variables a passed check's env_passthrough
	// let through.
	CodeEnvPassthrough FindingCode = "env-passthrough"
	// CodeDeferredFinding reports a deferred worker finding.
	CodeDeferredFinding FindingCode = "deferred-finding"
	// CodeRejectedFinding reports a rejected blocker or major worker finding.
	CodeRejectedFinding FindingCode = "rejected-finding"
	// CodeRejectedBlocker is the problem of a rejected blocker finding that no
	// later independent review has rebound.
	CodeRejectedBlocker FindingCode = "rejected-blocker"
	// CodeContract reports a worker contract problem or note.
	CodeContract FindingCode = "contract"
	// CodeTrace reports a requirement trace problem or note.
	CodeTrace FindingCode = "trace"
	// CodeAuthorship reports a test authorship problem or note.
	CodeAuthorship FindingCode = "authorship"
	// CodeExecution reports what the language pack's evidence rule found for
	// changed source.
	CodeExecution FindingCode = "execution"
	// CodeNoAdvisers notes a substantial task bound to no advisers.
	CodeNoAdvisers FindingCode = "no-advisers"
	// CodeCheckpointsNotRequired notes the plan's reason for skipping adviser
	// checkpoints.
	CodeCheckpointsNotRequired FindingCode = "checkpoints-not-required"
	// CodeAdviserClaimConflict reports a plan that skips checkpoints although
	// advisers are bound.
	CodeAdviserClaimConflict FindingCode = "adviser-claim-conflict"
	// CodeConsultation reports a missing or invalid adviser checkpoint.
	CodeConsultation FindingCode = "consultation"
	// CodeAdviserCoverage notes an adviser whose checkpoint did not succeed.
	CodeAdviserCoverage FindingCode = "adviser-coverage"
	// CodeSourceChanged reports source that changed while Verify ran.
	CodeSourceChanged FindingCode = "source-changed"
)

// Finding is one verify problem or limitation: its stable code and the text
// the JSON output prints.
type Finding struct {
	Code    FindingCode
	Message string
}

// verifyInput is what every verify rule reads: the record, the source
// snapshot and manifest captured once for the whole verification, and the
// task directory's artefacts.
type verifyInput struct {
	// env checks consultations with its validator.
	env       Env
	artifacts Artifacts
	record    Record
	snapshot  projectctx.Snapshot
	files     []projectctx.FileState
	view      *sourceView
}

// verifyFindings is what one rule reports. Problems block completion;
// limitations are disclosed.
type verifyFindings struct {
	problems    []Finding
	limitations []Finding
}

func (found *verifyFindings) problem(code FindingCode, format string, args ...any) {
	found.problems = append(found.problems, Finding{Code: code, Message: fmt.Sprintf(format, args...)})
}

func (found *verifyFindings) limitation(code FindingCode, message string) {
	found.limitations = append(found.limitations, Finding{Code: code, Message: message})
}

// coded gives each message the code.
func coded(code FindingCode, messages []string) []Finding {
	findings := make([]Finding, 0, len(messages))
	for _, message := range messages {
		findings = append(findings, Finding{Code: code, Message: message})
	}
	return findings
}

// verifyRule judges one aspect of a task at the verified snapshot.
type verifyRule func(ctx context.Context, in verifyInput) verifyFindings

// verifyRules run in this order, and Verify keeps each rule's problems and
// limitations in the order it reports them, so the JSON output lists them as
// it always has.
var verifyRules = [...]verifyRule{
	fixedLimitationsRule,
	localEvidenceRule,
	goToolRule,
	envPassthroughRule,
	deferredFindingsRule,
	rejectedFindingsRule,
	contractRule,
	traceRule,
	authorshipRule,
	executionRule,
	adviserBindingRule,
	consultationsRule,
}

// runVerifyRules runs every rule over in and joins what they report.
func runVerifyRules(ctx context.Context, in verifyInput) verifyFindings {
	var all verifyFindings
	for _, rule := range verifyRules {
		found := rule(ctx, in)
		all.problems = append(all.problems, found.problems...)
		all.limitations = append(all.limitations, found.limitations...)
	}
	return all
}

// messages returns the text of each finding, in order, never nil.
func messages(findings []Finding) []string {
	text := make([]string, 0, len(findings))
	for _, finding := range findings {
		text = append(text, finding.Message)
	}
	return text
}

func fixedLimitationsRule(context.Context, verifyInput) verifyFindings {
	return verifyFindings{limitations: coded(CodeFixedLimitation, FixedVerifyLimitations())}
}

func localEvidenceRule(_ context.Context, in verifyInput) verifyFindings {
	return verifyFindings{problems: coded(CodeLocalEvidence, localEvidenceProblems(in.artifacts, in.record, in.snapshot))}
}

func goToolRule(_ context.Context, in verifyInput) verifyFindings {
	return verifyFindings{limitations: coded(CodeGoTool, toolLimitations(in.record, in.snapshot))}
}

func envPassthroughRule(_ context.Context, in verifyInput) verifyFindings {
	return verifyFindings{limitations: coded(CodeEnvPassthrough, passthroughLimitations(in.record, in.snapshot))}
}

func deferredFindingsRule(_ context.Context, in verifyInput) verifyFindings {
	return verifyFindings{limitations: coded(CodeDeferredFinding, deferredFindingLimitations(in.record))}
}

func rejectedFindingsRule(_ context.Context, in verifyInput) verifyFindings {
	problems, limitations := rejectedFindingReport(in.artifacts, in.record)
	return verifyFindings{problems: coded(CodeRejectedBlocker, problems), limitations: coded(CodeRejectedFinding, limitations)}
}

func contractRule(_ context.Context, in verifyInput) verifyFindings {
	problems, notes := contractProblems(in.artifacts, in.record)
	return verifyFindings{problems: coded(CodeContract, problems), limitations: coded(CodeContract, notes)}
}

func traceRule(_ context.Context, in verifyInput) verifyFindings {
	problems, notes := traceVerifyEntries(in.artifacts, in.record, in.view)
	return verifyFindings{problems: coded(CodeTrace, problems), limitations: coded(CodeTrace, notes)}
}

// authorshipRule reports R16, R15 and R14, then R23's limitation, for a
// gated task.
func authorshipRule(_ context.Context, in verifyInput) verifyFindings {
	if !gated(in.record) {
		return verifyFindings{}
	}
	problems, notes := authorshipVerifyEntries(in.view.authorship(in.artifacts, in.record), in.record)
	return verifyFindings{problems: coded(CodeAuthorship, problems), limitations: coded(CodeAuthorship, notes)}
}

// executionRule asks the evidence rule about a substantial task whose source
// changed since start.
func executionRule(ctx context.Context, in verifyInput) verifyFindings {
	if in.record.Plan.Kind != "substantial" || equalSnapshot(in.record.Baseline, in.snapshot) {
		return verifyFindings{}
	}
	problems, notes := executionProblems(ctx, in.artifacts, in.record, in.snapshot, in.files)
	return verifyFindings{problems: coded(CodeExecution, problems), limitations: coded(CodeExecution, notes)}
}

// adviserBindingRule notes a substantial task's adviser binding and its
// checkpoint choice, and reports a plan that skips checkpoints although
// advisers are bound and both checkpoints are not attached.
func adviserBindingRule(_ context.Context, in verifyInput) verifyFindings {
	var found verifyFindings
	record := in.record
	if record.Plan.Kind != "substantial" {
		return found
	}
	if len(record.Advisers) == 0 {
		found.limitation(CodeNoAdvisers, noAdvisersLimitation)
	}
	if !checkpointsRequired(record.Plan) {
		found.limitation(CodeCheckpointsNotRequired, "adviser checkpoints not required: "+record.Plan.Checkpoints.Reason)
	}
	_, beforeAttached := record.Consultations["before"]
	_, finalAttached := record.Consultations["final"]
	if conflict, ok := adviserClaimConflict(record.Plan, record.Advisers); ok && !(beforeAttached && finalAttached) {
		// A usable before checkpoint is the one final-context preparation accepts.
		_, beforeErr := in.env.readConsultation(in.artifacts, record, "before", record.Baseline)
		switch {
		case beforeErr == nil:
			conflict += "; attach the final checkpoint"
		case equalSnapshot(record.Baseline, in.snapshot):
			conflict += "; attach the before and final checkpoints, or ask the user before abandoning this record and starting a corrected one"
		default:
			conflict += "; the source changed since start, so the before checkpoint can no longer be attached and a new record must not start over the changed source: stop and ask the user how to proceed"
		}
		found.problems = append(found.problems, Finding{Code: CodeAdviserClaimConflict, Message: conflict})
	}
	return found
}

// consultationsRule validates each required or attached checkpoint of a
// substantial task and notes each adviser whose checkpoint did not succeed.
func consultationsRule(_ context.Context, in verifyInput) verifyFindings {
	var found verifyFindings
	record := in.record
	if record.Plan.Kind != "substantial" {
		return found
	}
	required := checkpointsRequired(record.Plan)
	for _, phase := range []string{"before", "final"} {
		// A checkpoint attached although not required is still validated.
		if _, attached := record.Consultations[phase]; !required && !attached {
			continue
		}
		expected := in.snapshot
		if phase == "before" {
			expected = record.Baseline
		}
		consultation, err := in.env.readConsultation(in.artifacts, record, phase, expected)
		if err != nil {
			found.problem(CodeConsultation, "%s consultation: %v", phase, err)
			continue
		}
		for _, provider := range slices.Sorted(maps.Keys(record.Advisers)) {
			if status := consultation.Providers[provider].Status; status != "success" {
				found.limitation(CodeAdviserCoverage, fmt.Sprintf("%s %s coverage is %s; the coordinator's recorded source/check decision supplies the fallback.", phase, provider, status))
			}
		}
	}
	return found
}

// report sets the verification's findings and their text from found.
func (verification *Verification) report(found verifyFindings) {
	verification.ProblemFindings, verification.LimitationFindings = found.problems, found.limitations
	verification.Problems, verification.Limitations = messages(found.problems), messages(found.limitations)
}
