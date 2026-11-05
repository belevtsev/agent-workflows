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
	installation, err := workflow.RecordedInstallation(workflow.Normalize(roots.state))
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

func (w *setupWizard) credentials(ctx context.Context) error {
	store := credentials.Store{Directory: w.directory}
	snapshot, err := store.Read()
	if err != nil {
		return err
	}
	w.generation = snapshot.Generation
	for _, name := range credentials.Names {
		stored := snapshot.Keys[name] != ""
		environment := w.config.Getenv(name) != ""
		description := "Optional. Enter skips this key. Saved keys are plaintext in a private local file."
		if stored {
			description = "Enter keeps the saved key. A new value replaces it in the private local plaintext file."
		}
		if environment {
			description += " The environment key takes precedence; a saved replacement will be shadowed until that environment key is removed."
		}
		action := "keep"
		if err = w.choose(ctx, "03 / Keys", name, description, []wizardOption{{"Keep existing / skip", "keep"}, {"Enter a stored key", "enter"}}, &action); err != nil {
			return err
		}
		if action == "enter" {
			key := ""
			if err = w.input(ctx, "03 / Keys", name, "Hidden input. Empty input keeps the existing key or skips this provider.", &key, true); err != nil {
				return err
			}
			if key != "" {
				invalid := len(key) > 64<<10 || !utf8.ValidString(key) || strings.TrimSpace(key) != key
				for _, r := range key {
					invalid = invalid || unicode.IsSpace(r) || unicode.IsControl(r)
				}
				if invalid {
					return errors.New("API key must be bounded text without whitespace or control characters")
				}
				w.changes[name] = &key
			}
		}
	}
	probe := "no"
	if err = w.choose(ctx, "03 / Keys", "Check available keys online?", "Optional, off by default. Sends one tiny synthetic request per available provider; may incur a small charge. No project data is sent.", []wizardOption{{"Skip online check", "no"}, {"Check keys after applying", "yes"}}, &probe); err != nil {
		return err
	}
	w.checkKeys = probe == "yes"
	return nil
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
