package cli

import (
	"context"
	"errors"

	"github.com/belevtsev/kitsu/v3/internal/onboarding"
	"github.com/charmbracelet/x/term"
)

func onboardingTerminal(cfg Config) bool {
	for _, stream := range []any{cfg.In, cfg.Out, cfg.Err} {
		file, ok := stream.(interface{ Fd() uintptr })
		if !ok || !term.IsTerminal(file.Fd()) {
			return false
		}
	}
	return true
}

func launchTaskHost(ctx context.Context, cfg Config, argv []string) error {
	if len(argv) != 2 {
		return failf("host handoff requires an executable and one literal prompt")
	}
	code, err := onboarding.Launch(ctx, onboarding.LaunchOptions{Executable: argv[0], Prompt: argv[1], In: cfg.In, Out: cfg.Out, Err: cfg.Err})
	if errors.Is(ctx.Err(), context.Canceled) {
		return exit(130)
	}
	if ctx.Err() != nil {
		return failf("host interrupted: %v", ctx.Err())
	}
	if err == nil {
		return nil
	}
	if code > 0 {
		return exit(code)
	}
	return failf("start host: %v", err)
}
