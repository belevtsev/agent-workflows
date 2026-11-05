package cli

// Behavioural tests for the setup wizard, written from contract wizard-ux
// revision 1. Every test names the requirement it covers. Output assertions
// are made on text with CSI sequences removed; key values must never appear
// in what the wizard prints.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/internal/bundled"
	"github.com/belevtsev/agent-workflows/internal/credentials"
	"github.com/belevtsev/agent-workflows/internal/workflow"
)

const (
	wtUp        = "\x1b[A"
	wtDown      = "\x1b[B"
	wtRight     = "\x1b[C"
	wtLeft      = "\x1b[D"
	wtHome      = "\x1b[H"
	wtEnd       = "\x1b[F"
	wtShiftTab  = "\x1b[Z"
	wtEsc       = "\x1b"
	wtBackspace = "\x7f"
	wtCtrlC     = "\x03"
	wtCtrlD     = "\x04"

	wtVersion    = "v9.8.7"
	wtCancelLine = "Setup cancelled. Your configuration was not changed.\n"
	wtApplying   = "Applying settings…"

	wtSol   = "gpt-6.1-sol"
	wtTerra = "gpt-6.1-terra"
	wtLuna  = "gpt-6-luna"
	wtOpus  = "claude-opus-5-5"
	wtHaiku = "claude-haiku-5"

	// GitHub worker role models (R github-workers fixtures).
	wtAstra  = "gpt-6-astra"
	wtSonnet = "claude-sonnet-5-5"
)

// wtClear empties a visual text field whatever value it starts with.
var wtClear = strings.Repeat(wtBackspace, 24)

// wtModelNames lists the display names the contract shows for the models the
// review and finish lines may name; either the id or this name is accepted.
var wtModelNames = map[string]string{wtSol: "Sol 6.1", wtOpus: "Opus 5.5", wtSonnet: "Sonnet 5.5"}

// Change notes, with or without parentheses (rev 3 wrapping rule).
var (
	wtWasNote     = regexp.MustCompile(`\bwas `)
	wtChangedNote = regexp.MustCompile(`\bchanged\b`)
)

var wtCSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// ---------------------------------------------------------------------------
// Fixtures

type wtFixture struct {
	t     *testing.T
	home  string
	env   map[string]string
	plain bool
	view  workflow.ConfigurationView
}

// wtPhysicalHome resolves a temporary home to a symlink-free path (contract
// rev 2: the credential store refuses symlinked or group/world-accessible
// directories; macOS t.TempDir() lies under /var -> /private/var).
func wtPhysicalHome(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// wtConfigDir creates the owner-only credential store directory under home.
func wtConfigDir(t *testing.T, home string) string {
	t.Helper()
	configDir := filepath.Join(home, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return configDir
}

func newWTFixture(t *testing.T, installed bool) *wtFixture {
	t.Helper()
	home := wtPhysicalHome(t, t.TempDir())
	configDir := wtConfigDir(t, home)
	return &wtFixture{
		t:    t,
		home: home,
		env:  map[string]string{"HOME": home, "TERM": "xterm-256color"},
		view: wtView(home, configDir, installed),
	}
}

func wtRole(name, scope, model, effort string, choices []workflow.ModelChoice) workflow.RoleConfiguration {
	return workflow.RoleConfiguration{
		Role:    name,
		Scope:   scope,
		Default: workflow.ModelSelection{Model: model, Effort: effort},
		Choices: choices,
	}
}

// wtView builds a two-host view. Codex has two substantive roles plus the
// fallback, one bounded_execution and one bounded_evidence role; Claude Code
// has no bounded_execution role. Every default uses gpt-6.1-sol or
// claude-opus-5-5, and the Balanced pins (sol/xhigh, opus/high) are valid.
func wtView(home, configDir string, installed bool) workflow.ConfigurationView {
	codexDeep := []workflow.ModelChoice{
		{Model: wtSol, Efforts: []string{"high", "xhigh", "max"}},
		{Model: wtTerra, Efforts: []string{"medium", "high"}},
	}
	codexLight := []workflow.ModelChoice{
		{Model: wtLuna, Efforts: []string{"low", "medium", "high"}},
		{Model: wtSol, Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
	}
	claudeDeep := []workflow.ModelChoice{
		{Model: wtOpus, Efforts: []string{"high", "xhigh", "max"}},
	}
	claudeLight := []workflow.ModelChoice{
		{Model: wtOpus, Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
		{Model: wtHaiku, Efforts: []string{"low"}},
	}
	codex := workflow.HostConfiguration{
		ID: "codex", Root: filepath.Join(home, ".codex"), Detected: true, Installed: installed, Selected: true,
		Roles: []workflow.RoleConfiguration{
			wtRole("implementer", "substantive", wtSol, "max", codexDeep),
			wtRole("code_reviewer", "substantive", wtSol, "max", codexDeep),
			wtRole("executor", "bounded_execution", wtSol, "medium", codexLight),
			wtRole("explorer", "bounded_evidence", wtSol, "low", codexLight),
		},
		Fallback: wtRole("substantive_fallback", "substantive", wtSol, "max", codexDeep),
	}
	claude := workflow.HostConfiguration{
		ID: "claude", Root: filepath.Join(home, ".claude"), Detected: true, Installed: installed, Selected: true,
		Roles: []workflow.RoleConfiguration{
			wtRole("implementer", "substantive", wtOpus, "xhigh", claudeDeep),
			wtRole("explorer", "bounded_evidence", wtOpus, "low", claudeLight),
		},
		Fallback: wtRole("substantive_fallback", "substantive", wtOpus, "xhigh", claudeDeep),
	}
	view := workflow.ConfigurationView{
		Command:   "setup",
		Installed: installed,
		ConfigDir: configDir,
		Target:    bundled.Identity{Version: wtVersion},
		Hosts:     []workflow.HostConfiguration{codex, claude},
		SharedDefaults: workflow.Object{
			"model_context_window":         int64(272000),
			"features.memories":            true,
			"memories.generate_memories":   true,
			"memories.use_memories":        true,
			"memories.extract_model":       wtLuna,
			"memories.consolidation_model": wtSol,
		},
	}
	if installed {
		for h := range view.Hosts {
			host := &view.Hosts[h]
			for r := range host.Roles {
				host.Roles[r].Effective = host.Roles[r].Default
			}
			host.Fallback.Effective = host.Fallback.Default
		}
		view.SharedEffective = maps.Clone(view.SharedDefaults)
	}
	return view
}

func (f *wtFixture) host(id string) *workflow.HostConfiguration {
	f.t.Helper()
	for i := range f.view.Hosts {
		if f.view.Hosts[i].ID == id {
			return &f.view.Hosts[i]
		}
	}
	f.t.Fatalf("fixture has no host %q", id)
	return nil
}

func (f *wtFixture) role(hostID, name string) *workflow.RoleConfiguration {
	f.t.Helper()
	host := f.host(hostID)
	if name == "substantive_fallback" {
		return &host.Fallback
	}
	for i := range host.Roles {
		if host.Roles[i].Role == name {
			return &host.Roles[i]
		}
	}
	f.t.Fatalf("fixture host %q has no role %q", hostID, name)
	return nil
}

// config derives an isolated Config from the shared testConfig helper.
func (f *wtFixture) config(in io.Reader) (Config, *bytes.Buffer, *bytes.Buffer) {
	f.t.Helper()
	config, stdout, _, _ := testConfig(f.t)
	stderr := new(bytes.Buffer)
	home := f.home
	env := maps.Clone(f.env)
	config.In = in
	config.Err = stderr
	config.HomeDir = func() (string, error) { return home, nil }
	config.Getenv = func(name string) string { return env[name] }
	return config, stdout, stderr
}

type wtResult struct {
	sel    workflow.ConfigurationSelection
	err    error
	out    string // everything written to config.Err
	stdout string // everything written to config.Out
	wizard *setupWizard
}

func (f *wtFixture) runContext(ctx context.Context, in io.Reader) wtResult {
	f.t.Helper()
	config, stdout, stderr := f.config(in)
	w := newSetupWizard(config, options{plain: f.plain})
	sel, err := w.Select(ctx, f.view)
	return wtResult{sel: sel, err: err, out: stderr.String(), stdout: stdout.String(), wizard: w}
}

func (f *wtFixture) run(input string) wtResult {
	f.t.Helper()
	return f.runContext(f.t.Context(), strings.NewReader(input))
}

func (r wtResult) requireApplied(t *testing.T) {
	t.Helper()
	if r.err != nil {
		t.Fatalf("Select failed: %v\noutput tail:\n%s", r.err, wtTail(wtStrip(r.out)))
	}
	if r.stdout != "" {
		t.Fatalf("wizard wrote to config.Out: %q", r.stdout)
	}
}

// ---------------------------------------------------------------------------
// Output helpers

func wtStrip(s string) string {
	return strings.ReplaceAll(wtCSI.ReplaceAllString(s, ""), "\r", "")
}

// wtFlat removes box drawing and collapses all whitespace, so a check does not
// depend on borders or on where a long line wraps.
func wtFlat(s string) string {
	s = strings.Map(func(r rune) rune {
		if r >= 0x2500 && r <= 0x259F {
			return ' '
		}
		return r
	}, wtStrip(s))
	return strings.Join(strings.Fields(s), " ")
}

func wtTail(s string) string {
	if len(s) > 3000 {
		return s[len(s)-3000:]
	}
	return s
}

// wtFrames returns every visual frame (each starts with ESC [ H), stripped.
func wtFrames(raw string) []string {
	parts := strings.Split(raw, "\x1b[H")
	frames := make([]string, 0, len(parts))
	for _, part := range parts[1:] {
		frames = append(frames, wtStrip(part))
	}
	return frames
}

func wtFirstFrame(t *testing.T, raw, marker string) string {
	t.Helper()
	for _, frame := range wtFrames(raw) {
		if strings.Contains(frame, marker) {
			return frame
		}
	}
	t.Fatalf("no frame contains %q; output tail:\n%s", marker, wtTail(wtStrip(raw)))
	return ""
}

// wtLastFrame is the last frame drawn before the screen was restored.
func wtLastFrame(raw string) string {
	if end := strings.LastIndex(raw, "\x1b[?1049l"); end >= 0 {
		raw = raw[:end]
	}
	if start := strings.LastIndex(raw, "\x1b[H"); start >= 0 {
		raw = raw[start:]
	}
	return wtStrip(raw)
}

func wtLines(text, substr string) []string {
	var lines []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, substr) {
			lines = append(lines, line)
		}
	}
	return lines
}

func wtM(model string) string {
	if name, ok := wtModelNames[model]; ok {
		return "(?:" + regexp.QuoteMeta(model) + "|" + regexp.QuoteMeta(name) + ")"
	}
	return regexp.QuoteMeta(model)
}

func wtMatch(t *testing.T, text, pattern, what string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(text) {
		t.Fatalf("%s: no match for %q in:\n%s", what, pattern, wtTail(text))
	}
}

func wtNoMatch(t *testing.T, text, pattern, what string) {
	t.Helper()
	if regexp.MustCompile(pattern).MatchString(text) {
		t.Fatalf("%s: unexpected match for %q in:\n%s", what, pattern, wtTail(text))
	}
}

func wtContains(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("output lacks %q:\n%s", want, wtTail(text))
		}
	}
}

func wtInOrder(t *testing.T, text string, wants ...string) {
	t.Helper()
	at := 0
	for _, want := range wants {
		index := strings.Index(text[at:], want)
		if index < 0 {
			t.Fatalf("%q missing or out of order (wanted order %q) in:\n%s", want, wants, wtTail(text))
		}
		at += index + len(want)
	}
}

func wtNoSecrets(t *testing.T, text string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(text, secret) {
			t.Fatalf("a key value (%q) was printed", secret)
		}
	}
}

// ---------------------------------------------------------------------------
// Assertion helpers

func wtRequireHosts(t *testing.T, sel workflow.ConfigurationSelection, want ...string) {
	t.Helper()
	if !slices.Equal(sel.Hosts, want) {
		t.Fatalf("selected hosts = %q, want %q", sel.Hosts, want)
	}
}

// wtRequireRestored checks R terminal-safety on a visual output.
func wtRequireRestored(t *testing.T, out string) {
	t.Helper()
	enter, hide := strings.LastIndex(out, "\x1b[?1049h"), strings.LastIndex(out, "\x1b[?25l")
	if enter < 0 || hide < 0 {
		t.Fatalf("visual mode did not enter the alternate screen and hide the cursor: %q", wtTail(out))
	}
	if strings.LastIndex(out, "\x1b[?1049l") < enter {
		t.Fatal("alternate screen was not left after it was last entered")
	}
	if strings.LastIndex(out, "\x1b[?25h") < hide {
		t.Fatal("cursor was not shown again after it was last hidden")
	}
	if !strings.Contains(out, "\x1b[?25h\x1b[?1049l") {
		t.Fatal("restore sequence ESC[?25h ESC[?1049l missing")
	}
	first := strings.Index(out, "\x1b[H")
	if first < 0 || first < strings.Index(out, "\x1b[?1049h") || first < strings.Index(out, "\x1b[?25l") {
		t.Fatal("no frame starting with ESC[H was drawn after entering the alternate screen")
	}
}

func wtRequireVisualApplied(t *testing.T, out string) {
	t.Helper()
	wtRequireRestored(t, out)
	if !strings.HasSuffix(out, wtApplying+"\n") {
		t.Fatalf("visual output does not end with %q: %q", wtApplying, wtTail(out))
	}
	if strings.LastIndex(out, "\x1b[?1049l") > strings.LastIndex(out, wtApplying) {
		t.Fatal("applying message was written before the screen was restored")
	}
}

func wtRequireNoEscape(t *testing.T, out string) {
	t.Helper()
	if strings.IndexByte(out, 0x1b) >= 0 {
		t.Fatalf("plain output contains an ESC byte: %q", wtTail(out))
	}
}

// wtRequireCancelled checks R back-and-exit quit semantics.
func wtRequireCancelled(t *testing.T, r wtResult, visual bool) {
	t.Helper()
	if !errors.Is(r.err, context.Canceled) {
		t.Fatalf("Select error = %v, want context.Canceled\noutput tail:\n%s", r.err, wtTail(wtStrip(r.out)))
	}
	if r.err.Error() != "" {
		t.Fatalf("cancellation error text = %q, want empty", r.err.Error())
	}
	if code := ExitCode(r.err); code != 130 {
		t.Fatalf("ExitCode = %d, want 130", code)
	}
	if r.sel.Hosts != nil || r.sel.Preset != "" || r.sel.Preferences != nil {
		t.Fatalf("cancelled Select returned a non-zero selection: %+v", r.sel)
	}
	if !strings.HasSuffix(r.out, wtCancelLine) {
		t.Fatalf("output does not end with %q: %q", wtCancelLine, wtTail(r.out))
	}
	if r.stdout != "" {
		t.Fatalf("wizard wrote to config.Out: %q", r.stdout)
	}
	if visual {
		wtRequireRestored(t, r.out)
		if strings.LastIndex(r.out, "\x1b[?1049l") > strings.LastIndex(r.out, "Setup cancelled.") {
			t.Fatal("cancel message was written before the screen was restored")
		}
	} else {
		wtRequireNoEscape(t, r.out)
	}
}

func wtStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// wtOverride reads the stored override of a role (the fallback is stored in
// HostPreferences.Fallback).
func wtOverride(t *testing.T, prefs *workflow.Preferences, host, role string) workflow.ModelOverride {
	t.Helper()
	if prefs == nil {
		t.Fatal("selection has no Preferences")
	}
	hostPrefs := prefs.Hosts[host]
	if role == "substantive_fallback" {
		if hostPrefs.Fallback == nil {
			return workflow.ModelOverride{}
		}
		return *hostPrefs.Fallback
	}
	return hostPrefs.Roles[role]
}

func wtRequirePin(t *testing.T, prefs *workflow.Preferences, host, role, model, effort string) {
	t.Helper()
	got := wtOverride(t, prefs, host, role)
	if wtStr(got.Model) != model || wtStr(got.Effort) != effort {
		t.Fatalf("%s %s override = {%s, %s}, want {%s, %s}", host, role, wtStr(got.Model), wtStr(got.Effort), model, effort)
	}
}

func wtRequireNoRolePins(t *testing.T, prefs *workflow.Preferences) {
	t.Helper()
	if prefs == nil {
		t.Fatal("Advanced selection has nil Preferences")
	}
	for host, hostPrefs := range prefs.Hosts {
		for role, override := range hostPrefs.Roles {
			if override.Model != nil || override.Effort != nil {
				t.Fatalf("unexpected pin %s %s = {%s, %s}", host, role, wtStr(override.Model), wtStr(override.Effort))
			}
		}
		if hostPrefs.Fallback != nil && (hostPrefs.Fallback.Model != nil || hostPrefs.Fallback.Effort != nil) {
			t.Fatalf("unexpected fallback pin for %s", host)
		}
	}
}

func wtStoredKeys(t *testing.T, dir string) map[string]string {
	t.Helper()
	snapshot, err := credentials.Store{Directory: dir}.Read()
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}
	}
	if err != nil {
		t.Fatalf("read credential store: %v", err)
	}
	if snapshot.Keys == nil {
		return map[string]string{}
	}
	return snapshot.Keys
}

func wtStoreKeys(t *testing.T, dir string, keys map[string]string) {
	t.Helper()
	store := credentials.Store{Directory: dir}
	snapshot, err := store.Read()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read credential store: %v", err)
	}
	generation := snapshot.Generation
	changes := make(map[string]*string, len(keys))
	for name, value := range keys {
		changes[name] = &value
	}
	if _, err := store.Save(t.Context(), changes, &generation); err != nil {
		t.Fatalf("seed credential store: %v", err)
	}
}

// ---------------------------------------------------------------------------
// R compact-flow

func TestWizardDefaultFreshPathAppliesRecommended(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	r := f.run("\r\r\r\r")
	r.requireApplied(t)
	wtRequireHosts(t, r.sel, "codex", "claude")
	if r.sel.Preset != workflow.PresetRecommended || r.sel.Preferences != nil {
		t.Fatalf("default path selection = %+v, want Recommended without preferences", r.sel)
	}
	wtRequireVisualApplied(t, r.out)

	text := wtStrip(r.out)
	wtContains(t, text, "Codex", "Claude Code", "~/.codex", "~/.claude", "Review")
	if strings.Contains(text, f.home) {
		t.Fatalf("a path inside HOME was not shortened to ~: %s", wtTail(text))
	}
	// Answered steps are summarised on later screens.
	models := wtFirstFrame(t, r.out, "Balanced")
	wtMatch(t, models, `Hosts +Codex, Claude Code`, "hosts summary on the models screen")
	keys := wtFirstFrame(t, r.out, "Adviser keys")
	wtMatch(t, keys, `Hosts +Codex, Claude Code`, "hosts summary on the keys screen")
	wtMatch(t, keys, `Models +Recommended`, "models summary on the keys screen")
	if n := strings.Count(keys, "not set"); n < 3 {
		t.Fatalf("fresh keys screen shows %d \"not set\" statuses, want 3:\n%s", n, keys)
	}
}

func TestWizardLineFeedActsAsEnter(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	r := f.run("\n\n\n\n")
	r.requireApplied(t)
	wtRequireHosts(t, r.sel, "codex", "claude")
	if r.sel.Preset != workflow.PresetRecommended {
		t.Fatalf("preset = %q, want recommended", r.sel.Preset)
	}
}

// ---------------------------------------------------------------------------
// R hosts-checklist

func TestWizardHostsChecklist(t *testing.T) {
	t.Parallel()

	t.Run("digit toggles the nth not-installed host and installed hosts stay", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct {
			input string
			want  []string
		}{
			{"1\r\r\r\r", []string{"codex", "claude"}},
			{"\r\r\r\r", []string{"codex"}},
		} {
			f := newWTFixture(t, false)
			f.host("codex").Installed = true
			f.host("claude").Selected = false
			r := f.run(test.input)
			r.requireApplied(t)
			wtRequireHosts(t, r.sel, test.want...)
		}
	})

	t.Run("space toggles the row under the cursor", func(t *testing.T) {
		t.Parallel()
		f := newWTFixture(t, false)
		// "2" moves to Claude Code and unchecks it; Space checks it; Space unchecks it.
		r := f.run("2  \r\r\r\r")
		r.requireApplied(t)
		wtRequireHosts(t, r.sel, "codex")
	})

	t.Run("nothing checked keeps the hosts step", func(t *testing.T) {
		t.Parallel()
		f := newWTFixture(t, false)
		r := f.run("12\r" + "1\r" + "\r\r\r")
		r.requireApplied(t)
		wtRequireHosts(t, r.sel, "codex")
		wtContains(t, wtStrip(r.out), "Select at least one host to continue.")
	})

	t.Run("selection follows view order", func(t *testing.T) {
		t.Parallel()
		f := newWTFixture(t, false)
		f.view.Hosts[0], f.view.Hosts[1] = f.view.Hosts[1], f.view.Hosts[0]
		r := f.run("\r\r\r\r")
		r.requireApplied(t)
		wtRequireHosts(t, r.sel, "claude", "codex")
	})

	t.Run("no hosts step when every host is installed", func(t *testing.T) {
		t.Parallel()
		f := newWTFixture(t, true)
		// Models (Keep), Keys, Apply: three keys only.
		r := f.run("\r\r\r")
		r.requireApplied(t)
		wtRequireHosts(t, r.sel, "codex", "claude")
		if r.sel.Preset != "" || r.sel.Preferences != nil {
			t.Fatalf("Keep saved choices selection = %+v, want empty preset and nil preferences", r.sel)
		}
		wtContains(t, wtStrip(r.out), "Keep saved choices")
	})
}

// ---------------------------------------------------------------------------
// Models

func TestWizardModelsPresets(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		installed bool
		input     string
		want      workflow.Preset
	}{
		{"fresh balanced", false, "\r" + "2\r" + "\r\r", workflow.PresetBalanced},
		{"installed keep is the default", true, "\r\r\r", ""},
		{"installed recommended", true, "2\r" + "\r\r", workflow.PresetRecommended},
		{"installed balanced", true, "3\r" + "\r\r", workflow.PresetBalanced},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newWTFixture(t, test.installed)
			r := f.run(test.input)
			r.requireApplied(t)
			if r.sel.Preset != test.want || r.sel.Preferences != nil {
				t.Fatalf("selection = %+v, want preset %q and nil preferences", r.sel, test.want)
			}
		})
	}
}

func TestWizardAdvancedReleaseDefaults(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	// Hosts, Advanced, Done, Keys, Apply.
	r := f.run("\r" + "3\r" + "1\r" + "\r" + "\r")
	r.requireApplied(t)
	wtRequireNoRolePins(t, r.sel.Preferences)
	review := wtLastFrame(r.out)
	wtContains(t, review, "Preset     Advanced · release defaults")
	wtContains(t, wtStrip(r.out), "Done", "Codex roles", "Claude Code roles", "Memory and context")
}

func TestWizardAdvancedPinsOneRole(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "\r" + "3\r" + // Hosts, Advanced
		"2\r" + // Codex roles
		"2\r" + // code reviewer
		"2\r" + // gpt-6.1-sol
		"2\r" + // high (row 1 is Release default)
		wtLeft + "1\r" + // roles -> advanced menu, Done
		"\r" + "\r" // Keys, Apply
	r := f.run(input)
	r.requireApplied(t)
	wtRequirePin(t, r.sel.Preferences, "codex", "code_reviewer", wtSol, "high")
	if got := wtOverride(t, r.sel.Preferences, "codex", "implementer"); got.Model != nil || got.Effort != nil {
		t.Fatal("an unrelated role was pinned")
	}

	text := wtStrip(r.out)
	wtContains(t, text, "code reviewer", "substantive fallback", "Release default")

	// R readable-review: one pinned role is an exception below its row.
	review := wtLastFrame(r.out)
	wtContains(t, review, "Preset     Advanced · 1 pinned")
	flat := wtFlat(review)
	wtMatch(t, flat, `Main work `+wtM(wtSol)+` · max`, "codex main work row")
	wtNoMatch(t, flat, `Main work `+wtM(wtSol)+` · max · pinned`, "unpinned main work row")
	wtMatch(t, flat, `code reviewer `+wtM(wtSol)+` · high · pinned`, "pinned role exception line")
}

func TestWizardAdvancedMajorityPinnedRow(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "\r" + "3\r" + "2\r" +
		"1\r" + "2\r" + "2\r" + // implementer -> sol / high
		"2\r" + "2\r" + "2\r" + // code reviewer -> sol / high
		wtLeft + "1\r" + "\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtRequirePin(t, r.sel.Preferences, "codex", "implementer", wtSol, "high")
	wtRequirePin(t, r.sel.Preferences, "codex", "code_reviewer", wtSol, "high")

	review := wtLastFrame(r.out)
	wtContains(t, review, "Preset     Advanced · 2 pinned")
	flat := wtFlat(review)
	wtMatch(t, flat, `Main work `+wtM(wtSol)+` · high · pinned`, "row shows the pinned majority value")
	wtMatch(t, flat, `substantive fallback `+wtM(wtSol)+` · max`, "fallback differs from its row")
	wtNoMatch(t, flat, `substantive fallback `+wtM(wtSol)+` · max · pinned`, "unpinned fallback exception")
	for _, label := range []string{"implementer", "code reviewer"} {
		if strings.Contains(review, label) {
			t.Fatalf("role %q matches its row but is listed as an exception:\n%s", label, review)
		}
	}
}

func TestWizardAdvancedModelAndEffortMenus(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "\r" + "3\r" +
		"3\r" + "3\r" + "2\r" + "4\r" + // Claude Code roles, substantive fallback, opus, max
		wtLeft + "2\r" + // advanced menu, Codex roles
		"2\r" + "3\r" + "1\r" + // code reviewer, terra, medium (no Release default: max not offered)
		"3\r" + "3\r" + "1\r" + // executor, sol, Release default effort
		wtLeft + "1\r" + "\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtRequirePin(t, r.sel.Preferences, "claude", "substantive_fallback", wtOpus, "max")
	wtRequirePin(t, r.sel.Preferences, "codex", "code_reviewer", wtTerra, "medium")
	wtRequirePin(t, r.sel.Preferences, "codex", "executor", wtSol, "<nil>")
}

func TestWizardAdvancedBackNavigation(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "\r" + "3\r" + "2\r" +
		"2\r" + "2\r" + // code reviewer, sol -> effort menu
		wtLeft + // effort menu -> model menu
		"3\r" + "2\r" + // terra, high
		"1\r" + wtBackspace + // implementer model menu -> roles menu, no change
		wtLeft + // roles -> advanced menu
		wtLeft + // advanced -> Models
		"3\r" + "1\r" + // Advanced again (edits kept), Done
		"\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtRequirePin(t, r.sel.Preferences, "codex", "code_reviewer", wtTerra, "high")
	if got := wtOverride(t, r.sel.Preferences, "codex", "implementer"); got.Model != nil || got.Effort != nil {
		t.Fatalf("leaving the model menu changed the role: {%s, %s}", wtStr(got.Model), wtStr(got.Effort))
	}
}

func TestWizardMemoryAndContext(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "\r" + "3\r" + "4\r" + // Hosts, Advanced, Memory and context
		"1\r" + wtClear + "12x\r" + // invalid context window stays in the field
		wtClear + "5120009" + wtBackspace + "\r" + // 512000
		"2\r" + "3\r" + // Memory feature: Off
		"5\r" + "abc" + wtEsc + // Extraction model: Esc leaves without change
		"6\r" + wtClear + wtLuna + "\r" + // Consolidation model
		wtLeft + "1\r" + // advanced menu, Done
		"\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtRequireNoRolePins(t, r.sel.Preferences)
	shared := r.sel.Preferences.Shared
	if shared == nil {
		t.Fatal("memory and context edits were not stored")
	}
	if shared.ContextWindow == nil || *shared.ContextWindow != 512000 {
		t.Fatalf("context window = %v, want 512000", shared.ContextWindow)
	}
	if shared.Memories == nil || *shared.Memories {
		t.Fatalf("memory feature = %v, want Off", shared.Memories)
	}
	if shared.ExtractModel != nil {
		t.Fatalf("Esc in a text field changed the extraction model to %q", *shared.ExtractModel)
	}
	if wtStr(shared.ConsolidationModel) != wtLuna {
		t.Fatalf("consolidation model = %s, want %s", wtStr(shared.ConsolidationModel), wtLuna)
	}
	if shared.GenerateMemories != nil || shared.UseMemories != nil {
		t.Fatal("untouched memory settings were pinned")
	}

	text := wtStrip(r.out)
	wtContains(t, text, "Context window", "Memory feature", "Generate memories", "Use memories",
		"Extraction model", "Consolidation model",
		"Enter a positive whole number of tokens, or leave it empty.")
	if strings.Contains(text, "Quit setup?") {
		t.Fatal("Esc inside a text field opened the quit dialog")
	}
	review := wtLastFrame(r.out)
	wtContains(t, review, "Preset     Advanced · 3 pinned", "Context    512,000 tokens")
}

// ---------------------------------------------------------------------------
// R keys-one-screen

func TestWizardKeysScreenStatusesAndSave(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	wtStoreKeys(t, f.view.ConfigDir, map[string]string{
		credentials.TYPESAFE_API_KEY: "stored-ts-4401",
		credentials.SUBQ_API_KEY:     "stored-subq-4402",
	})
	f.env[credentials.SUBQ_API_KEY] = "env-subq-4403"
	f.env[credentials.OPENAI_API_KEY] = "env-openai-4406"
	secrets := []string{"stored-ts-4401", "stored-subq-4402", "env-subq-4403", "env-openai-4406", "new-ts-4404", "new-subq-4405"}

	input := "\r\r" + // Hosts, Models
		"1\r" + "new-ts-4404" + "\r" +
		"2\r" + "new-subq-4405" + "\r" +
		"5\r" + "\r" // Continue, Apply
	r := f.run(input)
	r.requireApplied(t)
	wtNoSecrets(t, r.out, secrets...)

	keys := wtFirstFrame(t, r.out, "Adviser keys")
	wtInOrder(t, wtFlat(keys), "TypeSafe (Jev)", "SubQ (Snap)", "OpenAI Decisions", "Check keys online", "Continue")
	wtContains(t, keys, "[ ]")
	wtStatus := func(text, label, want, reject string) {
		t.Helper()
		for _, line := range wtLines(text, label) {
			if strings.Contains(line, want) && (reject == "" || !strings.Contains(line, reject)) {
				return
			}
		}
		t.Fatalf("no %q line with status %q (without %q) in:\n%s", label, want, reject, text)
	}
	wtStatus(keys, "TypeSafe (Jev)", "saved", "environment")
	wtStatus(keys, "SubQ (Snap)", "from environment · overrides saved key", "")
	wtStatus(keys, "OpenAI Decisions", "from environment", "overrides")

	all := wtStrip(r.out)
	wtStatus(all, "TypeSafe (Jev)", "new key ready", "environment")
	wtStatus(all, "SubQ (Snap)", "new key ready · environment value still wins", "")
	wtContains(t, all, strings.Repeat("•", len("new-ts-4404")))

	// Select never stores keys.
	if got := wtStoredKeys(t, f.view.ConfigDir)[credentials.TYPESAFE_API_KEY]; got != "stored-ts-4401" {
		t.Fatalf("Select changed the stored TypeSafe key to %q", got)
	}
	if _, err := r.wizard.SaveCredentials(t.Context()); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	stored := wtStoredKeys(t, f.view.ConfigDir)
	if stored[credentials.TYPESAFE_API_KEY] != "new-ts-4404" || stored[credentials.SUBQ_API_KEY] != "new-subq-4405" {
		t.Fatalf("marked keys not stored: TypeSafe %q SubQ %q", stored[credentials.TYPESAFE_API_KEY], stored[credentials.SUBQ_API_KEY])
	}
	if _, ok := stored[credentials.OPENAI_API_KEY]; ok {
		t.Fatal("an unmarked key was stored")
	}
}

func TestWizardKeysInvalidValueClearsAndStays(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "\r\r" + "1\r" + "bad key" + "\r" + "good-key-7" + "\r" + "5\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtContains(t, wtStrip(r.out), "A key is one line of text without spaces or control characters.")
	wtNoSecrets(t, r.out, "good-key-7", "bad key")
	if _, err := r.wizard.SaveCredentials(t.Context()); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	if got := wtStoredKeys(t, f.view.ConfigDir)[credentials.TYPESAFE_API_KEY]; got != "good-key-7" {
		t.Fatalf("stored TypeSafe key = %q, want the value typed after the field was cleared", got)
	}
}

func TestWizardKeysEmptyEnterAndEscKeepCurrent(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	wtStoreKeys(t, f.view.ConfigDir, map[string]string{credentials.TYPESAFE_API_KEY: "kept-ts-5501"})
	input := "\r\r" +
		"1\r" + "\r" + // empty Enter keeps the saved key
		"2\r" + "esc-typed" + wtEsc + // Esc leaves the field with no change
		"5\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtNoSecrets(t, r.out, "kept-ts-5501", "esc-typed")
	text := wtStrip(r.out)
	if strings.Contains(text, "new key ready") {
		t.Fatal("an empty or abandoned field marked a key")
	}
	if strings.Contains(text, "Quit setup?") {
		t.Fatal("Esc inside a hidden field opened the quit dialog")
	}
	wtContains(t, wtLastFrame(r.out), "Keys       TypeSafe saved · SubQ not set · OpenAI not set")
	if _, err := r.wizard.SaveCredentials(t.Context()); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	stored := wtStoredKeys(t, f.view.ConfigDir)
	if stored[credentials.TYPESAFE_API_KEY] != "kept-ts-5501" {
		t.Fatalf("saved key changed to %q", stored[credentials.TYPESAFE_API_KEY])
	}
	if _, ok := stored[credentials.SUBQ_API_KEY]; ok {
		t.Fatal("an abandoned field was stored")
	}
}

func TestWizardKeysOnlineCheckToggle(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	f.env[credentials.TYPESAFE_API_KEY] = "env-ts-6601"
	// Digit 4 moves to the checkbox and toggles it on, Enter toggles it off,
	// Space toggles it on again.
	r := f.run("\r\r" + "4" + "\r" + " " + "5\r" + "\r")
	r.requireApplied(t)
	wtNoSecrets(t, r.out, "env-ts-6601")
	wtContains(t, wtStrip(r.out), "[x]", "[ ]")
	wtContains(t, wtFlat(wtLastFrame(r.out)),
		"Keys TypeSafe environment · SubQ not set · OpenAI not set · online check after applying")
}

func TestWizardHiddenInputRefusesNonTerminalFile(t *testing.T) {
	t.Parallel()
	requireRefused := func(t *testing.T, r wtResult) {
		t.Helper()
		if r.err == nil || !strings.Contains(r.err.Error(), "hidden key input requires a terminal") {
			t.Fatalf("Select error = %v, want hidden input refusal", r.err)
		}
		if errors.Is(r.err, context.Canceled) {
			t.Fatal("refusal reported as cancellation")
		}
	}

	t.Run("visual temp file", func(t *testing.T) {
		t.Parallel()
		f := newWTFixture(t, false)
		path := filepath.Join(t.TempDir(), "input")
		if err := os.WriteFile(path, []byte("\r\r1\r"), 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		r := f.runContext(t.Context(), file)
		requireRefused(t, r)
		wtRequireRestored(t, r.out)
	})

	t.Run("plain pipe", func(t *testing.T) {
		t.Parallel()
		f := newWTFixture(t, false)
		f.plain = true
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		if _, err := writer.WriteString("\n\n1\n"); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		r := f.runContext(t.Context(), reader)
		requireRefused(t, r)
		wtRequireNoEscape(t, r.out)
	})
}

// ---------------------------------------------------------------------------
// R readable-review

func TestWizardReviewFreshDefault(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	r := f.run("\r\r\r\r")
	r.requireApplied(t)
	review := wtLastFrame(r.out)
	wtContains(t, review, "Review",
		"Hosts      Codex, Claude Code",
		"Preset     Recommended",
		"Keys       none set",
		"Settings   ~/config")
	wtInOrder(t, review, "Apply settings", "Back", "Cancel setup")
	flat := wtFlat(review)
	wtInOrder(t, flat, "Codex", "Main work", "Claude Code", "Main work")
	wtMatch(t, flat, `Main work `+wtM(wtSol)+` · max Routine `+wtM(wtSol)+` · medium Lookups `+wtM(wtSol)+` · low`, "codex rows")
	wtMatch(t, flat, `Main work `+wtM(wtOpus)+` · xhigh Lookups `+wtM(wtOpus)+` · low`, "claude rows without Routine")
	if n := strings.Count(review, "Routine"); n != 1 {
		t.Fatalf("Routine rows = %d, want 1 (Claude Code has no bounded_execution role)", n)
	}
	if strings.Contains(review, "pinned") || strings.Contains(review, "(was") || strings.Contains(review, "(changed)") {
		t.Fatalf("fresh Recommended review shows pins or changes:\n%s", review)
	}
	wtMatch(t, review, `Memory     \S`, "memory line for codex")
	wtMatch(t, review, `Context    \d{1,3}(?:,\d{3})* tokens`, "context line for codex")
}

func TestWizardReviewWithoutCodexOmitsCodexLines(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	f.host("codex").Selected = false
	r := f.run("\r\r\r\r")
	r.requireApplied(t)
	wtRequireHosts(t, r.sel, "claude")
	review := wtLastFrame(r.out)
	wtContains(t, review, "Hosts      Claude Code")
	for _, absent := range []string{"Routine", "Memory     ", "Context    "} {
		if strings.Contains(review, absent) {
			t.Fatalf("review without codex shows %q:\n%s", absent, review)
		}
	}
}

func TestWizardReviewInstalledShowsPreviousValues(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, true)
	f.role("codex", "implementer").Effective = workflow.ModelSelection{Model: wtSol, Effort: "xhigh"}
	f.role("codex", "code_reviewer").Effective = workflow.ModelSelection{Model: wtSol, Effort: "high"}
	f.role("codex", "executor").Effective = workflow.ModelSelection{Model: wtSol, Effort: "high"}
	// Models row 2 is Recommended (row 1 is Keep saved choices).
	r := f.run("2\r" + "\r" + "\r")
	r.requireApplied(t)
	if r.sel.Preset != workflow.PresetRecommended {
		t.Fatalf("preset = %q, want recommended", r.sel.Preset)
	}
	review := wtLastFrame(r.out)
	flat := wtFlat(review)
	// Rev 3: a line too long for the panel moves its note, without
	// parentheses, to the next line, so the parentheses are optional here.
	wtMatch(t, flat, `Main work `+wtM(wtSol)+` · max \(?changed\)?`, "row whose previous values differed")
	wtMatch(t, flat, `Routine `+wtM(wtSol)+` · medium \(?was `+wtM(wtSol)+` · high\)?`, "row with one previous value")
	if len(wtWasNote.FindAllString(flat, -1)) != 1 || len(wtChangedNote.FindAllString(flat, -1)) != 1 {
		t.Fatalf("unchanged rows carry change notes:\n%s", review)
	}
	for _, line := range wtLines(review, "Lookups") {
		if wtWasNote.MatchString(line) || wtChangedNote.MatchString(line) {
			t.Fatalf("unchanged Lookups row has a note: %q", line)
		}
	}
}

func TestWizardReviewKeysSummaryMixedSources(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	wtStoreKeys(t, f.view.ConfigDir, map[string]string{credentials.TYPESAFE_API_KEY: "stored-ts-7701"})
	f.env[credentials.SUBQ_API_KEY] = "env-subq-7702"
	r := f.run("\r\r\r\r")
	r.requireApplied(t)
	wtNoSecrets(t, r.out, "stored-ts-7701", "env-subq-7702")
	wtContains(t, wtLastFrame(r.out), "Keys       TypeSafe saved · SubQ environment · OpenAI not set")
}

func TestWizardReviewInvalidSelection(t *testing.T) {
	t.Parallel()
	invalid := func(t *testing.T) *wtFixture {
		f := newWTFixture(t, false)
		// Balanced pins codex substantive roles to sol/xhigh, which this role refuses.
		f.role("codex", "code_reviewer").Choices = []workflow.ModelChoice{
			{Model: wtSol, Efforts: []string{"high", "max"}},
			{Model: wtTerra, Efforts: []string{"medium", "high"}},
		}
		return f
	}
	toReview := "\r" + "2\r" + "\r" // Hosts, Balanced, Keys

	t.Run("offers only back and cancel", func(t *testing.T) {
		t.Parallel()
		f := invalid(t)
		r := f.run(toReview)
		if r.err == nil || errors.Is(r.err, context.Canceled) || !strings.Contains(r.err.Error(), "terminal input ended before setup finished") {
			t.Fatalf("Select error = %v, want end of input", r.err)
		}
		text := wtStrip(r.out)
		wtContains(t, text, "These choices can't be applied:", "Back", "Cancel setup")
		if strings.Contains(text, "Apply settings") {
			t.Fatal("an invalid review offers Apply settings")
		}
	})

	t.Run("second action cancels", func(t *testing.T) {
		t.Parallel()
		f := invalid(t)
		wtRequireCancelled(t, f.run(toReview+"2\r"), true)
	})

	t.Run("back then a valid preset applies", func(t *testing.T) {
		t.Parallel()
		f := invalid(t)
		r := f.run(toReview + "1\r" + wtLeft + "1\r" + "5\r" + "\r")
		r.requireApplied(t)
		if r.sel.Preset != workflow.PresetRecommended {
			t.Fatalf("preset = %q, want recommended", r.sel.Preset)
		}
	})
}

func TestWizardReviewBackKeepsAnswers(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "2\r" + // Hosts: uncheck Claude Code
		"2\r" + // Balanced
		"1\r" + "rb-secret-1" + "\r" + "5\r" + // mark TypeSafe, Continue
		"2\r" + // Review: Back
		"5\r" + // Keys: Continue
		"1\r" // Apply
	r := f.run(input)
	r.requireApplied(t)
	wtRequireHosts(t, r.sel, "codex")
	if r.sel.Preset != workflow.PresetBalanced {
		t.Fatalf("preset = %q, want balanced", r.sel.Preset)
	}
	wtNoSecrets(t, r.out, "rb-secret-1")
	wtContains(t, wtLastFrame(r.out), "Keys       TypeSafe new · SubQ not set · OpenAI not set")
	if _, err := r.wizard.SaveCredentials(t.Context()); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	if got := wtStoredKeys(t, f.view.ConfigDir)[credentials.TYPESAFE_API_KEY]; got != "rb-secret-1" {
		t.Fatalf("marked key lost after Back: stored %q", got)
	}
}

// ---------------------------------------------------------------------------
// R back-and-exit

func TestWizardBackKeepsHostsAndPreset(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "2\r" + "2\r" + // codex only, Balanced
		wtLeft + wtBackspace + // Keys -> Models -> Hosts
		"\r" + "\r" + "\r" + "\r" // every kept answer confirmed with Enter
	r := f.run(input)
	r.requireApplied(t)
	wtRequireHosts(t, r.sel, "codex")
	if r.sel.Preset != workflow.PresetBalanced {
		t.Fatalf("preset after going back = %q, want the earlier Balanced answer", r.sel.Preset)
	}
}

func TestWizardLeftOnFirstStepDoesNothing(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		installed bool
		input     string
		hosts     []string
	}{
		{"hosts", false, wtLeft + wtBackspace + "\r\r\r\r", []string{"codex", "claude"}},
		{"models when hosts are installed", true, wtLeft + wtBackspace + "\r\r\r", []string{"codex", "claude"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newWTFixture(t, test.installed)
			r := f.run(test.input)
			r.requireApplied(t)
			wtRequireHosts(t, r.sel, test.hosts...)
		})
	}
}

func TestWizardBackFromKeysKeepsMarkedKeyAndToggle(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "\r\r" +
		"1\r" + "bk-secret-9" + "\r" + // mark TypeSafe
		"4" + // online check on
		wtLeft + "\r" + // Keys -> Models, Recommended again
		"5\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtNoSecrets(t, r.out, "bk-secret-9")
	wtContains(t, wtFlat(wtLastFrame(r.out)),
		"Keys TypeSafe new · SubQ not set · OpenAI not set · online check after applying")
}

func TestWizardBackKeepsAdvancedEdits(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "\r" + "3\r" + "2\r" + "2\r" + "2\r" + "2\r" + // pin code reviewer sol/high
		wtLeft + "1\r" + // Done
		wtLeft + // Keys -> Models
		"3\r" + "1\r" + // Advanced, Done
		"\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtRequirePin(t, r.sel.Preferences, "codex", "code_reviewer", wtSol, "high")
}

func TestWizardQuitDialogKeepGoing(t *testing.T) {
	t.Parallel()
	openers := map[string]string{"esc": wtEsc, "q": "q"}
	dismissals := map[string]string{"enter": "\r", "esc": wtEsc, "n": "n", "left": wtLeft, "backspace": wtBackspace}
	for openName, opener := range openers {
		for dismissName, dismissal := range dismissals {
			t.Run(openName+" then "+dismissName, func(t *testing.T) {
				t.Parallel()
				f := newWTFixture(t, false)
				// Hosts: codex only; Models: open and dismiss the dialog, then Balanced.
				r := f.run("2\r" + opener + dismissal + "2\r" + "\r" + "\r")
				r.requireApplied(t)
				wtRequireHosts(t, r.sel, "codex")
				if r.sel.Preset != workflow.PresetBalanced {
					t.Fatalf("preset = %q, want balanced", r.sel.Preset)
				}
				wtContains(t, wtStrip(r.out), "Quit setup?", "Keep going")
			})
		}
	}
}

func TestWizardQuitPaths(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, input string }{
		{"esc on hosts then y", wtEsc + "y"},
		{"esc then y", "\r" + wtEsc + "y"},
		{"q then y", "\r" + "q" + "y"},
		{"esc then quit row", "\r" + wtEsc + wtDown + "\r"},
		{"q then digit 2 enter", "\r" + "q" + "2\r"},
		{"ctrl-c in quit dialog", "\r" + wtEsc + wtCtrlC},
		{"ctrl-c in hosts", wtCtrlC},
		{"ctrl-d in models", "\r" + wtCtrlD},
		{"ctrl-c in hidden field", "\r\r" + "1\r" + "ab" + wtCtrlC},
		{"ctrl-d in text field", "\r" + "3\r" + "4\r" + "1\r" + "12" + wtCtrlD},
		{"cancel setup on review", "\r\r\r" + "3\r"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newWTFixture(t, false)
			wtRequireCancelled(t, f.run(test.input), true)
		})
	}
}

func TestWizardContextCancelQuits(t *testing.T) {
	t.Parallel()
	for _, plain := range []bool{false, true} {
		t.Run(fmt.Sprintf("plain=%v", plain), func(t *testing.T) {
			t.Parallel()
			f := newWTFixture(t, false)
			f.plain = plain
			reader, writer := io.Pipe()
			t.Cleanup(func() { writer.Close() })
			config, stdout, stderr := f.config(reader)
			w := newSetupWizard(config, options{plain: plain})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			type outcome struct {
				sel workflow.ConfigurationSelection
				err error
			}
			done := make(chan outcome, 1)
			go func() {
				sel, err := w.Select(ctx, f.view)
				done <- outcome{sel, err}
			}()
			first := "\r"
			if plain {
				first = "\n"
			}
			// The write returns once the wizard has consumed the Hosts answer.
			if _, err := writer.Write([]byte(first)); err != nil {
				t.Fatal(err)
			}
			cancel()
			select {
			case o := <-done:
				wtRequireCancelled(t, wtResult{sel: o.sel, err: o.err, out: stderr.String(), stdout: stdout.String()}, !plain)
			case <-time.After(10 * time.Second):
				t.Fatal("Select did not return after its context was cancelled")
			}
		})
	}
}

// Digits move without confirming; arrows, j/k, Tab and Shift-Tab, Home and
// End move; Right does nothing.
func TestWizardMenuNavigationKeys(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, keys string
		want       workflow.Preset
	}{
		{"down", wtDown, workflow.PresetBalanced},
		{"j", "j", workflow.PresetBalanced},
		{"tab", "\t", workflow.PresetBalanced},
		{"digit then up", "3" + wtUp, workflow.PresetBalanced},
		{"digit then k", "3k", workflow.PresetBalanced},
		{"digit then shift-tab", "3" + wtShiftTab, workflow.PresetBalanced},
		{"digit then home", "3" + wtHome, workflow.PresetRecommended},
		{"end then up", wtEnd + wtUp, workflow.PresetBalanced},
		{"right does nothing", wtRight, workflow.PresetRecommended},
		{"digit 2", "2", workflow.PresetBalanced},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newWTFixture(t, false)
			r := f.run("\r" + test.keys + "\r" + "\r" + "\r")
			r.requireApplied(t)
			if r.sel.Preset != test.want || r.sel.Preferences != nil {
				t.Fatalf("selection = %+v, want preset %q", r.sel, test.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// R terminal-safety

func TestWizardVisualRestoresTerminalOnErrors(t *testing.T) {
	t.Parallel()
	t.Run("end of input", func(t *testing.T) {
		t.Parallel()
		f := newWTFixture(t, false)
		r := f.run("\r")
		if r.err == nil || errors.Is(r.err, context.Canceled) || !strings.Contains(r.err.Error(), "terminal input ended before setup finished") {
			t.Fatalf("Select error = %v, want end of input", r.err)
		}
		if r.sel.Hosts != nil || r.sel.Preset != "" || r.sel.Preferences != nil {
			t.Fatalf("failed Select returned a selection: %+v", r.sel)
		}
		wtRequireRestored(t, r.out)
		if r.stdout != "" {
			t.Fatalf("wizard wrote to config.Out: %q", r.stdout)
		}
	})
	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		f := newWTFixture(t, false)
		r := f.run("")
		if r.err == nil || !strings.Contains(r.err.Error(), "terminal input ended before setup finished") {
			t.Fatalf("Select error = %v, want end of input", r.err)
		}
		wtRequireRestored(t, r.out)
	})
}

// ---------------------------------------------------------------------------
// R plain-mode

func TestWizardPlainModeTriggers(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		plain bool
		env   map[string]string
	}{
		{"option", true, nil},
		{"TERM=dumb", false, map[string]string{"TERM": "dumb"}},
		{"NO_COLOR", false, map[string]string{"NO_COLOR": "1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newWTFixture(t, false)
			f.plain = test.plain
			maps.Copy(f.env, test.env)
			r := f.run("\n\n\n\n")
			r.requireApplied(t)
			wtRequireHosts(t, r.sel, "codex", "claude")
			if r.sel.Preset != workflow.PresetRecommended || r.sel.Preferences != nil {
				t.Fatalf("selection = %+v, want Recommended", r.sel)
			}
			wtRequireNoEscape(t, r.out)
			wtContains(t, r.out, "[x]")
			defaults := false
			for _, line := range wtLines(r.out, "Recommended") {
				defaults = defaults || strings.Contains(line, "*")
			}
			if !defaults {
				t.Fatalf("no * marks the default Recommended row:\n%s", wtTail(r.out))
			}
			// The review body is printed before the Apply/Back/Cancel menu.
			review := strings.LastIndex(r.out, "Preset     Recommended")
			apply := strings.LastIndex(r.out, "Apply settings")
			if review < 0 || apply < review || !strings.Contains(r.out, "Hosts      Codex, Claude Code") {
				t.Fatalf("review body not printed before the actions:\n%s", wtTail(r.out))
			}

			// R friendly-finish in plain mode.
			message := r.wizard.finishMessage(workflow.Object{}, workflow.Object{})
			wtRequireNoEscape(t, message)
			if first, _, _ := strings.Cut(message, "\n"); first != "Setup complete · "+wtVersion {
				t.Fatalf("plain finish first line = %q", first)
			}
		})
	}
}

func TestWizardPlainChecklist(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, input string
		installed   string
		want        []string
		message     bool
	}{
		{"commas", "1,2\n" + "\n" + "2\n" + "\n" + "\n\n\n", "", []string{"claude"}, true},
		{"spaces", "1 2\n" + "\n" + "2\n" + "\n" + "\n\n\n", "", []string{"claude"}, true},
		{"single toggle", "2\n" + "\n" + "\n\n\n", "", []string{"codex"}, false},
		{"fixed rows are not numbered", "1\n" + "\n" + "\n\n\n", "codex", []string{"codex", "claude"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newWTFixture(t, false)
			f.plain = true
			if test.installed != "" {
				f.host(test.installed).Installed = true
				f.host("claude").Selected = false
			}
			r := f.run(test.input)
			r.requireApplied(t)
			wtRequireHosts(t, r.sel, test.want...)
			wtRequireNoEscape(t, r.out)
			if test.message {
				wtContains(t, r.out, "Select at least one host to continue.")
			}
		})
	}
}

func TestWizardPlainBack(t *testing.T) {
	t.Parallel()
	t.Run("first step", func(t *testing.T) {
		t.Parallel()
		f := newWTFixture(t, true)
		f.plain = true
		r := f.run("b\n" + "2\n" + "\n" + "\n")
		r.requireApplied(t)
		wtContains(t, r.out, "This is the first step.")
		if r.sel.Preset != workflow.PresetRecommended {
			t.Fatalf("preset = %q, want recommended", r.sel.Preset)
		}
	})
	t.Run("keeps host checks", func(t *testing.T) {
		t.Parallel()
		f := newWTFixture(t, false)
		f.plain = true
		r := f.run("2\n" + "\n" + "b\n" + "\n" + "2\n" + "\n" + "\n")
		r.requireApplied(t)
		wtRequireHosts(t, r.sel, "codex")
		if r.sel.Preset != workflow.PresetBalanced {
			t.Fatalf("preset = %q, want balanced", r.sel.Preset)
		}
	})
}

func TestWizardPlainInvalidEntryAsksAgain(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	f.plain = true
	r := f.run("\n" + "9\n" + "zz\n" + "2\n" + "\n" + "\n")
	r.requireApplied(t)
	if r.sel.Preset != workflow.PresetBalanced {
		t.Fatalf("preset = %q, want balanced after invalid entries", r.sel.Preset)
	}
}

func TestWizardPlainQuit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, input string }{
		{"q", "\n" + "q\n"},
		{"ctrl-c", "\n" + "\x03\n"},
		{"ctrl-d", "\n" + "\x04\n"},
		{"esc", "\n" + "\x1b\n"},
		{"cancel setup on review", "\n\n\n" + "3\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newWTFixture(t, false)
			f.plain = true
			wtRequireCancelled(t, f.run(test.input), false)
		})
	}
}

func TestWizardPlainTextFields(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	f.plain = true
	input := "\n" + "3\n" + "4\n" + // Hosts, Advanced, Memory and context
		"1\n" + "abc\n" + "512000\n" + // invalid then valid context window
		"1\n" + "\n" + // empty line keeps 512000
		"6\n" + wtLuna + "\n" + // consolidation model
		"6\n" + "default\n" + // cleared to the release default
		"5\n" + "b\n" + // back out of the extraction field
		"b\n" + "1\n" + // memory menu -> advanced menu, Done
		"\n" + "\n" // Keys, Apply
	r := f.run(input)
	r.requireApplied(t)
	wtRequireNoEscape(t, r.out)
	wtContains(t, r.out, "Enter a positive whole number of tokens, or leave it empty.", "512,000 tokens")
	if r.sel.Preferences == nil || r.sel.Preferences.Shared == nil {
		t.Fatal("text field edits not stored")
	}
	shared := r.sel.Preferences.Shared
	if shared.ContextWindow == nil || *shared.ContextWindow != 512000 {
		t.Fatalf("context window = %v, want 512000", shared.ContextWindow)
	}
	if shared.ConsolidationModel != nil {
		t.Fatalf("\"default\" did not clear the consolidation model: %q", *shared.ConsolidationModel)
	}
	if shared.ExtractModel != nil {
		t.Fatalf("\"b\" changed the extraction model: %q", *shared.ExtractModel)
	}
}

func TestWizardPlainHiddenKey(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	f.plain = true
	r := f.run("\n\n" + "1\n" + "plain-secret-77\n" + "\n" + "\n")
	r.requireApplied(t)
	wtRequireNoEscape(t, r.out)
	wtNoSecrets(t, r.out, "plain-secret-77")
	if _, err := r.wizard.SaveCredentials(t.Context()); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	if got := wtStoredKeys(t, f.view.ConfigDir)[credentials.TYPESAFE_API_KEY]; got != "plain-secret-77" {
		t.Fatalf("stored TypeSafe key = %q", got)
	}
}

// ---------------------------------------------------------------------------
// R friendly-finish

func TestWizardFinishMessageVisual(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	input := "\r\r" +
		"1\r" + "fm-ts-secret" + "\r" +
		"2\r" + "fm-subq-secret" + "\r" +
		"5\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	if _, err := r.wizard.SaveCredentials(t.Context()); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	message := r.wizard.finishMessage(
		workflow.Object{"registrations": []string{"archify", "graphify", "docker-agent-run"}},
		workflow.Object{"status": "saved", "checks": map[string]string{
			"jev": "verified", "subq_snap": "unavailable", "openai_decisions": "missing",
		}},
	)
	wtNoSecrets(t, message, "fm-ts-secret", "fm-subq-secret")
	lines := strings.Split(wtStrip(message), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	if lines[0] != "✓ Setup complete · "+wtVersion {
		t.Fatalf("first line = %q", lines[0])
	}
	find := func(pattern string) int {
		t.Helper()
		re := regexp.MustCompile("^" + pattern + "$")
		for i, line := range lines {
			if re.MatchString(line) {
				return i
			}
		}
		t.Fatalf("finish message has no line matching %q:\n%s", pattern, strings.Join(lines, "\n"))
		return -1
	}
	codex := find(regexp.QuoteMeta(fmt.Sprintf("  %-12s main work ", "Codex")) + wtM(wtSol) + ` · max`)
	claude := find(regexp.QuoteMeta(fmt.Sprintf("  %-12s main work ", "Claude Code")) + wtM(wtOpus) + ` · xhigh`)
	skills := find(regexp.QuoteMeta(fmt.Sprintf("  %-12s 3 registered", "Skills")))
	keys := find(regexp.QuoteMeta(fmt.Sprintf("  %-12s saved TypeSafe, SubQ", "Keys")))
	check := find(regexp.QuoteMeta(fmt.Sprintf("  %-12s TypeSafe verified · SubQ unavailable · OpenAI missing", "Key check")))
	start := find(regexp.QuoteMeta("Start a fresh agent session to use these settings.") + ".*")
	find(regexp.QuoteMeta("If cw is not found, open a new terminal."))
	if !(0 < codex && codex < claude && claude < skills && skills < keys && keys < check && check < start) {
		t.Fatalf("finish lines out of order:\n%s", strings.Join(lines, "\n"))
	}
	if lines[start-1] != "" {
		t.Fatalf("no blank line before the fresh-session line:\n%s", strings.Join(lines, "\n"))
	}
}

func TestWizardFinishMessageOmitsOptionalLines(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, true)
	r := f.run("\r\r\r") // Keep saved choices, Keys, Apply
	r.requireApplied(t)

	message := wtStrip(r.wizard.finishMessage(workflow.Object{"registrations": []any{"archify", "graphify"}}, workflow.Object{"status": "unchanged"}))
	wtContains(t, message, "✓ Setup complete · "+wtVersion,
		fmt.Sprintf("  %-12s 2 registered", "Skills"),
		"Start a fresh agent session to use these settings.")
	for _, absent := range []string{"If cw is not found", "Key check", fmt.Sprintf("  %-12s saved", "Keys")} {
		if strings.Contains(message, absent) {
			t.Fatalf("finish message for an installed, unsaved run contains %q:\n%s", absent, message)
		}
	}

	bare := wtStrip(r.wizard.finishMessage(workflow.Object{}, workflow.Object{}))
	if strings.Contains(bare, "registered") {
		t.Fatalf("finish message without registrations has a Skills line:\n%s", bare)
	}
}

// ---------------------------------------------------------------------------
// R github-workers (contract rev 3)

var wtGitHubRoleNames = []string{"commit_execution", "pr_authoring_routine", "pr_submission"}

// newWTGitHubFixture adds the three GitHub worker roles to every host: same
// Default and Choices within a host, Profile "github", Scope
// bounded_execution. Codex default gpt-6-luna/high (choices luna, astra);
// Claude Code default claude-sonnet-5-5/medium (choices sonnet, haiku). Fresh
// Models rows: 1 Recommended, 2 Balanced, 3 Advanced, 4 Codex, 5 Claude Code;
// installed: 1 Keep, 2 Recommended, 3 Balanced, 4 Advanced, 5 Codex, 6 Claude
// Code. Codex roles menu: 1 implementer, 2 code reviewer, 3 executor,
// 4 explorer, 5 commit execution, 6 pr authoring routine, 7 pr submission,
// 8 substantive fallback.
func newWTGitHubFixture(t *testing.T, installed bool) *wtFixture {
	t.Helper()
	f := newWTFixture(t, installed)
	codexChoices := []workflow.ModelChoice{
		{Model: wtLuna, Efforts: []string{"low", "medium", "high"}},
		{Model: wtAstra, Efforts: []string{"medium", "high"}},
	}
	claudeChoices := []workflow.ModelChoice{
		{Model: wtSonnet, Efforts: []string{"low", "medium", "high"}},
		{Model: wtHaiku, Efforts: []string{"low"}},
	}
	for h := range f.view.Hosts {
		host := &f.view.Hosts[h]
		model, effort, choices := wtLuna, "high", codexChoices
		if host.ID == "claude" {
			model, effort, choices = wtSonnet, "medium", claudeChoices
		}
		for _, name := range wtGitHubRoleNames {
			role := wtRole(name, "bounded_execution", model, effort, choices)
			role.Profile = "github"
			if installed {
				role.Effective = role.Default
			}
			host.Roles = append(host.Roles, role)
		}
	}
	return f
}

func wtOv(model, effort string) *workflow.ModelOverride {
	return &workflow.ModelOverride{Model: &model, Effort: &effort}
}

func wtHostIDs(view workflow.ConfigurationView) []string {
	ids := make([]string, 0, len(view.Hosts))
	for _, host := range view.Hosts {
		ids = append(ids, host.ID)
	}
	return ids
}

func wtPFS(t *testing.T, view workflow.ConfigurationView, sel workflow.ConfigurationSelection) workflow.Preferences {
	t.Helper()
	prefs, err := workflow.PreferencesForSelection(view, sel)
	if err != nil {
		t.Fatalf("PreferencesForSelection(%+v): %v", sel, err)
	}
	return prefs
}

// wtSaveOverrides stores saved preferences holding exactly the given
// "host/role" overrides. The schema version is taken from
// PreferencesForSelection so the saved value is well formed.
func wtSaveOverrides(t *testing.T, f *wtFixture, overrides map[string]*workflow.ModelOverride) {
	t.Helper()
	base := wtPFS(t, f.view, workflow.ConfigurationSelection{Hosts: wtHostIDs(f.view), Preset: workflow.PresetRecommended})
	saved := workflow.Preferences{Version: base.Version, Hosts: map[string]workflow.HostPreferences{}}
	for key, override := range overrides {
		host, role, _ := strings.Cut(key, "/")
		hostPrefs := saved.Hosts[host]
		if hostPrefs.Roles == nil {
			hostPrefs.Roles = map[string]workflow.ModelOverride{}
		}
		hostPrefs.Roles[role] = *override
		saved.Hosts[host] = hostPrefs
	}
	f.view.Preferences = saved
}

func wtSetGitHubEffective(f *wtFixture, host, model, effort string) {
	for _, name := range wtGitHubRoleNames {
		f.role(host, name).Effective = workflow.ModelSelection{Model: model, Effort: effort}
	}
}

// wtCanon flattens preferences to "host/role" -> "model · effort" (only set
// overrides), the version and the shared settings, so comparisons do not
// depend on nil versus empty maps.
func wtCanon(t *testing.T, prefs workflow.Preferences) map[string]string {
	t.Helper()
	out := map[string]string{"version": fmt.Sprint(prefs.Version)}
	for host, hostPrefs := range prefs.Hosts {
		for role, override := range hostPrefs.Roles {
			if override.Model != nil || override.Effort != nil {
				out[host+"/"+role] = wtStr(override.Model) + " · " + wtStr(override.Effort)
			}
		}
		if fallback := hostPrefs.Fallback; fallback != nil && (fallback.Model != nil || fallback.Effort != nil) {
			out[host+"/substantive_fallback"] = wtStr(fallback.Model) + " · " + wtStr(fallback.Effort)
		}
	}
	if prefs.Shared != nil {
		encoded, err := json.Marshal(prefs.Shared)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != "{}" {
			out["shared"] = string(encoded)
		}
	}
	return out
}

func wtGitHubEdits(host string, override *workflow.ModelOverride) map[string]*workflow.ModelOverride {
	edits := map[string]*workflow.ModelOverride{}
	for _, name := range wtGitHubRoleNames {
		edits[host+"/"+name] = override
	}
	return edits
}

// wtRequirePrefs compares PreferencesForSelection(view, sel) with
// PreferencesForSelection(view, base) after applying edits ("host/role" ->
// override; nil means no override).
func wtRequirePrefs(t *testing.T, view workflow.ConfigurationView, sel, base workflow.ConfigurationSelection, edits map[string]*workflow.ModelOverride) {
	t.Helper()
	got := wtCanon(t, wtPFS(t, view, sel))
	want := wtCanon(t, wtPFS(t, view, base))
	for key, override := range edits {
		if override == nil {
			delete(want, key)
			continue
		}
		want[key] = wtStr(override.Model) + " · " + wtStr(override.Effort)
	}
	if !maps.Equal(got, want) {
		t.Fatalf("effective preferences differ\n got: %v\nwant: %v\nselection: %+v", got, want, sel)
	}
}

func wtLastFrameWith(t *testing.T, raw, marker string) string {
	t.Helper()
	frames := wtFrames(raw)
	for i := len(frames) - 1; i >= 0; i-- {
		if strings.Contains(frames[i], marker) {
			return frames[i]
		}
	}
	t.Fatalf("no frame contains %q; output tail:\n%s", marker, wtTail(wtStrip(raw)))
	return ""
}

func wtHasLine(t *testing.T, text, pattern, what string) {
	t.Helper()
	re := regexp.MustCompile(pattern)
	for line := range strings.SplitSeq(text, "\n") {
		if re.MatchString(line) {
			return
		}
	}
	t.Fatalf("%s: no line matches %q in:\n%s", what, pattern, wtTail(text))
}

func wtNoLine(t *testing.T, text, pattern, what string) {
	t.Helper()
	re := regexp.MustCompile(pattern)
	for line := range strings.SplitSeq(text, "\n") {
		if re.MatchString(line) {
			t.Fatalf("%s: unexpected line %q", what, line)
		}
	}
}

func TestWizardGitHubRowsWithoutChangesKeepPresetSelection(t *testing.T) {
	t.Parallel()

	t.Run("fresh recommended", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, false)
		r := f.run("\r\r\r\r")
		r.requireApplied(t)
		wtRequireHosts(t, r.sel, "codex", "claude")
		if r.sel.Preset != workflow.PresetRecommended || r.sel.Preferences != nil {
			t.Fatalf("selection = %+v, want Recommended with nil preferences", r.sel)
		}

		models := wtFirstFrame(t, r.out, "GitHub workers")
		wtContains(t, models, "commits, routine PRs, PR submission")
		wtInOrder(t, models, "Recommended", "Balanced", "Advanced", "GitHub workers")
		wtHasLine(t, models, `Codex.*`+regexp.QuoteMeta(wtLuna+" · high"), "Codex GitHub row value")
		wtHasLine(t, models, `Claude Code.*`+wtM(wtSonnet)+` · medium`, "Claude Code GitHub row value")
		wtNoLine(t, models, `Codex.*`+regexp.QuoteMeta(wtLuna)+`.*pinned`, "unpinned Codex GitHub row")
		if strings.Contains(wtFlat(wtFirstFrame(t, r.out, "Adviser keys")), "GitHub pinned") {
			t.Fatal("Models summary says GitHub pinned without any GitHub pin")
		}

		// R readable-review: GitHub row between Main work and Routine.
		flat := wtFlat(wtLastFrame(r.out))
		wtMatch(t, flat, `Main work `+wtM(wtSol)+` · max GitHub `+regexp.QuoteMeta(wtLuna)+` · high Routine `+wtM(wtSol)+` · medium Lookups `+wtM(wtSol)+` · low`, "codex rows with GitHub")
		wtMatch(t, flat, `Main work `+wtM(wtOpus)+` · xhigh GitHub `+wtM(wtSonnet)+` · medium Lookups `+wtM(wtOpus)+` · low`, "claude rows with GitHub")
		if strings.Contains(flat, "pinned") {
			t.Fatalf("review shows pins without any:\n%s", flat)
		}
		wtContains(t, wtLastFrame(r.out), "  GitHub     ")
	})

	t.Run("installed balanced", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, true)
		r := f.run("3\r" + "\r" + "\r")
		r.requireApplied(t)
		if r.sel.Preset != workflow.PresetBalanced || r.sel.Preferences != nil {
			t.Fatalf("selection = %+v, want Balanced with nil preferences", r.sel)
		}
	})
}

func TestWizardGitHubChoicePinsEveryHostRole(t *testing.T) {
	t.Parallel()
	f := newWTGitHubFixture(t, false)
	input := "\r" + // Hosts
		"4\r" + // Codex GitHub row
		"3\r" + // gpt-6-astra (1 Release default, 2 luna, 3 astra)
		"2\r" + // medium (1 Release default, 2 medium, 3 high)
		"\r" + // cursor is back on Recommended
		"\r" + "\r" // Keys, Apply
	r := f.run(input)
	r.requireApplied(t)
	wtRequireHosts(t, r.sel, "codex", "claude")
	wtRequirePrefs(t, f.view, r.sel,
		workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended},
		wtGitHubEdits("codex", wtOv(wtAstra, "medium")))

	text := wtStrip(r.out)
	wtContains(t, text, "Release default", wtLuna, wtAstra)
	models := wtLastFrameWith(t, r.out, "GitHub workers")
	wtHasLine(t, models, `Codex.*`+regexp.QuoteMeta(wtAstra+" · medium · pinned"), "Codex row after the choice")
	wtHasLine(t, models, `Claude Code.*`+wtM(wtSonnet)+` · medium`, "Claude Code row unchanged")
	wtNoLine(t, models, `Claude Code.*pinned`, "Claude Code row unchanged")
	wtMatch(t, wtFlat(wtFirstFrame(t, r.out, "Adviser keys")), `Models Recommended.* · GitHub pinned`, "Models summary note")

	flat := wtFlat(wtLastFrame(r.out))
	wtMatch(t, flat, `GitHub `+regexp.QuoteMeta(wtAstra)+` · medium · pinned`, "codex GitHub review row")
	wtMatch(t, flat, `GitHub `+wtM(wtSonnet)+` · medium`, "claude GitHub review row")
	wtNoMatch(t, flat, `GitHub `+wtM(wtSonnet)+` · medium · pinned`, "claude GitHub review row")
	for _, label := range []string{"commit execution", "pr authoring routine", "pr submission"} {
		if strings.Contains(flat, label) {
			t.Fatalf("GitHub role %q matches its row but is listed as an exception:\n%s", label, flat)
		}
	}
}

func TestWizardGitHubChoiceReturnsToCurrentPreset(t *testing.T) {
	t.Parallel()
	f := newWTGitHubFixture(t, false)
	input := "\r" + "2\r" + // Hosts, Balanced
		wtLeft + // Keys -> Models (Balanced is current)
		"4\r" + "2\r" + "2\r" + // Codex GitHub: luna, low
		"\r" + // Enter on the preset row: Balanced
		"\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtRequirePrefs(t, f.view, r.sel,
		workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetBalanced},
		wtGitHubEdits("codex", wtOv(wtLuna, "low")))
}

func TestWizardGitHubReleaseDefaultUnpins(t *testing.T) {
	t.Parallel()
	f := newWTGitHubFixture(t, true)
	wtSetGitHubEffective(f, "codex", wtAstra, "medium")
	wtSaveOverrides(t, f, wtGitHubEdits("codex", wtOv(wtAstra, "medium")))
	input := "5\r" + // Codex GitHub row (installed: 1 Keep ... 4 Advanced)
		"1\r" + "1\r" + // Release default model and effort
		"\r" + // cursor back on Keep saved choices
		"\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtRequirePrefs(t, f.view, r.sel,
		workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}},
		wtGitHubEdits("codex", nil))

	models := wtFirstFrame(t, r.out, "GitHub workers")
	wtHasLine(t, models, `Codex.*`+regexp.QuoteMeta(wtAstra+" · medium · pinned"), "saved pin shown before the choice")
	after := wtLastFrameWith(t, r.out, "GitHub workers")
	wtHasLine(t, after, `Codex.*`+regexp.QuoteMeta(wtLuna+" · high"), "Codex row after Release default")
	wtNoLine(t, after, `Codex.*`+regexp.QuoteMeta(wtLuna)+`.*pinned`, "Codex row after Release default")
	if strings.Contains(wtFlat(wtFirstFrame(t, r.out, "Adviser keys")), "GitHub pinned") {
		t.Fatal("Models summary says GitHub pinned after unpinning")
	}
	// The contract's own example of the installed GitHub review row.
	wtMatch(t, wtFlat(wtLastFrame(r.out)),
		`GitHub `+regexp.QuoteMeta(wtLuna)+` · high \(?was `+regexp.QuoteMeta(wtAstra)+` · medium\)?`, "GitHub review row with previous value")
}

func TestWizardGitHubPresetKeepsSavedOverrides(t *testing.T) {
	t.Parallel()
	f := newWTGitHubFixture(t, true)
	wtSaveOverrides(t, f, map[string]*workflow.ModelOverride{
		"codex/commit_execution": wtOv(wtAstra, "medium"),
		"codex/pr_submission":    wtOv(wtLuna, "low"),
		"codex/executor":         wtOv(wtSol, "high"), // not a GitHub role: the preset replaces it
	})
	r := f.run("2\r" + "\r" + "\r") // Recommended, Keys, Apply
	r.requireApplied(t)
	if r.sel.Preset != "" || r.sel.Preferences == nil {
		t.Fatalf("selection = %+v, want empty preset with preferences", r.sel)
	}
	wtRequirePrefs(t, f.view, r.sel,
		workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended},
		map[string]*workflow.ModelOverride{
			"codex/commit_execution":     wtOv(wtAstra, "medium"),
			"codex/pr_submission":        wtOv(wtLuna, "low"),
			"codex/pr_authoring_routine": nil,
		})
	wtHasLine(t, wtFirstFrame(t, r.out, "GitHub workers"), `Codex.*differs per role`, "Codex row with mixed saved values")
	wtMatch(t, wtFlat(wtFirstFrame(t, r.out, "Adviser keys")), ` · GitHub pinned`, "Models summary note")
}

func TestWizardGitHubUnselectedHostUntouched(t *testing.T) {
	t.Parallel()
	f := newWTGitHubFixture(t, false)
	wtSaveOverrides(t, f, map[string]*workflow.ModelOverride{"claude/commit_execution": wtOv(wtHaiku, "low")})
	input := "2\r" + // Hosts: uncheck Claude Code
		"4\r" + "2\r" + "4\r" + // Codex GitHub row (the only one): luna, high
		"\r" + "\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	wtRequireHosts(t, r.sel, "codex")
	wtRequirePrefs(t, f.view, r.sel,
		workflow.ConfigurationSelection{Hosts: []string{"codex"}, Preset: workflow.PresetRecommended},
		wtGitHubEdits("codex", wtOv(wtLuna, "high")))
	models := wtFirstFrame(t, r.out, "GitHub workers")
	if lines := wtLines(models, "Claude Code"); len(lines) != 0 {
		t.Fatalf("Models shows a GitHub row for an unselected host: %q", lines)
	}
}

func TestWizardGitHubMenuNavigation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, input string
		edits       map[string]*workflow.ModelOverride
	}{
		{"backspace in effort menu returns to model menu", "\r" + "4\r" + "3\r" + wtBackspace + "2\r" + "2\r" + "\r\r\r",
			wtGitHubEdits("codex", wtOv(wtLuna, "low"))},
		{"down skips the heading", "\r" + "3" + wtDown + "\r" + "3\r" + "3\r" + "\r\r\r",
			wtGitHubEdits("codex", wtOv(wtAstra, "high"))},
		{"digit 5 picks claude code", "\r" + "5\r" + "2\r" + "3\r" + "\r\r\r",
			wtGitHubEdits("claude", wtOv(wtSonnet, "medium"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newWTGitHubFixture(t, false)
			r := f.run(test.input)
			r.requireApplied(t)
			wtRequirePrefs(t, f.view, r.sel,
				workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended},
				test.edits)
		})
	}

	// Rev 4 feedback: the saved GitHub pin makes a "Release default on Left"
	// defect change the Keep selection.
	for name, back := range map[string]string{"left": wtLeft, "backspace": wtBackspace} {
		t.Run(name+" in model menu changes nothing", func(t *testing.T) {
			t.Parallel()
			f := newWTGitHubFixture(t, true)
			wtSetGitHubEffective(f, "codex", wtAstra, "medium")
			wtSaveOverrides(t, f, wtGitHubEdits("codex", wtOv(wtAstra, "medium")))
			// Installed rows: 1 Keep ... 5 Codex. Open, leave, then Keep.
			r := f.run("5\r" + back + "1\r" + "\r" + "\r")
			r.requireApplied(t)
			if r.sel.Preset != "" || r.sel.Preferences != nil {
				t.Fatalf("selection = %+v, want Keep (empty preset, nil preferences)", r.sel)
			}
			wtRequirePrefs(t, f.view, r.sel, workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}}, nil)
			after := wtLastFrameWith(t, r.out, "GitHub workers")
			wtHasLine(t, after, `Codex.*`+regexp.QuoteMeta(wtAstra+" · medium · pinned"), "Codex row after leaving the model menu")
		})
	}

	t.Run("up from a host row reaches advanced", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, false)
		r := f.run("\r" + "4" + wtUp + "\r" + "1\r" + "\r" + "\r")
		r.requireApplied(t)
		wtRequireNoRolePins(t, r.sel.Preferences)
	})
}

func TestWizardGitHubAdvancedShowsAndKeepsPins(t *testing.T) {
	t.Parallel()
	f := newWTGitHubFixture(t, false)
	input := "\r" +
		"4\r" + "3\r" + "2\r" + // Codex GitHub: astra, medium
		"3\r" + "2\r" + // Advanced, Codex roles
		"7\r" + "2\r" + "2\r" + // pr submission: luna, low
		wtLeft + wtLeft + // roles -> advanced menu -> Models
		"1\r" + // Recommended
		"\r" + "\r"
	r := f.run(input)
	r.requireApplied(t)
	roles := wtFirstFrame(t, r.out, "code reviewer") // the Codex roles menu
	wtHasLine(t, roles, `pr submission.*`+regexp.QuoteMeta(wtAstra+" · medium"), "Advanced shows the GitHub row choice")
	wtHasLine(t, roles, `commit execution.*`+regexp.QuoteMeta(wtAstra+" · medium"), "Advanced shows the GitHub row choice")
	wtRequirePrefs(t, f.view, r.sel,
		workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended},
		map[string]*workflow.ModelOverride{
			"codex/commit_execution":     wtOv(wtAstra, "medium"),
			"codex/pr_authoring_routine": wtOv(wtAstra, "medium"),
			"codex/pr_submission":        wtOv(wtLuna, "low"),
		})
}

func TestWizardGitHubPlain(t *testing.T) {
	t.Parallel()
	f := newWTGitHubFixture(t, false)
	f.plain = true
	input := "\n" + // Hosts
		"4\n" + "3\n" + "2\n" + // Codex GitHub: astra, medium
		"\n" + // default is the preset row again (Recommended)
		"\n" + "\n"
	r := f.run(input)
	r.requireApplied(t)
	wtRequireNoEscape(t, r.out)
	wtRequirePrefs(t, f.view, r.sel,
		workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended},
		wtGitHubEdits("codex", wtOv(wtAstra, "medium")))
	wtHasLine(t, r.out, `^\D*GitHub workers`, "unnumbered GitHub heading")
	wtHasLine(t, r.out, `\b4\b.*Codex.*`+regexp.QuoteMeta(wtLuna+" · high"), "Codex GitHub row numbered 4")
	wtHasLine(t, r.out, `\b5\b.*Claude Code.*`+wtM(wtSonnet)+` · medium`, "Claude Code GitHub row numbered 5")
	wtHasLine(t, r.out, `Codex.*`+regexp.QuoteMeta(wtAstra+" · medium · pinned"), "Codex row after the choice")
}

// ---------------------------------------------------------------------------
// Contract rev 4 additions

// wtRetired is a model the release no longer offers for any role.
const wtRetired = "gpt-6-retired"

func wtCanonHost(hostPrefs workflow.HostPreferences) map[string]string {
	out := map[string]string{}
	for role, override := range hostPrefs.Roles {
		if override.Model != nil || override.Effort != nil {
			out[role] = wtStr(override.Model) + " · " + wtStr(override.Effort)
		}
	}
	if fallback := hostPrefs.Fallback; fallback != nil && (fallback.Model != nil || fallback.Effort != nil) {
		out["substantive_fallback"] = wtStr(fallback.Model) + " · " + wtStr(fallback.Effort)
	}
	return out
}

// R github-workers: an unselected host is never changed, also on the
// Advanced path (the contract's example).
func TestWizardGitHubUnselectedHostAdvancedPath(t *testing.T) {
	t.Parallel()
	choose := "\r" + "5\r" + "2\r" + "2\r"  // Hosts; Claude Code GitHub row: sonnet, low
	uncheck := wtLeft + "2\r"               // Models -> Hosts; uncheck Claude Code
	advanced := "3\r" + "1\r" + "\r" + "\r" // Advanced, Done, Continue, Apply

	t.Run("nothing saved", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, false)
		r := f.run(choose + uncheck + advanced)
		r.requireApplied(t)
		wtRequireHosts(t, r.sel, "codex")
		if r.sel.Preferences == nil {
			t.Fatal("Advanced returned nil preferences")
		}
		if hostPrefs, ok := r.sel.Preferences.Hosts["claude"]; ok {
			t.Fatalf("unselected Claude Code has an entry %v; nothing was saved for it", wtCanonHost(hostPrefs))
		}
		wtContains(t, wtLastFrame(r.out), "Preset     Advanced · release defaults")
	})

	t.Run("saved value kept exactly", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, false)
		wtSaveOverrides(t, f, map[string]*workflow.ModelOverride{"claude/commit_execution": wtOv(wtHaiku, "low")})
		r := f.run(choose + uncheck + advanced)
		r.requireApplied(t)
		wtRequireHosts(t, r.sel, "codex")
		if r.sel.Preferences == nil {
			t.Fatal("Advanced returned nil preferences")
		}
		got := wtCanonHost(r.sel.Preferences.Hosts["claude"])
		want := map[string]string{"commit_execution": wtHaiku + " · low"}
		if !maps.Equal(got, want) {
			t.Fatalf("unselected Claude Code preferences = %v, want the saved %v", got, want)
		}
	})

	// Rev 5: visiting Advanced (left with Done or Back) while Claude Code is
	// unchecked must not lose the session's choice. A saved Claude value makes
	// a defect that restores saved values visible.
	recheck := wtLeft + "2\r" // Models -> Hosts, check Claude Code again
	for _, test := range []struct{ name, between string }{
		{"checked again applies the choice", ""},
		{"checked again after advanced done", "3\r" + "1\r" + wtLeft}, // Advanced, Done, Keys -> Models
		{"checked again after advanced back", "3\r" + wtLeft},         // Advanced, Back to Models
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newWTGitHubFixture(t, false)
			wtSaveOverrides(t, f, map[string]*workflow.ModelOverride{"claude/commit_execution": wtOv(wtHaiku, "low")})
			// ... then Recommended, Continue, Apply.
			r := f.run(choose + uncheck + test.between + recheck + "1\r" + "\r" + "\r")
			r.requireApplied(t)
			wtRequireHosts(t, r.sel, "codex", "claude")
			models := wtLastFrameWith(t, r.out, "GitHub workers")
			wtHasLine(t, models, `Claude Code.*`+wtM(wtSonnet)+` · low · pinned`, "Claude Code row keeps the session's choice")
			wtRequirePrefs(t, f.view, r.sel,
				workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended},
				wtGitHubEdits("claude", wtOv(wtSonnet, "low")))
		})
	}
}

// R github-workers: a saved GitHub override the release no longer offers gives
// way to a preset; on Keep the row shows " · unavailable".
func TestWizardGitHubUnavailableSavedOverride(t *testing.T) {
	t.Parallel()

	t.Run("preset applies its value and keeps valid overrides", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, true)
		wtSaveOverrides(t, f, map[string]*workflow.ModelOverride{
			"codex/commit_execution": wtOv(wtRetired, "high"),
			"codex/pr_submission":    wtOv(wtLuna, "low"),
		})
		r := f.run("2\r" + "\r" + "\r") // Recommended, Continue, Apply
		r.requireApplied(t)
		wtRequirePrefs(t, f.view, r.sel,
			workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended},
			map[string]*workflow.ModelOverride{
				"codex/commit_execution":     nil,
				"codex/pr_authoring_routine": nil,
				"codex/pr_submission":        wtOv(wtLuna, "low"),
			})
	})

	t.Run("only unavailable overrides: balanced still applies", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, true)
		wtSaveOverrides(t, f, wtGitHubEdits("codex", wtOv(wtRetired, "high")))
		r := f.run("3\r" + "\r" + "\r") // Balanced, Continue, Apply
		r.requireApplied(t)
		wtRequirePrefs(t, f.view, r.sel,
			workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetBalanced},
			wtGitHubEdits("codex", nil))
	})

	t.Run("keep shows unavailable", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, true)
		wtSaveOverrides(t, f, wtGitHubEdits("codex", wtOv(wtRetired, "high")))
		input := "\r" + "\r" + // Keep, Continue: the review cannot apply
			"1\r" + wtLeft + // Back to Keys, then Models
			"2\r" + "5\r" + "\r" // Recommended, Continue, Apply
		r := f.run(input)
		r.requireApplied(t)
		frames := wtFrames(r.out)
		invalid := -1
		for i, frame := range frames {
			if strings.Contains(frame, "These choices can't be applied:") {
				invalid = i
				break
			}
		}
		if invalid < 0 {
			t.Fatalf("Keep with an unavailable saved override was offered as applicable:\n%s", wtTail(wtStrip(r.out)))
		}
		models := ""
		for _, frame := range frames[invalid+1:] {
			if strings.Contains(frame, "GitHub workers") {
				models = frame
				break
			}
		}
		if models == "" {
			t.Fatal("Models was not shown again after Back")
		}
		wtHasLine(t, models, `Codex.*`+regexp.QuoteMeta(wtRetired+" · high")+`.* · unavailable`, "Codex row with an unavailable saved value")
		wtNoLine(t, models, `Claude Code.*unavailable`, "Claude Code row has no unavailable saved value")
		wtRequirePrefs(t, f.view, r.sel,
			workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended},
			wtGitHubEdits("codex", nil))
	})
}

// R plain-mode: a lone "b" in the hidden key field goes back without marking.
func TestWizardPlainHiddenKeyBack(t *testing.T) {
	t.Parallel()
	f := newWTFixture(t, false)
	f.plain = true
	r := f.run("\n\n" + "1\n" + "b\n" + "\n" + "\n") // TypeSafe field, b, Continue, Apply
	r.requireApplied(t)
	wtRequireNoEscape(t, r.out)
	if strings.Contains(r.out, "new key ready") {
		t.Fatal(`"b" in the hidden key field marked a key`)
	}
	wtContains(t, r.out, "Keys       none set")
	if _, err := r.wizard.SaveCredentials(t.Context()); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	if got, ok := wtStoredKeys(t, f.view.ConfigDir)[credentials.TYPESAFE_API_KEY]; ok {
		t.Fatalf(`"b" was stored as a key (%d bytes)`, len(got))
	}
}

// Keys / R plain-mode: end of input inside any field is the non-cancellation
// end-of-input error.
func TestWizardEndOfInputInsideFields(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, input string
		plain       bool
	}{
		{"visual hidden typed", "\r\r" + "1\r" + "abc", false},
		{"visual hidden empty", "\r\r" + "1\r", false},
		{"visual text typed", "\r" + "3\r" + "4\r" + "1\r" + "12", false},
		{"visual text empty", "\r" + "3\r" + "4\r" + "1\r", false},
		{"plain hidden partial line", "\n\n" + "1\n" + "abc", true},
		{"plain hidden empty", "\n\n" + "1\n", true},
		{"plain text partial line", "\n" + "3\n" + "4\n" + "1\n" + "12", true},
		{"plain text empty", "\n" + "3\n" + "4\n" + "1\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newWTFixture(t, false)
			f.plain = test.plain
			r := f.run(test.input)
			if r.err == nil || !strings.Contains(r.err.Error(), "terminal input ended before setup finished") {
				t.Fatalf("Select error = %v, want end of input", r.err)
			}
			if errors.Is(r.err, context.Canceled) || ExitCode(r.err) == 130 {
				t.Fatalf("end of input reported as cancellation: %v (exit %d)", r.err, ExitCode(r.err))
			}
			if strings.Contains(r.out, "Setup cancelled.") {
				t.Fatal("end of input printed the cancel message")
			}
			if r.stdout != "" {
				t.Fatalf("wizard wrote to config.Out: %q", r.stdout)
			}
			if test.plain {
				wtRequireNoEscape(t, r.out)
			} else {
				wtRequireRestored(t, r.out)
			}
		})
	}
}

// R plain-mode: an invalid checklist line toggles nothing.
func TestWizardPlainChecklistInvalidLineTogglesNothing(t *testing.T) {
	t.Parallel()
	for _, line := range []string{"2 x", "2 9", "2,x"} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()
			f := newWTFixture(t, false)
			f.plain = true
			r := f.run(line + "\n" + "\n" + "\n\n\n") // invalid line, continue, Models, Keys, Apply
			r.requireApplied(t)
			wtRequireHosts(t, r.sel, "codex", "claude")
		})
	}
}

// ---------------------------------------------------------------------------
// Contract rev 6: which Advanced visits change a host's GitHub choice

// Opening Advanced and leaving it without editing GitHub roles changes nothing.
func TestWizardGitHubUneditedAdvancedVisitChangesNothing(t *testing.T) {
	t.Parallel()
	// Leaving with Done goes on to Keys, so Left returns to Models; leaving with
	// Back returns to Models directly.
	leaves := map[string]string{"done": "1\r" + wtLeft, "back": wtLeft}

	for name, leave := range leaves {
		t.Run("unoffered saved value still gives way to a preset after "+name, func(t *testing.T) {
			t.Parallel()
			f := newWTGitHubFixture(t, true)
			wtSaveOverrides(t, f, wtGitHubEdits("codex", wtOv(wtRetired, "high")))
			// Installed rows: 1 Keep, 2 Recommended, 3 Balanced, 4 Advanced.
			r := f.run("4\r" + leave + "2\r" + "\r" + "\r") // Advanced, leave, Recommended, Continue, Apply
			r.requireApplied(t)
			if r.sel.Preset != workflow.PresetRecommended || r.sel.Preferences != nil {
				t.Fatalf("selection = %+v, want Recommended with nil preferences as when chosen directly", r.sel)
			}
		})

		t.Run("row choice survives an unedited visit ("+name+")", func(t *testing.T) {
			t.Parallel()
			f := newWTGitHubFixture(t, false)
			input := "\r" + "4\r" + "3\r" + "2\r" + // Codex GitHub row: astra, medium
				"3\r" + leave + // Advanced, leave without edits
				"1\r" + "\r" + "\r" // Recommended, Continue, Apply
			r := f.run(input)
			r.requireApplied(t)
			wtRequirePrefs(t, f.view, r.sel,
				workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended},
				wtGitHubEdits("codex", wtOv(wtAstra, "medium")))
		})
	}
}

// Editing a host's GitHub roles in Advanced makes the edited values that
// host's choice and replaces an earlier row choice, also while the host is
// unchecked (the contract's example).
func TestWizardGitHubAdvancedEditReplacesRowChoice(t *testing.T) {
	t.Parallel()
	choose := "\r" + "5\r" + "2\r" + "2\r" // Hosts; Claude Code GitHub row: sonnet, low
	uncheck := wtLeft + "2\r"              // Models -> Hosts; uncheck Claude Code
	// Advanced menu: 1 Done, 2 Codex roles, 3 Claude Code roles. Claude Code
	// roles: 1 implementer, 2 explorer, 3 commit execution, 4 pr authoring
	// routine, 5 pr submission, 6 substantive fallback. Model menu: 1 Release
	// default, 2 sonnet, 3 haiku; haiku offers only low (no Release default
	// effort, the role default medium is not offered).
	haiku := "3\r" + "1\r"
	finish := wtLeft + "1\r" + // roles -> advanced menu, Done
		wtLeft + wtLeft + "2\r" + // Keys -> Models -> Hosts, check Claude Code again
		"1\r" + "\r" + "\r" // Recommended, Continue, Apply
	base := workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended}

	t.Run("every GitHub role edited while unchecked", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, false)
		edits := "3\r" + "3\r" + "3\r" + haiku + "4\r" + haiku + "5\r" + haiku
		r := f.run(choose + uncheck + edits + finish)
		r.requireApplied(t)
		wtRequireHosts(t, r.sel, "codex", "claude")
		wtRequirePrefs(t, f.view, r.sel, base, wtGitHubEdits("claude", wtOv(wtHaiku, "low")))
		models := wtLastFrameWith(t, r.out, "GitHub workers")
		wtHasLine(t, models, `Claude Code.*`+regexp.QuoteMeta(wtHaiku+" · low · pinned"), "Claude Code row shows the Advanced values")
	})

	t.Run("one GitHub role edited while unchecked", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, false)
		edits := "3\r" + "3\r" + "4\r" + haiku // Advanced, Claude Code roles, pr authoring routine
		r := f.run(choose + uncheck + edits + finish)
		r.requireApplied(t)
		wtRequireHosts(t, r.sel, "codex", "claude")
		got := wtCanon(t, wtPFS(t, f.view, r.sel))
		want := wtCanon(t, wtPFS(t, f.view, base))
		if got["claude/pr_authoring_routine"] != wtHaiku+" · low" {
			t.Fatalf("edited role = %q, want the Advanced value %s · low", got["claude/pr_authoring_routine"], wtHaiku)
		}
		rowChoice := wtSonnet + " · low"
		for _, role := range wtGitHubRoleNames {
			key := "claude/" + role
			if got[key] == rowChoice {
				t.Fatalf("%s kept the earlier row choice %q instead of the Advanced values", key, rowChoice)
			}
			delete(got, key)
			delete(want, key)
		}
		if !maps.Equal(got, want) {
			t.Fatalf("roles outside Claude Code's GitHub roles differ from Recommended\n got: %v\nwant: %v", got, want)
		}
	})
}

// Contract rev 7, "not selected" Example 2: Advanced shows the row choice while
// Claude Code is selected; Claude Code is unchecked afterwards and must not be
// changed.
func TestWizardGitHubRowChoiceSeenInAdvancedThenUnchecked(t *testing.T) {
	t.Parallel()
	input := "\r" + // Hosts
		"5\r" + "2\r" + "2\r" + // Claude Code GitHub row: sonnet, low
		"3\r" + "3\r" + // Advanced, Claude Code roles (shows the choice)
		wtLeft + wtLeft + // roles -> advanced menu -> Back to Models
		wtLeft + "2\r" + // Hosts: uncheck Claude Code, Enter
		"3\r" + "1\r" + // Advanced, Done
		"\r" + "\r" // Continue, Apply
	for _, saved := range []bool{false, true} {
		t.Run(fmt.Sprintf("saved=%v", saved), func(t *testing.T) {
			t.Parallel()
			f := newWTGitHubFixture(t, false)
			if saved {
				wtSaveOverrides(t, f, map[string]*workflow.ModelOverride{"claude/commit_execution": wtOv(wtHaiku, "low")})
			}
			r := f.run(input)
			r.requireApplied(t)
			wtRequireHosts(t, r.sel, "codex")

			roles := wtFirstFrame(t, r.out, "substantive fallback") // the Claude Code roles menu
			wtHasLine(t, roles, `commit execution.*`+wtM(wtSonnet)+` · low`, "Advanced shows the row choice while Claude Code is selected")

			if r.sel.Preferences == nil {
				t.Fatal("Advanced returned nil preferences")
			}
			hostPrefs, ok := r.sel.Preferences.Hosts["claude"]
			if !saved {
				if ok {
					t.Fatalf("unchecked Claude Code has an entry %v; nothing was saved for it", wtCanonHost(hostPrefs))
				}
			} else {
				want := map[string]string{"commit_execution": wtHaiku + " · low"}
				if got := wtCanonHost(hostPrefs); !ok || !maps.Equal(got, want) {
					t.Fatalf("unchecked Claude Code preferences = %v (present %v), want exactly the saved %v", got, ok, want)
				}
			}

			// No pins come from the choice: three would be counted if it leaked.
			// A saved value for the unchecked host may or may not be counted.
			review := wtLastFrame(r.out)
			preset := wtLines(review, "Preset     ")
			if len(preset) != 1 {
				t.Fatalf("review has %d Preset lines:\n%s", len(preset), review)
			}
			allowed := []string{"Preset     Advanced · release defaults"}
			if saved {
				allowed = append(allowed, "Preset     Advanced · 1 pinned")
			}
			if !slices.ContainsFunc(allowed, func(want string) bool { return strings.Contains(preset[0], want) }) {
				t.Fatalf("Preset line %q counts pins from the session's Claude Code choice; want one of %q", preset[0], allowed)
			}
		})
	}
}

// Contract rev 8, Example 3: an Advanced edit makes only the edited GitHub role
// chosen; a role Advanced showed with its saved value stays unchosen, so under a
// preset it keeps that value only if the release offers it.
func TestWizardGitHubAdvancedEditChoosesOnlyEditedRoles(t *testing.T) {
	t.Parallel()
	// Installed rows: 1 Keep, 2 Recommended, 3 Balanced, 4 Advanced. Codex roles
	// row 7 is pr submission; astra is model row 3; medium is effort row 2 (row 1
	// is Release default, as the role default high is offered for astra).
	input := "4\r" + "2\r" + "7\r" + "3\r" + "2\r" + // Advanced, Codex roles, pr submission: astra, medium
		wtLeft + wtLeft + // Back to the advanced menu, Back to Models
		"2\r" + "\r" + "\r" // Recommended, Continue, Apply
	base := workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}, Preset: workflow.PresetRecommended}

	t.Run("unoffered saved value gives way to the preset", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, true)
		wtSaveOverrides(t, f, map[string]*workflow.ModelOverride{"codex/commit_execution": wtOv(wtRetired, "high")})
		r := f.run(input)
		r.requireApplied(t)
		if strings.Contains(wtStrip(r.out), "These choices can't be applied:") {
			t.Fatal("editing one GitHub role made the untouched unoffered saved value a choice")
		}
		wtRequirePrefs(t, f.view, r.sel, base, map[string]*workflow.ModelOverride{
			"codex/commit_execution":     nil,
			"codex/pr_authoring_routine": nil,
			"codex/pr_submission":        wtOv(wtAstra, "medium"),
		})
	})

	t.Run("offered saved value is kept", func(t *testing.T) {
		t.Parallel()
		f := newWTGitHubFixture(t, true)
		wtSaveOverrides(t, f, map[string]*workflow.ModelOverride{"codex/commit_execution": wtOv(wtLuna, "low")})
		r := f.run(input)
		r.requireApplied(t)
		wtRequirePrefs(t, f.view, r.sel, base, map[string]*workflow.ModelOverride{
			"codex/commit_execution":     wtOv(wtLuna, "low"),
			"codex/pr_authoring_routine": nil,
			"codex/pr_submission":        wtOv(wtAstra, "medium"),
		})
	})
}

// Contract rev 9, Example 4: on a later Advanced visit an unchosen GitHub role
// shows its saved value, and changing it to "Release default" is an edit.
func TestWizardGitHubLaterAdvancedVisitShowsSavedValue(t *testing.T) {
	t.Parallel()
	f := newWTGitHubFixture(t, true)
	wtSaveOverrides(t, f, map[string]*workflow.ModelOverride{"codex/commit_execution": wtOv(wtAstra, "medium")})
	// Installed Models rows: 1 Keep ... 4 Advanced. Codex roles: 5 commit
	// execution, 7 pr submission. luna is model row 2; low is effort row 2.
	input := "4\r" + "2\r" + "7\r" + "2\r" + "2\r" + // Advanced, Codex roles, pr submission: luna, low
		wtLeft + wtLeft + // Back, Back to Models
		"4\r" + "2\r" + // Advanced again, Codex roles
		"5\r" + "1\r" + "1\r" + // commit execution: Release default, Release default
		wtLeft + wtLeft + // Back, Back to Models
		"1\r" + "\r" + "\r" // Keep, Continue, Apply
	r := f.run(input)
	r.requireApplied(t)

	// The first Codex roles frame after Models was shown again is the second visit.
	second := ""
	phase := 0
	for _, frame := range wtFrames(r.out) {
		roles := strings.Contains(frame, "substantive fallback") && strings.Contains(frame, "code reviewer")
		switch {
		case phase == 0 && roles:
			phase = 1
		case phase == 1 && strings.Contains(frame, "GitHub workers"):
			phase = 2
		case phase == 2 && roles:
			second = frame
		}
		if second != "" {
			break
		}
	}
	if second == "" {
		t.Fatalf("no second Codex roles menu found; output tail:\n%s", wtTail(wtStrip(r.out)))
	}
	wtHasLine(t, second, `commit execution.*`+regexp.QuoteMeta(wtAstra+" · medium · pinned"), "second visit shows the saved value")
	wtHasLine(t, second, `pr submission.*`+regexp.QuoteMeta(wtLuna+" · low"), "second visit shows the choice")

	wtRequirePrefs(t, f.view, r.sel,
		workflow.ConfigurationSelection{Hosts: []string{"codex", "claude"}},
		map[string]*workflow.ModelOverride{
			"codex/commit_execution": nil,
			"codex/pr_submission":    wtOv(wtLuna, "low"),
		})
	prefs := wtPFS(t, f.view, r.sel)
	if override := prefs.Hosts["codex"].Roles["commit_execution"]; override.Model != nil || override.Effort != nil {
		t.Fatalf("commit_execution kept an override {%s, %s} after Release default", wtStr(override.Model), wtStr(override.Effort))
	}
}

// ---------------------------------------------------------------------------
// End to end through Execute (R friendly-finish, R back-and-exit)

type wtService struct {
	view       workflow.ConfigurationView
	options    workflow.Options
	configured bool
	result     workflow.ConfigurationSelection
}

func (s *wtService) Execute(action string) (workflow.Object, error) {
	if s.options.Configure == nil {
		return workflow.Object{"command": action}, nil
	}
	s.configured = true
	selection, err := s.options.Configure(s.options.Context, s.view)
	if err != nil {
		return nil, err
	}
	s.result = selection
	return workflow.Object{"configuration_committed": true}, nil
}

func (s *wtService) Status() (workflow.Object, error) {
	return workflow.Object{"installed": true, "registrations": []string{"archify", "graphify"}}, nil
}

func wtExecuteConfig(t *testing.T, input string) (Config, *bytes.Buffer, *bytes.Buffer, *wtService, *strings.Reader) {
	t.Helper()
	config, stdout, _, _ := testConfig(t)
	home := wtPhysicalHome(t, homeOf(t, config))
	configDir := wtConfigDir(t, home)
	service := &wtService{view: wtView(home, configDir, false)}
	config.NewService = func(o workflow.Options) Service { service.options = o; return service }
	config.Terminal = func() bool { return true }
	reader := strings.NewReader(input)
	config.In = reader
	stderr := new(bytes.Buffer)
	config.Err = stderr
	config.Getenv = func(name string) string {
		switch name {
		case "HOME":
			return home
		case "TERM":
			return "xterm-256color"
		}
		return ""
	}
	return config, stdout, stderr, service, reader
}

func TestSetupExecuteRunsWizardAndFinishesOnStderr(t *testing.T) {
	t.Parallel()
	config, stdout, stderr, service, _ := wtExecuteConfig(t, "\r\r\r\r")
	if err := Execute(t.Context(), []string{"setup"}, config); err != nil {
		t.Fatalf("Execute: %v\nstderr tail:\n%s", err, wtTail(wtStrip(stderr.String())))
	}
	if !service.configured {
		t.Fatal("setup on a terminal did not run the wizard")
	}
	wtRequireHosts(t, service.result, "codex", "claude")
	if stdout.Len() != 0 {
		t.Fatalf("stdout after a wizard apply = %q, want empty", stdout.String())
	}
	raw := stderr.String()
	wtRequireRestored(t, raw)
	text := wtStrip(raw)
	wtContains(t, text, "Setup complete · "+wtVersion, "Start a fresh agent session to use these settings.")
	if strings.Index(text, wtApplying) > strings.Index(text, "Setup complete") {
		t.Fatal("finish message written before the wizard finished")
	}
}

func TestSetupExecuteWizardCancellation(t *testing.T) {
	t.Parallel()
	config, stdout, stderr, _, _ := wtExecuteConfig(t, wtCtrlC)
	err := Execute(t.Context(), []string{"setup"}, config)
	if !errors.Is(err, context.Canceled) || ExitCode(err) != 130 {
		t.Fatalf("Execute error = %v (exit %d), want cancellation with exit 130", err, ExitCode(err))
	}
	if err.Error() != "" {
		t.Fatalf("cancellation error text = %q, want empty", err.Error())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout after cancellation = %q, want empty", stdout.String())
	}
	text := wtStrip(stderr.String())
	wtContains(t, text, strings.TrimSuffix(wtCancelLine, "\n"))
	if strings.Contains(text, "Setup complete") {
		t.Fatal("a cancelled setup printed the finish message")
	}
}

func TestSetupExecuteMachineFlagsSkipWizard(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--json", "--non-interactive", "--dry-run"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			const input = "\r\r\r\r"
			config, stdout, stderr, service, reader := wtExecuteConfig(t, input)
			if err := Execute(t.Context(), []string{"setup", flag}, config); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if service.options.Configure != nil || service.configured {
				t.Fatalf("%s set Configure; the wizard must not run", flag)
			}
			if reader.Len() != len(input) {
				t.Fatalf("%s consumed terminal input", flag)
			}
			if strings.Contains(stderr.String(), "\x1b[?1049h") {
				t.Fatalf("%s drew the wizard", flag)
			}
			var report map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || len(report) == 0 {
				t.Fatalf("%s did not write a JSON report to stdout: %v: %q", flag, err, stdout.String())
			}
		})
	}
}
