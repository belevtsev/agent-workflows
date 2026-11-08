package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/credentials"
	"github.com/belevtsev/agent-workflows/v3/internal/userconfig"
)

// exitStatus is the process status of a command that reported its own result.
type exitStatus int

func (status exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(status)) }

const evaluationRefused = "Evaluation validation or output preflight failed; no exception details retained."

func evaluate(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 || arguments[0] != "multilingual" {
		return errors.New("eval requires multilingual")
	}
	flags := flag.NewFlagSet("eval multilingual", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", ".", "source checkout holding the corpus")
	live := flags.Bool("live", false, "explicitly allow one hosted baseline, without retries")
	output := flags.String("output-dir", "", "new private directory outside source and managed releases")
	configDir := flags.String("config-dir", "", "private credential directory for an explicitly hosted evaluation")
	if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, evaluationRefused)
		return exitStatus(1)
	}
	absolute, err := filepath.Abs(*source)
	if err != nil {
		return err
	}
	getenv := os.Getenv
	if *live {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		directory, e := userconfig.Dir(filepath.Clean(home), *configDir, os.Getenv)
		if e != nil {
			return e
		}
		getenv, e = (credentials.Resolver{Directory: directory, Getenv: os.Getenv}).Environment()
		if e != nil {
			return e
		}
	}
	status := consult.Evaluation{Source: absolute, Live: *live, OutputDir: *output, Getenv: getenv, Home: os.UserHomeDir, Executable: os.Executable, Stdout: os.Stdout, Stderr: os.Stderr}.Run(ctx)
	if status != 0 {
		return exitStatus(status)
	}
	return nil
}
