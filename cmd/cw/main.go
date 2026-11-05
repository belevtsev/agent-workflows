// cw manages the installation of the agent workflow skills.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/belevtsev/agent-workflows/internal/bundled"
	"github.com/belevtsev/agent-workflows/internal/cli"
)

var version = "dev"
var revision = "unknown"
var builtAt = "unknown"
var releaseIdentity = ""

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// A plain source build carries no workflow payload. Leaving Bundle nil makes
	// every lifecycle command refuse instead of acting on recorded state.
	var bundle bundled.Provider
	if bundled.Present() {
		bundle = bundled.Embedded
	}
	if err := cli.Execute(ctx, os.Args[1:], cli.Config{Version: version, Revision: revision, BuiltAt: builtAt, ReleaseIdentity: releaseIdentity, Bundle: bundle, In: os.Stdin, Out: os.Stdout, Err: os.Stderr}); err != nil {
		// A command that already reported its own result carries no message.
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, "agent-workflows:", err)
		}
		os.Exit(cli.ExitCode(err))
	}
}
