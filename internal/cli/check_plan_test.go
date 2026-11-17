package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

func TestCheckPlanJSONAndHumanOutput(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Write("kitsu.toml", "version=1\n[[check]]\nid=\"behavior\"\nrun=[\"never-execute\"]\nwhen=[\"src/**\"]\nexpect=\"invalid input is rejected\"\nverify=true\n[[check]]\nid=\"full\"\nrun=[\"never-execute\"]\n")
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", "base")
	for i := range 25 {
		r.Write(fmt.Sprintf("src/%02d", i), "changed")
	}
	r.Write("uncovered", "changed")
	t.Chdir(r.Dir)
	before := r.Git("status", "--porcelain=v1")
	var out, diagnostic bytes.Buffer
	cfg := Config{Out: &out, Err: &diagnostic}
	if err := runCheckPlan(t.Context(), cfg, []string{"--json"}); err != nil {
		t.Fatal(err)
	}
	var plan verification.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Checks) != 2 || len(plan.Checks[0].MatchedPaths) != 25 || len(plan.Uncovered) != 1 || diagnostic.Len() != 0 {
		t.Fatalf("incomplete JSON: %s; %s", out.String(), diagnostic.String())
	}
	out.Reset()
	if err := runCheckPlan(t.Context(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"All 2 declared checks", "No checks were executed", "invalid input is rejected", "and 17 more", "Uncovered changed paths (1): uncovered"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("human output missing %q: %s", want, out.String())
		}
	}
	if after := r.Git("status", "--porcelain=v1"); after != before {
		t.Fatal("plan modified tracked state")
	}
	for _, args := range [][]string{{"positional"}, {"--from", ""}, {"--", "program"}, {"--from", "missing"}} {
		if err := runCheckPlan(t.Context(), cfg, args); err == nil {
			t.Errorf("accepted invalid args %q", args)
		}
	}
}

func TestAddVerificationMetadata(t *testing.T) {
	r := kitsutest.NewRepo(t)
	old := "# keep existing bytes\r\nversion = 1\r\n"
	r.Write("kitsu.toml", old)
	t.Chdir(r.Dir)
	var out, diagnostic bytes.Buffer
	cfg := Config{Out: &out, Err: &diagnostic}
	args := []string{"behavior", "--paths", "evidence/**", "--when", "src/**", "--when", "tests/**", "--expect", "invalid input rejected", "--verify", "--", "never-execute"}
	if err := runAdd(t.Context(), cfg, args); err != nil {
		t.Fatal(err)
	}
	updated := r.Read("kitsu.toml")
	if !strings.HasPrefix(updated, old) {
		t.Fatal("add changed earlier bytes")
	}
	file, err := decl.Parse([]byte(updated))
	if err != nil {
		t.Fatal(err)
	}
	check := file.Checks[0]
	if !check.Verify || len(check.When) != 2 || check.Expect != "invalid input rejected" || len(check.Paths) != 1 {
		t.Fatalf("metadata not saved: %+v", check)
	}
	if err := runAdd(t.Context(), cfg, []string{"bad", "--verify", "--", "never-execute"}); err == nil {
		t.Fatal("accepted verify without expectation")
	}
	if r.Read("kitsu.toml") != updated {
		t.Fatal("invalid add modified declaration")
	}
}
