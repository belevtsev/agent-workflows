// cwdev runs maintainer-only evaluations and tools. It is not part of the
// released cw.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const help = `cwdev — maintainer evaluations and tools

Usage:
  cwdev eval multilingual [--source PATH] [--live --output-dir PATH] [--config-dir PATH]
  cwdev diagram-svg [--root DIR] [--out FILE] [--check] [--timeout DURATION] SPEC.json
  cwdev diagram-svg --all [--root DIR] [--check] [--timeout DURATION]

Evaluation validates its corpus offline unless --live explicitly allows one
hosted baseline.

diagram-svg exports an archify spec to the SVG the viewer's Export > SVG Auto
button downloads, next to the spec or at --out. It needs node and Chrome.
--all exports every docs/diagrams/*.json under --root (default .); --check
writes nothing and fails when a committed SVG differs from a fresh export.`

func run(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h" {
		fmt.Println(help)
		return nil
	}
	switch arguments[0] {
	case "eval":
		return evaluate(ctx, arguments[1:])
	case "diagram-svg":
		return diagramSVG(ctx, arguments[1:])
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
