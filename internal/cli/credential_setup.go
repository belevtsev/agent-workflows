package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/belevtsev/agent-workflows/internal/consult"
	"github.com/belevtsev/agent-workflows/internal/credentials"
	"github.com/belevtsev/agent-workflows/internal/workflow"
	"github.com/spf13/cobra"
)

func credentialDirectory(root *cobra.Command, config Config) (string, error) {
	roots, err := resolveRoots(root, config)
	if err != nil {
		return "", err
	}
	// A legacy installation counts as no recorded installation here, so
	// read-only commands keep working with the default roots.
	installation, err := workflow.RecordedInstallation(workflow.Normalize(roots.state))
	if errors.Is(err, workflow.ErrLegacyInstallation) {
		installation, err = workflow.Installation{}, nil
	}
	if err != nil {
		return "", err
	}
	explicit, _ := root.PersistentFlags().GetString("config-dir")
	return workflow.ResolveConfigDir(workflow.Normalize(roots.home), explicit, installation.ConfigDir, config.Getenv)
}

func credentialEnvironment(root *cobra.Command, config Config) (func(string) string, error) {
	directory, err := credentialDirectory(root, config)
	if err != nil {
		return nil, err
	}
	return (credentials.Resolver{Directory: directory, Getenv: config.Getenv}).Environment()
}

func providerName(name string) string {
	switch name {
	case credentials.TYPESAFE_API_KEY:
		return "TypeSafe (Jev)"
	case credentials.SUBQ_API_KEY:
		return "SubQ (Snap)"
	case credentials.OPENAI_API_KEY:
		return "OpenAI Decisions"
	}
	return name
}

func providerShort(name string) string {
	switch name {
	case credentials.TYPESAFE_API_KEY:
		return "TypeSafe"
	case credentials.SUBQ_API_KEY:
		return "SubQ"
	case credentials.OPENAI_API_KEY:
		return "OpenAI"
	}
	return name
}

// loadKeys records which keys are saved, without keeping their values.
func (w *setupWizard) loadKeys() error {
	if w.keysLoaded {
		return nil
	}
	snapshot, err := (credentials.Store{Directory: w.directory}).Read()
	if err != nil {
		return err
	}
	w.generation = snapshot.Generation
	for _, name := range credentials.Names {
		w.stored[name] = snapshot.Keys[name] != ""
	}
	clear(snapshot.Keys)
	w.keysLoaded = true
	return nil
}

func (w *setupWizard) keyStatus(name string) string {
	environment := w.config.Getenv(name) != ""
	switch {
	case w.changes[name] != nil && environment:
		return "new key ready · environment value still wins"
	case w.changes[name] != nil:
		return "new key ready"
	case environment && w.stored[name]:
		return "from environment · overrides saved key"
	case environment:
		return "from environment"
	case w.stored[name]:
		return "saved"
	}
	return "not set"
}

// keySummary describes every key for the review; brief shortens the online
// check note for the one-line step summary.
func (w *setupWizard) keySummary(brief bool) string {
	parts := make([]string, 0, len(credentials.Names)+1)
	known := false
	for _, name := range credentials.Names {
		known = known || w.changes[name] != nil || w.config.Getenv(name) != "" || w.stored[name]
	}
	if !known {
		parts = append(parts, "none set")
	}
	for _, name := range credentials.Names {
		if !known {
			break
		}
		status := "not set"
		switch {
		case w.changes[name] != nil:
			status = "new"
		case w.config.Getenv(name) != "":
			status = "environment"
		case w.stored[name]:
			status = "saved"
		}
		parts = append(parts, providerShort(name)+" "+status)
	}
	if w.checkKeys && brief {
		parts = append(parts, "online check")
	} else if w.checkKeys {
		parts = append(parts, "online check after applying")
	}
	return strings.Join(parts, " · ")
}

func validKey(key string) string {
	if key == "" {
		return ""
	}
	invalid := len(key) > 64<<10 || !utf8.ValidString(key) || strings.TrimSpace(key) != key
	for _, r := range key {
		invalid = invalid || unicode.IsSpace(r) || unicode.IsControl(r)
	}
	if invalid {
		return "A key is one line of text without spaces or control characters."
	}
	return ""
}

// keysStep lists every provider key on one screen. Choosing a key opens a
// hidden field; the online check and each shipped adviser's "Use at
// checkpoints" are toggles that change no key; Continue moves on.
func (w *setupWizard) keysStep(ctx context.Context) (nav, error) {
	if err := w.loadKeys(); err != nil {
		return navQuit, err
	}
	for {
		var items []menuItem
		uses := map[string]*bool{}
		for _, name := range credentials.Names {
			items = append(items, menuItem{label: providerName(name), value: name, detail: w.keyStatus(name)})
			// A shipped adviser's toggle follows its key row.
			for _, id := range w.view.Advisers {
				if variable, _ := consult.CredentialVariable(id); variable == name {
					use := w.advisers[id]
					uses[id] = &use
					items = append(items, menuItem{label: "Use at checkpoints", value: "use:" + id, detail: providerCheckName(id), checked: &use})
				}
			}
		}
		check := w.checkKeys
		items = append(items,
			menuItem{label: "Check keys online", value: "check", detail: "after applying · may cost a little", checked: &check},
			menuItem{label: "Continue", value: "continue"})
		intro := "Optional. Saved keys go to a private plaintext file in " + w.short(w.directory) + ". A key set in your environment takes precedence."
		if len(uses) > 0 {
			intro += " An unchecked adviser keeps its key and sits out tasks started afterwards."
		}
		m := &menu{step: stepKeys, title: "Adviser keys", help: intro, items: items, value: "continue", back: true}
		value, n, err := w.pick(ctx, m)
		w.checkKeys = check
		for id, use := range uses {
			w.advisers[id] = *use
		}
		if err != nil || n != navNext {
			return n, err
		}
		// The screen returns to Continue after each key, so Enter moves on.
		if value == "continue" {
			return navNext, nil
		}
		back := "Esc goes back"
		if w.plain {
			back = "b goes back"
		}
		help := "Paste or type the " + value + " value. Enter with an empty field keeps the current key; " + back + " without changes."
		if w.config.Getenv(value) != "" {
			help += " " + value + " is also set in your environment and will take precedence."
		}
		key, n, err := w.edit(ctx, editSpec{step: stepKeys, title: providerName(value) + " key", help: help, secret: true, validate: validKey})
		if err != nil || n == navQuit {
			return n, err
		}
		if n == navNext && key != "" {
			w.changes[value] = &key
		}
	}
}

func (w *setupWizard) SaveCredentials(ctx context.Context) (workflow.Object, error) {
	report := workflow.Object{"status": "unchanged", "committed": false}
	if len(w.changes) > 0 {
		result, err := (credentials.Store{Directory: w.directory}).Save(ctx, w.changes, &w.generation)
		report["committed"] = result.Committed
		report["durability_verified"] = result.DurabilityVerified
		if err != nil {
			report["status"] = "not_saved"
			if result.Committed {
				report["status"] = "persistence_unconfirmed"
			}
			return report, errors.New("settings were applied; credential storage did not complete reliably; reload saved keys before retrying")
		}
		if result.Committed {
			report["status"] = "saved"
		}
	}
	if err := ctx.Err(); err != nil {
		report["check_status"] = "cancelled"
		return report, err
	}
	if w.checkKeys {
		fmt.Fprintln(w.config.Err, "Checking available keys with synthetic requests…")
		getenv, err := (credentials.Resolver{Directory: w.directory, Getenv: w.config.Getenv}).Environment()
		if err != nil {
			return report, err
		}
		checks, err := consult.CheckCredentials(ctx, getenv)
		report["checks"] = checks
		if err != nil {
			report["check_status"] = "failed"
			return report, err
		}
		report["check_status"] = "complete"
	}
	return report, nil
}
