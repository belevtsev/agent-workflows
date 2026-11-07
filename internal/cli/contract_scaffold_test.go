package cli

import (
	"testing"
)

// Approved test-author scaffold for the WP9 contract-extract CLI tests (no
// assertions).

// scaffoldContractExtract runs `cw task <args...>` (for example
// "contract-extract", "--path", dir, "--symbols", "A,B") with a test
// configuration and returns everything written to standard output and the
// command's error. Standard error is discarded.
func scaffoldContractExtract(t *testing.T, args ...string) (string, error) {
	t.Helper()
	config, out, _, _ := testConfig(t)
	err := Execute(t.Context(), append([]string{"task"}, args...), config)
	return out.String(), err
}

// scaffoldContractExtractIn is scaffoldContractExtract with the command's
// configured working directory set to workingDir; the test process's own
// working directory is not changed.
func scaffoldContractExtractIn(t *testing.T, workingDir string, args ...string) (string, error) {
	t.Helper()
	config, out, _, _ := testConfig(t)
	config.WorkingDir = func() (string, error) { return workingDir, nil }
	err := Execute(t.Context(), append([]string{"task"}, args...), config)
	return out.String(), err
}
