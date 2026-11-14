package roles_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// Requirement "Role set", scenario "The table".
func TestTheTable(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	result := repo.CW("roles")
	if result.Code != 0 {
		t.Errorf("cw roles exited %d, want 0:\n%s", result.Code, result.Output())
	}
	rows := map[string]int{}
	total := 0
	for _, line := range strings.Split(result.Stdout, "\n") {
		first, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		first = strings.TrimSuffix(first, ",")
		if contains(roleIDs, first) {
			rows[first]++
			total++
		}
	}
	for _, id := range roleIDs {
		if rows[id] != 1 {
			t.Errorf("cw roles prints %d rows for %s, want 1", rows[id], id)
		}
	}
	if total != 7 {
		t.Errorf("cw roles prints %d role rows, want 7:\n%s", total, result.Stdout)
	}
}

// The table fits a terminal: below the row of a role stand its job and what
// it may write, and no line is wider than 120 columns (U15).
func TestTheTableFitsATerminal(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	lines := strings.Split(repo.CW("roles").Stdout, "\n")
	for _, line := range lines {
		if utf8.RuneCountInString(line) > 120 {
			t.Errorf("a line of cw roles has %d columns, want at most 120:\n%s", utf8.RuneCountInString(line), line)
		}
	}
	var out struct {
		Roles []struct{ ID, Job, Writes string } `json:"roles"`
	}
	if err := json.Unmarshal([]byte(repo.CW("roles", "--json").Stdout), &out); err != nil || len(out.Roles) != 7 {
		t.Fatalf("cw roles --json: %v, %d roles", err, len(out.Roles))
	}
	for _, role := range out.Roles {
		found := false
		for i, line := range lines {
			if first, _, _ := strings.Cut(line, " "); first == role.ID && i+2 < len(lines) {
				found = lines[i+1] == "    "+role.Job && lines[i+2] == "    May write: "+role.Writes+"."
			}
		}
		if !found {
			t.Errorf("the row of %s is not followed by its job and by what it may write:\n%s", role.ID, strings.Join(lines, "\n"))
		}
	}
}

// Requirement "Role set", scenario "The table as JSON".
func TestTheTableAsJSON(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	result := repo.CW("roles", "--json")
	if result.Code != 0 {
		t.Fatalf("cw roles --json exited %d:\n%s", result.Code, result.Output())
	}
	var out struct {
		Roles []map[string]json.RawMessage `json:"roles"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &out); err != nil {
		t.Fatalf("cw roles --json did not print one JSON object: %v\n%s", err, result.Stdout)
	}
	var architect map[string]json.RawMessage
	for _, entry := range out.Roles {
		var id string
		if err := json.Unmarshal(entry["id"], &id); err == nil && id == "architect" {
			architect = entry
		}
	}
	if architect == nil {
		t.Fatalf("roles holds no entry with id architect:\n%s", result.Stdout)
	}
	for _, key := range []string{"job", "writes"} {
		var value string
		if err := json.Unmarshal(architect[key], &value); err != nil || value == "" {
			t.Errorf("the entry of architect holds no %s: %s", key, architect[key])
		}
	}
	var models map[string]map[string]json.RawMessage
	if err := json.Unmarshal(architect["models"], &models); err != nil {
		t.Fatalf("the entry of architect holds no models object: %v: %s", err, architect["models"])
	}
	for _, host := range []string{"claude", "codex"} {
		entry, ok := models[host]
		if !ok {
			t.Errorf("models of architect holds no entry for %s: %s", host, architect["models"])
			continue
		}
		for _, key := range []string{"model", "effort"} {
			var value string
			if err := json.Unmarshal(entry[key], &value); err != nil || value == "" {
				t.Errorf("models.%s of architect holds no %s: %s", host, key, architect["models"])
			}
		}
	}
}

// Requirement "Default models", scenario "The defaults".
func TestTheDefaults(t *testing.T) {
	t.Parallel()
	roles := rolesJSON(t, demo(t))
	if got := roles["lookup"].Models["claude"].Model; got != "haiku" {
		t.Errorf("the claude model of lookup is %q, want haiku", got)
	}
	if got := roles["git"].Models["claude"].Model; got != "sonnet" {
		t.Errorf("the claude model of git is %q, want sonnet", got)
	}
	for _, id := range []string{"architect", "reviewer"} {
		if got := roles[id].Models["claude"]; got != (hostModel{Model: "opus", Effort: "xhigh"}) {
			t.Errorf("the claude model of %s is %+v, want opus with effort xhigh", id, got)
		}
	}
	// A review runs at the effort of the work it reviews.
	if got, want := roles["reviewer"].Models["codex"], roles["architect"].Models["codex"]; got != want {
		t.Errorf("the codex model of reviewer is %+v, want that of architect, %+v", got, want)
	}
}

// Requirement "Roles file", scenario "A repository changes one model".
func TestARepositoryChangesOneModel(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	before := rolesJSON(t, repo)
	repo.Write("cw-roles.toml", rolesTOML+"\n[claude.developer]\nmodel = \"sonnet\"\n")
	after := rolesJSON(t, repo)

	if got := after["developer"].Models["claude"]; got != (hostModel{Model: "sonnet", Effort: "high"}) {
		t.Errorf("the claude model of developer is %+v, want sonnet with effort high", got)
	}
	if got, want := after["developer"].Models["codex"], before["developer"].Models["codex"]; got != want {
		t.Errorf("the codex model of developer changed from %+v to %+v", want, got)
	}
	for _, id := range roleIDs {
		if id == "developer" {
			continue
		}
		for _, host := range []string{"claude", "codex"} {
			if got, want := after[id].Models[host], before[id].Models[host]; got != want {
				t.Errorf("the %s model of %s changed from %+v to %+v", host, id, want, got)
			}
		}
	}
}

// Requirement "Roles file", scenario "An unknown role is refused".
func TestAnUnknownRoleIsRefused(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	// rolesTOML has four lines; [claude.tester] is the fifth.
	repo.Write("cw-roles.toml", rolesTOML+"[claude.tester]\nmodel = \"opus\"\n")
	result := repo.CW("roles")
	if result.Code != 1 {
		t.Errorf("cw roles exited %d, want 1", result.Code)
	}
	out := result.Output()
	if !strings.Contains(out, "cw-roles.toml") {
		t.Errorf("the output does not name cw-roles.toml:\n%s", out)
	}
	if !lineWith(out, "cw-roles.toml", "5") || !namesLine(out, 5) {
		t.Errorf("the output does not name line 5, the line of [claude.tester]:\n%s", out)
	}
	if !lineWith(out, "tester", "unknown") {
		t.Errorf("the output does not name the role tester as unknown:\n%s", out)
	}
}

// namesLine says whether text names a line number, as `file:5` or
// `line 5`.
func namesLine(text string, n int) bool {
	number := strconv.Itoa(n)
	return strings.Contains(text, ":"+number) || strings.Contains(text, "line "+number)
}

// Requirement "Roles file", scenario "A model name is not judged".
func TestAModelNameIsNotJudged(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	repo.Write("cw-roles.toml", rolesTOML+"\n[claude.git]\nmodel = \"no-such-model\"\n")
	result := repo.CW("roles")
	if result.Code != 0 {
		t.Errorf("cw roles exited %d, want 0:\n%s", result.Code, result.Output())
	}
	found := false
	for _, line := range strings.Split(result.Stdout, "\n") {
		first, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		if first == "git" && strings.Contains(line, "no-such-model") {
			found = true
		}
	}
	if !found {
		t.Errorf("cw roles does not print no-such-model for git:\n%s", result.Stdout)
	}
}

// Requirement "Text budget", scenario "The sizes are printed".
func TestTheSizesArePrinted(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	roles := rolesJSON(t, repo)
	for _, id := range roleIDs {
		text := repo.CW("roles", "text", id)
		if text.Code != 0 {
			t.Errorf("cw roles text %s exited %d:\n%s", id, text.Code, text.Output())
			continue
		}
		// The design counts each Unicode code point as one character.
		want := utf8.RuneCountInString(text.Stdout)
		got := roles[id].TextChars
		switch {
		case got == nil:
			t.Errorf("the entry of %s holds no text_chars", id)
		case *got != want:
			t.Errorf("text_chars of %s is %d, but cw roles text %s prints %d characters", id, *got, id, want)
		}
	}
}
