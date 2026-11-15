package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gate"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

// MaxContext is the most characters `kitsu context` prints.
const MaxContext = 1000

// runContext prints what an agent should know at the start of a session in
// a gated repository. It prints nothing where the root of the working tree
// holds no declaration, and it never fails a session: outside a repository
// it prints nothing and exits 0. For a host it reads the host's notice from
// standard input, which says where the session works; that is not always the
// directory the host starts a hook in.
func runContext(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("context")
	host := flags.String("host", "", "")
	if p, err := parseArgs(flags, args); err != nil || len(p.pos) > 0 || p.dash || (*host != "" && *host != "claude" && *host != "codex") {
		return usagef("usage: kitsu context [--host claude|codex], with the host's notice on standard input when a host is named")
	}
	dir, err := os.Getwd()
	var notice struct {
		Dir string `json:"cwd"`
	}
	if *host != "" && json.NewDecoder(cfg.In).Decode(&notice) == nil && notice.Dir != "" {
		dir, err = notice.Dir, nil
	}
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
// core.hooksPath is set, so that kitsu writes no hook in this clone. Where the
// hooks do not run, the text says so before it says what the gate does.
func contextText(hooks, mode string, configured bool) string {
	intro, state := "This repository is gated by kitsu. git", "The hooks are installed.\n"
	switch {
	case hooks != gate.Installed && configured:
		intro, state = "This repository declares checks for kitsu, but the hooks git runs in this clone (core.hooksPath) do not all call kitsu, so nothing is gated here yet; `kitsu init` prints the line to add to each. Once they call kitsu, git", ""
	case hooks != gate.Installed:
		intro, state = "This repository declares checks for kitsu, but the hooks are not installed in this clone, so nothing is gated here yet; `kitsu init` installs them. Once they are installed, git", ""
	case mode == store.Observe:
		state = "The hooks are installed. The gate only observes here: it records what it would deny and allows the operation.\n"
	}
	return intro + " allows a commit or a push only when every check declared in kitsu.toml has a recorded passing run on exactly the content that is committed or pushed.\n" +
		"- `kitsu run` executes the declared checks that have no passing run on the current content and records the results. Run it before you commit.\n" +
		"- `kitsu status` says whether a commit would be allowed and which checks are missing.\n" +
		"- A denied commit or push names what is missing. Produce the evidence with `kitsu run`; do not go around the hook.\n" + state
}
