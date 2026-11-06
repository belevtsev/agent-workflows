package cli

import (
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/credentials"
	"github.com/belevtsev/agent-workflows/internal/workflow"
)

// Contract adviser-switch, revision 3: C1-C4 and E1-E5.

// Keys rows with view.Advisers = [subq_snap, openai_decisions] (E2).
const (
	adviserRowSubQToggle   = 3
	adviserRowOpenAIToggle = 5
	adviserRowContinue     = 7
	adviserPresetBalanced  = 2
	adviserPresetAdvanced  = 3
)

// adviserDriver writes the same answers for the TUI and the plain wizard.
type adviserDriver struct {
	name  string
	plain bool
}

var adviserDrivers = []adviserDriver{{"tui", false}, {"plain", true}}

// enter accepts the current default or cursor row.
func (d adviserDriver) enter() string {
	if d.plain {
		return "\n"
	}
	return "\r"
}

// toggle picks checkbox row n, which toggles it.
func (d adviserDriver) toggle(n int) string {
	if d.plain {
		return strconv.Itoa(n) + "\n"
	}
	return strconv.Itoa(n)
}

// choose picks row n and activates it.
func (d adviserDriver) choose(n int) string { return strconv.Itoa(n) + d.enter() }

func (d adviserDriver) back() string {
	if d.plain {
		return "b\n"
	}
	return wtLeft
}

// adviserFixture is a fresh (Hosts, Models, Keys, Review) wizard whose target
// release enables SubQ and OpenAI, with saved disabling those IDs.
func adviserFixture(t *testing.T, d adviserDriver, saved ...string) *wtFixture {
	t.Helper()
	f := newWTFixture(t, false)
	f.plain = d.plain
	f.view.Advisers = []string{"subq_snap", "openai_decisions"}
	if len(saved) > 0 {
		f.view.Preferences.Advisers = &workflow.AdviserPreferences{Disabled: slices.Clone(saved)}
	}
	return f
}

func adviserSameSet(got, want []string) bool {
	a, b := slices.Clone(got), slices.Clone(want)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// adviserRequireSelection checks the adviser choice the wizard returned: in
// Preferences.Advisers when it returned explicit Preferences (E3), otherwise
// in a non-nil ConfigurationSelection.Advisers (C2).
func adviserRequireSelection(t *testing.T, sel workflow.ConfigurationSelection, want ...string) {
	t.Helper()
	if sel.Preferences != nil {
		if sel.Advisers != nil {
			t.Fatalf("selection sets both Preferences and Advisers (%+v)", *sel.Advisers)
		}
		got := sel.Preferences.Advisers
		if len(want) == 0 {
			if got != nil && len(got.Disabled) != 0 {
				t.Fatalf("Preferences.Advisers disables %q, want none", got.Disabled)
			}
			return
		}
		if got == nil || !adviserSameSet(got.Disabled, want) {
			t.Fatalf("Preferences.Advisers = %+v, want disabled %q", got, want)
		}
		return
	}
	if sel.Advisers == nil {
		t.Fatalf("selection carries no Advisers; want disabled %q", want)
	}
	if !adviserSameSet(sel.Advisers.Disabled, want) {
		t.Fatalf("selection Advisers disables %q, want %q", sel.Advisers.Disabled, want)
	}
}

// adviserRequireSaved checks what applying the selection would save.
func adviserRequireSaved(t *testing.T, f *wtFixture, sel workflow.ConfigurationSelection, want ...string) {
	t.Helper()
	prefs, err := workflow.PreferencesForSelection(f.view, sel)
	if err != nil {
		t.Fatalf("PreferencesForSelection: %v", err)
	}
	if len(want) == 0 {
		if prefs.Advisers != nil {
			t.Fatalf("saved advisers = %+v, want nil", *prefs.Advisers)
		}
		return
	}
	if prefs.Advisers == nil || !adviserSameSet(prefs.Advisers.Disabled, want) {
		t.Fatalf("saved advisers = %+v, want disabled %q", prefs.Advisers, want)
	}
}

func adviserLines(text, substr string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, substr) {
			lines = append(lines, line)
		}
	}
	return lines
}

// adviserReview is the Review step text: the last TUI frame, or the whole
// plain transcript (no earlier plain step prints an "Advisers" line).
func adviserReview(d adviserDriver, r wtResult) string {
	if d.plain {
		return wtStrip(r.out)
	}
	return wtStrip(wtLastFrame(r.out))
}

func adviserFinish(r wtResult) string {
	return wtStrip(r.wizard.finishMessage(
		workflow.Object{"registrations": []string{"archify", "graphify"}},
		workflow.Object{"status": "saved", "checks": map[string]string{
			"jev": "verified", "subq_snap": "unavailable", "openai_decisions": "missing",
		}},
	))
}

// adviserRequireAdviserLine requires exactly one line naming "Advisers" that
// contains every want (G6: matched by content, not spacing).
func adviserRequireAdviserLine(t *testing.T, what, text string, wants ...string) string {
	t.Helper()
	lines := adviserLines(text, "Advisers")
	if len(lines) != 1 {
		t.Fatalf("%s has %d Advisers lines %q, want 1 in:\n%s", what, len(lines), lines, text)
	}
	flat := wtFlat(lines[0])
	for _, want := range wants {
		if !strings.Contains(flat, want) {
			t.Fatalf("%s Advisers line %q lacks %q", what, flat, want)
		}
	}
	return flat
}

func adviserRequireNoAdviserLine(t *testing.T, what, text string) {
	t.Helper()
	if lines := adviserLines(text, "Advisers"); len(lines) != 0 {
		t.Fatalf("%s has Advisers lines %q with no adviser disabled", what, lines)
	}
}

var adviserPlainRow = regexp.MustCompile(`^\s*(?:\*\s+)?(\d{1,2})\s{2,}(.*?)\s*$`)

// adviserPlainRows returns the last numbered list of a plain transcript.
func adviserPlainRows(out string) []string {
	var rows []string
	for _, line := range strings.Split(wtStrip(out), "\n") {
		m := adviserPlainRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[1] == "1" {
			rows = nil
		}
		if m[1] != strconv.Itoa(len(rows)+1) {
			continue
		}
		rows = append(rows, m[2])
	}
	return rows
}

type adviserRowWant struct {
	has    []string
	hasNot []string
}

func adviserRequirePlainRows(t *testing.T, out string, wants []adviserRowWant) {
	t.Helper()
	rows := adviserPlainRows(out)
	if len(rows) != len(wants) {
		t.Fatalf("Keys rows %q, want %d rows", rows, len(wants))
	}
	for i, want := range wants {
		for _, s := range want.has {
			if !strings.Contains(rows[i], s) {
				t.Fatalf("Keys row %d %q lacks %q (rows %q)", i+1, rows[i], s, rows)
			}
		}
		for _, s := range want.hasNot {
			if strings.Contains(rows[i], s) {
				t.Fatalf("Keys row %d %q contains %q (rows %q)", i+1, rows[i], s, rows)
			}
		}
	}
}

// adviserInOrder requires wants to appear in text in order, each after the
// previous one (repeated labels allowed).
func adviserInOrder(t *testing.T, text string, wants ...string) {
	t.Helper()
	rest := text
	for _, want := range wants {
		i := strings.Index(rest, want)
		if i < 0 {
			t.Fatalf("%q missing or out of order in:\n%s", want, text)
		}
		rest = rest[i+len(want):]
	}
}

// adviserStopAtKeys runs Hosts and Models with their defaults and ends the
// input on the Keys step.
func adviserStopAtKeys(t *testing.T, f *wtFixture, d adviserDriver) string {
	t.Helper()
	r := f.run(d.enter() + d.enter())
	if r.err == nil {
		t.Fatalf("expected the wizard to stop on Keys when input ends")
	}
	if d.plain {
		return r.out
	}
	return wtFlat(wtLastFrame(r.out))
}

// E1, E2, C1: toggle rows, their position and their initial state.
func TestAdviserWizardKeysRowsAndInitialToggles(t *testing.T) {
	for _, d := range adviserDrivers {
		t.Run(d.name+"/none saved", func(t *testing.T) {
			f := adviserFixture(t, d)
			keys := adviserStopAtKeys(t, f, d)
			if d.plain {
				adviserRequirePlainRows(t, keys, []adviserRowWant{
					{has: []string{"TypeSafe (Jev)"}, hasNot: []string{"Use at checkpoints"}},
					{has: []string{"SubQ (Snap)"}, hasNot: []string{"Use at checkpoints"}},
					{has: []string{"[x]", "Use at checkpoints", "SubQ"}},
					{has: []string{"OpenAI Decisions"}, hasNot: []string{"Use at checkpoints"}},
					{has: []string{"[x]", "Use at checkpoints", "OpenAI"}},
					{has: []string{"Check keys online"}},
					{has: []string{"Continue"}},
				})
				return
			}
			adviserInOrder(t, keys, "TypeSafe (Jev)", "SubQ (Snap)", "[x] Use at checkpoints SubQ",
				"OpenAI Decisions", "[x] Use at checkpoints OpenAI", "Check keys online", "Continue")
			if n := strings.Count(keys, "Use at checkpoints"); n != 2 {
				t.Fatalf("Keys frame has %d toggles, want 2:\n%s", n, keys)
			}
		})

		t.Run(d.name+"/openai saved off", func(t *testing.T) {
			f := adviserFixture(t, d, "openai_decisions")
			keys := adviserStopAtKeys(t, f, d)
			if d.plain {
				adviserRequirePlainRows(t, keys, []adviserRowWant{
					{has: []string{"TypeSafe (Jev)"}},
					{has: []string{"SubQ (Snap)"}},
					{has: []string{"[x]", "Use at checkpoints", "SubQ"}},
					{has: []string{"OpenAI Decisions"}},
					{has: []string{"[ ]", "Use at checkpoints", "OpenAI"}},
					{has: []string{"Check keys online"}},
					{has: []string{"Continue"}},
				})
				return
			}
			adviserInOrder(t, keys, "[x] Use at checkpoints SubQ", "[ ] Use at checkpoints OpenAI")
		})

		t.Run(d.name+"/both saved off", func(t *testing.T) {
			f := adviserFixture(t, d, "subq_snap", "openai_decisions")
			keys := adviserStopAtKeys(t, f, d)
			if d.plain {
				rows := adviserPlainRows(keys)
				if len(rows) != 7 || !strings.Contains(rows[2], "[ ]") || !strings.Contains(rows[4], "[ ]") {
					t.Fatalf("Keys rows %q, want rows 3 and 5 unchecked", rows)
				}
				return
			}
			adviserInOrder(t, keys, "[ ] Use at checkpoints SubQ", "[ ] Use at checkpoints OpenAI")
		})

		t.Run(d.name+"/only openai enabled by the release", func(t *testing.T) {
			f := adviserFixture(t, d)
			f.view.Advisers = []string{"openai_decisions"}
			keys := adviserStopAtKeys(t, f, d)
			if d.plain {
				adviserRequirePlainRows(t, keys, []adviserRowWant{
					{has: []string{"TypeSafe (Jev)"}, hasNot: []string{"Use at checkpoints"}},
					{has: []string{"SubQ (Snap)"}, hasNot: []string{"Use at checkpoints"}},
					{has: []string{"OpenAI Decisions"}, hasNot: []string{"Use at checkpoints"}},
					{has: []string{"[x]", "Use at checkpoints", "OpenAI"}},
					{has: []string{"Check keys online"}},
					{has: []string{"Continue"}},
				})
				return
			}
			adviserInOrder(t, keys, "SubQ (Snap)", "OpenAI Decisions", "[x] Use at checkpoints OpenAI", "Check keys online")
			if n := strings.Count(keys, "Use at checkpoints"); n != 1 {
				t.Fatalf("Keys frame has %d toggles, want 1:\n%s", n, keys)
			}
		})

		t.Run(d.name+"/no advisers in view", func(t *testing.T) {
			f := adviserFixture(t, d, "openai_decisions")
			f.view.Advisers = nil
			r := f.run(d.enter() + d.enter())
			if strings.Contains(wtStrip(r.out), "Use at checkpoints") {
				t.Fatalf("a view without Advisers shows a toggle:\n%s", wtStrip(r.out))
			}
		})
	}
}

// C2, C3, E5: unchecking saves Disabled and names it in Review and finish.
func TestAdviserWizardUncheckedAdviserIsSavedAndShown(t *testing.T) {
	for _, d := range adviserDrivers {
		t.Run(d.name+"/openai off", func(t *testing.T) {
			f := adviserFixture(t, d)
			r := f.run(d.enter() + d.enter() + d.toggle(adviserRowOpenAIToggle) + d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			adviserRequireSelection(t, r.sel, "openai_decisions")
			adviserRequireSaved(t, f, r.sel, "openai_decisions")
			line := adviserRequireAdviserLine(t, "Review", adviserReview(d, r), "OpenAI off")
			if strings.Contains(line, "SubQ") {
				t.Fatalf("Review Advisers line %q names SubQ, which stays on", line)
			}
			adviserRequireAdviserLine(t, "finish message", adviserFinish(r), "OpenAI off")
		})

		t.Run(d.name+"/both off", func(t *testing.T) {
			f := adviserFixture(t, d)
			r := f.run(d.enter() + d.enter() + d.toggle(adviserRowSubQToggle) + d.toggle(adviserRowOpenAIToggle) +
				d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			adviserRequireSelection(t, r.sel, "subq_snap", "openai_decisions")
			adviserRequireSaved(t, f, r.sel, "subq_snap", "openai_decisions")
			adviserRequireAdviserLine(t, "Review", adviserReview(d, r), "SubQ", "OpenAI", "off")
			adviserRequireAdviserLine(t, "finish message", adviserFinish(r), "SubQ", "OpenAI", "off")
		})

		t.Run(d.name+"/toggled twice stays on", func(t *testing.T) {
			f := adviserFixture(t, d)
			r := f.run(d.enter() + d.enter() + d.toggle(adviserRowOpenAIToggle) + d.toggle(adviserRowOpenAIToggle) +
				d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			adviserRequireSelection(t, r.sel)
			adviserRequireSaved(t, f, r.sel)
			adviserRequireNoAdviserLine(t, "Review", adviserReview(d, r))
		})
	}
}

// C2 (empty list), C3 (no line): all checked saves none disabled.
func TestAdviserWizardAllCheckedAddsNoAdviserLine(t *testing.T) {
	for _, d := range adviserDrivers {
		t.Run(d.name, func(t *testing.T) {
			f := adviserFixture(t, d)
			r := f.run(d.enter() + d.enter() + d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			adviserRequireSelection(t, r.sel)
			adviserRequireSaved(t, f, r.sel)
			adviserRequireNoAdviserLine(t, "Review", adviserReview(d, r))
			adviserRequireNoAdviserLine(t, "finish message", adviserFinish(r))
		})
	}
}

// C1 (starts unchecked when saved off) and C2 (an empty list re-enables).
func TestAdviserWizardRecheckingClearsSavedDisable(t *testing.T) {
	for _, d := range adviserDrivers {
		t.Run(d.name, func(t *testing.T) {
			f := adviserFixture(t, d, "openai_decisions")
			r := f.run(d.enter() + d.enter() + d.toggle(adviserRowOpenAIToggle) + d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			adviserRequireSelection(t, r.sel)
			adviserRequireSaved(t, f, r.sel)
			adviserRequireNoAdviserLine(t, "Review", adviserReview(d, r))
			adviserRequireNoAdviserLine(t, "finish message", adviserFinish(r))
		})
	}
}

// C1: toggling changes no stored key.
func TestAdviserWizardToggleKeepsStoredKeys(t *testing.T) {
	for _, d := range adviserDrivers {
		t.Run(d.name, func(t *testing.T) {
			f := adviserFixture(t, d)
			wtStoreKeys(t, f.view.ConfigDir, map[string]string{
				credentials.SUBQ_API_KEY:   "stored-subq-7101",
				credentials.OPENAI_API_KEY: "stored-openai-7102",
			})
			before := wtStoredKeys(t, f.view.ConfigDir)
			r := f.run(d.enter() + d.enter() + d.toggle(adviserRowSubQToggle) + d.toggle(adviserRowOpenAIToggle) +
				d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			adviserRequireSelection(t, r.sel, "subq_snap", "openai_decisions")
			if _, err := r.wizard.SaveCredentials(t.Context()); err != nil {
				t.Fatalf("SaveCredentials: %v", err)
			}
			if after := wtStoredKeys(t, f.view.ConfigDir); !reflect.DeepEqual(after, before) {
				t.Fatalf("stored keys changed by toggles: before %v after %v", before, after)
			}
			wtNoSecrets(t, r.out, "stored-subq-7101")
			wtNoSecrets(t, r.out, "stored-openai-7102")
		})
	}
}

// C4: choosing a preset in Models does not reset the toggles.
func TestAdviserWizardPresetKeepsToggles(t *testing.T) {
	for _, d := range adviserDrivers {
		t.Run(d.name+"/back to balanced after unchecking", func(t *testing.T) {
			f := adviserFixture(t, d)
			r := f.run(d.enter() + d.enter() + d.toggle(adviserRowOpenAIToggle) + d.back() +
				d.choose(adviserPresetBalanced) + d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			if r.sel.Preset != workflow.PresetBalanced {
				t.Fatalf("preset = %q, want balanced", r.sel.Preset)
			}
			adviserRequireSelection(t, r.sel, "openai_decisions")
			adviserRequireSaved(t, f, r.sel, "openai_decisions")
			adviserRequireAdviserLine(t, "Review", adviserReview(d, r), "OpenAI off")
		})

		t.Run(d.name+"/saved off survives balanced", func(t *testing.T) {
			f := adviserFixture(t, d, "openai_decisions")
			r := f.run(d.enter() + d.choose(adviserPresetBalanced) + d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			if r.sel.Preset != workflow.PresetBalanced {
				t.Fatalf("preset = %q, want balanced", r.sel.Preset)
			}
			adviserRequireSelection(t, r.sel, "openai_decisions")
			adviserRequireSaved(t, f, r.sel, "openai_decisions")
			adviserRequireAdviserLine(t, "Review", adviserReview(d, r), "OpenAI off")
		})
	}
}

// E3: on the Advanced path the toggles go into Preferences.Advisers.
func TestAdviserWizardAdvancedPathCarriesToggles(t *testing.T) {
	for _, d := range adviserDrivers {
		t.Run(d.name+"/openai off", func(t *testing.T) {
			f := adviserFixture(t, d)
			// Hosts, Advanced, Done, then uncheck OpenAI.
			r := f.run(d.enter() + d.choose(adviserPresetAdvanced) + d.enter() + d.toggle(adviserRowOpenAIToggle) +
				d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			if r.sel.Preferences == nil {
				t.Fatalf("precondition: the Advanced path returned no Preferences (selection %+v)", r.sel)
			}
			adviserRequireSelection(t, r.sel, "openai_decisions")
			adviserRequireSaved(t, f, r.sel, "openai_decisions")
			adviserRequireAdviserLine(t, "Review", adviserReview(d, r), "OpenAI off")
		})

		t.Run(d.name+"/recheck keeps untoggled jev off", func(t *testing.T) {
			f := adviserFixture(t, d, "jev", "openai_decisions")
			r := f.run(d.enter() + d.choose(adviserPresetAdvanced) + d.enter() + d.toggle(adviserRowOpenAIToggle) +
				d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			if r.sel.Preferences == nil {
				t.Fatalf("precondition: the Advanced path returned no Preferences (selection %+v)", r.sel)
			}
			adviserRequireSelection(t, r.sel, "jev")
			adviserRequireSaved(t, f, r.sel, "jev")
		})
	}
}

// E4: a saved-off provider without a toggle stays off.
func TestAdviserWizardUntoggledSavedDisableSurvives(t *testing.T) {
	for _, d := range adviserDrivers {
		t.Run(d.name+"/defaults", func(t *testing.T) {
			f := adviserFixture(t, d, "jev")
			r := f.run(d.enter() + d.enter() + d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			adviserRequireSelection(t, r.sel, "jev")
			adviserRequireSaved(t, f, r.sel, "jev")
		})

		t.Run(d.name+"/with openai unchecked", func(t *testing.T) {
			f := adviserFixture(t, d, "jev")
			r := f.run(d.enter() + d.enter() + d.toggle(adviserRowOpenAIToggle) + d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			adviserRequireSelection(t, r.sel, "jev", "openai_decisions")
			adviserRequireSaved(t, f, r.sel, "jev", "openai_decisions")
			adviserRequireAdviserLine(t, "Review", adviserReview(d, r), "OpenAI off")
		})

		// C3 names any disabled adviser, including one without a toggle.
		t.Run(d.name+"/review names jev", func(t *testing.T) {
			f := adviserFixture(t, d, "jev")
			r := f.run(d.enter() + d.enter() + d.choose(adviserRowContinue) + d.enter())
			r.requireApplied(t)
			line := adviserRequireAdviserLine(t, "Review", adviserReview(d, r), "off")
			if !strings.Contains(line, "TypeSafe") && !strings.Contains(line, "Jev") {
				t.Fatalf("Review Advisers line %q does not name TypeSafe/Jev", line)
			}
		})
	}
}
