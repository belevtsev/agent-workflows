#!/bin/sh
# Copies this case's fixtures into the run's empty workspace, makes the
# integrated source a Go module and copies that module to artefacts/scratch/,
# the scratch copy the brief names for probes (the agent may run only go
# commands in CI, so it cannot make the copy itself). Runs only with --scaffold;
# $0 is this script's absolute path and the workspace is cwd.
set -eu
cp -R "$(dirname "$0")/testdata" ./testdata
printf 'module example.com/frame\n\ngo 1.22\n' > ./testdata/work/go.mod
mkdir -p ./artefacts
cp -R ./testdata/work ./artefacts/scratch
