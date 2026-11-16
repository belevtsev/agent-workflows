package proc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

const foregroundHelper = "kitsu-proc-foreground-helper"

func TestForegroundHelper(t *testing.T) {
	index := slices.Index(os.Args, foregroundHelper)
	if index < 0 {
		return
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(81)
	}
	data, _ := json.Marshal(struct {
		Args          []string
		Input, Marker string
	}{os.Args[index+1:], string(input), os.Getenv("KITSU_PROC_FOREGROUND_MARKER")})
	fmtData := append(data, '\n')
	_, _ = os.Stdout.Write(fmtData)
	_, _ = io.WriteString(os.Stderr, "foreground diagnostic")
	if os.Getenv("KITSU_PROC_FOREGROUND_EXIT") == "wait" {
		time.Sleep(time.Hour)
	}
	code, _ := strconv.Atoi(os.Getenv("KITSU_PROC_FOREGROUND_EXIT"))
	os.Exit(code)
}

func TestForegroundPreservesLiteralArgumentsStreamsAndStatus(t *testing.T) {
	t.Parallel()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	request := "literal 'quotes'; $(program) C:\\work\\file\nnext line"
	var out, diagnostics bytes.Buffer
	got := proc.RunForeground(t.Context(), proc.ForegroundSpec{
		Path: program, Args: []string{"-test.run=^TestForegroundHelper$", "--", foregroundHelper, request},
		Env:   []string{"KITSU_PROC_FOREGROUND_MARKER=explicit", "KITSU_PROC_FOREGROUND_EXIT=17", "GORACE=atexit_sleep_ms=0"},
		Stdin: strings.NewReader("interactive input"), Stdout: &out, Stderr: &diagnostics,
	})
	var result struct {
		Args          []string
		Input, Marker string
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || !slices.Equal(result.Args, []string{request}) || result.Input != "interactive input" || result.Marker != "explicit" {
		t.Fatalf("lost argv, input or explicit environment: %s; %v", &out, err)
	}
	if got.Code != 17 || got.Err == nil || got.Stopped || got.Cleanup != nil || diagnostics.String() != "foreground diagnostic" {
		t.Fatalf("lost exit or diagnostics: %+v, %q", got, &diagnostics)
	}
}

func TestForegroundNilEnvironmentDoesNotInherit(t *testing.T) {
	t.Setenv("KITSU_PROC_FOREGROUND_MARKER", "must not inherit")
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	got := proc.RunForeground(t.Context(), proc.ForegroundSpec{Path: program, Args: []string{"-test.run=^TestForegroundHelper$", "--", foregroundHelper}, Stdout: &out})
	var result struct{ Marker string }
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || got.Err != nil || got.Code != 0 || result.Marker != "" {
		t.Fatalf("nil environment inherited caller values: %s; %+v; %v", &out, got, err)
	}
}

type foregroundCancelWriter struct{ cancel context.CancelFunc }

func (w foregroundCancelWriter) Write(data []byte) (int, error) {
	w.cancel()
	return len(data), nil
}

func TestForegroundCancellationAndStartFailure(t *testing.T) {
	t.Parallel()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	running, stop := context.WithCancel(ctx)
	defer stop()
	ended, end := context.WithCancel(ctx)
	end()
	for _, test := range []struct {
		name string
		ctx  context.Context
		out  io.Writer
	}{
		{name: "during execution", ctx: running, out: foregroundCancelWriter{stop}},
		{name: "before execution", ctx: ended, out: io.Discard},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := proc.RunForeground(test.ctx, proc.ForegroundSpec{
				Path: program, Args: []string{"-test.run=^TestForegroundHelper$", "--", foregroundHelper},
				Env: []string{"KITSU_PROC_FOREGROUND_EXIT=wait", "GORACE=atexit_sleep_ms=0"}, Stdout: test.out,
			})
			if got.Code != -1 || !got.Cancelled || !got.Stopped || got.Err == nil || ctx.Err() != nil {
				t.Fatalf("cancellation not propagated: %+v; guard=%v", got, ctx.Err())
			}
		})
	}
	got := proc.RunForeground(ctx, proc.ForegroundSpec{Path: filepath.Join(t.TempDir(), "missing-host")})
	if got.Code != -1 || got.Err == nil || got.Stopped {
		t.Fatalf("start failure treated as a child exit: %+v", got)
	}
}
