// cw manages the installation of the agent workflow skills.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/belevtsev/agent-workflows/internal/cli"
)

var version = "dev"
var revision = "unknown"
var builtAt = "unknown"
var releaseIdentity = ""

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.Execute(ctx, os.Args[1:], cli.Config{Version: version, Revision: revision, BuiltAt: builtAt, ReleaseIdentity: releaseIdentity, In: os.Stdin, Out: os.Stdout, Err: os.Stderr}); err != nil {
		// A command that already reported its own result carries no message.
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, "agent-workflows:", err)
		}
		os.Exit(cli.ExitCode(err))
	}
}
