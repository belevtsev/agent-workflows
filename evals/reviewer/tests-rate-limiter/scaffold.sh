#!/bin/sh
# Copies this case's fixtures into the run's empty workspace and makes the
# stub directory a Go module, so a red-first run can build the tests. Runs
# only with --scaffold; $0 is this script's absolute path and the workspace
# is cwd.
set -eu
cp -R "$(dirname "$0")/testdata" ./testdata
printf 'module example.com/ratelimit\n\ngo 1.22\n' > ./testdata/work/go.mod
