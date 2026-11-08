#!/bin/sh
# Copies this case's fixtures into the run's empty workspace and makes the
# integrated source a Go module, and gives the reviewer a scratch copy of it
# under artefacts/scratch for its test runs and reproductions. Runs only with --scaffold; $0 is this script's absolute path
# and the workspace is cwd.
set -eu
cp -R "$(dirname "$0")/testdata" ./testdata
printf 'module example.com/storage\n\ngo 1.25\n' > ./testdata/storage/go.mod
mkdir -p ./artefacts
cp -R ./testdata/storage ./artefacts/scratch
