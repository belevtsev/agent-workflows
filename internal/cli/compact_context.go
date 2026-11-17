package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
)

func compactEngine(ctx context.Context, cfg Config) (*session, *contextpack.Engine, error) {
	s, err := open(ctx, cfg, true)
	if err != nil {
		return nil, nil, err
	}
	e, err := contextpack.Open(s.repo)
	if err != nil {
		return nil, nil, failf("%v", err)
	}
	return s, e, nil
}

func runContextCollect(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("context collect")
	requestFile := flags.String("request", "", "")
	task := flags.String("task", "", "")
	budget := flags.Int("max-bytes", contextpack.DefaultBytes, "")
	flags.Bool("json", false, "") // This interface always emits JSON.
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if p.dash || len(p.pos) != 0 || *requestFile == "" || *task != "" && !taskName.MatchString(*task) {
		return usagef("collect requires --request <file|-> and an optional valid --task")
	}
	if err := compactBudget(*budget); err != nil {
		return err
	}
	reader := cfg.In
	if *requestFile != "-" {
		f, err := os.Open(*requestFile)
		if err != nil {
			return failf("read context request: %v", err)
		}
		defer func() { _ = f.Close() }()
		reader = f
	}
	request, err := contextpack.Decode(reader)
	if err != nil {
		return usagef("context request: %v", err)
	}
	return collectContextRequest(ctx, cfg, request, *task, *budget)
}

func collectContextRequest(ctx context.Context, cfg Config, request contextpack.Request, task string, budget int) error {
	s, e, err := compactEngine(ctx, cfg)
	if err != nil {
		return err
	}
	before, err := e.Snapshot(ctx)
	if err != nil {
		return failf("%v", err)
	}
	var metadata json.RawMessage
	if task != "" {
		views, err := uxTaskViews(ctx, s, task)
		if err != nil {
			return failf("%v", err)
		}
		metadata, _ = json.Marshal(views[0])
	}
	a, hit, err := e.Collect(ctx, request, metadata)
	if err != nil {
		return compactFailure(err)
	}
	if a.State != before {
		return failf("repository changed while preparing task context; collect again")
	}
	return printCompact(cfg, a, "", "", budget, hit)
}

func runContextShow(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("context show")
	item, cursor := flags.String("item", "", ""), flags.String("cursor", "", "")
	budget := flags.Int("max-bytes", contextpack.DefaultBytes, "")
	flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if p.dash || len(p.pos) != 1 {
		return usagef("show requires one artifact id")
	}
	if err := compactBudget(*budget); err != nil {
		return err
	}
	s, e, err := compactEngine(ctx, cfg)
	if err != nil {
		return err
	}
	a, err := e.Load(ctx, p.pos[0])
	if err != nil {
		return compactFailure(err)
	}
	if err := validateCompactMetadata(ctx, s, a); err != nil {
		return compactFailure(err)
	}
	if *item != "" && a.Kind == "checks" {
		for i := range a.Items {
			if a.Items[i].ID == *item && a.Items[i].LogRunID != "" {
				lines, err := compactLog(s, a.Items[i].LogRunID)
				if err != nil {
					return compactFailure(err)
				}
				a.Items[i].Lines = lines
			}
		}
	}
	return printCompact(cfg, a, *item, *cursor, *budget, false)
}

func compactBudget(budget int) error {
	if budget < 512 || budget > contextpack.MaxBytes {
		return usagef("--max-bytes must be between 512 and %d", contextpack.MaxBytes)
	}
	return nil
}

func printCompact(cfg Config, a contextpack.Artifact, item, cursor string, budget int, hit bool) error {
	data, err := contextpack.Render(a, item, cursor, budget, hit)
	if err != nil {
		return usagef("%v", err)
	}
	if _, err := cfg.Out.Write(data); err != nil {
		return failf("write context: %v", err)
	}
	return nil
}

func compactFailure(err error) error {
	if err == context.Canceled {
		return exit(130)
	}
	return failf("context: %v", err)
}

// collectLog keeps the runner's complete retained log, including its existing overflow marker.
func collectLog(name string, reader io.Reader) ([]contextpack.Line, error) {
	data, err := io.ReadAll(io.LimitReader(reader, contextpack.MaxArtifact+1))
	if err != nil {
		return nil, err
	}
	if len(data) > contextpack.MaxArtifact {
		return nil, fmt.Errorf("log exceeds artifact size limit: %s", name)
	}
	lines := contextpack.Lines(name, 1, string(data))
	number, offset := 1, 0
	for i := range lines {
		lines[i].Number, lines[i].Offset = number, offset
		text := lines[i].Text
		number += strings.Count(text, "\n")
		if last := strings.LastIndexByte(text, '\n'); last >= 0 {
			offset = len(text) - last - 1
		} else {
			offset += len(text)
		}
	}
	return lines, nil
}

func compactLog(s *session, id string) ([]contextpack.Line, error) {
	if len(id) != 25 || strings.ContainsAny(id, "/\\:\x00") {
		return nil, fmt.Errorf("invalid retained log id")
	}
	root, err := os.OpenRoot(s.store.Dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open("logs/" + id + ".log")
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("retained log is not a regular file")
	}
	return collectLog("logs/"+id+".log", f)
}
