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
)

func run(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h" {
		fmt.Println("cwdev — developer automation\n\nUsage:\n  cwdev check [--source PATH] [--binary PATH] [--race]\n              [--release-version v1.0.N --revision SHA --dist PATH]\n  cwdev release build|publish --version v1.0.N --revision SHA\n              [--source PATH] [--dist PATH]\n\nRelease builds require a clean exact source revision. Publication reads GH_TOKEN\nand reconciles uncertain writes without overwriting tags or assets.")
		return nil
	}
	action := arguments[0]
	arguments = arguments[1:]
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
		return devcheck.Check(ctx, devcheck.Options{Source: absSource, Binary: resolve(*binary, filepath.Join(".bin", "cw")), Race: *race, ReleaseVersion: *releaseVersion, Revision: *revision, Dist: absDist})
	}
	if *version == "" || *revision == "" {
		return errors.New("release requires --version and --revision")
	}
	if *race || *releaseVersion != "" || *binary != "" {
		return errors.New("verification options apply only to check")
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

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cwdev:", err)
		os.Exit(1)
	}
}
