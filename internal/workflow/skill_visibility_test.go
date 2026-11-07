package workflow

// Tests for contract WP4-visibility revision 2 (requirements V1-V6): a skill can
// be left out of the hosts' automatic skill listing, Claude Code and Codex must
// agree on that choice, and the shipped suite hides exactly the agreed set. Both
// host flags must be plain YAML 1.2 booleans; YAML 1.1 words such as yes/no/on/off
// and quoted or tagged values are rejected.

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// visSkillMD renders a SKILL.md whose frontmatter carries extra (raw YAML lines)
// after the name and description.
func visSkillMD(name, extra string) string {
	return "---\nname: " + name + "\ndescription: A bounded fixture workflow.\n" + extra + "---\n\n# Fixture\n\n"
}

const (
	visDMITrue        = "disable-model-invocation: true\n"
	visDMIFalse       = "disable-model-invocation: false\n"
	visPolicyFalse    = "policy:\n  allow_implicit_invocation: false\n"
	visPolicyTrue     = "policy:\n  allow_implicit_invocation: true\n"
	visInterfaceOnly  = "interface:\n  display_name: \"Example\"\n  short_description: \"A fixture skill\"\n"
	visHiddenCodex    = visInterfaceOnly + visPolicyFalse
	visNoOpenAIConfig = "\x00absent" // sentinel: do not write agents/openai.yaml
)

// visWriteSkill writes source/SKILL.md with the given frontmatter extras and,
// unless openai is the absent sentinel, source/agents/openai.yaml.
func visWriteSkill(t *testing.T, fixture *suiteFixture, source, name, frontmatter, openai string) {
	t.Helper()
	fixture.write(t, source+"/SKILL.md", visSkillMD(name, frontmatter))
	if openai != visNoOpenAIConfig {
		fixture.write(t, source+"/agents/openai.yaml", openai)
	}
}

func visValid(t *testing.T, fixture *suiteFixture) {
	t.Helper()
	if _, err := ValidateSuite(fixture.root); err != nil {
		t.Fatalf("ValidateSuite() error = %v; want nil", err)
	}
}

// visAddRegistration adds name -> source to the fixture manifest.
func visAddRegistration(t *testing.T, fixture *suiteFixture, name, source string) {
	t.Helper()
	registrations, ok := fixture.manifest["registrations"].(Object)
	if !ok {
		t.Fatalf("fixture registrations = %T; want Object", fixture.manifest["registrations"])
	}
	registrations[name] = source
	fixture.writeManifest(t)
}

// Baseline: the unmodified fixture is valid, so every rejection below is caused
// by the visibility change under test.
func TestSkillVisibilityFixtureBaselineIsValid(t *testing.T) {
	t.Parallel()
	visValid(t, newSuiteFixture(t))
}

type visCase struct {
	name        string
	frontmatter string
	openai      string
	wantErr     string // empty means ValidateSuite must succeed
}

func visRun(t *testing.T, cases []visCase, apply func(t *testing.T, fixture *suiteFixture, c visCase)) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSuiteFixture(t)
			apply(t, fixture, c)
			if c.wantErr == "" {
				visValid(t, fixture)
				return
			}
			fixture.invalid(t, c.wantErr)
		})
	}
}

func visTopLevelExample(t *testing.T, fixture *suiteFixture, c visCase) {
	t.Helper()
	visWriteSkill(t, fixture, "skills/example", "example", c.frontmatter, c.openai)
}

// V1: disable-model-invocation is an accepted key; only YAML booleans are valid.
func TestSkillVisibilityDisableModelInvocationValue(t *testing.T) {
	t.Parallel()
	visRun(t, []visCase{
		{name: "V1 boolean true with matching Codex policy is accepted", frontmatter: visDMITrue, openai: visHiddenCodex},
		{name: "V1 boolean false without Codex file is accepted", frontmatter: visDMIFalse, openai: visNoOpenAIConfig},
		{name: "V1 absent key is accepted", frontmatter: "", openai: visNoOpenAIConfig},
		{name: "V1 capitalized True is accepted as hidden", frontmatter: "disable-model-invocation: True\n", openai: visHiddenCodex},
		{name: "V1 upper-case TRUE is accepted as hidden", frontmatter: "disable-model-invocation: TRUE\n", openai: visHiddenCodex},
		{name: "V1 capitalized False is accepted as listed", frontmatter: "disable-model-invocation: False\n", openai: visNoOpenAIConfig},
		{name: "V1 upper-case FALSE is accepted as listed", frontmatter: "disable-model-invocation: FALSE\n", openai: visNoOpenAIConfig},
		// Spellings must also carry their meaning: True without a hidden Codex
		// policy is a mismatch, and FALSE with a hidden Codex policy is one too.
		{name: "V1 V2 capitalized True counts as hidden", frontmatter: "disable-model-invocation: True\n", openai: visNoOpenAIConfig, wantErr: "skill listing differs between hosts: example"},
		{name: "V1 V2 upper-case FALSE counts as listed", frontmatter: "disable-model-invocation: FALSE\n", openai: visHiddenCodex, wantErr: "skill listing differs between hosts: example"},
		// A quoted string must be rejected even when the Codex side says hidden,
		// so a validator that coerced "true" to true would fail this case.
		{name: "V1 quoted string true with hidden Codex policy is rejected", frontmatter: "disable-model-invocation: \"true\"\n", openai: visHiddenCodex, wantErr: "disable-model-invocation"},
		{name: "V1 quoted string true without Codex file is rejected", frontmatter: "disable-model-invocation: \"true\"\n", openai: visNoOpenAIConfig, wantErr: "disable-model-invocation"},
		{name: "V1 quoted string false is rejected", frontmatter: "disable-model-invocation: \"false\"\n", openai: visNoOpenAIConfig, wantErr: "disable-model-invocation"},
		{name: "V1 number one is rejected", frontmatter: "disable-model-invocation: 1\n", openai: visHiddenCodex, wantErr: "disable-model-invocation"},
		{name: "V1 number zero is rejected", frontmatter: "disable-model-invocation: 0\n", openai: visNoOpenAIConfig, wantErr: "disable-model-invocation"},
		{name: "V1 list is rejected", frontmatter: "disable-model-invocation: [true]\n", openai: visHiddenCodex, wantErr: "disable-model-invocation"},
		{name: "V1 mapping is rejected", frontmatter: "disable-model-invocation:\n  value: true\n", openai: visHiddenCodex, wantErr: "disable-model-invocation"},
		// YAML 1.1 boolean words are strings to the hosts. Pairing the "true"
		// words with a hidden Codex policy makes the case fail if a validator read
		// them as booleans; the "false" words are paired with no Codex file.
		{name: "V1 YAML 1.1 yes is rejected", frontmatter: "disable-model-invocation: yes\n", openai: visHiddenCodex, wantErr: "disable-model-invocation"},
		{name: "V1 YAML 1.1 Yes is rejected", frontmatter: "disable-model-invocation: Yes\n", openai: visHiddenCodex, wantErr: "disable-model-invocation"},
		{name: "V1 YAML 1.1 on is rejected", frontmatter: "disable-model-invocation: on\n", openai: visHiddenCodex, wantErr: "disable-model-invocation"},
		{name: "V1 YAML 1.1 ON is rejected", frontmatter: "disable-model-invocation: ON\n", openai: visHiddenCodex, wantErr: "disable-model-invocation"},
		{name: "V1 YAML 1.1 no is rejected", frontmatter: "disable-model-invocation: no\n", openai: visNoOpenAIConfig, wantErr: "disable-model-invocation"},
		{name: "V1 YAML 1.1 off is rejected", frontmatter: "disable-model-invocation: off\n", openai: visNoOpenAIConfig, wantErr: "disable-model-invocation"},
		{name: "V1 single-quoted true is rejected", frontmatter: "disable-model-invocation: 'true'\n", openai: visHiddenCodex, wantErr: "disable-model-invocation"},
		{name: "V1 tagged boolean is rejected", frontmatter: "disable-model-invocation: !!bool true\n", openai: visHiddenCodex, wantErr: "disable-model-invocation"},
	}, visTopLevelExample)
}

// V2: Claude and Codex listing must agree for every entrypoint.
func TestSkillVisibilityHostsAgree(t *testing.T) {
	t.Parallel()
	const mismatch = "skill listing differs between hosts: example"
	visRun(t, []visCase{
		// Accepted pairs.
		{name: "V2 hidden on both hosts is accepted", frontmatter: visDMITrue, openai: visHiddenCodex},
		{name: "V2 hidden on both hosts with policy-only Codex file is accepted", frontmatter: visDMITrue, openai: visPolicyFalse},
		{name: "V2 absent and absent Codex file is accepted", frontmatter: "", openai: visNoOpenAIConfig},
		{name: "V2 absent and explicit Codex true is accepted", frontmatter: "", openai: visInterfaceOnly + visPolicyTrue},
		{name: "V2 false and explicit Codex true is accepted", frontmatter: visDMIFalse, openai: visPolicyTrue},
		{name: "V2 false and Codex file without policy is accepted", frontmatter: visDMIFalse, openai: visInterfaceOnly},
		// Claude hidden, Codex listed.
		{name: "V2 Claude hidden without Codex file is rejected", frontmatter: visDMITrue, openai: visNoOpenAIConfig, wantErr: mismatch},
		{name: "V2 Claude hidden with Codex file lacking policy is rejected", frontmatter: visDMITrue, openai: visInterfaceOnly, wantErr: mismatch},
		{name: "V2 Claude hidden with empty Codex policy is rejected", frontmatter: visDMITrue, openai: "policy: {}\n", wantErr: mismatch},
		{name: "V2 Claude hidden with Codex true is rejected", frontmatter: visDMITrue, openai: visInterfaceOnly + visPolicyTrue, wantErr: mismatch},
		// Codex hidden, Claude listed.
		{name: "V2 Codex hidden with absent Claude key is rejected", frontmatter: "", openai: visHiddenCodex, wantErr: mismatch},
		{name: "V2 Codex hidden with Claude false is rejected", frontmatter: visDMIFalse, openai: visHiddenCodex, wantErr: mismatch},
	}, visTopLevelExample)
}

// V2: a Codex file belonging to a different skill does not make another skill
// consistent; the file must sit next to the hidden SKILL.md.
func TestSkillVisibilityCodexFileBelongsToItsSkill(t *testing.T) {
	t.Parallel()
	fixture := newSuiteFixture(t)
	visWriteSkill(t, fixture, "skills/example", "example", visDMITrue, visNoOpenAIConfig)
	fixture.write(t, "skills/agents/openai.yaml", visHiddenCodex)
	fixture.invalid(t, "skill listing differs between hosts: example")
}

// V3: agents/openai.yaml shape errors.
func TestSkillVisibilityCodexMetadataShape(t *testing.T) {
	t.Parallel()
	const metadata = "invalid Codex skill metadata"
	const policy = "invalid Codex skill policy"
	visRun(t, []visCase{
		{name: "V3 top-level list is rejected", frontmatter: "", openai: "- policy\n- interface\n", wantErr: metadata},
		{name: "V3 top-level scalar is rejected", frontmatter: "", openai: "listed\n", wantErr: metadata},
		{name: "V3 top-level list with hidden Claude is rejected", frontmatter: visDMITrue, openai: "- allow_implicit_invocation: false\n", wantErr: metadata},
		{name: "V3 policy scalar is rejected", frontmatter: "", openai: "policy: false\n", wantErr: policy},
		{name: "V3 policy list is rejected", frontmatter: "", openai: "policy:\n  - allow_implicit_invocation: false\n", wantErr: policy},
		{name: "V3 policy list with hidden Claude is rejected", frontmatter: visDMITrue, openai: "policy:\n  - allow_implicit_invocation: false\n", wantErr: policy},
		// A quoted "false" must not count as hidden: with hidden Claude it would
		// otherwise be accepted, without it it would otherwise be a mismatch.
		{name: "V3 quoted string false with hidden Claude is rejected", frontmatter: visDMITrue, openai: "policy:\n  allow_implicit_invocation: \"false\"\n", wantErr: policy},
		{name: "V3 quoted string false with listed Claude is rejected", frontmatter: "", openai: "policy:\n  allow_implicit_invocation: \"false\"\n", wantErr: policy},
		{name: "V3 quoted string true is rejected", frontmatter: "", openai: "policy:\n  allow_implicit_invocation: \"true\"\n", wantErr: policy},
		{name: "V3 number zero with hidden Claude is rejected", frontmatter: visDMITrue, openai: "policy:\n  allow_implicit_invocation: 0\n", wantErr: policy},
		{name: "V3 list value is rejected", frontmatter: "", openai: "policy:\n  allow_implicit_invocation: [false]\n", wantErr: policy},
		// YAML 1.1 words and quoted booleans follow the V1 plain-boolean rule.
		// The "false" words are paired with hidden Claude so that reading them as
		// booleans would make the case pass validation instead of failing.
		{name: "V3 YAML 1.1 no with hidden Claude is rejected", frontmatter: visDMITrue, openai: "policy:\n  allow_implicit_invocation: no\n", wantErr: policy},
		{name: "V3 YAML 1.1 No with hidden Claude is rejected", frontmatter: visDMITrue, openai: "policy:\n  allow_implicit_invocation: No\n", wantErr: policy},
		{name: "V3 YAML 1.1 off with hidden Claude is rejected", frontmatter: visDMITrue, openai: "policy:\n  allow_implicit_invocation: off\n", wantErr: policy},
		{name: "V3 YAML 1.1 OFF with hidden Claude is rejected", frontmatter: visDMITrue, openai: "policy:\n  allow_implicit_invocation: OFF\n", wantErr: policy},
		{name: "V3 YAML 1.1 yes is rejected", frontmatter: "", openai: "policy:\n  allow_implicit_invocation: yes\n", wantErr: policy},
		{name: "V3 YAML 1.1 on is rejected", frontmatter: "", openai: "policy:\n  allow_implicit_invocation: on\n", wantErr: policy},
		{name: "V3 single-quoted false with hidden Claude is rejected", frontmatter: visDMITrue, openai: "policy:\n  allow_implicit_invocation: 'false'\n", wantErr: policy},
		{name: "V3 tagged boolean with hidden Claude is rejected", frontmatter: visDMITrue, openai: "policy:\n  allow_implicit_invocation: !!bool false\n", wantErr: policy},
		// Valid shapes.
		{name: "V3 interface-only file is valid and listed", frontmatter: "", openai: visInterfaceOnly},
		{name: "V3 policy mapping with boolean is valid", frontmatter: visDMITrue, openai: visHiddenCodex},
		{name: "V3 upper-case FALSE is a plain boolean meaning hidden", frontmatter: visDMITrue, openai: "policy:\n  allow_implicit_invocation: FALSE\n"},
		{name: "V3 capitalized True is a plain boolean meaning listed", frontmatter: "", openai: "policy:\n  allow_implicit_invocation: True\n"},
		{name: "V3 V2 capitalized False counts as hidden", frontmatter: "", openai: "policy:\n  allow_implicit_invocation: False\n", wantErr: "skill listing differs between hosts: example"},
	}, visTopLevelExample)
}

// V4: protected skills must stay listed even when hidden consistently.
func TestSkillVisibilityProtectedSkillsStayListed(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"archify", "code-review", "graphify", "task-orchestration", "test-strategy"} {
		for _, hidden := range []bool{false, true} {
			label := "V4 " + name + " listed is accepted"
			if hidden {
				label = "V4 " + name + " hidden on both hosts is rejected"
			}
			t.Run(label, func(t *testing.T) {
				t.Parallel()
				fixture := newSuiteFixture(t)
				source := "skills/" + name
				if name != "task-orchestration" {
					visAddRegistration(t, fixture, name, source)
				}
				if !hidden {
					visWriteSkill(t, fixture, source, name, "", visNoOpenAIConfig)
					visValid(t, fixture)
					return
				}
				visWriteSkill(t, fixture, source, name, visDMITrue, visHiddenCodex)
				fixture.invalid(t, "skill must stay listed: "+name)
			})
		}
	}
}

// V4 control: a non-protected skill hidden on both hosts is accepted, so the
// protected-name rejection above is specific to the protected names.
func TestSkillVisibilityUnprotectedSkillMayBeHidden(t *testing.T) {
	t.Parallel()
	fixture := newSuiteFixture(t)
	visAddRegistration(t, fixture, "archived", "skills/archived")
	visWriteSkill(t, fixture, "skills/archived", "archived", visDMITrue, visHiddenCodex)
	visValid(t, fixture)
}

// V5: V1-V3 apply to nested bundle entrypoints, with the Codex file next to the
// nested SKILL.md.
func TestSkillVisibilityNestedEntrypoints(t *testing.T) {
	t.Parallel()
	const nested = "third_party/bundle/skills/bundled-tool"
	type nestedCase struct {
		name        string
		frontmatter string
		nestedCodex string // written next to the nested SKILL.md unless absent
		rootCodex   string // written at the bundle source root unless absent
		wantErr     string
	}
	cases := []nestedCase{
		{name: "V5 nested listed skill is accepted", nestedCodex: visNoOpenAIConfig, rootCodex: visNoOpenAIConfig},
		{name: "V5 nested hidden on both hosts is accepted", frontmatter: visDMITrue, nestedCodex: visHiddenCodex, rootCodex: visNoOpenAIConfig},
		{name: "V5 V2 nested Claude hidden without Codex file is rejected", frontmatter: visDMITrue, nestedCodex: visNoOpenAIConfig, rootCodex: visNoOpenAIConfig, wantErr: "skill listing differs between hosts: bundled-tool"},
		{name: "V5 V2 nested Codex file at bundle root does not count", frontmatter: visDMITrue, nestedCodex: visNoOpenAIConfig, rootCodex: visHiddenCodex, wantErr: "skill listing differs between hosts: bundled-tool"},
		{name: "V5 V2 nested Codex hidden with listed Claude is rejected", nestedCodex: visHiddenCodex, rootCodex: visNoOpenAIConfig, wantErr: "skill listing differs between hosts: bundled-tool"},
		{name: "V5 V2 nested Claude hidden with Codex true is rejected", frontmatter: visDMITrue, nestedCodex: visPolicyTrue, rootCodex: visNoOpenAIConfig, wantErr: "skill listing differs between hosts: bundled-tool"},
		{name: "V5 V1 nested quoted string is rejected", frontmatter: "disable-model-invocation: \"true\"\n", nestedCodex: visHiddenCodex, rootCodex: visNoOpenAIConfig, wantErr: "disable-model-invocation"},
		{name: "V5 V1 nested YAML 1.1 yes is rejected", frontmatter: "disable-model-invocation: yes\n", nestedCodex: visHiddenCodex, rootCodex: visNoOpenAIConfig, wantErr: "disable-model-invocation"},
		{name: "V5 V3 nested YAML 1.1 off is rejected", frontmatter: visDMITrue, nestedCodex: "policy:\n  allow_implicit_invocation: off\n", rootCodex: visNoOpenAIConfig, wantErr: "invalid Codex skill policy"},
		{name: "V5 V1 nested number is rejected", frontmatter: "disable-model-invocation: 1\n", nestedCodex: visNoOpenAIConfig, rootCodex: visNoOpenAIConfig, wantErr: "disable-model-invocation"},
		{name: "V5 V3 nested top-level list is rejected", nestedCodex: "- policy\n", rootCodex: visNoOpenAIConfig, wantErr: "invalid Codex skill metadata"},
		{name: "V5 V3 nested policy scalar is rejected", nestedCodex: "policy: hidden\n", rootCodex: visNoOpenAIConfig, wantErr: "invalid Codex skill policy"},
		{name: "V5 V3 nested quoted boolean is rejected", frontmatter: visDMITrue, nestedCodex: "policy:\n  allow_implicit_invocation: \"false\"\n", rootCodex: visNoOpenAIConfig, wantErr: "invalid Codex skill policy"},
		{name: "V5 V3 nested interface-only file is listed", nestedCodex: visInterfaceOnly, rootCodex: visNoOpenAIConfig},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSuiteFixture(t)
			visAddRegistration(t, fixture, "bundle", "third_party/bundle")
			visWriteSkill(t, fixture, nested, "bundled-tool", c.frontmatter, c.nestedCodex)
			if c.rootCodex != visNoOpenAIConfig {
				fixture.write(t, "third_party/bundle/agents/openai.yaml", c.rootCodex)
			}
			if c.wantErr == "" {
				visValid(t, fixture)
				return
			}
			fixture.invalid(t, c.wantErr)
		})
	}
}

// visShippedVisibility reads one shipped entrypoint's visibility on each host
// directly from its SKILL.md frontmatter and sibling agents/openai.yaml.
func visShippedVisibility(t *testing.T, skillPath string) (claudeHidden, codexHidden bool) {
	t.Helper()
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		t.Fatalf("%s: missing frontmatter", skillPath)
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		t.Fatalf("%s: unterminated frontmatter", skillPath)
	}
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &frontmatter); err != nil {
		t.Fatalf("%s: frontmatter: %v", skillPath, err)
	}
	if value, ok := frontmatter["disable-model-invocation"]; ok {
		flag, isBool := value.(bool)
		if !isBool {
			t.Fatalf("%s: disable-model-invocation = %#v; want boolean", skillPath, value)
		}
		claudeHidden = flag
	}
	codexPath := filepath.Join(filepath.Dir(skillPath), "agents", "openai.yaml")
	codexData, err := os.ReadFile(codexPath)
	if os.IsNotExist(err) {
		return claudeHidden, false
	}
	if err != nil {
		t.Fatal(err)
	}
	var codex map[string]any
	if err := yaml.Unmarshal(codexData, &codex); err != nil {
		t.Fatalf("%s: %v", codexPath, err)
	}
	policy, ok := codex["policy"]
	if !ok {
		return claudeHidden, false
	}
	policyMap, ok := policy.(map[string]any)
	if !ok {
		t.Fatalf("%s: policy = %#v; want mapping", codexPath, policy)
	}
	value, ok := policyMap["allow_implicit_invocation"]
	if !ok {
		return claudeHidden, false
	}
	allow, ok := value.(bool)
	if !ok {
		t.Fatalf("%s: allow_implicit_invocation = %#v; want boolean", codexPath, value)
	}
	return claudeHidden, !allow
}

// visShippedEntrypoints returns repository-relative SKILL.md paths under every
// registration in ../../skills-manifest.json.
func visShippedEntrypoints(t *testing.T, repo string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "skills-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Registrations map[string]string `json:"registrations"`
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Registrations) == 0 {
		t.Fatal("skills-manifest.json has no registrations")
	}
	var entrypoints []string
	for _, source := range manifest.Registrations {
		root := filepath.Join(repo, filepath.FromSlash(source))
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && entry.Name() == "SKILL.md" {
				relative, err := filepath.Rel(repo, path)
				if err != nil {
					return err
				}
				entrypoints = append(entrypoints, filepath.ToSlash(relative))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", source, err)
		}
	}
	sort.Strings(entrypoints)
	return entrypoints
}

// V6: the shipped suite hides exactly the agreed 27 skills on both hosts and
// lists every other entrypoint on both hosts. Of the skills added with
// shell-portability, principal-engineer, observability and performance are
// hidden; shell-portability stays listed.
func TestSkillVisibilityShippedContent(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..")
	hidden := map[string]bool{}
	for _, name := range []string{
		"docker-agent-config", "docker-agent-deploy", "docker-agent-run", "docker-build-strategies",
		"docker-compose-patterns", "docker-project-foundations",
		"docker-sandboxes-env", "docker-sandboxes-kits", "docker-sandboxes-lifecycle",
		"docker-sandboxes-network-credentials",
	} {
		hidden["third_party/docker-skills/"+name+"/SKILL.md"] = true
	}
	for _, name := range []string{
		"golang-samber-do", "golang-samber-hot", "golang-samber-lo", "golang-samber-mo", "golang-samber-oops",
		"golang-samber-ro", "golang-samber-slog", "golang-uber-fx", "golang-uber-dig", "golang-google-wire",
		"golang-swagger", "golang-graphql", "golang-spf13-viper",
	} {
		hidden["third_party/cc-skills-golang/skills/"+name+"/SKILL.md"] = true
	}
	hidden["third_party/db-postgres/SKILL.md"] = true
	for _, name := range []string{"principal-engineer", "observability", "performance"} {
		hidden["skills/"+name+"/SKILL.md"] = true
	}
	if len(hidden) != 27 {
		t.Fatalf("expected hidden set has %d entries; want 27", len(hidden))
	}

	entrypoints := visShippedEntrypoints(t, repo)
	seen := map[string]bool{}
	hiddenCount := 0
	for _, entrypoint := range entrypoints {
		seen[entrypoint] = true
		claude, codex := visShippedVisibility(t, filepath.Join(repo, filepath.FromSlash(entrypoint)))
		want := hidden[entrypoint]
		if claude != want || codex != want {
			t.Errorf("V6 %s: hidden on Claude=%v Codex=%v; want both %v", entrypoint, claude, codex, want)
		}
		if claude && codex {
			hiddenCount++
		}
	}
	for entrypoint := range hidden {
		if !seen[entrypoint] {
			t.Errorf("V6 expected hidden entrypoint %s is not a registered SKILL.md", entrypoint)
		}
	}
	if hiddenCount != 27 {
		t.Errorf("V6 hidden entrypoints = %d; want 27", hiddenCount)
	}
	// Named listed skills (V6 and V4) must exist and be listed on both hosts.
	for _, entrypoint := range []string{
		"third_party/archify/SKILL.md", "third_party/typesafe-ai/SKILL.md",
		"third_party/docker-skills/docker-destructive-guardrails/SKILL.md",
		"skills/code-review/SKILL.md", "skills/graphify/SKILL.md",
		"skills/task-orchestration/SKILL.md", "skills/test-strategy/SKILL.md",
		"skills/shell-portability/SKILL.md",
	} {
		if !seen[entrypoint] {
			t.Errorf("V6 listed entrypoint %s is not a registered SKILL.md", entrypoint)
			continue
		}
		claude, codex := visShippedVisibility(t, filepath.Join(repo, filepath.FromSlash(entrypoint)))
		if claude || codex {
			t.Errorf("V6 %s: hidden on Claude=%v Codex=%v; want listed on both", entrypoint, claude, codex)
		}
	}
	if _, err := ValidateSuite(repo); err != nil {
		t.Fatalf("V6 ValidateSuite(%q) error = %v; want nil", repo, err)
	}
}
