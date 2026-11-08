#!/bin/sh
# Copies this case's fixtures into the run's empty workspace. Runs only with
# --scaffold; $0 is this script's absolute path and the workspace is cwd.
set -eu
cp -R "$(dirname "$0")/testdata" ./testdata
