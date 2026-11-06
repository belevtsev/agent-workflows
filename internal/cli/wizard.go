package cli

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/belevtsev/agent-workflows/internal/credentials"
	"github.com/belevtsev/agent-workflows/internal/workflow"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

// Wizard steps, in the order the stepper shows them.
const (
	stepHosts = iota
	stepModels
	stepKeys
	stepReview
)

var stepNames = [...]string{"Hosts", "Models", "Keys", "Review"}

// nav is what a step or menu asks the wizard to do next.
type nav int

const (
	navNext nav = iota
	navBack
	navQuit
)

// menuItem is one row. A row with checked set is a checkbox; a fixed row is
// shown but never selected; a heading labels the rows below it, and an empty
// heading is a blank line.
type menuItem struct {
	label, value, detail string
	checked              *bool
	fixed, heading       bool
}

func (item menuItem) selectable() bool { return !item.fixed && !item.heading }

type menu struct {
	step        int
	title, help string
	body        string // preformatted text shown above the rows
	items       []menuItem
	value       string // initially selected row
	checklist   bool   // Enter continues from any row; Space toggles
	back        bool   // a previous step or level exists
	message     string
	check       func() string // checklist gate; a non-empty result blocks Enter
	scroll      int           // first body line shown when the body is windowed
	page        int           // body lines shown at once, or 0 when all fit
}

type editSpec struct {
	step        int
	title, help string
	secret      bool
	value       string
	validate    func(string) string
}

type finishInfo struct {
	version string
	hosts   []string
	first   bool
}

type setupWizard struct {
	config                    Config
	plain, started, checkKeys bool
	directory, generation     string
	home                      string
	changes                   map[string]*string
	stored                    map[string]bool
	keysLoaded                bool
	reader                    cancelreader.CancelReader
	keys                      *keyReader
	outputErr                 error

	view      workflow.ConfigurationView
	hostPicks map[string]bool
	preset    string
	prefs     *workflow.Preferences
	github    map[string]map[string]workflow.ModelOverride // GitHub worker role values chosen in this session, by host
	finish    *finishInfo
}

// cancelledSetup reports a user cancellation that the wizard has already
// explained, so the command prints no extra error line and exits 130.
type cancelledSetup struct{}

func (cancelledSetup) Error() string { return "" }
func (cancelledSetup) Unwrap() error { return context.Canceled }

func (w *setupWizard) print(format string, args ...any) {
	if w.outputErr != nil {
		return
	}
	if _, err := fmt.Fprintf(w.config.Err, format, args...); err != nil {
		w.outputErr = errors.New("terminal output is unavailable")
	}
}

// Stop the cancellation watcher before closing its owned descriptors.
func watchInput(ctx context.Context, reader cancelreader.CancelReader) func() {
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			reader.Cancel()
		case <-stop:
		}
	}()
	return func() { close(stop); <-done; reader.Close() }
}

func terminalAvailable(config Config) bool {
	if config.Terminal != nil {
		return config.Terminal()
	}
	in, iok := config.In.(*os.File)
	out, ook := config.Out.(*os.File)
	err, eok := config.Err.(*os.File)
	return iok && ook && eok && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd())) && term.IsTerminal(int(err.Fd()))
}

func newSetupWizard(config Config, o options) *setupWizard {
	return &setupWizard{config: config, plain: o.plain || config.Getenv("TERM") == "dumb" || config.Getenv("NO_COLOR") != "", changes: map[string]*string{}, stored: map[string]bool{}}
}

// Select runs the interactive setup. A cancellation restores the terminal,
// says the configuration was not changed and returns cancelledSetup.
func (w *setupWizard) Select(ctx context.Context, v workflow.ConfigurationView) (workflow.ConfigurationSelection, error) {
	w.started = true
	selection, err := w.run(ctx, v)
	if errors.Is(err, context.Canceled) {
		w.print("Setup cancelled. Your configuration was not changed.\n")
		return workflow.ConfigurationSelection{}, cancelledSetup{}
	}
	if err != nil {
		return workflow.ConfigurationSelection{}, err
	}
	w.print("Applying settings…\n")
	if w.outputErr != nil {
		return selection, w.outputErr
	}
	return selection, nil
}

func (w *setupWizard) run(ctx context.Context, v workflow.ConfigurationView) (_ workflow.ConfigurationSelection, resultErr error) {
	w.view = v
	w.directory = v.ConfigDir
	w.home = w.config.Getenv("HOME")
	w.hostPicks = map[string]bool{}
	w.github = map[string]map[string]workflow.ModelOverride{}
	for _, h := range v.Hosts {
		if !h.Installed {
			w.hostPicks[h.ID] = h.Selected
		}
	}
	w.preset = "recommended"
	if v.Installed {
		w.preset = "keep"
	}
	reader, err := cancelreader.NewReader(w.config.In)
	if err != nil {
		return workflow.ConfigurationSelection{}, errors.New("terminal input is unavailable")
	}
	w.reader = reader
	stopInput := watchInput(ctx, reader)
	defer stopInput()
	if w.plain {
		w.print("\nAGENT WORKFLOWS · SETUP\nRelease %s\nEnter keeps the default. Type b to go back or q to quit.\n", v.Target.Version)
	} else {
		leave, err := w.enterScreen()
		if err != nil {
			return workflow.ConfigurationSelection{}, err
		}
		w.keys = newKeyReader(reader)
		defer func() {
			w.keys.close()
			resultErr = errors.Join(resultErr, leave())
		}()
	}
	steps := []func(context.Context) (nav, error){w.hostsStep, w.modelsStep, w.keysStep, w.reviewStep}
	for i := 0; i < len(steps); {
		n, err := steps[i](ctx)
		if err != nil {
			return workflow.ConfigurationSelection{}, err
		}
		if w.outputErr != nil {
			return workflow.ConfigurationSelection{}, w.outputErr
		}
		switch n {
		case navNext:
			i++
		case navBack:
			i = max(i-1, 0)
		case navQuit:
			return workflow.ConfigurationSelection{}, context.Canceled
		}
	}
	if err = ctx.Err(); err != nil {
		return workflow.ConfigurationSelection{}, err
	}
	_, selection, err := w.compiled()
	return selection, err
}

func (w *setupWizard) pick(ctx context.Context, m *menu) (string, nav, error) {
	if w.plain {
		return w.plainPick(ctx, m)
	}
	return w.tuiPick(ctx, m)
}

func (w *setupWizard) edit(ctx context.Context, s editSpec) (string, nav, error) {
	if s.validate == nil {
		s.validate = func(string) string { return "" }
	}
	if w.plain {
		return w.plainEdit(ctx, s)
	}
	return w.tuiEdit(ctx, s)
}

func (m *menu) selectable() []int {
	var rows []int
	for i, item := range m.items {
		if item.selectable() {
			rows = append(rows, i)
		}
	}
	return rows
}

func (m *menu) initial() int {
	rows := m.selectable()
	for _, i := range rows {
		if m.items[i].value == m.value {
			return i
		}
	}
	if len(rows) > 0 {
		return rows[0]
	}
	return 0
}

// move steps to the next selectable row in direction delta, wrapping around.
func (m *menu) move(cursor, delta int) int {
	n := len(m.items)
	for range n {
		cursor = (cursor + delta + n) % n
		if m.items[cursor].selectable() {
			return cursor
		}
	}
	return cursor
}

func (m *menu) toggle(i int) {
	if i >= 0 && i < len(m.items) && m.items[i].checked != nil && m.items[i].selectable() {
		*m.items[i].checked = !*m.items[i].checked
	}
}

func hostName(id string) string {
	switch id {
	case "codex":
		return "Codex"
	case "claude":
		return "Claude Code"
	}
	return id
}

// short shows a path under the home directory with ~.
func (w *setupWizard) short(path string) string {
	if w.home != "" && w.home != "/" {
		if path == w.home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(path, strings.TrimSuffix(w.home, "/")+"/"); ok {
			return "~/" + rest
		}
	}
	return path
}

func (w *setupWizard) hostsInteractive() bool {
	return slices.ContainsFunc(w.view.Hosts, func(h workflow.HostConfiguration) bool { return !h.Installed })
}

func (w *setupWizard) selectedHosts() []string {
	var hosts []string
	for _, h := range w.view.Hosts {
		if h.Installed || w.hostPicks[h.ID] {
			hosts = append(hosts, h.ID)
		}
	}
	return hosts
}

// baseSelection is the chosen preset or edited preferences, before the GitHub
// worker layer.
func (w *setupWizard) baseSelection() workflow.ConfigurationSelection {
	s := workflow.ConfigurationSelection{Hosts: w.selectedHosts()}
	switch w.preset {
	case "recommended", "balanced":
		s.Preset = workflow.Preset(w.preset)
	case "advanced":
		if w.prefs != nil {
			p := *w.prefs
			s.Preferences = &p
		}
	}
	return s
}

const githubProfile = "github"

// githubRoles are a host's GitHub worker roles: those using the github profile.
func githubRoles(h workflow.HostConfiguration) []workflow.RoleConfiguration {
	var roles []workflow.RoleConfiguration
	for _, r := range h.Roles {
		if r.Profile == githubProfile {
			roles = append(roles, r)
		}
	}
	return roles
}

func sameString(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func sameOverride(a, b workflow.ModelOverride) bool {
	return sameString(a.Model, b.Model) && sameString(a.Effort, b.Effort)
}

func cloneOverride(ov workflow.ModelOverride) workflow.ModelOverride {
	var out workflow.ModelOverride
	if ov.Model != nil {
		m := *ov.Model
		out.Model = &m
	}
	if ov.Effort != nil {
		e := *ov.Effort
		out.Effort = &e
	}
	return out
}

// uniformGitHub returns the override all of a host's GitHub roles share, or
// false when they differ or the host has none.
func uniformGitHub(h workflow.HostConfiguration, hp workflow.HostPreferences) (workflow.ModelOverride, bool) {
	roles := githubRoles(h)
	if len(roles) == 0 {
		return workflow.ModelOverride{}, false
	}
	first := hp.Roles[roles[0].Role]
	for _, r := range roles[1:] {
		if !sameOverride(hp.Roles[r.Role], first) {
			return workflow.ModelOverride{}, false
		}
	}
	return first, true
}

// compiled applies the GitHub worker layer on top of the base selection. A
// GitHub role chosen in this session (w.github) always takes that value;
// otherwise a preset keeps the role's saved value when it is still offered.
// When the layer changes nothing, the base selection is returned unchanged.
func (w *setupWizard) compiled() (workflow.Preferences, workflow.ConfigurationSelection, error) {
	s := w.baseSelection()
	prefs, err := workflow.PreferencesForSelection(w.view, s)
	if err != nil {
		return prefs, s, err
	}
	changed := false
	for _, h := range w.view.Hosts {
		if !slices.Contains(s.Hosts, h.ID) {
			continue
		}
		chosen := w.github[h.ID]
		for _, r := range githubRoles(h) {
			ov, picked := chosen[r.Role]
			if !picked {
				// Keep and Advanced already carry the role's value. A preset
				// keeps the saved value, unless this release no longer offers
				// it; then the preset's value stands, as it did before.
				if s.Preset == "" {
					continue
				}
				if ov = w.view.Preferences.Hosts[h.ID].Roles[r.Role]; !offered(r, ov) {
					ov = prefs.Hosts[h.ID].Roles[r.Role]
				}
			}
			if sameOverride(prefs.Hosts[h.ID].Roles[r.Role], ov) {
				continue
			}
			hp := prefs.Hosts[h.ID]
			if ov.Model == nil && ov.Effort == nil {
				delete(hp.Roles, r.Role)
			} else {
				if hp.Roles == nil {
					hp.Roles = map[string]workflow.ModelOverride{}
				}
				hp.Roles[r.Role] = cloneOverride(ov)
			}
			if prefs.Hosts == nil {
				prefs.Hosts = map[string]workflow.HostPreferences{}
			}
			prefs.Hosts[h.ID] = hp
			changed = true
		}
	}
	if !changed {
		return prefs, s, nil
	}
	chosen := prefs
	s = workflow.ConfigurationSelection{Hosts: s.Hosts, Preferences: &chosen}
	prefs, err = workflow.PreferencesForSelection(w.view, s)
	return prefs, s, err
}

// offered reports whether the role's choices include the model and effort an
// override gives, with unset fields taken from the role's default.
func offered(r workflow.RoleConfiguration, ov workflow.ModelOverride) bool {
	v := afterValue(r, ov)
	i := slices.IndexFunc(r.Choices, func(c workflow.ModelChoice) bool { return c.Model == v.model })
	return i >= 0 && slices.Contains(r.Choices[i].Efforts, v.effort)
}

// githubPreview is the value a host's GitHub roles get from the roles chosen
// in this session and, for the others, from the saved preferences, for when
// the whole selection can't be applied. It reports false when such a saved
// value is one this release no longer offers.
func (w *setupWizard) githubPreview(h workflow.HostConfiguration) (string, bool) {
	saved, available := w.view.Preferences.Hosts[h.ID], true
	roles := map[string]workflow.ModelOverride{}
	for _, r := range githubRoles(h) {
		ov, picked := w.github[h.ID][r.Role]
		if !picked {
			ov = saved.Roles[r.Role]
			available = available && offered(r, ov)
		}
		roles[r.Role] = ov
	}
	return githubText(h, workflow.Preferences{Hosts: map[string]workflow.HostPreferences{h.ID: {Roles: roles}}}), available
}

// githubText shows the value a host's GitHub roles will get.
func githubText(h workflow.HostConfiguration, prefs workflow.Preferences) string {
	var values []roleValue
	for _, r := range githubRoles(h) {
		if v := afterValue(r, prefs.Hosts[h.ID].Roles[r.Role]); !slices.Contains(values, v) {
			values = append(values, v)
		}
	}
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0].String()
	}
	return "differs per role"
}

func (w *setupWizard) hostsStep(ctx context.Context) (nav, error) {
	if !w.hostsInteractive() {
		return navNext, nil
	}
	m := &menu{step: stepHosts, title: "Hosts", help: "Choose the agents cw should set up.", checklist: true}
	picks := map[string]*bool{}
	for _, h := range w.view.Hosts {
		if h.Installed {
			m.items = append(m.items, menuItem{label: hostName(h.ID), value: h.ID, detail: w.short(h.Root) + " · already set up", fixed: true})
			continue
		}
		picked := w.hostPicks[h.ID]
		picks[h.ID] = &picked
		detail := w.short(h.Root)
		if !h.Detected {
			detail += " · not found yet"
		}
		m.items = append(m.items, menuItem{label: hostName(h.ID), value: h.ID, detail: detail, checked: &picked})
	}
	m.check = func() string {
		for _, item := range m.items {
			if item.fixed || item.checked != nil && *item.checked {
				return ""
			}
		}
		return "Select at least one host to continue."
	}
	_, n, err := w.pick(ctx, m)
	for id, picked := range picks {
		w.hostPicks[id] = *picked
	}
	return n, err
}

var presetDetails = map[string]string{
	"keep":        "your saved choices",
	"recommended": "Sol 6.1/max + Opus 5.5/high, xhigh",
	"balanced":    "Sol 6.1/xhigh + Opus 5.5/high",
	"advanced":    "choose each role and memory setting",
}

func presetName(preset string) string {
	switch preset {
	case "keep":
		return "Keep saved choices"
	case "recommended":
		return "Recommended"
	case "balanced":
		return "Balanced"
	case "advanced":
		return "Advanced"
	}
	return preset
}

func (w *setupWizard) modelsStep(ctx context.Context) (nav, error) {
	cursor := w.preset
	for {
		var items []menuItem
		if w.view.Installed {
			items = append(items, menuItem{label: "Keep saved choices", value: "keep", detail: "recommended for updates"})
		}
		for _, preset := range []string{"recommended", "balanced", "advanced"} {
			items = append(items, menuItem{label: presetName(preset), value: preset, detail: presetDetails[preset]})
		}
		prefs, _, compileErr := w.compiled()
		var github []menuItem
		for _, id := range w.selectedHosts() {
			h := w.view.Hosts[slices.IndexFunc(w.view.Hosts, func(h workflow.HostConfiguration) bool { return h.ID == id })]
			if len(githubRoles(h)) == 0 {
				continue
			}
			detail := githubText(h, prefs)
			if compileErr != nil {
				text, available := w.githubPreview(h)
				switch {
				case w.preset == "advanced":
					detail = "fix the Advanced choices first"
				case !available:
					detail = text + " · unavailable"
				default:
					detail = text
				}
			}
			github = append(github, menuItem{label: "  " + hostName(id), value: "github:" + id, detail: detail})
		}
		if len(github) > 0 {
			items = append(items, menuItem{heading: true}, menuItem{label: "GitHub workers", detail: "commits, routine PRs, PR submission", heading: true})
			items = append(items, github...)
		}
		m := &menu{step: stepModels, title: "Models", help: "A preset sets the main-work models and replaces other saved overrides, including memory and context. GitHub workers keep the choice shown below; select a host to change it.", items: items, value: cursor, back: w.hostsInteractive()}
		value, n, err := w.pick(ctx, m)
		if err != nil || n != navNext {
			return n, err
		}
		if id, ok := strings.CutPrefix(value, "github:"); ok {
			cursor = value
			if n, err = w.githubWorkers(ctx, id, prefs); err != nil || n == navQuit {
				return n, err
			}
			// After a change, Enter continues with the preset; after Back the
			// cursor stays on the row.
			if n == navNext {
				cursor = w.preset
			}
			continue
		}
		w.preset = value
		if value != "advanced" {
			return navNext, nil
		}
		if n, err = w.advanced(ctx); err != nil || n != navBack {
			return n, err
		}
		cursor = w.preset
	}
}

// githubWorkers sets one model and effort for all of a host's GitHub roles.
func (w *setupWizard) githubWorkers(ctx context.Context, id string, prefs workflow.Preferences) (nav, error) {
	h := w.view.Hosts[slices.IndexFunc(w.view.Hosts, func(h workflow.HostConfiguration) bool { return h.ID == id })]
	roles := githubRoles(h)
	if len(roles) == 0 {
		return navQuit, errors.New("selected host has no GitHub worker roles")
	}
	r := roles[0]
	current, uniform := uniformGitHub(h, prefs.Hosts[id])
	r.Override = current
	labels := make([]string, 0, len(roles))
	for _, role := range roles {
		labels = append(labels, githubRoleName(role.Role))
	}
	list := strings.Join(labels, ", ")
	if len(labels) > 1 {
		list = strings.Join(labels[:len(labels)-1], ", ") + " and " + labels[len(labels)-1]
	}
	help := "Used for " + list + ". A model you pick stays pinned; the release default follows future updates."
	if !uniform {
		r.Override = workflow.ModelOverride{}
		help = "These roles differ now; your choice replaces them all (" + list + ")."
	}
	ov, n, err := w.role(ctx, hostName(id)+" · GitHub workers", help, r)
	if err != nil || n != navNext {
		return n, err
	}
	chosen := map[string]workflow.ModelOverride{}
	for _, role := range roles {
		chosen[role.Role] = cloneOverride(ov)
	}
	w.github[id] = chosen
	return navNext, nil
}

// githubValues copies the overrides of a host's GitHub roles.
func githubValues(h workflow.HostConfiguration, hp workflow.HostPreferences) map[string]workflow.ModelOverride {
	values := map[string]workflow.ModelOverride{}
	for _, r := range githubRoles(h) {
		values[r.Role] = cloneOverride(hp.Roles[r.Role])
	}
	return values
}

// setGitHub sets the given GitHub role overrides of a host in p; an empty
// override removes the role's entry and roles not in values stay as they are.
func setGitHub(p *workflow.Preferences, h workflow.HostConfiguration, values map[string]workflow.ModelOverride) {
	hp := p.Hosts[h.ID]
	for _, r := range githubRoles(h) {
		ov, ok := values[r.Role]
		if !ok {
			continue
		}
		if ov.Model == nil && ov.Effort == nil {
			delete(hp.Roles, r.Role)
			continue
		}
		if hp.Roles == nil {
			hp.Roles = map[string]workflow.ModelOverride{}
		}
		hp.Roles[r.Role] = cloneOverride(ov)
	}
	if p.Hosts == nil {
		p.Hosts = map[string]workflow.HostPreferences{}
	}
	p.Hosts[h.ID] = hp
}

func sameValues(a, b map[string]workflow.ModelOverride) bool {
	if len(a) != len(b) {
		return false
	}
	for role, ov := range a {
		if other, ok := b[role]; !ok || !sameOverride(ov, other) {
			return false
		}
	}
	return true
}

// githubRoleName is a GitHub worker role in words.
func githubRoleName(role string) string {
	switch role {
	case "commit_execution":
		return "commits"
	case "pr_authoring_routine":
		return "routine PR text"
	case "pr_submission":
		return "PR submission"
	}
	return roleLabel(role)
}

func (w *setupWizard) advanced(ctx context.Context) (nav, error) {
	if w.prefs == nil {
		data, err := json.Marshal(w.view.Preferences)
		if err != nil {
			return navQuit, err
		}
		var p workflow.Preferences
		if err = json.Unmarshal(data, &p); err != nil {
			return navQuit, err
		}
		p.Version = 1
		if p.Hosts == nil {
			p.Hosts = map[string]workflow.HostPreferences{}
		}
		w.prefs = &p
	}
	p := w.prefs
	// While Advanced is open, its role menus show the GitHub choices of selected
	// hosts as if pinned. On the way out, for a host whose GitHub roles were
	// edited here, the edited roles become chosen and the others keep what
	// Advanced showed: a shown choice stays chosen, a shown saved or release
	// value stays unchosen. Every host's GitHub roles then go back to the
	// values they had before, so a choice lives only in w.github, which applies
	// to selected hosts, and never stays behind for a host unchecked later.
	selected := w.selectedHosts()
	before := map[string]map[string]workflow.ModelOverride{}
	present := map[string]bool{}
	for _, h := range w.view.Hosts {
		before[h.ID] = githubValues(h, p.Hosts[h.ID])
		_, present[h.ID] = p.Hosts[h.ID]
	}
	shown := map[string]bool{}
	for _, h := range w.view.Hosts {
		if chosen, ok := w.github[h.ID]; ok && slices.Contains(selected, h.ID) {
			setGitHub(p, h, chosen)
			shown[h.ID] = true
		}
	}
	entry := map[string]map[string]workflow.ModelOverride{}
	for _, h := range w.view.Hosts {
		entry[h.ID] = githubValues(h, p.Hosts[h.ID])
	}
	defer func() {
		for _, h := range w.view.Hosts {
			if now := githubValues(h, p.Hosts[h.ID]); !sameValues(now, entry[h.ID]) {
				chosen := map[string]workflow.ModelOverride{}
				for _, r := range githubRoles(h) {
					if !sameOverride(now[r.Role], entry[h.ID][r.Role]) {
						chosen[r.Role] = now[r.Role]
					} else if ov, ok := w.github[h.ID][r.Role]; ok && shown[h.ID] {
						chosen[r.Role] = ov
					}
				}
				w.github[h.ID] = chosen
			}
			setGitHub(p, h, before[h.ID])
			if hp := p.Hosts[h.ID]; !present[h.ID] && len(hp.Roles) == 0 && hp.Fallback == nil {
				delete(p.Hosts, h.ID)
			}
		}
	}()
	cursor := "done"
	for {
		items := []menuItem{{label: "Done", value: "done", detail: "review these choices"}}
		for _, host := range w.view.Hosts {
			items = append(items, menuItem{label: hostName(host.ID) + " roles", value: host.ID, detail: pinnedText(pinnedRoles(p.Hosts[host.ID]))})
		}
		items = append(items, menuItem{label: "Memory and context", value: "shared", detail: pinnedText(pinnedShared(p.Shared))})
		m := &menu{step: stepModels, title: "Advanced", help: "Pick what to customize. Anything you leave alone keeps its saved choice.", items: items, value: cursor, back: true}
		value, n, err := w.pick(ctx, m)
		if err != nil || n != navNext {
			return n, err
		}
		cursor = value
		switch value {
		case "done":
			return navNext, nil
		case "shared":
			n, err = w.shared(ctx, p)
		default:
			n, err = w.hostRoles(ctx, value, p)
		}
		if err != nil || n == navQuit {
			return n, err
		}
	}
}

func pinnedText(n int) string {
	switch n {
	case 0:
		return "release defaults"
	case 1:
		return "1 pinned"
	}
	return strconv.Itoa(n) + " pinned"
}

func pinnedRoles(hp workflow.HostPreferences) int {
	n := 0
	for _, ov := range hp.Roles {
		if ov.Model != nil || ov.Effort != nil {
			n++
		}
	}
	if hp.Fallback != nil && (hp.Fallback.Model != nil || hp.Fallback.Effort != nil) {
		n++
	}
	return n
}

func pinnedShared(s *workflow.SharedPreferences) int {
	if s == nil {
		return 0
	}
	n := 0
	for _, set := range []bool{s.ContextWindow != nil, s.Memories != nil, s.GenerateMemories != nil, s.UseMemories != nil, s.ExtractModel != nil, s.ConsolidationModel != nil} {
		if set {
			n++
		}
	}
	return n
}

func roleLabel(role string) string { return strings.ReplaceAll(role, "_", " ") }

func hostRolesOf(h workflow.HostConfiguration) []workflow.RoleConfiguration {
	return append(append([]workflow.RoleConfiguration{}, h.Roles...), h.Fallback)
}

func roleOverride(h workflow.HostConfiguration, hp workflow.HostPreferences, r workflow.RoleConfiguration) workflow.ModelOverride {
	if r.Role == h.Fallback.Role {
		if hp.Fallback != nil {
			return *hp.Fallback
		}
		return workflow.ModelOverride{}
	}
	return hp.Roles[r.Role]
}

func (w *setupWizard) hostRoles(ctx context.Context, id string, p *workflow.Preferences) (nav, error) {
	idx := slices.IndexFunc(w.view.Hosts, func(h workflow.HostConfiguration) bool { return h.ID == id })
	if idx < 0 {
		return navQuit, errors.New("selected host is unavailable")
	}
	host := w.view.Hosts[idx]
	cursor := ""
	for {
		var items []menuItem
		for _, r := range hostRolesOf(host) {
			items = append(items, menuItem{label: roleLabel(r.Role), value: r.Role, detail: afterValue(r, roleOverride(host, p.Hosts[host.ID], r)).String()})
		}
		m := &menu{step: stepModels, title: hostName(host.ID) + " roles", help: "Pick a role to change. Pinned values stay fixed across updates; release defaults follow them.", items: items, value: cursor, back: true}
		name, n, err := w.pick(ctx, m)
		if err != nil || n != navNext {
			return n, err
		}
		cursor = name
		roles := hostRolesOf(host)
		r := roles[slices.IndexFunc(roles, func(r workflow.RoleConfiguration) bool { return r.Role == name })]
		hp := p.Hosts[host.ID]
		r.Override = roleOverride(host, hp, r)
		override, n, err := w.role(ctx, hostName(host.ID)+" · "+roleLabel(name), roleHelp(r), r)
		if err != nil || n == navQuit {
			return n, err
		}
		if n != navNext {
			continue
		}
		if name == host.Fallback.Role {
			hp.Fallback = &override
		} else {
			if hp.Roles == nil {
				hp.Roles = map[string]workflow.ModelOverride{}
			}
			hp.Roles[name] = override
		}
		p.Hosts[host.ID] = hp
	}
}

func roleHelp(r workflow.RoleConfiguration) string {
	if r.Effective.Model == "" {
		return "New role. An explicit choice stays pinned; the release default follows future updates."
	}
	return fmt.Sprintf("Now %s · %s. An explicit choice stays pinned, even when it equals today's default.", r.Effective.Model, r.Effective.Effort)
}

func (w *setupWizard) role(ctx context.Context, title, help string, r workflow.RoleConfiguration) (workflow.ModelOverride, nav, error) {
	ov := r.Override
	model, effort := "inherit", "inherit"
	if ov.Model != nil {
		model = *ov.Model
	}
	if ov.Effort != nil {
		effort = *ov.Effort
	}
	for {
		models := []menuItem{{label: "Release default", value: "inherit", detail: r.Default.Model}}
		for _, c := range r.Choices {
			models = append(models, menuItem{label: c.Model, value: c.Model})
		}
		value, n, err := w.pick(ctx, &menu{step: stepModels, title: title + " · model", help: help, items: models, value: model, back: true})
		if err != nil || n != navNext {
			return ov, n, err
		}
		model = value
		selected := model
		if selected == "inherit" {
			selected = r.Default.Model
		}
		var allowed []string
		for _, c := range r.Choices {
			if c.Model == selected {
				allowed = c.Efforts
				break
			}
		}
		var efforts []menuItem
		if slices.Contains(allowed, r.Default.Effort) {
			efforts = append(efforts, menuItem{label: "Release default", value: "inherit", detail: r.Default.Effort})
		}
		for _, e := range allowed {
			efforts = append(efforts, menuItem{label: e, value: e})
		}
		if len(efforts) == 0 {
			return ov, navQuit, errors.New("selected role has no supported effort choices")
		}
		if effort == "inherit" && !slices.Contains(allowed, r.Default.Effort) || effort != "inherit" && !slices.Contains(allowed, effort) {
			effort = efforts[0].value
		}
		value, n, err = w.pick(ctx, &menu{step: stepModels, title: title + " · effort", help: "Efforts this model supports.", items: efforts, value: effort, back: true})
		if err != nil || n == navQuit {
			return ov, n, err
		}
		if n == navBack {
			continue
		}
		effort = value
		out := workflow.ModelOverride{}
		if model != "inherit" {
			m := model
			out.Model = &m
		}
		if effort != "inherit" {
			e := effort
			out.Effort = &e
		}
		return out, navNext, nil
	}
}

func (w *setupWizard) shared(ctx context.Context, p *workflow.Preferences) (nav, error) {
	if p.Shared == nil {
		p.Shared = &workflow.SharedPreferences{}
	}
	s := p.Shared
	cursor := ""
	for {
		boolText := func(v *bool, key string) string {
			if v == nil {
				return "release default (" + onOff(w.view.SharedDefaults[key]) + ")"
			}
			return onOff(*v) + " · pinned"
		}
		textValue := func(v *string, key string) string {
			if v == nil {
				return "release default (" + fmt.Sprint(w.view.SharedDefaults[key]) + ")"
			}
			return *v + " · pinned"
		}
		window := "release default (" + tokens(w.view.SharedDefaults["model_context_window"]) + ")"
		if s.ContextWindow != nil {
			window = tokens(*s.ContextWindow) + " · pinned"
		}
		items := []menuItem{
			{label: "Context window", value: "window", detail: window},
			{label: "Memory feature", value: "memories", detail: boolText(s.Memories, "features.memories")},
			{label: "Generate memories", value: "generate", detail: boolText(s.GenerateMemories, "memories.generate_memories")},
			{label: "Use memories", value: "use", detail: boolText(s.UseMemories, "memories.use_memories")},
			{label: "Extraction model", value: "extract", detail: textValue(s.ExtractModel, "memories.extract_model")},
			{label: "Consolidation model", value: "consolidate", detail: textValue(s.ConsolidationModel, "memories.consolidation_model")},
		}
		selected, n, err := w.pick(ctx, &menu{step: stepModels, title: "Memory and context", help: "Codex memory and context settings. Release defaults follow future updates; explicit values stay pinned.", items: items, value: cursor, back: true})
		if err != nil || n != navNext {
			return n, err
		}
		cursor = selected
		switch selected {
		case "window":
			value := ""
			if s.ContextWindow != nil {
				value = strconv.FormatInt(*s.ContextWindow, 10)
			}
			value, n, err = w.edit(ctx, editSpec{step: stepModels, title: "Context window", help: "A positive token count. Leave it empty to follow the release default.", value: value, validate: func(v string) string {
				if v == "" {
					return ""
				}
				if n, err := strconv.ParseInt(v, 10, 64); err != nil || n <= 0 {
					return "Enter a positive whole number of tokens, or leave it empty."
				}
				return ""
			}})
			if err != nil || n == navQuit {
				return n, err
			}
			if n == navNext {
				s.ContextWindow = nil
				if value != "" {
					parsed, _ := strconv.ParseInt(value, 10, 64)
					s.ContextWindow = &parsed
				}
			}
		case "extract", "consolidate":
			field := &s.ExtractModel
			title := "Extraction model"
			if selected == "consolidate" {
				field, title = &s.ConsolidationModel, "Consolidation model"
			}
			value := ""
			if *field != nil {
				value = **field
			}
			value, n, err = w.edit(ctx, editSpec{step: stepModels, title: title, help: "A model name stays pinned. Leave it empty to follow the release default.", value: value, validate: func(v string) string {
				if strings.ContainsFunc(v, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
					return "A model name has no spaces or control characters."
				}
				return ""
			}})
			if err != nil || n == navQuit {
				return n, err
			}
			if n == navNext {
				*field = nil
				if value != "" {
					*field = &value
				}
			}
		default:
			fields := map[string]**bool{"memories": &s.Memories, "generate": &s.GenerateMemories, "use": &s.UseMemories}
			field, ok := fields[selected]
			if !ok {
				return navQuit, errors.New("selected memory field is unavailable")
			}
			value := "inherit"
			if *field != nil {
				value = strconv.FormatBool(**field)
			}
			label := items[slices.IndexFunc(items, func(i menuItem) bool { return i.value == selected })].label
			value, n, err = w.pick(ctx, &menu{step: stepModels, title: label, help: "Release defaults follow future updates; explicit values stay pinned.", items: []menuItem{{label: "Release default", value: "inherit"}, {label: "On", value: "true"}, {label: "Off", value: "false"}}, value: value, back: true})
			if err != nil || n == navQuit {
				return n, err
			}
			if n == navNext {
				*field = nil
				if value != "inherit" {
					b := value == "true"
					*field = &b
				}
			}
		}
	}
}

func onOff(v any) string {
	switch b := v.(type) {
	case bool:
		if b {
			return "on"
		}
		return "off"
	case nil:
		return "not set"
	}
	return fmt.Sprint(v)
}

// tokens formats a context window as "512,000 tokens".
func tokens(v any) string {
	var n int64
	switch x := v.(type) {
	case int64:
		n = x
	case int:
		n = int64(x)
	case float64:
		n = int64(x)
	case nil:
		return "not set"
	default:
		return fmt.Sprint(v)
	}
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 && digits[i-1] != '-' {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String() + " tokens"
}

func (w *setupWizard) reviewStep(ctx context.Context) (nav, error) {
	hosts := w.selectedHosts()
	if len(hosts) == 0 {
		return navQuit, errors.New("select at least one installed agent host")
	}
	m := &menu{step: stepReview, title: "Review", value: "apply", back: true}
	prefs, _, err := w.compiled()
	if err != nil {
		m.help = "These choices can't be applied: " + err.Error()
		m.items = []menuItem{{label: "Back", value: "back"}, {label: "Cancel setup", value: "cancel"}}
	} else {
		m.body = w.reviewText(prefs, hosts)
		m.items = []menuItem{{label: "Apply settings", value: "apply"}, {label: "Back", value: "back", detail: "change keys"}, {label: "Cancel setup", value: "cancel"}}
	}
	value, n, err := w.pick(ctx, m)
	if err != nil || n != navNext {
		return n, err
	}
	switch value {
	case "apply":
		w.finish = &finishInfo{version: w.view.Target.Version, first: !w.view.Installed}
		for _, id := range hosts {
			host := w.view.Hosts[slices.IndexFunc(w.view.Hosts, func(h workflow.HostConfiguration) bool { return h.ID == id })]
			if row, ok := scopeRows(host, prefs.Hosts[id], false)["substantive"]; ok {
				w.finish.hosts = append(w.finish.hosts, fmt.Sprintf("%-12s main work %s", hostName(id), row.value.plain()))
			}
		}
		return navNext, nil
	case "back":
		return navBack, nil
	}
	return navQuit, nil
}

// roleValue is a role's model and effort after the selection is applied.
type roleValue struct {
	model, effort string
	pinned        bool
}

func (r roleValue) plain() string { return r.model + " · " + r.effort }

func (r roleValue) String() string {
	if r.pinned {
		return r.plain() + " · pinned"
	}
	return r.plain()
}

func afterValue(r workflow.RoleConfiguration, ov workflow.ModelOverride) roleValue {
	v := roleValue{model: r.Default.Model, effort: r.Default.Effort, pinned: ov.Model != nil || ov.Effort != nil}
	if ov.Model != nil {
		v.model = *ov.Model
	}
	if ov.Effort != nil {
		v.effort = *ov.Effort
	}
	return v
}

type scopeRow struct {
	value      roleValue
	change     string
	exceptions []roleException
}

type roleException struct {
	role   string
	value  roleValue
	change string
}

// reviewWidth is the widest review line that fits the default panel; a longer
// exception moves its change note to the next line.
const reviewWidth = 70

var scopeOrder = []string{"substantive", githubProfile, "bounded_execution", "bounded_evidence"}

func scopeLabel(scope string) string {
	switch scope {
	case "substantive":
		return "Main work"
	case githubProfile:
		return "GitHub"
	case "bounded_execution":
		return "Routine"
	case "bounded_evidence":
		return "Lookups"
	}
	return roleLabel(scope)
}

// scopeRows groups a host's roles by scope. Each row shows the value most of
// its roles share; any role with another value or pin state is an exception.
func scopeRows(h workflow.HostConfiguration, hp workflow.HostPreferences, installed bool) map[string]scopeRow {
	type entry struct {
		role          string
		after, before roleValue
		hasBefore     bool
	}
	groups := map[string][]entry{}
	for _, r := range hostRolesOf(h) {
		scope := r.Scope
		if scope == "" {
			scope = "substantive"
		}
		if r.Profile == githubProfile {
			scope = githubProfile
		}
		e := entry{role: r.Role, after: afterValue(r, roleOverride(h, hp, r))}
		if installed && r.Effective.Model != "" {
			e.before, e.hasBefore = roleValue{model: r.Effective.Model, effort: r.Effective.Effort}, true
		}
		groups[scope] = append(groups[scope], e)
	}
	changeOf := func(entries []entry, after roleValue) string {
		var befores []string
		changed := false
		for _, e := range entries {
			if !e.hasBefore {
				continue
			}
			if e.before.plain() != after.plain() {
				changed = true
			}
			if !slices.Contains(befores, e.before.plain()) {
				befores = append(befores, e.before.plain())
			}
		}
		switch {
		case !changed:
			return ""
		case len(befores) == 1:
			return "was " + befores[0]
		}
		return "changed"
	}
	rows := map[string]scopeRow{}
	for scope, entries := range groups {
		counts := map[roleValue]int{}
		best := entries[0].after
		for _, e := range entries {
			counts[e.after]++
			if counts[e.after] > counts[best] {
				best = e.after
			}
		}
		var same []entry
		row := scopeRow{value: best}
		for _, e := range entries {
			if e.after == best {
				same = append(same, e)
				continue
			}
			row.exceptions = append(row.exceptions, roleException{role: e.role, value: e.after, change: changeOf([]entry{e}, e.after)})
		}
		row.change = changeOf(same, best)
		rows[scope] = row
	}
	return rows
}

func (w *setupWizard) reviewText(p workflow.Preferences, hosts []string) string {
	var b strings.Builder
	line := func(label, value string) { fmt.Fprintf(&b, "%-10s %s\n", label, value) }
	names := make([]string, 0, len(hosts))
	for _, id := range hosts {
		names = append(names, hostName(id))
	}
	line("Hosts", strings.Join(names, ", "))
	preset := presetName(w.preset)
	if w.preset == "advanced" {
		preset += " · " + pinnedText(pinnedTotal(&p))
	}
	line("Preset", preset)
	for _, h := range w.view.Hosts {
		if !slices.Contains(hosts, h.ID) {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n", hostName(h.ID))
		rows := scopeRows(h, p.Hosts[h.ID], w.view.Installed)
		var scopes []string
		for _, s := range scopeOrder {
			if _, ok := rows[s]; ok {
				scopes = append(scopes, s)
			}
		}
		var others []string
		for s := range rows {
			if !slices.Contains(scopeOrder, s) {
				others = append(others, s)
			}
		}
		slices.Sort(others)
		for _, s := range append(scopes, others...) {
			row := rows[s]
			text := fmt.Sprintf("  %-10s %s", scopeLabel(s), row.value.String())
			switch {
			case row.change == "":
			case len([]rune(text))+len(row.change)+3 <= reviewWidth:
				text += " (" + row.change + ")"
			default:
				text += "\n" + strings.Repeat(" ", 13) + row.change
			}
			b.WriteString(text + "\n")
			for _, e := range row.exceptions {
				line := "    " + roleLabel(e.role) + "  " + e.value.String()
				switch {
				case e.change == "":
				case len([]rune(line))+len(e.change)+3 <= reviewWidth:
					line += " (" + e.change + ")"
				default:
					line += "\n      " + e.change
				}
				b.WriteString(line + "\n")
			}
		}
	}
	if slices.Contains(hosts, "codex") {
		b.WriteString("\n")
		after := func(key string) any {
			if v, ok := sharedValues(p.Shared)[key]; ok {
				return v
			}
			return w.view.SharedDefaults[key]
		}
		before := func(key string) any {
			if !w.view.Installed {
				return nil
			}
			return w.view.SharedEffective[key]
		}
		memory := memoryText(after)
		if was := memoryText(before); w.view.Installed && was != "" && was != memory {
			memory += " (was " + was + ")"
		}
		line("Memory", memory)
		window := tokens(after("model_context_window"))
		if was := before("model_context_window"); was != nil && tokens(was) != window {
			window += " (was " + tokens(was) + ")"
		}
		line("Context", window)
	}
	b.WriteString("\n")
	line("Keys", w.keySummary(false))
	line("Settings", w.short(w.view.ConfigDir))
	return strings.TrimRight(b.String(), "\n")
}

// pinnedTotal counts the pinned roles and shared settings in prefs.
func pinnedTotal(prefs *workflow.Preferences) int {
	if prefs == nil {
		return 0
	}
	n := pinnedShared(prefs.Shared)
	for _, hp := range prefs.Hosts {
		n += pinnedRoles(hp)
	}
	return n
}

func sharedValues(s *workflow.SharedPreferences) map[string]any {
	out := map[string]any{}
	if s == nil {
		return out
	}
	if s.ContextWindow != nil {
		out["model_context_window"] = *s.ContextWindow
	}
	if s.Memories != nil {
		out["features.memories"] = *s.Memories
	}
	if s.GenerateMemories != nil {
		out["memories.generate_memories"] = *s.GenerateMemories
	}
	if s.UseMemories != nil {
		out["memories.use_memories"] = *s.UseMemories
	}
	if s.ExtractModel != nil {
		out["memories.extract_model"] = *s.ExtractModel
	}
	if s.ConsolidationModel != nil {
		out["memories.consolidation_model"] = *s.ConsolidationModel
	}
	return out
}

// memoryText summarizes the memory settings, or "" when none are known.
func memoryText(get func(string) any) string {
	feature := get("features.memories")
	if feature == nil {
		return ""
	}
	if feature == false {
		return "off"
	}
	parts := []string{onOff(feature)}
	if get("memories.generate_memories") == false {
		parts = append(parts, "not generated")
	}
	if get("memories.use_memories") == false {
		parts = append(parts, "not used")
	}
	extract, consolidate := fmt.Sprint(get("memories.extract_model")), fmt.Sprint(get("memories.consolidation_model"))
	if extract == consolidate {
		parts = append(parts, "model "+extract)
	} else {
		parts = append(parts, "extract "+extract, "consolidate "+consolidate)
	}
	return strings.Join(parts, " · ")
}

// summaries are the one-line results of the steps before step.
func (w *setupWizard) summaries(step int) [][2]string {
	var out [][2]string
	if step > stepHosts {
		var names []string
		for _, id := range w.selectedHosts() {
			names = append(names, hostName(id))
		}
		out = append(out, [2]string{"Hosts", strings.Join(names, ", ")})
	}
	if step > stepModels {
		value := presetName(w.preset)
		prefs, _, err := w.compiled()
		if w.preset == "advanced" {
			counted := w.prefs
			if err == nil {
				counted = &prefs
			}
			value += " · " + pinnedText(pinnedTotal(counted))
		} else if d := presetDetails[w.preset]; d != "" && w.preset != "keep" {
			value += " · " + d
		}
		if err == nil {
			for _, id := range w.selectedHosts() {
				h := w.view.Hosts[slices.IndexFunc(w.view.Hosts, func(h workflow.HostConfiguration) bool { return h.ID == id })]
				if ov, ok := uniformGitHub(h, prefs.Hosts[id]); !ok && len(githubRoles(h)) > 0 || ov.Model != nil || ov.Effort != nil {
					value += " · GitHub pinned"
					break
				}
			}
		}
		out = append(out, [2]string{"Models", value})
	}
	if step > stepKeys {
		out = append(out, [2]string{"Keys", w.keySummary(true)})
	}
	return out
}

// finishMessage is the human summary printed after a wizard-driven apply.
func (w *setupWizard) finishMessage(status, credentialResult workflow.Object) string {
	info := w.finish
	if info == nil {
		info = &finishInfo{version: w.view.Target.Version}
	}
	var b strings.Builder
	head := "Setup complete · " + info.version
	if w.plain {
		b.WriteString(head + "\n")
	} else {
		b.WriteString(styleActive.Bold(true).Render("✓ "+head) + "\n")
	}
	for _, h := range info.hosts {
		b.WriteString("  " + h + "\n")
	}
	switch regs := status["registrations"].(type) {
	case []any:
		fmt.Fprintf(&b, "  %-12s %d registered\n", "Skills", len(regs))
	case []string:
		fmt.Fprintf(&b, "  %-12s %d registered\n", "Skills", len(regs))
	}
	if credentialResult["status"] == "saved" {
		var saved []string
		for _, name := range credentials.Names {
			if w.changes[name] != nil {
				saved = append(saved, providerShort(name))
			}
		}
		fmt.Fprintf(&b, "  %-12s saved %s\n", "Keys", strings.Join(saved, ", "))
	}
	if checks, ok := credentialResult["checks"].(map[string]string); ok {
		var parts []string
		for _, provider := range []string{"jev", "subq_snap", "openai_decisions"} {
			if result, ok := checks[provider]; ok {
				parts = append(parts, providerCheckName(provider)+" "+result)
			}
		}
		fmt.Fprintf(&b, "  %-12s %s\n", "Key check", strings.Join(parts, " · "))
	}
	b.WriteString("\nStart a fresh agent session to use these settings. Run cw status for details.\n")
	if info.first {
		b.WriteString("If cw is not found, open a new terminal.\n")
	}
	return b.String()
}

func providerCheckName(provider string) string {
	switch provider {
	case "jev":
		return "TypeSafe"
	case "subq_snap":
		return "SubQ"
	case "openai_decisions":
		return "OpenAI"
	}
	return provider
}
