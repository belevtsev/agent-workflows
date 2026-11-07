// Package devcheck runs developer verification without shell business logic.
package devcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/belevtsev/agent-workflows/internal/release"
	"github.com/belevtsev/agent-workflows/internal/workflow"
)

type Options struct {
	Source         string
	Binary         string
	Race           bool
	ReleaseVersion string
	Revision       string
	Dist           string
	Output         io.Writer
}

// Check runs formatting, vet, maintained-source validation, native tests, and a
// fresh isolated installation. On main it overlaps cross-builds with checks and
// reuses the native release executable for smoke instead of building it twice.
func Check(ctx context.Context, options Options) error {
	if options.Output == nil {
		options.Output = os.Stdout
	}
	if err := workflow.ValidateAutomation(options.Source); err != nil {
		return err
	}
	revision, stamp, err := release.SourceIdentity(ctx, options.Source, options.Revision, true)
	if err != nil {
		return err
	}
	if options.ReleaseVersion != "" && runtime.GOOS != "linux" {
		return errors.New("CI release preparation must run on Linux")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	built := make(chan error, 1)
	buildRead := false
	if options.ReleaseVersion != "" {
		go func() {
			_, err := release.Build(ctx, release.BuildOptions{Source: options.Source, Version: options.ReleaseVersion, Revision: revision, Dist: options.Dist})
			built <- err
		}()
		defer func() {
			cancel()
			if !buildRead {
				<-built
			}
		}()
	}
	run := func(name string, arguments ...string) error {
		command := exec.CommandContext(ctx, name, arguments...)
		command.Dir = options.Source
		command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=go1.27.1", "GOFLAGS=-mod=mod")
		command.Stdout, command.Stderr = options.Output, options.Output
		if err := command.Run(); err != nil {
			return fmt.Errorf("%s %s: %w", name, strings.Join(arguments, " "), err)
		}
		return nil
	}
	format := exec.CommandContext(ctx, "gofmt", "-l", "cmd", "internal")
	format.Dir = options.Source
	formatted, err := format.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gofmt: %w\n%s", err, formatted)
	}
	if len(bytes.TrimSpace(formatted)) != 0 {
		return fmt.Errorf("Go formatting differs:\n%s", formatted)
	}
	// Cheap checks come first so a formatting, vet or manifest mistake fails in
	// seconds rather than after the tests.
	if err := run("go", "vet", "./..."); err != nil {
		return err
	}
	if _, err := workflow.ValidateSuite(options.Source); err != nil {
		return err
	}
	// The timeout turns a hung test into a failure with goroutine dumps inside
	// the CI job's 10-minute limit, which also covers about a minute of setup,
	// vet, validation and smoke. 8m is twice internal/workflow's slowest race
	// time in CI (up to 240s); with graphify and taskflow no longer paying the
	// race runtime's exit delay per helper child, workflow is the slowest
	// package locally. Recheck after CI runs this.
	testArgs := []string{"test", "-count=1", "-timeout=8m"}
	if options.Race {
		testArgs = append(testArgs, "-race")
	}
	if err := run("go", append(testArgs, "./...")...); err != nil {
		return err
	}
	if options.ReleaseVersion != "" {
		err := <-built
		buildRead = true
		if err != nil {
			return err
		}
		name := fmt.Sprintf("cw_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
		if err := ExtractBinary(filepath.Join(options.Dist, name), options.Binary); err != nil {
			return err
		}
	} else if err := release.BuildBinary(ctx, options.Source, options.Binary, "v0.0.101", revision, stamp, runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}
	if err := StandaloneNativeSmoke(ctx, options.Source, options.Binary, options.Output); err != nil {
		return err
	}
	_, err = fmt.Fprintln(options.Output, "Native formatting, vet, source validation, tests and installation smoke passed.")
	return err
}

// CheckScripts shares the source validator's Git-aware automation policy.
func CheckScripts(source string) error {
	return workflow.ValidateAutomation(source)
}
