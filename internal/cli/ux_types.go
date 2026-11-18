package cli

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/harness"
)

// uxCommand describes a preferred command. Help and shell completion use the
// same metadata as dispatch; legacy commands retain their existing handlers.
type uxCommand struct {
	path, args, summary string
	flags               []uxFlag
	operands            [][2]string
	examples            []string
	run                 func(context.Context, Config, []string) error
	host, requiredHost  bool
	hidden              bool
	guard               string
	replacement         string
}

type uxFlag struct {
	name, value, description string
	choices                  []string
}

func uxOption(name, value, description string, choices ...string) uxFlag {
	return uxFlag{name, value, description, choices}
}

var taskEntry = uxOption("entry", "stage", "start at "+strings.Join(harness.Entries, ", "), harness.Entries...)
var taskUntil = uxOption("until", "outcome", "finish at "+strings.Join(harness.Outcomes, ", "), harness.Outcomes...)

func (option uxFlag) bindChoice(flags *flag.FlagSet, dst *string) {
	flags.Func(option.name, option.description, func(value string) error {
		if !slices.Contains(option.choices, value) {
			return fmt.Errorf("--%s must be one of %s", option.name, strings.Join(option.choices, ", "))
		}
		*dst = value
		return nil
	})
}
