package taskflow

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// An Env without a consultation validator fails closed: every use case that
// checks a stored or offered consultation refuses instead of trusting it.
// Oracle: with a nil validator, attaching a record that cw consult's validator
// accepts returns errNoConsultationValidator and stores nothing; with that
// record attached through the production validator, dispatching a worker and
// preparing the final context return errNoConsultationValidator, and Verify
// returns ErrIncomplete with a before-consultation problem naming the missing
// validator.
func TestNilConsultationValidatorRefusesEveryConsultationCheck(t *testing.T) {
	record, _ := fixtureTask(t, substantialPlan(t))
	unchecked := Env{}
	path := writeConsultation(t, record.Contexts["before"], nil)

	if _, err := unchecked.AttachConsultation(t.Context(), record.Directory, "before", path); !errors.Is(err, errNoConsultationValidator) {
		t.Fatalf("attach without a validator: got %v, want %v", err, errNoConsultationValidator)
	}
	stored, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, attached := stored.Consultations["before"]; attached {
		t.Fatal("attach without a validator stored the consultation")
	}

	if _, err := productionEnv.AttachConsultation(t.Context(), record.Directory, "before", path); err != nil {
		t.Fatalf("attach with the production validator: %v", err)
	}
	brief := workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}, "focused")
	if _, err := unchecked.AssignWorker(t.Context(), record.Directory, brief); !errors.Is(err, errNoConsultationValidator) {
		t.Errorf("assign without a validator: got %v, want %v", err, errNoConsultationValidator)
	}
	if _, err := unchecked.PrepareContext(t.Context(), record.Directory, "final"); !errors.Is(err, errNoConsultationValidator) {
		t.Errorf("final context without a validator: got %v, want %v", err, errNoConsultationValidator)
	}
	verification, err := unchecked.Verify(t.Context(), record.Directory)
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("verify without a validator: got %v, want %v", err, ErrIncomplete)
	}
	if verification.Complete {
		t.Error("verify without a validator reported the task complete")
	}
	if !slices.ContainsFunc(verification.Problems, func(problem string) bool {
		return strings.HasPrefix(problem, "before consultation: ") && strings.Contains(problem, errNoConsultationValidator.Error())
	}) {
		t.Errorf("verify without a validator: problems %q name no missing validator", verification.Problems)
	}
}
