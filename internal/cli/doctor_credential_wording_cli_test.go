package cli

// Contract: verify-adviser-claim, interface contract revision 4, R7: the
// credential check details reach `cw doctor --summary` problems. Credential
// values are fake fixture values in a controlled environment.

import (
	"strings"
	"testing"
)

// R7 through cw doctor --summary --all: with every provider credential in cw's own
// environment and none in the Codex MCP launcher environment or the Codex
// shell policy, the launcher and shell checks are missing (n = 0) and their
// entries under optional (the credential checks are optional) carry the R7
// details; the environment check is configured and is
// no problem. Fails when the summary carries the old or any other wording, or
// prints a credential value.
func TestDoctorSummaryCarriesCredentialWording(t *testing.T) {
	f := newCompactDoctorFixture(t) // its Codex config has an MCP server with no env and no shell policy
	outcome := f.run(t, fixtureCredentials("jev", "subq_snap", "openai_decisions"), "--summary", "--all")
	if strings.Contains(outcome.stdout+outcome.stderr, "fixture-secret") {
		t.Fatal("R7: doctor --summary printed a credential value")
	}
	line := compactLine(t, "doctor --summary --all", []byte(outcome.stdout))
	var problems map[string]map[string]any
	compactDecode(t, line["optional"], &problems)
	for check, detail := range map[string]string{
		"credentials_launcher":    "0 of 3 provider credential variables present in the Codex MCP launcher environment, not cw's own; cw keys status reports the keys cw can use; authentication was not attempted",
		"credentials_codex_shell": "0 of 3 provider credential variables present in the Codex shell environment policy, not cw's own; cw keys status reports the keys cw can use; authentication was not attempted",
	} {
		entry, ok := problems[check]
		if !ok {
			t.Fatalf("R7: doctor --summary --all optional lacks %s: %s", check, line["optional"])
		}
		if entry["status"] != "missing" {
			t.Errorf("R7: %s status = %v, want missing", check, entry["status"])
		}
		if entry["detail"] != detail {
			t.Errorf("R7: %s summary detail\n got %q\nwant %q", check, entry["detail"], detail)
		}
	}
	if _, ok := problems["credentials_environment"]; ok {
		t.Errorf("R7: credentials_environment is listed although all 3 variables are set: %s", line["optional"])
	}
}
