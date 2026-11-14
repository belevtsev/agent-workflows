// cwdev runs maintainer-only evaluations. It is not part of the released cw.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const help = `cwdev — maintainer evaluations

Usage:
  cwdev eval multilingual [--source PATH] [--live --output-dir PATH] [--config-dir PATH]

Evaluation validates its corpus offline unless --live explicitly allows one
hosted baseline.`

func run(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h" {
		fmt.Println(help)
		return nil
	}
	if arguments[0] == "eval" {
		return evaluate(ctx, arguments[1:])
	}
	return fmt.Errorf("unknown developer command: %s", arguments[0])
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if status, ok := errors.AsType[exitStatus](err); ok {
			os.Exit(int(status))
		}
		fmt.Fprintln(os.Stderr, "cwdev:", err)
		os.Exit(1)
	}
}
