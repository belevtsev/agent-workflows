// cw is the runtime for the agent-workflows skills.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/belevtsev/agent-workflows/v3/internal/cli"
)

// Release builds set these with -ldflags -X.
var (
	version  = "dev"
	revision = "unknown"
	builtAt  = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	info, _ := debug.ReadBuildInfo()
	v, r, b := buildIdentity(version, revision, builtAt, info)
	if err := cli.Execute(ctx, os.Args[1:], cli.Config{Version: v, Revision: r, BuiltAt: b, In: os.Stdin, Out: os.Stdout, Err: os.Stderr}); err != nil {
		// A command that already reported its own result carries no message.
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, "agent-workflows:", err)
		}
		os.Exit(cli.ExitCode(err))
	}
}

// buildIdentity returns the linker-set identity v, r and b, falling back to
// the module version and VCS stamp that go install and go build record in
// info. A linker-set value always wins; info may be nil.
func buildIdentity(v, r, b string, info *debug.BuildInfo) (string, string, string) {
	if info == nil {
		return v, r, b
	}
	if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch {
		case setting.Key == "vcs.revision" && r == "unknown":
			r = setting.Value
		case setting.Key == "vcs.time" && b == "unknown":
			b = setting.Value
		}
	}
	return v, r, b
}
