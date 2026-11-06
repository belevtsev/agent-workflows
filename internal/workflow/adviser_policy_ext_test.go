package workflow_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/consult"
	"github.com/belevtsev/agent-workflows/internal/workflow"
)

// B4: after install, ConfiguredAdvisers on the effective policy never returns
// a disabled provider, even when its key is set.
func TestAdviserConfiguredAdvisersSkipDisabledProviders(t *testing.T) {
	keys := map[string]string{
		"TYPESAFE_API_KEY": "adviser-key-ts",
		"SUBQ_API_KEY":     "adviser-key-subq",
		"OPENAI_API_KEY":   "adviser-key-openai",
	}
	getenv := func(name string) string { return keys[name] }

	for _, disabled := range [][]string{
		{"openai_decisions"},
		{"subq_snap"},
		{"jev", "subq_snap", "openai_decisions"},
	} {
		t.Run(strings.Join(disabled, "+"), func(t *testing.T) {
			effective, shipped := workflow.AdviserInstallForConsult(t, disabled...)

			all, err := consult.ConfiguredAdvisers(shipped, getenv)
			if err != nil {
				t.Fatalf("ConfiguredAdvisers(shipped): %v", err)
			}
			for _, id := range []string{"subq_snap", "openai_decisions"} {
				if _, ok := all[id]; !ok {
					t.Fatalf("precondition: with every key set the shipped policy binds %v, which lacks %s", all, id)
				}
			}

			got, err := consult.ConfiguredAdvisers(effective, getenv)
			if err != nil {
				t.Fatalf("ConfiguredAdvisers(effective): %v", err)
			}
			if got == nil {
				t.Fatalf("ConfiguredAdvisers(effective) returned nil")
			}
			want := maps.Clone(all)
			for _, id := range disabled {
				delete(want, id)
			}
			if !maps.Equal(got, want) {
				t.Fatalf("ConfiguredAdvisers(effective) = %v, want %v (disabled %q)", got, want, disabled)
			}
		})
	}
}
