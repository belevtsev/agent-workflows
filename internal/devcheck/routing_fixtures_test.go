package devcheck

import (
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type routingCase struct {
	ID               string     `json:"id"`
	Mode             string     `json:"mode"`
	InputDir         string     `json:"input_dir"`
	TargetRead       string     `json:"target_read"`
	RequiredReads    []string   `json:"required_reads"`
	AnyReads         [][]string `json:"any_reads"`
	ForbiddenReads   []string   `json:"forbidden_reads"`
	RequiredClaims   []string   `json:"required_claims"`
	ForbiddenClaims  []string   `json:"forbidden_claims"`
	ForbiddenEffects []string   `json:"forbidden_effects"`
}

type routingFixtures struct {
	Version int           `json:"version"`
	Purpose string        `json:"purpose"`
	Cases   []routingCase `json:"cases"`
}

type routingObservation struct {
	Reads          []string `json:"reads"`
	Claims         []string `json:"claims"`
	Effects        []string `json:"effects"`
	OracleAccessed bool     `json:"oracle_accessed"`
}

type cannedRoutingRecord struct {
	ID              string             `json:"id"`
	CaseID          string             `json:"case_id"`
	ExpectedPass    bool               `json:"expected_pass"`
	ExpectedReasons []string           `json:"expected_reasons"`
	Observation     routingObservation `json:"observation"`
}

type cannedRoutingFixtures struct {
	Version int                   `json:"version"`
	Purpose string                `json:"purpose"`
	Records []cannedRoutingRecord `json:"records"`
}

func decodeRoutingFixture[T any](path string) (T, error) {
	var value T
	data, err := os.ReadFile(path)
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(data, &value, json.RejectUnknownMembers(true))
	return value, err
}

// regularRoutingFile keeps evaluator paths and links out of isolated raw inputs.
func regularRoutingFile(root, relative string) error {
	if !filepath.IsLocal(relative) || filepath.ToSlash(filepath.Clean(relative)) != relative {
		return fmt.Errorf("non-local or unclean fixture path: %s", relative)
	}
	current := root
	for component := range strings.SplitSeq(relative, "/") {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("fixture link is not permitted: %s", relative)
		}
	}
	info, err := os.Stat(current)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("fixture must be a regular file: %s", relative)
	}
	return nil
}

// Raw cases may contain project trees; evaluator artifacts and links remain
// excluded at every depth rather than only beside prompt.md.
func validateRoutingInputArtifacts(root, inputDir string) error {
	files := 0
	err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(inputDir)), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !filepath.IsLocal(relative) || filepath.ToSlash(filepath.Clean(relative)) != relative {
			return fmt.Errorf("non-local or unclean fixture path: %s", relative)
		}
		name := strings.ToLower(entry.Name())
		if strings.Contains(name, "oracle") || strings.Contains(name, "canned") || strings.Contains(name, "expected") {
			return fmt.Errorf("evaluator artifact in raw input: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if err := regularRoutingFile(root, relative); err != nil {
			return err
		}
		files++
		return nil
	})
	if err != nil {
		return err
	}
	if files < 2 {
		return fmt.Errorf("missing raw artifacts: %s", inputDir)
	}
	return nil
}

func validateRoutingFixtures(source, root string, fixtures routingFixtures) error {
	if fixtures.Version != 1 || fixtures.Purpose == "" || len(fixtures.Cases) == 0 {
		return fmt.Errorf("invalid routing fixture header")
	}
	ids := map[string]bool{}
	inputs := map[string]bool{}
	var names []string
	for _, item := range fixtures.Cases {
		reads := append(slices.Clone(item.RequiredReads), item.ForbiddenReads...)
		reads = append(reads, item.TargetRead)
		for _, group := range item.AnyReads {
			reads = append(reads, group...)
		}
		for _, read := range reads {
			parts := strings.Split(read, "/")
			for index, part := range parts {
				if part == "skills" && index+1 < len(parts) {
					names = append(names, parts[index+1])
				}
			}
		}
	}
	for _, item := range fixtures.Cases {
		if item.ID == "" || ids[item.ID] || inputs[item.InputDir] {
			return fmt.Errorf("empty or duplicate case/input: %s", item.ID)
		}
		ids[item.ID], inputs[item.InputDir] = true, true
		if !slices.Contains([]string{"explicit", "implicit", "adjacent-negative"}, item.Mode) {
			return fmt.Errorf("invalid discovery mode: %s", item.ID)
		}
		if item.InputDir != "inputs/"+item.ID || (item.Mode != "adjacent-negative" && len(item.RequiredReads) == 0) || len(item.RequiredClaims) == 0 || len(item.ForbiddenEffects) == 0 {
			return fmt.Errorf("incomplete or misplaced case: %s", item.ID)
		}
		if item.Mode == "adjacent-negative" && !slices.Contains(item.ForbiddenReads, item.TargetRead) {
			return fmt.Errorf("adjacent case must exclude its target: %s", item.ID)
		}
		if item.Mode != "adjacent-negative" && !slices.Contains(item.RequiredReads, item.TargetRead) {
			return fmt.Errorf("positive case must require its target: %s", item.ID)
		}
		reads := append(slices.Clone(item.RequiredReads), item.ForbiddenReads...)
		reads = append(reads, item.TargetRead)
		for _, group := range item.AnyReads {
			if len(group) == 0 {
				return fmt.Errorf("empty alternative read group: %s", item.ID)
			}
			reads = append(reads, group...)
			for _, read := range group {
				if slices.Contains(item.ForbiddenReads, read) {
					return fmt.Errorf("contradictory read alternatives: %s", item.ID)
				}
			}
		}
		for _, read := range reads {
			if !strings.HasPrefix(read, "skills/") && !strings.HasPrefix(read, "third_party/") {
				return fmt.Errorf("read outside skill resources: %s", read)
			}
			if err := regularRoutingFile(source, read); err != nil {
				return fmt.Errorf("case %s read: %w", item.ID, err)
			}
		}
		for _, read := range item.RequiredReads {
			if slices.Contains(item.ForbiddenReads, read) {
				return fmt.Errorf("contradictory required read: %s", item.ID)
			}
		}
		for _, claim := range item.RequiredClaims {
			if claim == "" || slices.Contains(item.ForbiddenClaims, claim) {
				return fmt.Errorf("empty or contradictory claim: %s", item.ID)
			}
		}
		promptPath := item.InputDir + "/prompt.md"
		if err := regularRoutingFile(root, promptPath); err != nil {
			return fmt.Errorf("case %s prompt: %w", item.ID, err)
		}
		prompt, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(promptPath)))
		if err != nil || len(strings.TrimSpace(string(prompt))) == 0 {
			return fmt.Errorf("missing prompt: %s", item.ID)
		}
		if item.Mode == "explicit" {
			name := filepath.Base(filepath.Dir(item.TargetRead))
			if !strings.Contains(string(prompt), "$"+name) {
				return fmt.Errorf("explicit case lacks its invocation: %s", item.ID)
			}
		} else {
			for _, name := range names {
				if strings.Contains(strings.ToLower(string(prompt)), strings.ToLower(name)) {
					return fmt.Errorf("implicit/adjacent prompt names a skill: %s", item.ID)
				}
			}
		}
		if err := validateRoutingInputArtifacts(root, item.InputDir); err != nil {
			return fmt.Errorf("case %s artifact: %w", item.ID, err)
		}
	}
	return nil
}

// scoreRouting compares independently judged claims and actual file reads. It
// does not infer semantic correctness from output wording or run a hosted model.
func scoreRouting(item routingCase, observation routingObservation) []string {
	var reasons []string
	if observation.OracleAccessed {
		reasons = append(reasons, "oracle access invalidates evaluation")
	}
	for _, read := range item.RequiredReads {
		if !slices.Contains(observation.Reads, read) {
			reasons = append(reasons, "required read missing: "+read)
		}
	}
	for _, group := range item.AnyReads {
		if !slices.ContainsFunc(group, func(read string) bool { return slices.Contains(observation.Reads, read) }) {
			reasons = append(reasons, "alternative read missing: "+strings.Join(group, " | "))
		}
	}
	for _, read := range item.ForbiddenReads {
		if slices.Contains(observation.Reads, read) {
			reasons = append(reasons, "forbidden read: "+read)
		}
	}
	for _, claim := range item.RequiredClaims {
		if !slices.Contains(observation.Claims, claim) {
			reasons = append(reasons, "required claim missing: "+claim)
		}
	}
	for _, claim := range item.ForbiddenClaims {
		if slices.Contains(observation.Claims, claim) {
			reasons = append(reasons, "forbidden claim: "+claim)
		}
	}
	for _, effect := range item.ForbiddenEffects {
		if slices.Contains(observation.Effects, effect) {
			reasons = append(reasons, "forbidden effect: "+effect)
		}
	}
	return reasons
}

func TestSkillRoutingFixtureStructure(t *testing.T) {
	source := filepath.Join("..", "..")
	root := filepath.Join("testdata", "skill-routing")
	fixtures, err := decodeRoutingFixture[routingFixtures](filepath.Join(root, "oracles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRoutingFixtures(source, root, fixtures); err != nil {
		t.Fatal(err)
	}
	modes := map[string]int{}
	for _, item := range fixtures.Cases {
		modes[item.Mode]++
	}
	if len(fixtures.Cases) != 12 || modes["explicit"] != 3 || modes["implicit"] != 5 || modes["adjacent-negative"] != 4 {
		t.Fatalf("routing coverage differs: %v", modes)
	}
	legacy, err := decodeRoutingFixture[map[string]any](filepath.Join("testdata", "backend-skills", "scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	scenarios, ok := legacy["scenarios"].([]any)
	if !ok || len(scenarios) != 15 {
		t.Fatal("historical fifteen-scenario corpus is missing")
	}
}

func TestSkillRoutingCannedScoring(t *testing.T) {
	root := filepath.Join("testdata", "skill-routing")
	fixtures, err := decodeRoutingFixture[routingFixtures](filepath.Join(root, "oracles.json"))
	if err != nil {
		t.Fatal(err)
	}
	canned, err := decodeRoutingFixture[cannedRoutingFixtures](filepath.Join(root, "canned.json"))
	if err != nil {
		t.Fatal(err)
	}
	if canned.Version != 1 || canned.Purpose == "" || len(canned.Records) == 0 {
		t.Fatal("invalid canned record header")
	}
	cases := map[string]routingCase{}
	for _, item := range fixtures.Cases {
		cases[item.ID] = item
	}
	ids := map[string]bool{}
	passes, failures := map[string]bool{}, 0
	for _, record := range canned.Records {
		if record.ID == "" || ids[record.ID] {
			t.Fatalf("duplicate or empty canned record: %s", record.ID)
		}
		ids[record.ID] = true
		item, ok := cases[record.CaseID]
		if !ok {
			t.Fatalf("canned record names unknown case: %s", record.CaseID)
		}
		t.Run(record.ID, func(t *testing.T) {
			reasons := scoreRouting(item, record.Observation)
			if (len(reasons) == 0) != record.ExpectedPass || !slices.Equal(reasons, record.ExpectedReasons) {
				t.Fatalf("scoring differs: pass=%v reasons=%v, want pass=%v reasons=%v", len(reasons) == 0, reasons, record.ExpectedPass, record.ExpectedReasons)
			}
		})
		if record.ExpectedPass {
			passes[record.CaseID] = true
		} else {
			failures++
		}
	}
	if len(passes) != len(cases) || failures < 5 {
		t.Fatalf("canned coverage is incomplete: cases=%d passes=%d failures=%d", len(cases), len(passes), failures)
	}
}

func TestSkillRoutingRejectsUnsafeOrBiasedFixtures(t *testing.T) {
	root := filepath.Join("testdata", "skill-routing")
	fixtures, err := decodeRoutingFixture[routingFixtures](filepath.Join(root, "oracles.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*routingFixtures){
		"duplicate case": func(value *routingFixtures) { value.Cases = append(value.Cases, value.Cases[0]) },
		"oracle input":   func(value *routingFixtures) { value.Cases[0].InputDir = "oracles.json" },
		"escaping read":  func(value *routingFixtures) { value.Cases[0].RequiredReads[0] = "skills/../../oracles.json" },
		"unavailable reference": func(value *routingFixtures) {
			value.Cases[0].RequiredReads[0] = "skills/backend-security-review/missing.md"
		},
		"contradictory route": func(value *routingFixtures) {
			value.Cases[0].ForbiddenReads = slices.Clone(value.Cases[0].RequiredReads)
		},
		"missing negative exclusion": func(value *routingFixtures) { value.Cases[2].ForbiddenReads = nil },
		"implicit skill hint":        func(value *routingFixtures) { value.Cases[0].Mode = "implicit" },
		"empty alternative group":    func(value *routingFixtures) { value.Cases[0].AnyReads = [][]string{{}} },
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(fixtures)
			if err != nil {
				t.Fatal(err)
			}
			var changed routingFixtures
			if err := json.Unmarshal(encoded, &changed); err != nil {
				t.Fatal(err)
			}
			mutate(&changed)
			if err := validateRoutingFixtures(filepath.Join("..", ".."), root, changed); err == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
	for _, data := range []string{`{"version":1,"version":2}`, `{"version":1,"unknown":true}`} {
		var changed routingFixtures
		if err := json.Unmarshal([]byte(data), &changed, json.RejectUnknownMembers(true)); err == nil {
			t.Fatal("ambiguous or unknown fixture JSON accepted")
		}
	}
	linkedRoot := t.TempDir()
	if err := os.Symlink(root, filepath.Join(linkedRoot, "inputs")); err != nil {
		t.Fatal(err)
	}
	if err := regularRoutingFile(linkedRoot, "inputs/oracles.json"); err == nil {
		t.Fatal("fixture symlink accepted")
	}
}
