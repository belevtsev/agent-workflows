package cli

import (
	"context"
	"fmt"
	"runtime"
)

func runVersion(_ context.Context, cfg Config, args []string) error {
	if err := noArguments("version", args); err != nil {
		return err
	}
	_, err := fmt.Fprintf(cfg.Out, "gatecub %s\nrevision %s\nbuilt %s\n%s/%s\n",
		cfg.Version, cfg.Revision, cfg.BuiltAt, runtime.GOOS, runtime.GOARCH)
	return err
}
