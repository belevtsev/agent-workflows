package langpack

import (
	"context"
	"errors"
	"testing"
)

// TrustedGOROOT is the GOROOT that the resolver of TrustGoOnPath reports.
const TrustedGOROOT = "/trusted/goroot"

// TrustGoOnPath makes LoadGoFacts, until t ends, run the go command that
// env's PATH names, found as ResolveGo finds it (findGo), without verifying
// it, so that a test can drive LoadGoFacts with a fake go script. The
// resolver runs nothing and reports TrustedGOROOT. ResolveGo itself, and
// LoadGoFacts with it, are tested in gotool_test.go. Call it only from a test
// that is not parallel, as it replaces a package variable.
func TrustGoOnPath(t testing.TB) {
	t.Helper()
	saved := resolveGo
	resolveGo = func(_ context.Context, _, _ string, env []string) (GoTool, error) {
		if path := findGo(env); path != "" {
			return GoTool{Path: path, GOROOT: TrustedGOROOT}, nil
		}
		return GoTool{}, errors.New("go was not found on PATH")
	}
	t.Cleanup(func() { resolveGo = saved })
}

// JudgeHelpers is judgeHelpers: it judges the assembler and linker a
// compiler at cc names, which a test can fake with a script.
var JudgeHelpers = judgeHelpers
