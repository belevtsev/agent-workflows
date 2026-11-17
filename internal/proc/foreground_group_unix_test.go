//go:build linux || darwin

package proc_test

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

func TestForegroundGroupHelper(t *testing.T) {
	if slices.Contains(os.Args, foregroundHelper) {
		fmt.Fprintln(os.Stdout, syscall.Getpgrp())
		os.Exit(0)
	}
}

func TestForegroundKeepsTheCallersProcessGroup(t *testing.T) {
	t.Parallel()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	got := proc.RunForeground(t.Context(), proc.ForegroundSpec{
		Path: program, Args: []string{"-test.run=^TestForegroundGroupHelper$", "--", foregroundHelper},
		Env: []string{"GORACE=atexit_sleep_ms=0"}, Stdout: &out,
	})
	group, err := strconv.Atoi(strings.TrimSpace(out.String()))
	if err != nil || got.Err != nil || got.Code != 0 || group != syscall.Getpgrp() {
		t.Fatalf("host cannot share the terminal process group: output=%s; result=%+v; %v", &out, got, err)
	}
}
