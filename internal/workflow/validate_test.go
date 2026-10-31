package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type suiteFixture struct {
	root     string
	manifest Object
}

func newSuiteFixture(t *testing.T) *suiteFixture {
	t.Helper()
	fixture := &suiteFixture{
		root: t.TempDir(),
		manifest: Object{
			"version":             1,
			"registrations":       Object{"example": "skills/example", "task-orchestration": "skills/task-orchestration"},
			"global_instructions": "global/AGENTS.md", "required_licenses": []any{"LICENSE"},
			"model_policy": "skills/task-orchestration/model-policy.yaml",
		},
	}
	fixture.write(t, "global/AGENTS.md", "# Working conventions\n\nUse source evidence.\n")
	fixture.write(t, "LICENSE", "Fixture permission notice.\n")
	fixture.skill(t, "skills/example", "example", "")
	fixture.skill(t, "skills/task-orchestration", "task-orchestration", "")
	for _, name := range []string{"model-policy.yaml", "references/jev-questions.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "skills", "task-orchestration", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		fixture.write(t, "skills/task-orchestration/"+name, string(data))
	}
	fixture.writeManifest(t)
	return fixture
}

func (fixture *suiteFixture) write(t *testing.T, name, value string) string {
	t.Helper()
	target := filepath.Join(fixture.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return target
}

func (fixture *suiteFixture) skill(t *testing.T, source, name, body string) {
	t.Helper()
	fixture.write(t, source+"/SKILL.md", "---\nname: "+name+"\ndescription: A bounded fixture workflow.\n---\n\n# Fixture\n\n"+body)
}

func (fixture *suiteFixture) writeManifest(t *testing.T) {
	t.Helper()
	data, err := json.Marshal(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture.write(t, "skills-manifest.json", string(data))
}

func (fixture *suiteFixture) invalid(t *testing.T, message string) {
	t.Helper()
	_, err := ValidateSuite(fixture.root)
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("ValidateSuite() error = %v; want containing %q", err, message)
	}
	if strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("validation leaked source contents: %v", err)
	}
}

func suiteSnapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	snapshot := map[string][32]byte{}
	err := filepath.WalkDir(root, func(target string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, target)
		if err != nil {
			return err
		}
		snapshot[relative] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestValidateSuiteReportAndDefaultsWithoutWrites(t *testing.T) {
	fixture := newSuiteFixture(t)
	before := suiteSnapshot(t, fixture.root)
	report, err := ValidateSuite(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if report["version"] != 1 || report["skill_count"] != 2 || report["markdown_files_checked"] != 3 || report["required_licenses"] != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if !reflect.DeepEqual(report["registrations"], fixture.manifest["registrations"]) {
		t.Fatalf("changed registrations: %#v", report["registrations"])
	}
	manifest, err := LoadManifest(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := ModelDefaults(fixture.root, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defaults, map[string]string{"model": "gpt-6.1-sol", "model_reasoning_effort": "max"}) {
		t.Fatalf("unexpected coordinator defaults: %#v", defaults)
	}
	if after := suiteSnapshot(t, fixture.root); !reflect.DeepEqual(before, after) {
		t.Fatal("validation changed source files")
	}
}

func TestValidateSourceTree(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	report, err := ValidateSuite(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report["registrations"].(Object)) != 26 || report["skill_count"].(int) != 71 {
		t.Fatalf("unexpected maintained suite report: %#v", report)
	}
	manifest, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := ModelDefaults(root, manifest)
	if err != nil || defaults["model"] != "gpt-6.1-sol" || defaults["model_reasoning_effort"] != "max" {
		t.Fatalf("maintained suite defaults = %#v, error = %v", defaults, err)
	}
}

func TestValidateSuiteAcceptsVendoredMetadataAndComplexLinks(t *testing.T) {
	fixture := newSuiteFixture(t)
	fixture.manifest["registrations"].(Object)["bundle"] = "vendor/bundle"
	fixture.write(t, "vendor/bundle/skills/nested/SKILL.md", `---
name: nested
description: A bundled fixture.
license: MIT
allowed-tools: Read Bash(go:*)
compatibility: Codex
metadata:
  version: "1.4.1"
  upstream:
    paths: ["**/*.go"]
    user-invocable: true
---
`)
	fixture.write(t, "docs/shared guide.md", "# Shared guide\n")
	fixture.write(t, "skills/example/reference.md", "# Reference\n")
	fixture.write(t, "skills/example/local(part).md", "# Balanced path\n")
	fixture.write(t, "skills/example/escaped file.md", "# Escaped path\n")
	fixture.skill(t, "skills/example", "example", "[local](reference.md#heading)\n"+
		"[label [with brackets]](local(part).md)\n[shared](<../../docs/shared guide.md> \"A title\")\n"+
		"[escaped](escaped\\ file.md)\n[query](reference.md?view=source#anchor)\n![image](reference.md)\n"+
		"[external](https://example.invalid/missing)\n[mail](mailto:author@example.invalid)\n[same](#heading)\n"+
		"- Nested reference:\n    [shared](<../../docs/shared guide.md>)\n\n[shared]: <../../docs/shared guide.md> \"Title\"\n"+
		"`[literal](missing.md)`\n```markdown\n[example](missing.md)\n```\n\n    [indented](missing.md)\n"+
		"do.Invoke[Database](injector)\n<!-- [comment](missing.md) -->\n")
	fixture.write(t, "skills/example/assets/templates/README.md", "# {project-name}\n\n[License](./LICENSE)\n[Contributing](CONTRIBUTING.md)\n[Generated](docs/{component}.md)\n")
	fixture.writeManifest(t)
	report, err := ValidateSuite(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if report["skill_count"] != 3 {
		t.Fatalf("unexpected skill count: %#v", report)
	}
	fixture.write(t, "skills/example/assets/templates/README.md", "# {project-name}\n\n[Source resource](../missing.md)\n")
	fixture.invalid(t, "missing local Markdown reference")
}

func TestValidateManifestRejectsUnsafeInputs(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, *suiteFixture)
		want   string
	}{
		{"boolean version", func(t *testing.T, f *suiteFixture) { f.manifest["version"] = true; f.writeManifest(t) }, "manifest version"},
		{"fractional version", func(t *testing.T, f *suiteFixture) {
			f.write(t, "skills-manifest.json", strings.ReplaceAll(string(mustSuiteJSON(t, f.manifest)), `"version":1`, `"version":1.0`))
		}, "manifest version"},
		{"duplicate key", func(t *testing.T, f *suiteFixture) { f.write(t, "skills-manifest.json", `{"version":1,"version":1}`) }, "duplicate mapping key"},
		{"extra field", func(t *testing.T, f *suiteFixture) { f.manifest["unexpected"] = "fixture-secret"; f.writeManifest(t) }, "invalid fields"},
		{"empty registrations", func(t *testing.T, f *suiteFixture) { f.manifest["registrations"] = Object{}; f.writeManifest(t) }, "nonempty mapping"},
		{"overlap", func(t *testing.T, f *suiteFixture) {
			f.manifest["registrations"].(Object)["bundle"] = "skills"
			f.writeManifest(t)
		}, "overlap"},
		{"missing license", func(t *testing.T, f *suiteFixture) {
			if err := os.Remove(filepath.Join(f.root, "LICENSE")); err != nil {
				t.Fatal(err)
			}
		}, "license is missing"},
		{"empty license", func(t *testing.T, f *suiteFixture) { f.write(t, "LICENSE", " \n") }, "license is empty"},
		{"duplicate license", func(t *testing.T, f *suiteFixture) {
			f.manifest["required_licenses"] = []any{"LICENSE", "LICENSE"}
			f.writeManifest(t)
		}, "duplicate path"},
		{"policy outside skill", func(t *testing.T, f *suiteFixture) {
			f.write(t, "outside.yaml", "version: 2\n")
			f.manifest["model_policy"] = "outside.yaml"
			f.writeManifest(t)
		}, "inside a registered"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSuiteFixture(t)
			test.change(t, fixture)
			fixture.invalid(t, test.want)
		})
	}
	for _, value := range []string{"../elsewhere", "/tmp/elsewhere", "C:\\private", "skills/../example", "skills//example", "./skills/example", "skills/example/", ".venv/example", "skills\x00/example"} {
		t.Run("path "+value, func(t *testing.T) {
			fixture := newSuiteFixture(t)
			fixture.manifest["registrations"].(Object)["example"] = value
			fixture.writeManifest(t)
			want := "path"
			if strings.HasPrefix(value, ".venv/") {
				want = "private or generated directory"
			}
			fixture.invalid(t, want)
		})
	}
}

func mustSuiteJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestValidateSuiteRejectsSkillMetadataProblems(t *testing.T) {
	for _, frontmatter := range []string{
		"# No frontmatter\n",
		"---\nname: example\n",
		"---\n[]\n---\n",
		"---\nname: example\nname: duplicate\ndescription: fixture-secret\n---\n",
		"---\nname: example\ndescription: !!python/object:object {}\n---\n",
		"---\nname: example\ndescription: text\nmetadata:\n  inner:\n    duplicate: one\n    duplicate: two\n---\n",
		"---\nname: example\ndescription: text\nmetadata:\n  inner: &base {duplicate: one}\n  merged: {<<: *base, duplicate: two}\n---\n",
		"---\nname: example\ndescription: text\nmetadata: [invalid]\n---\n",
		"---\nname: Bad Name\ndescription: text\n---\n",
		"---\nname: example\ndescription: [invalid]\n---\n",
		"---\nname: example\ndescription: yes\n---\n",
		"---\nname: example\ndescription: 1:30\n---\n",
		"---\nname: example\ndescription: !!binary dGV4dA==\n---\n",
		"---\nname: example\ndescription: text\nmetadata: !!set {invalid: null}\n---\n",
		"---\nname: example\ndescription: text\nlicense: []\n---\n",
		"---\nname: example\ndescription: text\nunexpected: true\n---\n",
	} {
		t.Run(frontmatter, func(t *testing.T) {
			fixture := newSuiteFixture(t)
			fixture.write(t, "skills/example/SKILL.md", frontmatter)
			fixture.invalid(t, "skill")
		})
	}
	t.Run("duplicate skill", func(t *testing.T) {
		fixture := newSuiteFixture(t)
		fixture.skill(t, "skills/task-orchestration/nested", "example", "")
		fixture.invalid(t, "duplicate skill name")
	})
	t.Run("registration mismatch", func(t *testing.T) {
		fixture := newSuiteFixture(t)
		fixture.skill(t, "skills/example", "different", "")
		fixture.invalid(t, "registration does not match")
	})
	t.Run("empty registration", func(t *testing.T) {
		fixture := newSuiteFixture(t)
		fixture.manifest["registrations"].(Object)["empty"] = "skills/empty"
		if err := os.Mkdir(filepath.Join(fixture.root, "skills/empty"), 0o700); err != nil {
			t.Fatal(err)
		}
		fixture.writeManifest(t)
		fixture.invalid(t, "no skill entrypoints")
	})
}

func TestValidateSuiteRejectsPrivateArtifactsAndSymlinks(t *testing.T) {
	for _, name := range []string{"auth.json", "config.toml", ".env", ".env.local", ".DS_Store", "scripts/cache.pyc", "skills/example/__pycache__/helper.pyc", "private-baseline/SKILL.md", "skills/example/.git"} {
		t.Run(name, func(t *testing.T) {
			fixture := newSuiteFixture(t)
			fixture.write(t, name, "fixture-secret")
			_, err := ValidateSuite(fixture.root)
			if err == nil || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("unsafe artifact error = %v", err)
			}
		})
	}
	for _, target := range []string{"LICENSE", "skills"} {
		t.Run("symlink "+target, func(t *testing.T) {
			fixture := newSuiteFixture(t)
			if err := os.Symlink(filepath.Join(fixture.root, target), filepath.Join(fixture.root, "linked")); err != nil {
				t.Fatal(err)
			}
			fixture.invalid(t, "symlink")
		})
	}
	t.Run("source symlink", func(t *testing.T) {
		fixture := newSuiteFixture(t)
		link := filepath.Join(t.TempDir(), "source")
		if err := os.Symlink(fixture.root, link); err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateSuite(link); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("manifest ancestor symlink escapes", func(t *testing.T) {
		fixture := newSuiteFixture(t)
		if err := os.Symlink(t.TempDir(), filepath.Join(fixture.root, "outside")); err != nil {
			t.Fatal(err)
		}
		fixture.manifest["registrations"].(Object)["example"] = "outside/missing"
		fixture.writeManifest(t)
		if _, err := LoadManifest(fixture.root); err == nil || !strings.Contains(err.Error(), "escapes") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestValidateSuiteIgnoredArtifactsRequireRealUntrackedGitCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is required to verify checkout-local artifact rules")
	}
	fixture := newSuiteFixture(t)
	git := func(arguments ...string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", arguments...)
		command.Dir = fixture.root
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", arguments, err, output)
		}
	}
	git("init", "--quiet")
	fixture.write(t, ".gitignore", ".venv/\n__pycache__/\n")
	fixture.write(t, ".venv/lib/ignored.py", "disposable local environment\n")
	fixture.write(t, "scripts/__pycache__/helper.pyc", "disposable bytecode\n")
	if err := os.Symlink("/outside/local-runtime", filepath.Join(fixture.root, ".venv/python")); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateSuite(fixture.root); err != nil {
		t.Fatal(err)
	}
	git("add", "--force", "--", ".venv/lib/ignored.py")
	fixture.invalid(t, "private or generated directory")
	git("rm", "--cached", "--force", "--", ".venv/lib/ignored.py")
	fixture.skill(t, "skills/example", "example", "[Cache](../../.venv/lib/ignored.py)")
	fixture.invalid(t, "private or generated directory")
	fixture.skill(t, "skills/example", "example", "")
	if err := os.RemoveAll(filepath.Join(fixture.root, ".git")); err != nil {
		t.Fatal(err)
	}
	fixture.invalid(t, "private or generated directory")
}

func TestValidateSuiteIgnoredNativeArtifactsRequireRealUntrackedGitCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is required to verify checkout-local artifact rules")
	}
	checkout := func(t *testing.T) (*suiteFixture, func(...string)) {
		t.Helper()
		fixture := newSuiteFixture(t)
		git := func(arguments ...string) {
			t.Helper()
			command := exec.CommandContext(t.Context(), "git", arguments...)
			command.Dir = fixture.root
			command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v: %s", arguments, err, output)
			}
		}
		git("init", "--quiet")
		fixture.write(t, ".gitignore", ".bin/\n.bin.lock/\ndist/\n.cw-release-*/\n")
		return fixture, git
	}
	artifacts := []string{".bin/cw", ".bin.lock/owner", "dist/cw_linux_arm64.tar.gz", ".cw-release-fixture/cw_linux_arm64.tar.gz"}
	t.Run("ignored native build and release directories", func(t *testing.T) {
		fixture, _ := checkout(t)
		for _, name := range artifacts {
			fixture.write(t, name, "disposable native build artifact\n")
		}
		if _, err := ValidateSuite(fixture.root); err != nil {
			t.Fatalf("ignored native artifacts in a real Git checkout were rejected: %v", err)
		}
	})
	for _, name := range artifacts {
		for _, condition := range []string{"tracked", "extracted", "unignored", "symlink directory"} {
			t.Run(name+" "+condition, func(t *testing.T) {
				fixture, git := checkout(t)
				if condition == "symlink directory" {
					if err := os.Symlink(t.TempDir(), filepath.Join(fixture.root, filepath.Dir(name))); err != nil {
						t.Fatal(err)
					}
					fixture.invalid(t, "symlink in source tree")
					return
				}
				fixture.write(t, name, "private artifact must not enter a source snapshot\n")
				switch condition {
				case "tracked":
					git("add", "--force", "--", name)
				case "extracted":
					if err := os.RemoveAll(filepath.Join(fixture.root, ".git")); err != nil {
						t.Fatal(err)
					}
				case "unignored":
					fixture.write(t, ".gitignore", "")
				}
				fixture.invalid(t, "private or generated directory")
			})
		}
	}
}

func TestValidateMarkdownRejectsInvalidLiveReferences(t *testing.T) {
	for _, body := range []string{
		"[missing](missing.md)", "![missing](missing.png)", "[missing]: missing.md\n",
		"[outside](../../../outside.md)", "[encoded](%2e%2e/%2e%2e/%2e%2e/outside.md)",
		"[absolute](/Users/someone/private.md)", "[windows](C:/private.md)", "[file](file:///tmp/private.md)",
		"[placeholder](references/{missing}.md)", "<!--\n```bash\nignored\n-->\n[missing](missing.md)",
		"[cache](../../.venv/local.md)",
	} {
		t.Run(body, func(t *testing.T) {
			fixture := newSuiteFixture(t)
			fixture.skill(t, "skills/example", "example", body)
			fixture.invalid(t, "Markdown")
		})
	}
}

func TestModelPolicyRejectsUnsafeRoutingAndSafeguards(t *testing.T) {
	changes := []struct {
		name string
		edit func(Object)
	}{
		{"version boolean", func(p Object) { p["version"] = true }},
		{"missing field", func(p Object) { delete(p, "lookup_boundary") }},
		{"duplicate activation", func(p Object) {
			p["activation"].(Object)["any_of"] = []any{"cross_component_change", "cross_component_change"}
		}},
		{"luna substantive", func(p Object) {
			profile := p["profiles"].(Object)["sol"].(Object)
			profile["model"], profile["user_requested_efforts"] = "gpt-6-luna", []any{}
		}},
		{"Sol default max under legacy rule", func(p Object) { p["effort_policy"].(Object)["max_for_sol"] = "explicit_user_request" }},
		{"unsupported effort", func(p Object) { p["profiles"].(Object)["luna"].(Object)["reasoning_effort"] = "ultra" }},
		{"duplicate requested effort", func(p Object) { p["profiles"].(Object)["sol"].(Object)["user_requested_efforts"] = []any{"max", "max"} }},
		{"configured max mismatch", func(p Object) { p["effort_policy"].(Object)["configured_max_profiles"] = []any{} }},
		{"automatic escalation", func(p Object) { p["effort_policy"].(Object)["automatic_escalation"] = true }},
		{"coordinator mismatch", func(p Object) { p["effort_policy"].(Object)["default_substantive"] = "high" }},
		{"review evidence route", func(p Object) { p["roles"].(Object)["review"] = "luna" }},
		{"unknown fallback evidence", func(p Object) { p["unknown_role_profile"] = "luna" }},
		{"worker fallback evidence", func(p Object) { p["fallbacks"].(Object)["luna_unavailable"] = "luna" }},
		{"unsafe unavailable Sol fallback", func(p Object) { p["fallbacks"].(Object)["sol_unavailable"] = "luna" }},
		{"unsafe unavailable Jev fallback", func(p Object) { p["fallbacks"].(Object)["jev_unavailable_or_inconclusive"] = "automatic_escalation" }},
		{"runtime specialist override", func(p Object) {
			p["fixed_specialists"].(Object)["context_explorer"].(Object)["respect_runtime_model"] = false
		}},
		{"mutable Jev alias", func(p Object) { p["consultation"].(Object)["model"] = "jev-latest" }},
		{"deadline float", func(p Object) { p["consultation"].(Object)["deadline_seconds"] = 30.5 }},
		{"automatic retries", func(p Object) { p["consultation"].(Object)["automatic_retries"] = true }},
		{"question escapes", func(p Object) { p["consultation"].(Object)["questions"] = "../../global/AGENTS.md" }},
		{"publication weakened", func(p Object) { p["publication"].(Object)["refresh_before_write"] = false }},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			fixture := newSuiteFixture(t)
			policyPath := filepath.Join(fixture.root, fixture.manifest["model_policy"].(string))
			data, err := os.ReadFile(policyPath)
			if err != nil {
				t.Fatal(err)
			}
			var policy Object
			if err := yaml.Unmarshal(data, &policy); err != nil {
				t.Fatal(err)
			}
			change.edit(policy)
			data, err = yaml.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(policyPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			fixture.invalid(t, "model policy is invalid")
			if _, err := ModelDefaults(fixture.root, fixture.manifest); err == nil {
				t.Fatal("ModelDefaults accepted unsafe policy")
			}
		})
	}
	t.Run("duplicate policy key", func(t *testing.T) {
		fixture := newSuiteFixture(t)
		fixture.write(t, fixture.manifest["model_policy"].(string), "version: 2\nversion: 2\n")
		fixture.invalid(t, "duplicate mapping key")
	})
	t.Run("duplicate question keys", func(t *testing.T) {
		fixture := newSuiteFixture(t)
		fixture.write(t, "skills/task-orchestration/references/jev-questions.json", `{"version":1,"questions":{"duplicate":{},"duplicate":{}}}`)
		fixture.invalid(t, "duplicate mapping key")
	})
	t.Run("invalid choice", func(t *testing.T) {
		fixture := newSuiteFixture(t)
		fixture.write(t, "skills/task-orchestration/references/jev-questions.json", `{"version":1,"questions":{"one":{"type":"choice","instructions":"text","criteria":{"only":"one"}}}}`)
		fixture.invalid(t, "Choice needs at least two options")
	})
}

func TestModelPolicyDecisionProviders(t *testing.T) {
	dual := func(consultation Object) {
		consultation["providers"] = Object{"jev": consultation["model"], "subq_snap": "subq-snap-preview"}
		consultation["comparison"] = "codex_evidence_and_verification"
	}
	optional := func(consultation Object) {
		dual(consultation)
		consultation["optional_providers"] = Object{"openai_decisions": "gpt-6-luna"}
	}
	required := func(consultation Object) {
		dual(consultation)
		consultation["providers"].(Object)["openai_decisions"] = "gpt-6-luna"
	}
	cases := []struct {
		name  string
		valid bool
		edit  func(Object)
	}{
		{"legacy version 2", true, func(Object) {}},
		{"dual providers", true, dual},
		{"optional OpenAI adviser", true, optional},
		{"required three advisers", true, required},
		{"required three advisers matching pinned Jev", true, func(c Object) {
			c["model"] = "jev-1.15.0"
			required(c)
		}},
		{"matching pinned Jev version", true, func(c Object) {
			c["model"] = "jev-1.14.0"
			dual(c)
		}},
		{"providers only", false, func(c Object) {
			dual(c)
			delete(c, "comparison")
		}},
		{"comparison only", false, func(c Object) {
			dual(c)
			delete(c, "providers")
		}},
		{"null providers", false, func(c Object) { dual(c); c["providers"] = nil }},
		{"provider sequence", false, func(c Object) { dual(c); c["providers"] = []any{"jev", "subq_snap"} }},
		{"missing Jev", false, func(c Object) { dual(c); delete(c["providers"].(Object), "jev") }},
		{"missing Snap", false, func(c Object) { dual(c); delete(c["providers"].(Object), "subq_snap") }},
		{"unknown provider", false, func(c Object) { dual(c); c["providers"].(Object)["other"] = "other-model" }},
		{"Jev model mismatch", false, func(c Object) { dual(c); c["providers"].(Object)["jev"] = "jev-1.12.0" }},
		{"Jev model type", false, func(c Object) { dual(c); c["providers"].(Object)["jev"] = Object{"model": c["model"]} }},
		{"Snap model alias", false, func(c Object) { dual(c); c["providers"].(Object)["subq_snap"] = "subq-latest" }},
		{"Snap model type", false, func(c Object) { dual(c); c["providers"].(Object)["subq_snap"] = []any{"subq-snap-preview"} }},
		{"comparison null", false, func(c Object) { dual(c); c["comparison"] = nil }},
		{"comparison votes", false, func(c Object) { dual(c); c["comparison"] = "majority_vote" }},
		{"optional without required pair", false, func(c Object) { c["optional_providers"] = Object{"openai_decisions": "gpt-6-luna"} }},
		{"optional without comparison", false, func(c Object) { optional(c); delete(c, "comparison") }},
		{"optional without providers", false, func(c Object) { optional(c); delete(c, "providers") }},
		{"null optional providers", false, func(c Object) { optional(c); c["optional_providers"] = nil }},
		{"optional provider sequence", false, func(c Object) { optional(c); c["optional_providers"] = []any{"openai_decisions"} }},
		{"empty optional providers", false, func(c Object) { optional(c); c["optional_providers"] = Object{} }},
		{"unknown optional provider", false, func(c Object) { optional(c); c["optional_providers"] = Object{"other": "gpt-6-luna"} }},
		{"extra optional provider", false, func(c Object) { optional(c); c["optional_providers"].(Object)["other"] = "gpt-6-luna" }},
		{"OpenAI model alias", false, func(c Object) { optional(c); c["optional_providers"].(Object)["openai_decisions"] = "gpt-latest" }},
		{"OpenAI different model", false, func(c Object) { optional(c); c["optional_providers"].(Object)["openai_decisions"] = "gpt-6.1-sol" }},
		{"OpenAI null model", false, func(c Object) { optional(c); c["optional_providers"].(Object)["openai_decisions"] = nil }},
		{"OpenAI boolean model", false, func(c Object) { optional(c); c["optional_providers"].(Object)["openai_decisions"] = true }},
		{"OpenAI model mapping", false, func(c Object) {
			optional(c)
			c["optional_providers"].(Object)["openai_decisions"] = Object{"model": "gpt-6-luna"}
		}},
		{"OpenAI cannot replace required Snap", false, func(c Object) {
			optional(c)
			c["providers"] = Object{"jev": c["model"], "openai_decisions": "gpt-6-luna"}
		}},
		{"required triple without comparison", false, func(c Object) { required(c); delete(c, "comparison") }},
		{"required triple missing Jev", false, func(c Object) { required(c); delete(c["providers"].(Object), "jev") }},
		{"required triple missing Snap", false, func(c Object) { required(c); delete(c["providers"].(Object), "subq_snap") }},
		{"required OpenAI model alias", false, func(c Object) { required(c); c["providers"].(Object)["openai_decisions"] = "gpt-latest" }},
		{"required OpenAI wrong model", false, func(c Object) { required(c); c["providers"].(Object)["openai_decisions"] = "gpt-6.1-sol" }},
		{"required OpenAI null model", false, func(c Object) { required(c); c["providers"].(Object)["openai_decisions"] = nil }},
		{"required OpenAI boolean model", false, func(c Object) { required(c); c["providers"].(Object)["openai_decisions"] = true }},
		{"required OpenAI model mapping", false, func(c Object) {
			required(c)
			c["providers"].(Object)["openai_decisions"] = Object{"model": "gpt-6-luna"}
		}},
		{"required OpenAI model sequence", false, func(c Object) { required(c); c["providers"].(Object)["openai_decisions"] = []any{"gpt-6-luna"} }},
		{"unknown required provider", false, func(c Object) { required(c); c["providers"].(Object)["other"] = "other-model" }},
		{"OpenAI required and optional", false, func(c Object) { optional(c); c["providers"].(Object)["openai_decisions"] = "gpt-6-luna" }},
		{"required triple with null optional map", false, func(c Object) { required(c); c["optional_providers"] = nil }},
		{"required triple with empty optional map", false, func(c Object) { required(c); c["optional_providers"] = Object{} }},
		{"unknown legacy field", false, func(c Object) { c["unknown"] = true }},
		{"unknown dual field", false, func(c Object) { dual(c); c["unknown"] = true }},
		{"unknown optional field", false, func(c Object) { optional(c); c["unknown"] = true }},
		{"unknown required triple field", false, func(c Object) { required(c); c["unknown"] = true }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSuiteFixture(t)
			policyPath := filepath.Join(fixture.root, fixture.manifest["model_policy"].(string))
			data, err := os.ReadFile(policyPath)
			if err != nil {
				t.Fatal(err)
			}
			var policy Object
			if err := yaml.Unmarshal(data, &policy); err != nil {
				t.Fatal(err)
			}
			consultation := policy["consultation"].(Object)
			delete(consultation, "providers")
			delete(consultation, "comparison")
			delete(consultation, "optional_providers")
			test.edit(consultation)
			data, err = yaml.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(policyPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if test.valid {
				if _, err := ValidateSuite(fixture.root); err != nil {
					t.Fatalf("valid consultation rejected: %v", err)
				}
				if _, err := ModelDefaults(fixture.root, fixture.manifest); err != nil {
					t.Fatalf("valid model defaults rejected: %v", err)
				}
			} else {
				fixture.invalid(t, "model policy is invalid")
				if _, err := ModelDefaults(fixture.root, fixture.manifest); err == nil {
					t.Fatal("model defaults accepted invalid consultation")
				}
			}
		})
	}
}

func TestSuiteYAMLAliasesAndDuplicateMergeKeys(t *testing.T) {
	for _, valid := range []string{"name: example\ndescription: text\nmetadata:\n  first: &first {a: 1}\n  second: {<<: *first, b: 2}\n", "name: example\ndescription: text\nmetadata:\n  first: &first {a: 1}\n  second: &second {b: 2}\n  merged: {<<: [*first, *second]}\n"} {
		if _, err := suiteYAML([]byte(valid)); err != nil {
			t.Fatalf("valid aliases rejected: %v", err)
		}
	}
	for _, invalid := range []string{"a: &a {recursive: *a}\n", "a: &a {key: one}\nb: &b {key: two}\nc: {<<: [*a, *b]}\n", "metadata: {1: one, 1.0: two}\n", "a: !!python/name:fixture-secret ''\n", "version: 2\n---\nversion: 2\n"} {
		if _, err := suiteYAML([]byte(invalid)); err == nil || bytes.Contains([]byte(err.Error()), []byte("fixture-secret")) {
			t.Fatalf("unsafe YAML error = %v", err)
		}
	}
}
