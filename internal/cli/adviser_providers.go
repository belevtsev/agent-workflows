package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
)

func adviserProviderName(provider string) string {
	if provider == "" {
		return "cli"
	}
	return provider
}

func runAdviserProviders(cfg Config, args []string) error {
	flags := newFlags("advisers providers")
	asJSON := flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 || p.dash {
		return usagef("usage: kitsu advisers providers [--json]")
	}
	if *asJSON {
		return printJSON(cfg, struct {
			Experimental     bool                `json:"experimental"`
			EnabledByDefault bool                `json:"enabled_by_default"`
			Providers        []advisers.Provider `json:"providers"`
		}{Experimental: true, EnabledByDefault: false, Providers: advisers.Providers()})
	}
	fmt.Fprintln(cfg.Out, "Experimental adviser presets; none are enabled by default.")
	fmt.Fprintln(cfg.Out, "Configure an adviser and repository role access to opt in.")
	fmt.Fprintln(cfg.Out)
	w := tabwriter.NewWriter(cfg.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tDEFAULT MODEL\tBASE URL\tAPI KEY ENV\tKINDS")
	for _, p := range advisers.Providers() {
		model, base, key := p.Model, p.BaseURL, p.APIKeyEnv
		if p.ID == "systemone" {
			model, base, key = "required", "required", "required"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.ID, model, base, key, strings.Join(p.Kinds, ","))
	}
	return w.Flush()
}

func printConfiguredAdvisersJSON(cfg Config, settings *advisers.Config) error {
	type entry struct {
		advisers.Definition
		Kinds []string `json:"kinds"`
	}
	entries := make(map[string]entry, len(settings.Advisers))
	for name, d := range settings.Advisers {
		entries[name] = entry{d, advisers.Kinds(d.Provider)}
	}
	return printJSON(cfg, struct {
		*advisers.Config
		Advisers map[string]entry `json:"advisers"`
	}{settings, entries})
}

func printBoundAdvisersJSON(cfg Config, binding *advisers.Binding) error {
	type entry struct {
		advisers.Identity
		Kinds []string `json:"kinds"`
	}
	entries := make(map[string]entry, len(binding.Advisers))
	for name, d := range binding.Advisers {
		entries[name] = entry{d, advisers.Kinds(d.Provider)}
	}
	return printJSON(cfg, struct {
		*advisers.Binding
		Advisers map[string]entry `json:"advisers"`
	}{binding, entries})
}

func printAdviserAnswer(cfg Config, result advisers.Execution) {
	if result.Result == nil {
		fmt.Fprintln(cfg.Out, result.Answer)
		return
	}
	r := result.Result
	switch r.Kind {
	case "decision":
		fmt.Fprintf(cfg.Out, "Choice: %s\n", r.Choice)
	case "score":
		fmt.Fprintf(cfg.Out, "Score: %g\n", *r.Score)
	case "boolean":
		fmt.Fprintf(cfg.Out, "Probability of yes: %g\n", *r.ProbabilityTrue)
	}
	if r.Confidence != nil {
		fmt.Fprintf(cfg.Out, "Confidence: %g\n", *r.Confidence)
	}
	for _, p := range r.Probabilities {
		fmt.Fprintf(cfg.Out, "  %s: %g\n", p.ID, p.Probability)
	}
}
