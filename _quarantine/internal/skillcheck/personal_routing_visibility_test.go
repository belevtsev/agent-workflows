package skillcheck

// Tests for contract WP4-visibility revision 1, requirements R1 and R2: the
// personal routing corpus may only expect implicit routing to skills that both
// hosts list automatically.

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const visPersonalCorpus = "testdata/personal-skill-routing"

// visTargetHidden reports whether the skill at the repository-relative SKILL.md
// path is hidden on either host: Claude via disable-model-invocation: true, or
// Codex via agents/openai.yaml policy.allow_implicit_invocation: false.
func visTargetHidden(source, target string) (bool, error) {
	skillPath := filepath.Join(source, filepath.FromSlash(target))
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return false, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return false, fmt.Errorf("%s: missing frontmatter", target)
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return false, fmt.Errorf("%s: unterminated frontmatter", target)
	}
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &frontmatter); err != nil {
		return false, fmt.Errorf("%s: %w", target, err)
	}
	if value, ok := frontmatter["disable-model-invocation"]; ok {
		flag, isBool := value.(bool)
		if !isBool {
			return false, fmt.Errorf("%s: disable-model-invocation is not a boolean", target)
		}
		if flag {
			return true, nil
		}
	}
	codexData, err := os.ReadFile(filepath.Join(filepath.Dir(skillPath), "agents", "openai.yaml"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var codex struct {
		Policy struct {
			AllowImplicitInvocation *bool `yaml:"allow_implicit_invocation"`
		} `yaml:"policy"`
	}
	if err := yaml.Unmarshal(codexData, &codex); err != nil {
		return false, fmt.Errorf("%s agents/openai.yaml: %w", target, err)
	}
	allow := codex.Policy.AllowImplicitInvocation
	return allow != nil && !*allow, nil
}

// visRoutingViolations returns one message per case that expects implicit
// routing to a hidden skill or that targets a hidden skill without being explicit.
func visRoutingViolations(source string, fixtures routingFixtures) ([]string, error) {
	var violations []string
	for _, routing := range fixtures.Cases {
		hidden, err := visTargetHidden(source, routing.TargetRead)
		if err != nil {
			return nil, fmt.Errorf("case %s: %w", routing.ID, err)
		}
		if hidden && routing.Mode != "explicit" {
			violations = append(violations, fmt.Sprintf("case %s targets hidden %s with mode %q", routing.ID, routing.TargetRead, routing.Mode))
		}
	}
	return violations, nil
}

// R2: the shipped personal corpus never expects implicit routing to a hidden skill.
func TestPersonalRoutingVisibilityShipped(t *testing.T) {
	t.Parallel()
	source := filepath.Join("..", "..")
	fixtures, err := decodeRoutingFixture[routingFixtures](filepath.Join(visPersonalCorpus, "oracles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures.Cases) == 0 {
		t.Fatal("R2 personal corpus has no cases")
	}
	violations, err := visRoutingViolations(source, fixtures)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Errorf("R2 %s", violation)
	}
	// Every implicit case's target must be listed on both hosts.
	for _, routing := range fixtures.Cases {
		if routing.Mode != "implicit" {
			continue
		}
		hidden, err := visTargetHidden(source, routing.TargetRead)
		if err != nil {
			t.Fatal(err)
		}
		if hidden {
			t.Errorf("R2 implicit case %s targets hidden %s", routing.ID, routing.TargetRead)
		}
	}
}

// R2: the cross-check rejects implicit routing to a hidden skill, on either host,
// and accepts the agreed combinations.
func TestPersonalRoutingVisibilityCrossCheck(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	write := func(name, value string) {
		target := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	skill := func(name, extra string) string {
		return "---\nname: " + name + "\ndescription: Fixture.\n" + extra + "---\n\n# Fixture\n"
	}
	write("skills/listed/SKILL.md", skill("listed", ""))
	write("skills/listed-codex/SKILL.md", skill("listed-codex", "disable-model-invocation: false\n"))
	write("skills/listed-codex/agents/openai.yaml", "interface:\n  display_name: \"Listed\"\npolicy:\n  allow_implicit_invocation: true\n")
	write("skills/hidden/SKILL.md", skill("hidden", "disable-model-invocation: true\n"))
	write("skills/hidden/agents/openai.yaml", "policy:\n  allow_implicit_invocation: false\n")
	write("skills/claude-hidden/SKILL.md", skill("claude-hidden", "disable-model-invocation: true\n"))
	write("skills/codex-hidden/SKILL.md", skill("codex-hidden", ""))
	write("skills/codex-hidden/agents/openai.yaml", "policy:\n  allow_implicit_invocation: false\n")

	cases := []struct {
		name   string
		mode   string
		target string
		want   bool // want a violation
	}{
		{name: "R2 implicit to listed is accepted", mode: "implicit", target: "skills/listed/SKILL.md"},
		{name: "R2 implicit to explicitly listed is accepted", mode: "implicit", target: "skills/listed-codex/SKILL.md"},
		{name: "R2 explicit to listed is accepted", mode: "explicit", target: "skills/listed/SKILL.md"},
		{name: "R2 explicit to hidden is accepted", mode: "explicit", target: "skills/hidden/SKILL.md"},
		{name: "R2 implicit to hidden is rejected", mode: "implicit", target: "skills/hidden/SKILL.md", want: true},
		{name: "R2 implicit to Claude-hidden is rejected", mode: "implicit", target: "skills/claude-hidden/SKILL.md", want: true},
		{name: "R2 implicit to Codex-hidden is rejected", mode: "implicit", target: "skills/codex-hidden/SKILL.md", want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixtures := routingFixtures{Version: 1, Cases: []routingCase{{ID: "case", Mode: c.mode, TargetRead: c.target}}}
			violations, err := visRoutingViolations(source, fixtures)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(violations) > 0; got != c.want {
				t.Fatalf("violations = %v; want violation %v", violations, c.want)
			}
		})
	}

	// R2 against shipped files: the pre-R1 docker case (implicit, targeting the
	// now-hidden docker-build-strategies) must be reported.
	shipped := filepath.Join("..", "..")
	fixtures := routingFixtures{Version: 1, Cases: []routingCase{{ID: "docker", Mode: "implicit", TargetRead: "third_party/docker-skills/docker-build-strategies/SKILL.md"}}}
	violations, err := visRoutingViolations(shipped, fixtures)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Fatalf("R2 implicit docker case against shipped files: violations = %v; want one", violations)
	}
}

// R1: the docker case is explicit with an explicit prompt; nothing else in the
// case changed, and the diagram case stays implicit.
func TestPersonalRoutingDockerCaseIsExplicit(t *testing.T) {
	t.Parallel()
	fixtures, err := decodeRoutingFixture[routingFixtures](filepath.Join(visPersonalCorpus, "oracles.json"))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]routingCase{}
	for _, routing := range fixtures.Cases {
		byID[routing.ID] = routing
	}
	docker, ok := byID["docker"]
	if !ok {
		t.Fatal("R1 docker case is missing")
	}
	wantDocker := routingCase{
		ID: "docker", Mode: "explicit", InputDir: "inputs/docker",
		TargetRead:       "third_party/docker-skills/docker-build-strategies/SKILL.md",
		RequiredReads:    []string{"third_party/docker-skills/docker-build-strategies/SKILL.md"},
		RequiredClaims:   []string{"dependency layer follows complete source copy", "single stage includes build toolchain", "runtime privilege and required files need verification", "no executed build evidence"},
		ForbiddenClaims:  []string{"build passed", "runtime proven"},
		ForbiddenEffects: []string{"build", "containers", "installation", "implementation", "publication"},
	}
	if !reflect.DeepEqual(docker, wantDocker) {
		t.Errorf("R1 docker case = %+v; want %+v", docker, wantDocker)
	}

	diagram, ok := byID["diagram"]
	if !ok {
		t.Fatal("R1 diagram case is missing")
	}
	if diagram.Mode != "implicit" || diagram.TargetRead != "third_party/archify/SKILL.md" {
		t.Errorf("R1 diagram case mode=%q target=%q; want implicit third_party/archify/SKILL.md", diagram.Mode, diagram.TargetRead)
	}

	prompt, err := os.ReadFile(filepath.Join(visPersonalCorpus, "inputs", "docker", "prompt.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(prompt)
	if !strings.HasPrefix(text, "Use $docker-build-strategies to review the attached Dockerfile") {
		t.Errorf("R1 docker prompt does not start with the explicit invocation: %q", text)
	}
	// The original meaning and limits are preserved.
	flat := strings.Join(strings.Fields(text), " ")
	for _, phrase := range []string{
		"backend build reliability, cache behavior, image size, and runtime access",
		"Explain concrete changes and verification limits in chat.",
		"Do not build, run containers, install tools, edit files, or publish.",
		"You may read local files.",
	} {
		if !strings.Contains(flat, phrase) {
			t.Errorf("R1 docker prompt lost %q", phrase)
		}
	}
	if err := validateRoutingFixtures(filepath.Join("..", ".."), visPersonalCorpus, fixtures); err != nil {
		t.Errorf("R1 validateRoutingFixtures() error = %v", err)
	}
}
