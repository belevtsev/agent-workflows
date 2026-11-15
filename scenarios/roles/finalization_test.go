package roles_test

import "testing"

// Finalization has a separate, explicit scope. The ordinary spec writer
// still cannot change the current specifications.
func TestSpecWriterFinalization(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"added", "changed", "removed"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			repo := demo(t)
			const current = "openspec/specs/greeting/spec.md"
			repo.Write("openspec/changes/add-greeting/specs/greeting/spec.md", "# greeting\n")
			if kind != "added" {
				repo.Write(current, "# old greeting\n")
			}
			before := tree(t, repo)
			const baseline = "refs/gatecub/finalize/add-greeting"
			repo.Git("update-ref", baseline, before)
			if kind == "removed" {
				repo.Remove(current)
			} else {
				repo.Write(current, "# current greeting\n")
			}
			args := []string{"roles", "check", "spec-writer", "--from", baseline, "--task", "add-greeting"}
			ordinary := repo.Gatecub(args...)
			if ordinary.Code != 5 {
				t.Fatalf("ordinary scope exited %d, want 5:\n%s", ordinary.Code, ordinary.Output())
			}
			assertLists(t, ordinary, []string{current}, nil)
			finalized := repo.Gatecub(append(args, "--finalize")...)
			if finalized.Code != 0 {
				t.Errorf("finalization exited %d, want 0:\n%s", finalized.Code, finalized.Output())
			}
		})
	}
}

func TestFinalizationDoesNotExpandItsOwnScope(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	repo.Write("openspec/changes/add-greeting/specs/greeting/spec.md", "# greeting\n")
	repo.Write("openspec/changes/other-task/specs/auth/spec.md", "# authorization\n")
	before := tree(t, repo)
	const allowed = "openspec/specs/greeting/spec.md"
	forbidden := []string{
		"openspec/specs/auth/spec.md",
		"openspec/changes/add-greeting/specs/auth/spec.md",
		"openspec/specs/greeting/notes.md",
		"openspec/changes/add-greeting/proposal.md",
		"src/app.txt",
	}
	repo.Write(allowed, "# greeting\n")
	for _, path := range forbidden {
		repo.Write(path, "changed during finalization\n")
	}
	result := repo.Gatecub("roles", "check", "spec-writer", "--from", before, "--task", "add-greeting", "--finalize")
	if result.Code != 5 {
		t.Fatalf("finalization exited %d, want 5:\n%s", result.Code, result.Output())
	}
	assertLists(t, result, forbidden, []string{allowed})
}

func TestFinalizationRequiresTheTaskScope(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		role string
		task string
		code int
	}{
		{"missing task", "spec-writer", "", 2},
		{"another role", "developer", "add-greeting", 2},
		{"task without specifications", "spec-writer", "add-greeting", 1},
		{"missing task folder", "spec-writer", "missing-task", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			repo := demo(t)
			args := []string{"roles", "check", c.role, "--from", tree(t, repo), "--finalize"}
			if c.task != "" {
				args = append(args, "--task", c.task)
			}
			result := repo.Gatecub(args...)
			if result.Code != c.code {
				t.Errorf("finalization exited %d, want %d:\n%s", result.Code, c.code, result.Output())
			}
		})
	}
}
