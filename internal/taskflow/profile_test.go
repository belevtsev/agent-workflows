package taskflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// documentedProfiles reads the profiles table of docs/operations.md
// ("Profiles") into the profiles it documents, and the name of the one marked
// "(default)".
func documentedProfiles(t *testing.T) (map[string]taskflow.Profile, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "operations.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "\n### Profiles\n")
	if !ok {
		t.Fatal(`docs/operations.md has no "### Profiles" section`)
	}
	section, _, _ = strings.Cut(section, "\n### ")
	backticked := regexp.MustCompile("`([a-z-]+)`")
	capped := regexp.MustCompile("^`([a-z-]+)` ([0-9]+)$")
	stopAt := regexp.MustCompile(`stop at ([0-9]+)%`)
	profiles := map[string]taskflow.Profile{}
	defaultName := ""
	var share float64
	for line := range strings.Lines(section) {
		if !strings.HasPrefix(line, "| ") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) != 6 {
			t.Fatalf("profiles table row has %d cells, want 6: %q", len(cells), line)
		}
		if cells[0] == "Profile" {
			m := stopAt.FindStringSubmatch(cells[4])
			if m == nil {
				t.Fatalf("budget header %q does not say where the task stops", cells[4])
			}
			percent, _ := strconv.Atoi(m[1])
			share = float64(percent) / 100
			continue
		}
		if strings.HasPrefix(cells[0], "---") {
			continue
		}
		name := backticked.FindStringSubmatch(cells[0])
		if name == nil {
			t.Fatalf("profile cell %q names no profile", cells[0])
		}
		if strings.Contains(cells[0], "(default)") {
			defaultName = name[1]
		}
		profile := taskflow.Profile{Name: name[1], Rounds: map[string]int{}}
		for _, m := range backticked.FindAllStringSubmatch(cells[1], -1) {
			profile.Stages = append(profile.Stages, m[1])
		}
		for entry := range strings.SplitSeq(cells[2], ", ") {
			m := capped.FindStringSubmatch(entry)
			if m == nil {
				t.Fatalf("round cap %q is not `stage` N", entry)
			}
			profile.Rounds[m[1]], _ = strconv.Atoi(m[2])
		}
		if profile.FixRounds, err = strconv.Atoi(cells[3]); err != nil {
			t.Fatalf("fix rounds %q: %v", cells[3], err)
		}
		if profile.Budget.MaxUSD, err = strconv.ParseFloat(cells[4], 64); err != nil {
			t.Fatalf("budget %q: %v", cells[4], err)
		}
		profiles[profile.Name] = profile
	}
	if share == 0 {
		t.Fatal("profiles table has no header row")
	}
	for name, profile := range profiles {
		profile.Budget.StopAt = share
		profiles[name] = profile
	}
	return profiles, defaultName
}

// Oracle: the profiles table in docs/operations.md "Profiles". Every built-in
// profile equals its documented row, field by field, the documented set is
// the built-in set, and the row marked "(default)" is the profile a plan
// without one gets. A change to either side alone fails here.
func TestBuiltInProfilesEqualTheDocumentedTable(t *testing.T) {
	documented, defaultName := documentedProfiles(t)
	names := make([]string, 0, len(documented))
	for name := range documented {
		names = append(names, name)
	}
	slices.Sort(names)
	if want := []string{"lite", "standard", "strict"}; !slices.Equal(names, want) {
		t.Fatalf("documented profiles %q, want %q", names, want)
	}
	if defaultName != taskflow.DefaultProfile {
		t.Errorf("documented default %q, built-in default %q", defaultName, taskflow.DefaultProfile)
	}
	for _, name := range names {
		got, ok := taskflow.BuiltInProfile(name)
		if !ok {
			t.Errorf("profile %s is documented but not built in", name)
			continue
		}
		if !reflect.DeepEqual(got, documented[name]) {
			t.Errorf("built-in profile %s = %+v, documented %+v", name, got, documented[name])
		}
	}
	if _, ok := taskflow.BuiltInProfile("custom"); ok {
		t.Error("an undocumented profile is built in")
	}
}

// Oracle: docs/operations.md "Profiles". A plan without a profile records
// lite; strict records the spec and adversarial stages; a plan budget lowers
// the profile's; a plan budget above the profile's, or an unknown profile, is
// refused as invalid input with no task created.
func TestStartRecordsThePlanProfile(t *testing.T) {
	for _, c := range []struct {
		name    string
		profile string
		budget  *taskflow.Budget
		want    func(t *testing.T, p *taskflow.Profile)
		refused string
	}{
		{name: "no profile records lite", want: func(t *testing.T, p *taskflow.Profile) {
			lite, _ := taskflow.BuiltInProfile("lite")
			if p == nil || !reflect.DeepEqual(*p, lite) {
				t.Errorf("recorded %+v, want lite %+v", p, lite)
			}
		}},
		{name: "strict records the spec and adversarial stages", profile: "strict", want: func(t *testing.T, p *taskflow.Profile) {
			if p == nil || p.Name != "strict" || !slices.Contains(p.Stages, "spec") || !slices.Contains(p.Stages, "spec-review") || !slices.Contains(p.Stages, "adversarial") || p.Budget.MaxUSD != 100 {
				t.Errorf("recorded %+v, want strict with spec, spec-review and adversarial and 100 USD", p)
			}
		}},
		{name: "lower plan budget applies", profile: "standard", budget: &taskflow.Budget{MaxUSD: 12.5}, want: func(t *testing.T, p *taskflow.Profile) {
			if p == nil || p.Name != "standard" || p.Budget != (taskflow.Budget{MaxUSD: 12.5, StopAt: 0.8}) {
				t.Errorf("recorded %+v, want standard with 12.5 USD stopping at 0.8", p)
			}
		}},
		{name: "plan budget above the profile's", budget: &taskflow.Budget{MaxUSD: 16}, refused: "plan budget max_usd 16 exceeds profile lite's 15; a plan budget may only lower the profile's"},
		{name: "plan stop_at above the profile's", profile: "strict", budget: &taskflow.Budget{MaxUSD: 50, StopAt: 0.9}, refused: "plan budget stop_at 0.9 exceeds profile strict's 0.8; a plan budget may only lower the profile's"},
		{name: "unknown profile", profile: "custom", refused: `plan profile "custom" is not lite, standard or strict`},
		{name: "zero budget", budget: &taskflow.Budget{}, refused: "plan budget: max_usd must be a positive amount"},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, store := spRepo(t, map[string]string{"a.txt": "a\n"})
			plan := spPlan("go", "version")
			plan.Profile, plan.Budget = c.profile, c.budget
			record, err := taskflow.StartWithPacks(context.Background(), repo, store, plan, nil, taskflow.SelectionDefault, nil)
			if c.refused != "" {
				if err == nil || err.Error() != c.refused || !errors.Is(err, taskflow.ErrInvalidInput) {
					t.Fatalf("start = %v, want ErrInvalidInput %q", err, c.refused)
				}
				if entries, _ := os.ReadDir(store); len(entries) != 0 {
					t.Fatalf("a refused start left %d entries in the store", len(entries))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			c.want(t, record.Profile)
			loaded, err := taskflow.Load(record.Directory)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(loaded.Profile, record.Profile) {
				t.Errorf("loaded profile %+v, started with %+v", loaded.Profile, record.Profile)
			}
		})
	}
}

// rewriteRevision replaces member of the stored revision file with value,
// or removes it when value is nil or null.
func rewriteRevision(t *testing.T, file, member string, value json.RawMessage) {
	t.Helper()
	data, err := os.ReadFile(file)
	var members map[string]json.RawMessage
	if err != nil || json.Unmarshal(data, &members) != nil {
		t.Fatalf("cannot decode %s: %v", file, err)
	}
	if value == nil || string(value) == "null" {
		delete(members, member)
	} else {
		members[member] = value
	}
	out, err := json.Marshal(members)
	if err != nil || os.WriteFile(file, out, 0o600) != nil {
		t.Fatalf("cannot rewrite %s: %v", file, err)
	}
}

// Oracle: the profile is fixed at start like the packs. Load refuses a later
// revision whose profile differs from revision 1's, raised, renamed or
// dropped, with exactly "immutable task profile changed"; the control row
// re-encodes it unchanged and must load, so the edit itself is not what is
// refused.
func TestLaterRevisionThatChangesTheProfileFailsToLoad(t *testing.T) {
	const refused = "immutable task profile changed"
	for _, c := range []struct {
		name  string
		value json.RawMessage
	}{
		{"control: profile unchanged", nil},
		{"budget raised", json.RawMessage(`{"name":"lite","stages":["tests","implementation","test-review","code-review"],"rounds":{"tests":2},"fix_rounds":2,"budget":{"max_usd":1000,"stop_at":0.8}}`)},
		{"round cap raised", json.RawMessage(`{"name":"lite","stages":["tests","implementation","test-review","code-review"],"rounds":{"tests":9},"fix_rounds":2,"budget":{"max_usd":15,"stop_at":0.8}}`)},
		{"profile dropped", json.RawMessage(`null`)},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			repo, store := spRepo(t, map[string]string{"a.txt": "a\n"})
			record, err := taskflow.StartWithPacks(ctx, repo, store, spPlan("go", "version"), nil, taskflow.SelectionDefault, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := taskflow.PrepareContext(ctx, record.Directory, "before"); err != nil {
				t.Fatalf("precondition: write a second revision: %v", err)
			}
			second := filepath.Join(record.Directory, "record-000002.json")
			value := c.value
			if value == nil {
				value, _ = json.Marshal(record.Profile)
			}
			rewriteRevision(t, second, "profile", value)
			_, err = taskflow.Load(record.Directory)
			if c.value == nil {
				if err != nil {
					t.Fatalf("control: re-encoding the profile unchanged gives %v", err)
				}
				return
			}
			if err == nil || err.Error() != refused {
				t.Fatalf("Load = %v, want exactly %q", err, refused)
			}
		})
	}
}

// Oracle: a record started before profiles existed has no profile and loads
// with none, so no limits apply to it; a stored profile that disagrees with
// its plan, or a plan profile with no stored profile, is damaged and refused.
func TestStoredProfileAgreesWithItsPlan(t *testing.T) {
	for _, c := range []struct {
		name    string
		edit    func(t *testing.T, file string)
		refused string
	}{
		{"record without a profile loads", func(t *testing.T, file string) { rewriteRevision(t, file, "profile", nil) }, ""},
		{"profile name differs from the plan", func(t *testing.T, file string) {
			rewriteRevision(t, file, "profile", json.RawMessage(`{"name":"strict","stages":["spec"],"rounds":{},"fix_rounds":2,"budget":{"max_usd":100,"stop_at":0.8}}`))
		}, "invalid stored profile: its name differs from the plan's profile"},
		{"unknown stage", func(t *testing.T, file string) {
			rewriteRevision(t, file, "profile", json.RawMessage(`{"name":"lite","stages":["deploy"],"rounds":{},"fix_rounds":2,"budget":{"max_usd":15,"stop_at":0.8}}`))
		}, "invalid stored profile stages"},
		{"plan names a profile the record lacks", func(t *testing.T, file string) {
			data, _ := os.ReadFile(file)
			var members map[string]json.RawMessage
			_ = json.Unmarshal(data, &members)
			var plan map[string]json.RawMessage
			_ = json.Unmarshal(members["plan"], &plan)
			plan["profile"] = json.RawMessage(`"lite"`)
			encoded, _ := json.Marshal(plan)
			rewriteRevision(t, file, "plan", encoded)
			rewriteRevision(t, file, "profile", nil)
		}, "invalid stored profile: the plan names a profile or budget but the record has no profile"},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, store := spRepo(t, map[string]string{"a.txt": "a\n"})
			record, err := taskflow.StartWithPacks(context.Background(), repo, store, spPlan("go", "version"), nil, taskflow.SelectionDefault, nil)
			if err != nil {
				t.Fatal(err)
			}
			c.edit(t, filepath.Join(record.Directory, "record-000001.json"))
			loaded, err := taskflow.Load(record.Directory)
			if c.refused == "" {
				if err != nil || loaded.Profile != nil {
					t.Fatalf("Load = %+v, %v; want a record with no profile", loaded.Profile, err)
				}
				return
			}
			if err == nil || err.Error() != c.refused {
				t.Fatalf("Load = %v, want exactly %q", err, c.refused)
			}
		})
	}
}
