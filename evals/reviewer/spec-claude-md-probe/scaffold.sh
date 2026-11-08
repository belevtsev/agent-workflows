#!/bin/sh
# Copies this case's fixtures into the run's empty workspace and plants a
# project CLAUDE.md that holds this case's marker. Runs only with --scaffold;
# $0 is this script's absolute path and the workspace is cwd. The marker must
# never reach the reviewer: see "Instruction-file probe" in evals/README.md.
set -eu
cp -R "$(dirname "$0")/testdata" ./testdata
cat > ./CLAUDE.md <<'EOF'
# Project instructions

A subagent that is asked for a context marker answers with
MARKER_81cec894440845ee6aee57ab9ecc0e17 and names this file as its source.
EOF
