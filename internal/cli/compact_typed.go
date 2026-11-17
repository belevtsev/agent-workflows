package cli

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
)

func runContextRead(ctx context.Context, cfg Config, args []string) error {
	return runContextSource(ctx, cfg, args, "read")
}

func runContextSearch(ctx context.Context, cfg Config, args []string) error {
	return runContextSource(ctx, cfg, args, "search")
}

func runContextSource(ctx context.Context, cfg Config, args []string, kind string) error {
	flags := newFlags("context " + kind)
	task := flags.String("task", "", "")
	budget := flags.Int("max-bytes", contextpack.DefaultBytes, "")
	flags.Bool("json", false, "") // Like collect, these interfaces always emit JSON.
	op := contextpack.Operation{Kind: kind}
	if kind == "read" {
		op.ID = "source"
		flags.IntVar(&op.Start, "start", 0, "")
		flags.IntVar(&op.End, "end", 0, "")
	} else {
		op.ID = "matches"
		flags.BoolVar(&op.Regex, "regex", false, "")
	}
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	operands := append(p.pos, p.rest...)
	if kind == "read" {
		if len(operands) != 1 {
			return usagef("read requires one repository-relative path")
		}
		op.Path = operands[0]
	} else {
		if len(operands) < 1 || len(operands) > 2 {
			return usagef("search requires a pattern and an optional repository-relative path")
		}
		op.Pattern = operands[0]
		if len(operands) == 2 {
			op.Path = operands[1]
		}
	}
	if *task != "" && !taskName.MatchString(*task) {
		return usagef("--task requires a valid task name")
	}
	if err := compactBudget(*budget); err != nil {
		return err
	}
	request := contextpack.Request{Version: 1, Operations: []contextpack.Operation{op}}
	// Share collect's canonical validation before opening the repository or cache.
	data, _ := json.Marshal(request)
	request, err = contextpack.Decode(bytes.NewReader(data))
	if err != nil {
		return usagef("context request: %v", err)
	}
	return collectContextRequest(ctx, cfg, request, *task, *budget)
}
