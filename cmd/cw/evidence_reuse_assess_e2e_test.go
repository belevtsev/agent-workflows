package main_test

import (
	json "encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const reuseSmallPlan = `{"title":"Reused evidence","kind":"small",
 "requirements":[{"id":"behaviour","description":"alpha keeps its behaviour","check_ids":["alpha"],"allow_non_execution":false}],
 "checks":[{"id":"alpha","argv":["go","test","-count=1","./alpha"],"timeout_seconds":300}]}`

const reuseSmallAssessment = `{"requirements":[{"id":"behaviour","status":"satisfied","evidence":[{"path":"alpha/alpha.go","start_line":1,"end_line":1}],"check_ids":["alpha"]}],
 "decision":{"status":"resolved","reason":"alpha passed at these bytes","evidence":[{"path":"alpha/alpha.go","start_line":1,"end_line":1}],"check_ids":["alpha"]}}`

// Oracle: a small task passes alpha, is assessed and verifies complete; after
// a commit of the same bytes verify is still complete, with no new run and no
// new assessment, and lists the reuse; one changed byte makes it incomplete.
func TestReuseE2EAssessCommitVerifyStaysComplete(t *testing.T) {
	e := newEvidenceE2E(t)
	e.setUpModule()
	task, code, output := e.start(e.store, "plan.json", reuseSmallPlan)
	if code != 0 {
		t.Fatalf("start: exit %d: %s", code, output)
	}
	e.task = task
	e.write("alpha/alpha.go", alphaSource+"\nfunc Other() string { return \"other\" }\n")
	e.runExpect(true, "alpha")
	e.mustCW("task", "assess", "--task", e.task, "--input", e.file("assess.json", reuseSmallAssessment))
	if v, _ := e.assessedVerify(); !v.Complete {
		t.Fatalf("before the commit: incomplete, problems %q", v.Problems)
	}
	_, runs := e.assessedVerify()
	e.git("add", "-A")
	e.git("commit", "-qm", "alpha")
	v, got := e.assessedVerify()
	if !v.Complete || !assessedReuse(v, "alpha") {
		t.Errorf("after a commit of the same bytes: complete %v, problems %q, limitations %q; want complete with alpha reused", v.Complete, v.Problems, v.Limitations)
	}
	if got != runs {
		t.Errorf("executed runs went from %d to %d; want no new run", runs, got)
	}
	e.write("alpha/alpha.go", alphaSource+"\nfunc Other() string { return \"otheR\" }\n")
	if v, _ := e.assessedVerify(); v.Complete {
		t.Errorf("after a one-byte edit: complete, want incomplete")
	}
}

// assessedVerify runs verify and returns the verification and the number of
// runs the task recorded.
func (e *evidenceE2E) assessedVerify() (taskflow.Verification, int) {
	e.t.Helper()
	_, stdout, stderr := e.cw("task", "verify", "--full", "--task", e.task)
	var verification taskflow.Verification
	if err := json.Unmarshal([]byte(stdout), &verification); err != nil {
		e.t.Fatalf("verify output is not a verification: %v\nstdout %s\nstderr %s", err, stdout, stderr)
	}
	record, err := taskflow.Load(e.task)
	if err != nil {
		e.t.Fatalf("load task: %v", err)
	}
	return verification, len(record.Checks)
}

func assessedReuse(verification taskflow.Verification, check string) bool {
	return slices.ContainsFunc(verification.Limitations, func(line string) bool { return strings.HasPrefix(line, "check "+check+" reuses run ") })
}
