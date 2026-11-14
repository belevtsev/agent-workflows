package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/gitx"
	"github.com/belevtsev/agent-workflows/v3/internal/store"
)

// MaxContext is the most characters `cw context` prints.
const MaxContext = 1000

// runContext prints what an agent should know at the start of a session in
// a gated repository. It prints nothing where the root of the working tree
// holds no declaration, and it never fails a session: outside a repository
// it prints nothing and exits 0.
func runContext(ctx context.Context, cfg Config, args []string) error {
	if err := noArguments("context", args); err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil
	}
	repo, err := gitx.Open(ctx, dir)
	if err != nil || repo.Root == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(repo.Root, decl.FileName)); err != nil {
		return nil
	}
	mode, _ := store.Open(repo.CommonDir).Mode()
	_, configured, _ := repo.HooksDir(ctx)
	fmt.Fprint(cfg.Out, contextText(gate.State(ctx, repo), mode, configured))
	return nil
}

// contextText is the text for one state of the gate. configured says that
// core.hooksPath is set, so that cw writes no hook in this clone.
func contextText(hooks, mode string, configured bool) string {
	var b strings.Builder
	b.WriteString("This repository is gated by cw. git allows a commit or a push only when every check declared in cw.toml has a recorded passing run on exactly the content that is committed or pushed.\n")
	b.WriteString("- `cw run` executes the declared checks that have no passing run on the current content and records the results. Run it before you commit.\n")
	b.WriteString("- `cw status` says whether a commit would be allowed and which checks are missing.\n")
	b.WriteString("- A denied commit or push names what is missing. Produce the evidence with `cw run`; do not go around the hook.\n")
	switch {
	case hooks != gate.Installed && configured:
		b.WriteString("The hooks git runs in this clone (core.hooksPath) do not all call cw, so nothing is gated here yet; `cw init` prints the line to add to each.\n")
	case hooks != gate.Installed:
		b.WriteString("The hooks are not installed in this clone, so nothing is gated here yet; `cw init` installs them.\n")
	case mode == store.Observe:
		b.WriteString("The hooks are installed. The gate only observes here: it records what it would deny and allows the operation.\n")
	default:
		b.WriteString("The hooks are installed.\n")
	}
	return b.String()
}
