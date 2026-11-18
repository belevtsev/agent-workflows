// Command releasecmd assembles, checks and publishes Kitsu's release bundle.
// It runs only in the Release workflow and is not distributed with Kitsu.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "release: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) < 3 || len(args) > 4 {
		return errors.New("usage: releasecmd prepare|verify|publish TAG DIRECTORY [COMMIT]")
	}
	operation, tag, directory := args[0], args[1], args[2]
	if !semverTag.MatchString(tag) {
		return fmt.Errorf("invalid release tag %q", tag)
	}
	if operation == "prepare" && len(args) == 3 {
		return prepareBundle(tag, directory, filepath.Join(os.Getenv("RUNNER_TEMP"), "release-notes.md"))
	}
	if operation == "verify" && len(args) == 3 {
		_, err := checkBundle(tag, directory)
		return err
	}
	if operation == "publish" && len(args) == 4 {
		repo := os.Getenv("GH_REPO")
		if repo != "belevtsev/kitsu" || !commitSHA.MatchString(args[3]) || args[3] != os.Getenv("GITHUB_SHA") {
			return errors.New("publication requires the Kitsu repository and the exact workflow SHA")
		}
		gh := func(args ...string) ([]byte, error) {
			return callGitHub(ctx, directory, args)
		}
		assets, err := checkBundle(tag, directory)
		if err != nil {
			return err
		}
		return publishRelease(gh, repo, tag, args[3], assets)
	}
	return errors.New("usage: releasecmd prepare|verify|publish TAG DIRECTORY [COMMIT]")
}

func callGitHub(ctx context.Context, directory string, args []string) ([]byte, error) {
	result := proc.Run(ctx, proc.Spec{
		Path: "gh", Args: args, Dir: directory, Env: os.Environ(),
		StdoutLimit: 16 << 20, StderrLimit: 64 << 10, Timeout: 3 * time.Minute,
	})
	switch {
	case result.Stopped:
		return nil, fmt.Errorf("gh %s %s did not finish", args[0], args[1])
	case result.Overflow:
		return nil, fmt.Errorf("gh %s %s returned an oversized response", args[0], args[1])
	case result.Err != nil || result.Cleanup != nil:
		return nil, fmt.Errorf("gh %s %s: %s: %w", args[0], args[1], result.Stderr, errors.Join(result.Err, result.Cleanup))
	default:
		return result.Stdout, nil
	}
}

// checkErrClose preserves a copy or checksum error over a secondary close error.
func checkErrClose(err *error, file *os.File) {
	*err = cmp.Or(*err, file.Close())
}
