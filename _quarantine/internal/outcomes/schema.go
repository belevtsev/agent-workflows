package outcomes

import (
	_ "embed"
	"slices"
)

//go:embed schema.json
var observationSchema []byte

// Schema returns the versioned interchange schema and a minimal example. It is
// local documentation; generating it never resolves projects or credentials.
func Schema() []byte { return slices.Clone(observationSchema) }
