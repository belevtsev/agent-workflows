package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/pluginsetup"
)

func runUXAgentSetup(ctx context.Context, cfg Config, args []string) error {
	var host, archiveDir string
	flags := newFlags("agent setup")
	bindOnboardingHost(flags, &host)
	flags.Func("archive-dir", "", func(value string) error {
		if archiveDir != "" || value == "" {
			return errors.New("supply one nonempty --archive-dir")
		}
		archiveDir = value
		return nil
	})
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 || p.dash {
		return usagef("agent setup takes no positional arguments")
	}
	if archiveDir == "" {
		return usagef("--archive-dir is required")
	}
	marketplace, err := pluginsetup.Install(ctx, host, archiveDir)
	if errors.Is(err, context.Canceled) {
		return exit(130)
	}
	if err != nil {
		return failf("set up %s plugin: %s", host, decl.Plain(err.Error()))
	}
	fmt.Fprintf(cfg.Out, "Installed kitsu@kitsu for %s from %s.\n", host, decl.Plain(marketplace))
	return nil
}
