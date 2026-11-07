// cwdev provides source verification and release automation for maintainers.
// It is not included in end-user cw archives.
package main

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/belevtsev/agent-workflows/internal/devcheck"
	"github.com/belevtsev/agent-workflows/internal/release"
	"github.com/belevtsev/agent-workflows/internal/sandboxtest"
)

func run(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h" {
		fmt.Println("cwdev — developer automation\n\nUsage:\n  cwdev check [--source PATH] [--binary PATH] [--race] [--snapshot]\n              [--release-version v1.0.N --revision SHA --dist PATH]\n  cwdev release build|publish --version v1.0.N --revision SHA\n              [--source PATH] [--dist PATH]\n  cwdev eval multilingual [--source PATH] [--live --output-dir PATH]\n  cwdev sandbox test --source PATH --backend docker|sbx [--output FILE]\n\nRelease builds require a clean exact source revision. check --snapshot runs the\nsame gates in a disposable commit of the working tree (tracked and unignored\nfiles) and prints its tree hash; compare it with git rev-parse HEAD^{tree} after\ncommitting. It cannot prepare release assets. Publication reads GH_TOKEN\nand reconciles uncertain writes without overwriting tags or assets. Evaluation\nvalidates its corpus offline unless --live explicitly allows one hosted baseline.")
		return nil
	}
	action := arguments[0]
	arguments = arguments[1:]
	if action == "eval" {
		return evaluate(ctx, arguments)
	}
	if action == "sandbox" {
		return sandbox(ctx, arguments)
	}
	if action == "release" {
		if len(arguments) == 0 || (arguments[0] != "build" && arguments[0] != "publish") {
			return errors.New("release requires build or publish")
		}
		action = "release-" + arguments[0]
		arguments = arguments[1:]
	} else if action != "check" {
		return fmt.Errorf("unknown developer command: %s", action)
	}
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	source := flags.String("source", ".", "source checkout")
	dist := flags.String("dist", "", "release asset directory")
	version := flags.String("version", "", "semantic release version")
	revision := flags.String("revision", "", "exact source commit SHA")
	binary := flags.String("binary", "", "native check executable")
	race := flags.Bool("race", false, "run native race checks")
	snapshot := flags.Bool("snapshot", false, "verify a disposable commit of the working tree instead of requiring a clean checkout")
	releaseVersion := flags.String("release-version", "", "prepare release assets concurrently with verification")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected developer command arguments")
	}
	absSource, err := filepath.Abs(*source)
	if err != nil {
		return err
	}
	resolve := func(value, fallback string) string {
		if value == "" {
			value = fallback
		}
		if !filepath.IsAbs(value) {
			return filepath.Join(absSource, value)
		}
		return value
	}
	absDist := resolve(*dist, "dist")
	if action == "check" {
		if *version != "" {
			return errors.New("--version applies to release; check uses --release-version")
		}
		// An empty binary defaults inside the verified tree, which is the copy
		// under --snapshot.
		binaryPath := ""
		if *binary != "" {
			binaryPath = resolve(*binary, "")
		}
		return devcheck.Check(ctx, devcheck.Options{Source: absSource, Binary: binaryPath, Race: *race, ReleaseVersion: *releaseVersion, Revision: *revision, Dist: absDist, Snapshot: *snapshot})
	}
	if *version == "" || *revision == "" {
		return errors.New("release requires --version and --revision")
	}
	if *race || *snapshot || *releaseVersion != "" || *binary != "" {
		return errors.New("verification options --race, --snapshot, --release-version and --binary apply only to check")
	}
	if action == "release-build" {
		assets, err := release.Build(ctx, release.BuildOptions{Source: absSource, Version: *version, Revision: *revision, Dist: absDist})
		if err != nil {
			return err
		}
		return json.MarshalWrite(os.Stdout, map[string]any{"version": *version, "revision": *revision, "dist": assets.Directory, "checksums": assets.Digests}, json.Deterministic(true))
	}
	publication, err := release.Publish(ctx, release.PublishOptions{Source: absSource, Version: *version, Revision: *revision, Dist: absDist, Token: os.Getenv("GH_TOKEN")})
	if err != nil {
		return err
	}
	return json.MarshalWrite(os.Stdout, publication, json.Deterministic(true))
}

func sandbox(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 || (arguments[0] != "test" && arguments[0] != "probe") {
		return errors.New("sandbox requires test")
	}
	action := arguments[0]
	flags := flag.NewFlagSet("sandbox "+action, flag.ContinueOnError)
	if action == "test" {
		source := flags.String("source", ".", "clean exact source checkout for host-side artifact preparation")
		backend := flags.String("backend", "docker", "explicit docker or standalone sbx backend")
		output := flags.String("output", "", "safe JSON report destination")
		if err := flags.Parse(arguments[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected sandbox arguments")
		}
		report, runErr := sandboxtest.Run(ctx, sandboxtest.Options{Source: *source, Backend: *backend, Output: *output})
		writeErr := json.MarshalWrite(os.Stdout, report, json.Deterministic(true))
		return errors.Join(runErr, writeErr)
	}
	first := flags.String("first", "", "first prepared standalone executable")
	second := flags.String("second", "", "second prepared standalone executable")
	root := flags.String("root", "", "private synthetic acceptance root")
	expectedRevision := flags.String("expected-revision", "", "trusted exact source revision for both prepared executables")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *first == "" || *second == "" || *root == "" || *expectedRevision == "" {
		return errors.New("internal sandbox probe requires first, second, root and expected source revision")
	}
	// The dedicated test author owns Probe's behavioral assertions.
	// Infrastructure supplies trusted inputs and process isolation.
	result, probeErr := sandboxtest.Probe(ctx, *first, *second, *root, *expectedRevision)
	if result == nil {
		return errors.Join(probeErr, errors.New("independent acceptance probe returned no JSON result"))
	}
	return errors.Join(probeErr, json.MarshalWrite(os.Stdout, result, json.Deterministic(true)))
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if status, ok := errors.AsType[exitStatus](err); ok {
			os.Exit(int(status))
		}
		fmt.Fprintln(os.Stderr, "cwdev:", err)
		os.Exit(1)
	}
}
