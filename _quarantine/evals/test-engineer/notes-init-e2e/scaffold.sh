#!/bin/sh
# Copies this case's fixtures into the run's empty workspace. Runs only with
# --scaffold; $0 is this script's absolute path and the workspace is cwd.
set -eu
cp -R "$(dirname "$0")/testdata" ./testdata
# The mutants hold implementation lines and feed only the mutant gate that runs
# after the eval (evals/mutants_test.go); the agent under test never sees them.
rm -rf ./testdata/impl-mutants
