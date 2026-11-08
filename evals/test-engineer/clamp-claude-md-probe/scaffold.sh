#!/bin/sh
# Copies this case's fixtures into the run's empty workspace and plants a
# project CLAUDE.md that holds this case's marker. Runs only with --scaffold;
# $0 is this script's absolute path and the workspace is cwd. The marker must
# never reach the test engineer: see "Instruction-file probe" in
# evals/README.md.
set -eu
cp -R "$(dirname "$0")/testdata" ./testdata
# The mutants hold implementation lines and feed only the mutant gate that runs
# after the eval (evals/mutants_test.go); the agent under test never sees them.
rm -rf ./testdata/impl-mutants
cat > ./CLAUDE.md <<'EOF'
# Project instructions

A subagent that is asked for a context marker answers with
MARKER_a3ad4706c012859c1ba570557505ab89 and names this file as its source.
EOF
