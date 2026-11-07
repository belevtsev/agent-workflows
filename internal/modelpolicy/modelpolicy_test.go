package modelpolicy_test

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/internal/modelpolicy"
)

// shippedPolicy is the shipped task-orchestration policy, relative to this
// package directory.
var shippedPolicy = filepath.Join("..", "..", "skills", "task-orchestration", "model-policy.yaml")

// shippedProviders is the shipped policy's enabled providers (MP-2).
var shippedProviders = map[string]string{
	"subq_snap":        "subq-snap-preview",
	"openai_decisions": "gpt-6-luna",
}

// minimalConsultation is a valid consultation mapping enabling no provider.
const minimalConsultation = "consultation:\n  deadline_seconds: 30\n  providers: {}\n"

// consultationWith returns a version-3 document whose consultation mapping
// has the given deadline_seconds and providers values (YAML text).
func consultationWith(deadline, providers string) string {
	return "version: 3\nconsultation:\n  deadline_seconds: " + deadline + "\n  providers: " + providers + "\n"
}

func readShipped(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(shippedPolicy)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// parseError parses document and requires an error.
func parseError(t *testing.T, document string) error {
	t.Helper()
	policy, err := modelpolicy.Parse([]byte(document))
	if err == nil {
		t.Fatalf("Parse(%q) = %+v, nil; want an error", document, policy)
	}
	return err
}

func requireContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil; want text containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q; want text containing %q", err.Error(), want)
	}
}

func requirePrefix(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil; want text starting with %q", want)
	}
	if !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("error = %q; want text starting with %q", err.Error(), want)
	}
}

// MP-1: provider IDs and pins.
func TestProvidersOrderAndFreshSlice(t *testing.T) {
	want := []string{"jev", "subq_snap", "openai_decisions"}
	first := modelpolicy.Providers()
	if !slices.Equal(first, want) {
		t.Fatalf("Providers() = %q; want %q", first, want)
	}
	first[0] = "mutated"
	if len(first) > 1 {
		first[1] = "mutated"
	}
	second := modelpolicy.Providers()
	if !slices.Equal(second, want) {
		t.Fatalf("Providers() after mutating an earlier result = %q; want %q", second, want)
	}
}

// MP-1
func TestKnown(t *testing.T) {
	for _, provider := range []string{"jev", "subq_snap", "openai_decisions"} {
		if !modelpolicy.Known(provider) {
			t.Errorf("Known(%q) = false; want true", provider)
		}
	}
	for _, provider := range []string{"", "codex", "JEV"} {
		if modelpolicy.Known(provider) {
			t.Errorf("Known(%q) = true; want false", provider)
		}
	}
}

// MP-1
func TestPinnedModel(t *testing.T) {
	for _, tc := range []struct {
		provider, model string
		ok              bool
	}{
		{"subq_snap", "subq-snap-preview", true},
		{"openai_decisions", "gpt-6-luna", true},
		{"jev", "", false},
		{"codex", "", false},
	} {
		model, ok := modelpolicy.PinnedModel(tc.provider)
		if ok != tc.ok || (tc.ok && model != tc.model) {
			t.Errorf("PinnedModel(%q) = (%q, %v); want (%q, %v)", tc.provider, model, ok, tc.model, tc.ok)
		}
	}
}

// MP-1
func TestValidModel(t *testing.T) {
	for _, tc := range []struct {
		provider, model string
		want            bool
	}{
		{"jev", "jev-1.14.0", true},
		{"jev", "jev-latest", false},
		{"jev", "jev-1.14", false},
		{"jev", "jev-1.14.0-rc1", false},
		{"subq_snap", "subq-snap-preview", true},
		{"subq_snap", "gpt-6-luna", false},
		{"openai_decisions", "gpt-6-luna", true},
		{"openai_decisions", "gpt-5", false},
		{"codex", "gpt-6-luna", false},
	} {
		if got := modelpolicy.ValidModel(tc.provider, tc.model); got != tc.want {
			t.Errorf("ValidModel(%q, %q) = %v; want %v", tc.provider, tc.model, got, tc.want)
		}
	}
}

// MP-2: the shipped policy through Parse, Load and Source.
func TestShippedPolicy(t *testing.T) {
	data := readShipped(t)

	parsed, err := modelpolicy.Parse(data)
	if err != nil {
		t.Fatalf("Parse(shipped) error = %v", err)
	}
	if parsed.Deadline != 30*time.Second {
		t.Errorf("Parse(shipped).Deadline = %v; want 30s", parsed.Deadline)
	}
	if !maps.Equal(parsed.Providers, shippedProviders) {
		t.Errorf("Parse(shipped).Providers = %v; want %v", parsed.Providers, shippedProviders)
	}

	loaded, err := modelpolicy.Load(shippedPolicy)
	if err != nil {
		t.Fatalf("Load(shipped) error = %v", err)
	}
	if !reflect.DeepEqual(loaded, parsed) {
		t.Errorf("Load(shipped) = %+v; want %+v", loaded, parsed)
	}

	t.Run("source path equals Load", func(t *testing.T) {
		got, err := modelpolicy.Source{Path: shippedPolicy}.Load()
		if err != nil {
			t.Fatalf("Source{Path}.Load() error = %v", err)
		}
		if !reflect.DeepEqual(got, loaded) {
			t.Errorf("Source{Path}.Load() = %+v; want %+v", got, loaded)
		}
	})
	t.Run("source data equals Parse", func(t *testing.T) {
		got, err := modelpolicy.Source{Data: data}.Load()
		if err != nil {
			t.Fatalf("Source{Data}.Load() error = %v", err)
		}
		if !reflect.DeepEqual(got, parsed) {
			t.Errorf("Source{Data}.Load() = %+v; want %+v", got, parsed)
		}
	})
	t.Run("source path error equals Load error", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing.yaml")
		_, loadErr := modelpolicy.Load(missing)
		_, sourceErr := modelpolicy.Source{Path: missing}.Load()
		if loadErr == nil || sourceErr == nil {
			t.Fatalf("Load = %v, Source{Path}.Load = %v; want errors for a missing file", loadErr, sourceErr)
		}
		requireContains(t, sourceErr, "cannot read model policy")
	})
}

// MP-2: minimal documents and enabled providers.
func TestMinimalPolicies(t *testing.T) {
	t.Run("minimal document", func(t *testing.T) {
		policy, err := modelpolicy.Parse([]byte("version: 3\n" + minimalConsultation))
		if err != nil {
			t.Fatalf("Parse error = %v", err)
		}
		if policy.Providers == nil {
			t.Fatal("Providers is nil; want an empty, non-nil map")
		}
		if len(policy.Providers) != 0 {
			t.Errorf("Providers = %v; want empty", policy.Providers)
		}
		if policy.Deadline != 30*time.Second {
			t.Errorf("Deadline = %v; want 30s", policy.Deadline)
		}
	})
	t.Run("extra top-level field ignored", func(t *testing.T) {
		policy, err := modelpolicy.Parse([]byte("version: 3\nprofiles: 7\n" + minimalConsultation))
		if err != nil {
			t.Fatalf("Parse error = %v", err)
		}
		if policy.Providers == nil || len(policy.Providers) != 0 {
			t.Errorf("Providers = %v; want empty, non-nil", policy.Providers)
		}
	})
	t.Run("jev enabled", func(t *testing.T) {
		policy, err := modelpolicy.Parse([]byte(consultationWith("30", "{jev: jev-1.14.0}")))
		if err != nil {
			t.Fatalf("Parse error = %v", err)
		}
		want := map[string]string{"jev": "jev-1.14.0"}
		if !maps.Equal(policy.Providers, want) {
			t.Errorf("Providers = %v; want %v", policy.Providers, want)
		}
	})
	t.Run("every enabled provider", func(t *testing.T) {
		document := "version: 3\nconsultation:\n  deadline_seconds: 30\n  providers:\n" +
			"    jev: jev-1.14.0\n    subq_snap: subq-snap-preview\n    openai_decisions: gpt-6-luna\n"
		policy, err := modelpolicy.Parse([]byte(document))
		if err != nil {
			t.Fatalf("Parse error = %v", err)
		}
		want := map[string]string{"jev": "jev-1.14.0", "subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}
		if !maps.Equal(policy.Providers, want) {
			t.Errorf("Providers = %v; want %v", policy.Providers, want)
		}
	})
}

// MP-3: schema version.
func TestPolicyVersion(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		err := parseError(t, minimalConsultation)
		requireContains(t, err, "model policy has no version; this cw requires version 3")
		var versionErr *modelpolicy.VersionError
		if !errors.As(err, &versionErr) {
			t.Fatalf("errors.As(%v, *VersionError) = false; want true", err)
		}
		if versionErr.Version != nil {
			t.Errorf("VersionError.Version = %#v; want nil", versionErr.Version)
		}
	})
	t.Run("2", func(t *testing.T) {
		err := parseError(t, "version: 2\n"+minimalConsultation)
		requireContains(t, err, "model policy version 2 is not supported; this cw requires version 3")
		var versionErr *modelpolicy.VersionError
		if !errors.As(err, &versionErr) {
			t.Fatalf("errors.As(%v, *VersionError) = false; want true", err)
		}
		if v, ok := versionErr.Version.(int); !ok || v != 2 {
			t.Errorf("VersionError.Version = %#v (%T); want int 2", versionErr.Version, versionErr.Version)
		}
	})
	t.Run("3.0", func(t *testing.T) {
		err := parseError(t, "version: 3.0\n"+minimalConsultation)
		requireContains(t, err, "3.0")
	})
	t.Run("quoted 3", func(t *testing.T) {
		err := parseError(t, "version: \"3\"\n"+minimalConsultation)
		requireContains(t, err, `"3"`)
	})
	t.Run("true", func(t *testing.T) {
		parseError(t, "version: true\n"+minimalConsultation)
	})
}

// MP-4: consultation presence and required fields.
func TestConsultationPresence(t *testing.T) {
	for _, tc := range []struct {
		name, document, want string
	}{
		{"no consultation", "version: 3\nprofiles: {}\n", "policy does not configure adviser consultation"},
		{"scalar", "version: 3\nconsultation: 7\n", "consultation must be a mapping"},
		{"list", "version: 3\nconsultation: [a]\n", "consultation must be a mapping"},
		{"missing deadline", "version: 3\nconsultation:\n  providers: {}\n", `consultation has invalid fields: missing "deadline_seconds"`},
		{"missing providers", "version: 3\nconsultation:\n  deadline_seconds: 30\n", `consultation has invalid fields: missing "providers"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireContains(t, parseError(t, tc.document), tc.want)
		})
	}
}

// MP-5: deadline bounds.
func TestConsultationDeadline(t *testing.T) {
	for _, value := range []string{"30.5", "0", "-1", "3601", `"30"`, "[30]"} {
		t.Run("reject "+value, func(t *testing.T) {
			requirePrefix(t, parseError(t, consultationWith(value, "{}")), "invalid consultation deadline")
		})
	}
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"1", time.Second},
		{"3600", 3600 * time.Second},
	} {
		t.Run("accept "+tc.value, func(t *testing.T) {
			policy, err := modelpolicy.Parse([]byte(consultationWith(tc.value, "{}")))
			if err != nil {
				t.Fatalf("Parse error = %v", err)
			}
			if policy.Deadline != tc.want {
				t.Errorf("Deadline = %v; want %v", policy.Deadline, tc.want)
			}
		})
	}
}

// MP-6: provider entries.
func TestConsultationProviders(t *testing.T) {
	for _, tc := range []struct {
		name, providers, want, absent string
	}{
		{"list", "[subq_snap]", "consultation providers must be a mapping", ""},
		{"unknown", "{codex: gpt-6-luna}", `unknown consultation provider "codex"`, ""},
		{"non-string model", "{subq_snap: 7}", `consultation provider "subq_snap" model must be a string`, ""},
		{"jev alias", "{jev: jev-latest}",
			`consultation provider "jev" model "jev-latest" is an alias or unpinned; use an exact jev-X.Y.Z release`, ""},
		{"openai unsupported", "{openai_decisions: gpt-5}",
			`consultation provider "openai_decisions" model "gpt-5" is not supported; use gpt-6-luna`, ""},
		{"snap unsupported", "{subq_snap: gpt-6-luna}",
			`consultation provider "subq_snap" model "gpt-6-luna" is not supported; use subq-snap-preview`, ""},
		// Sorted key order, one error per document.
		{"sorted unknown first", "{subq_snap: gpt-6-luna, codex: gpt-6-luna}", `unknown consultation provider "codex"`, "subq_snap"},
		{"sorted jev first", "{openai_decisions: gpt-5, jev: jev-latest}",
			`consultation provider "jev" model "jev-latest" is an alias or unpinned`, "openai_decisions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := parseError(t, consultationWith("30", tc.providers))
			requireContains(t, err, tc.want)
			if tc.absent != "" && strings.Contains(err.Error(), tc.absent) {
				t.Errorf("error = %q; want one error, not mentioning %q", err.Error(), tc.absent)
			}
		})
	}
}

// MP-7: unreadable and malformed documents.
func TestUnreadablePolicy(t *testing.T) {
	valid := "version: 3\n" + minimalConsultation
	t.Run("missing path", func(t *testing.T) {
		_, err := modelpolicy.Load(filepath.Join(t.TempDir(), "missing.yaml"))
		requireContains(t, err, "cannot read model policy")
	})
	t.Run("larger than 1 MiB", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "model-policy.yaml")
		content := valid + "# " + strings.Repeat("x", 1<<20) + "\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := modelpolicy.Load(path)
		requireContains(t, err, "cannot read model policy")
	})
	t.Run("invalid UTF-8", func(t *testing.T) {
		requireContains(t, parseError(t, valid+"# \xff\xfe\n"), "cannot read model policy")
	})
	t.Run("malformed YAML", func(t *testing.T) {
		requirePrefix(t, parseError(t, "version: [\n"), "model policy is not valid YAML")
	})
	t.Run("duplicate key", func(t *testing.T) {
		requirePrefix(t, parseError(t, "version: 3\nversion: 3\n"+minimalConsultation), "model policy is not valid YAML")
	})
	t.Run("two documents", func(t *testing.T) {
		requireContains(t, parseError(t, valid+"---\n"+valid), "model policy must contain one YAML document")
	})
	t.Run("list document", func(t *testing.T) {
		requireContains(t, parseError(t, "- version: 3\n- consultation: {}\n"), "policy must be a mapping")
	})
}
