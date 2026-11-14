#!/bin/sh
# accept.sh <task> <repo>
#
# Runs the acceptance tests of a benchmark task on a copy of the last commit
# of a repository that an arm of tokens.py left. No arm saw these tests.
set -eu
dir=$(dirname "$0"); task=$1; repo=$2
copy=$(mktemp -d); trap 'rm -rf "$copy"' EXIT
git -C "$repo" archive HEAD | tar -x -C "$copy"
case $task in
duration) cp "$dir/acceptance/duration_acceptance.py" "$copy/accept_duration.py"; (cd "$copy" && PYTHONDONTWRITEBYTECODE=1 python3 -m unittest accept_duration 2>&1 | tail -4) ;;
slug) cp "$dir/acceptance/slug_acceptance_test.go.txt" "$copy/slug_acceptance_test.go"; (cd "$copy" && go test -count=1 -run TestAccepted ./... 2>&1 | tail -6) ;;
esac
