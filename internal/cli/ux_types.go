package cli

import "context"

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
