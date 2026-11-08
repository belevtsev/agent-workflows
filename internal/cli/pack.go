package cli

import (
	"fmt"
	"slices"
	"strings"

	agentworkflows "github.com/belevtsev/agent-workflows/v3"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/belevtsev/agent-workflows/v3/internal/userconfig"
	"github.com/spf13/cobra"
)

// packListReport is the result of cw pack list: every built-in pack and
// whether the effective selection holds it.
type packListReport struct {
	Selection string      `json:"selection"`
	Packs     []packEntry `json:"packs"`
}

type packEntry struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Detect       []string `json:"detect"`
	EvidenceRule string   `json:"evidence_rule"`
	Selected     bool     `json:"selected"`
}

// packChangeReport is the result of cw pack add or remove: the selection
// after the change and whether packs.json was written.
type packChangeReport struct {
	Selected []string `json:"selected"`
	Written  bool     `json:"written"`
}

// packSelection is the built-in packs and the user's effective selection:
// the ids in packs.json when it exists (explicit), else the ids of
// langpack.DefaultSelection.
type packSelection struct {
	builtIn   []langpack.Manifest
	directory string
	explicit  bool
	ids       []string
}

func builtInPacks() ([]langpack.Manifest, error) {
	builtIn, err := langpack.LoadManifests(agentworkflows.Packs)
	if err != nil {
		return nil, fmt.Errorf("built-in packs are invalid: %w", err)
	}
	return builtIn, nil
}

// effectiveSelection reads packs.json from the configuration directory. It is
// the only place cw applies R-DEFAULT-SELECTION.
func effectiveSelection(root *cobra.Command, config Config) (packSelection, error) {
	builtIn, err := builtInPacks()
	if err != nil {
		return packSelection{}, err
	}
	directory, err := credentialDirectory(root, config)
	if err != nil {
		return packSelection{}, fmt.Errorf("cannot read the pack selection: %w", err)
	}
	ids, found, err := userconfig.SelectedPacks(directory)
	if err != nil {
		return packSelection{}, fmt.Errorf("cannot read the pack selection: %w", err)
	}
	selection := packSelection{builtIn: builtIn, directory: directory, explicit: found, ids: []string{}}
	if found {
		selection.ids = append(selection.ids, ids...)
		return selection, nil
	}
	for _, manifest := range langpack.DefaultSelection(builtIn) {
		selection.ids = append(selection.ids, manifest.ID)
	}
	slices.Sort(selection.ids)
	return selection, nil
}

// startPacks returns the selection kind and the selected built-in manifests
// that cw task start records. An explicitly selected pack that is not built in
// fails the start.
func startPacks(root *cobra.Command, config Config) (string, []langpack.Manifest, error) {
	selection, err := effectiveSelection(root, config)
	if err != nil {
		return "", nil, err
	}
	selected := []langpack.Manifest{}
	for _, id := range selection.ids {
		index := slices.IndexFunc(selection.builtIn, func(manifest langpack.Manifest) bool { return manifest.ID == id })
		if index < 0 {
			return "", nil, fmt.Errorf("selected pack %q is not built into this cw; run 'cw pack remove %s'", id, id)
		}
		selected = append(selected, selection.builtIn[index])
	}
	if selection.explicit {
		return taskflow.SelectionExplicit, selected, nil
	}
	return taskflow.SelectionDefault, selected, nil
}

func addPackCommands(root *cobra.Command, config Config) {
	pack := &cobra.Command{
		Use:     "pack",
		Aliases: []string{"lang"},
		Short:   "List, add or remove the language and domain packs selected for your tasks",
		Long: "Manage the pack selection in packs.json of the configuration directory: --config-dir, else $XDG_CONFIG_HOME/agent-workflows, else ~/.config/agent-workflows. " +
			"With no packs.json every built-in language pack is selected; 'cw pack add' and 'cw pack remove' write an explicit packs.json that replaces that default for all your repositories, and deleting the file restores it. " +
			"'cw task start' records the selected packs whose detect file is at the top of the selected path, and 'cw task verify' applies their evidence rules. 'cw lang' is an alias.",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	oneID := func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return usage(fmt.Errorf("pack %s takes exactly one pack id", cmd.Name()))
		}
		return nil
	}
	pack.AddCommand(&cobra.Command{Use: "list", Short: "Print the built-in packs and which are selected", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		selection, err := effectiveSelection(root, config)
		if err != nil {
			return err
		}
		report := packListReport{Selection: taskflow.SelectionDefault, Packs: []packEntry{}}
		if selection.explicit {
			report.Selection = taskflow.SelectionExplicit
		}
		for _, manifest := range selection.builtIn {
			report.Packs = append(report.Packs, packEntry{ID: manifest.ID, Kind: manifest.Kind, Detect: manifest.Detect, EvidenceRule: manifest.EvidenceRule, Selected: slices.Contains(selection.ids, manifest.ID)})
		}
		return writeJSON(cmd.OutOrStdout(), report)
	}})
	change := func(cmd *cobra.Command, edit func([]string) []string) error {
		selection, err := effectiveSelection(root, config)
		if err != nil {
			return err
		}
		next := edit(slices.Clone(selection.ids))
		slices.Sort(next)
		report := packChangeReport{Selected: next}
		dryRun, _ := root.PersistentFlags().GetBool("dry-run")
		if (!selection.explicit || !slices.Equal(next, selection.ids)) && !dryRun {
			if err := userconfig.SavePacks(selection.directory, next); err != nil {
				return fmt.Errorf("cannot save the pack selection: %w", err)
			}
			report.Written = true
		}
		return writeJSON(cmd.OutOrStdout(), report)
	}
	pack.AddCommand(&cobra.Command{Use: "add ID", Short: "Select a built-in pack", Args: oneID, RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		builtIn, err := builtInPacks()
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(builtIn, func(manifest langpack.Manifest) bool { return manifest.ID == id }) {
			names := []string{}
			for _, manifest := range builtIn {
				names = append(names, manifest.ID)
			}
			return usage(fmt.Errorf("unknown pack %q; built-in packs: %s", id, strings.Join(names, ", ")))
		}
		return change(cmd, func(ids []string) []string {
			if slices.Contains(ids, id) {
				return ids
			}
			return append(ids, id)
		})
	}})
	pack.AddCommand(&cobra.Command{Use: "remove ID", Short: "Deselect a pack", Args: oneID, RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		if !langpack.ValidID(id) {
			return usage(fmt.Errorf("invalid pack id %q", id))
		}
		return change(cmd, func(ids []string) []string {
			return slices.DeleteFunc(ids, func(selected string) bool { return selected == id })
		})
	}})
	root.AddCommand(pack)
}
