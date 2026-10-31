package workflow

import "context"

// Object retains the deployed version-one ownership and recovery JSON schema.
type Object = map[string]any

type Paths struct {
	Source string
	Home   string
	Codex  string
	Claude string
	State  string
}

// RuntimeIdentity identifies a verified native manager independently of the
// active skill snapshot. Rolling back skills must not downgrade their manager.
type RuntimeIdentity struct {
	Revision string `json:"revision"`
	Version  string `json:"version"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

type RuntimeCandidate struct {
	Path     string
	Identity RuntimeIdentity
}

type RuntimePreparer func(context.Context, string, string, string) (RuntimeCandidate, error)
