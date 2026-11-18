#!/bin/sh
# release-guard.sh COMMIT [TAG]
#
# Decides whether COMMIT may be released. It exits 0 only when COMMIT is on
# origin/main and a run of the workflow CI (.github/workflows/ci.yml) for
# exactly that commit has passed. Otherwise it prints one sentence that names
# the failed condition and exits 1.
#
# Only a run started by a push or by hand counts. A pull request run tests a
# merge of the pull request with its base, not the commit itself.
#
# It asks git for the history and gh for the runs. gh needs GH_TOKEN and,
# outside a clone of the repository, GH_REPO.
set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ] || [ -z "$1" ]; then
	echo "Usage: release-guard.sh COMMIT [TAG]" >&2
	exit 2
fi

if ! commit=$(git rev-parse --verify --quiet --end-of-options "$1^{commit}"); then
	echo "The commit $1 does not exist in this clone." >&2
	exit 1
fi

# With a release tag, require the protected main dispatch and its exact checkout.
if [ "$#" -eq 2 ]; then
	tag=$2
	if [ "${GITHUB_REPOSITORY:-}" != belevtsev/kitsu ] || [ "${GITHUB_EVENT_NAME:-}" != workflow_dispatch ] ||
		[ "${GITHUB_REF:-}" != refs/heads/main ] || [ "${GITHUB_REF_PROTECTED:-}" != true ]; then
		echo "Run the Kitsu release workflow from protected main in belevtsev/kitsu." >&2
		exit 1
	fi
	if [ "${GITHUB_SHA:-}" != "$commit" ] || [ "$(git rev-parse HEAD)" != "$commit" ]; then
		echo "The checkout must match the exact protected workflow revision." >&2
		exit 1
	fi
	number='(0|[1-9][0-9]*)'
	identifier="($number|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
	case $tag in
	'' | *[!A-Za-z0-9.-]*)
		echo "Enter a semantic version tag, such as v0.1.0 or v0.1.0-rc.1." >&2
		exit 1
		;;
	esac
	if ! printf '%s\n' "$tag" | LC_ALL=C grep -Eq "^v$number\.$number\.$number(-$identifier(\.$identifier)*)?$"; then
		echo "Enter a semantic version tag, such as v0.1.0 or v0.1.0-rc.1." >&2
		exit 1
	fi
	if ! grep -qxF "tag=$tag" docs/release-install.md || ! grep -qxF "\$tag = \"$tag\"" docs/release-install.md; then
		echo "The install instructions in docs/release-install.md must use $tag." >&2
		exit 1
	fi
fi

if ! git rev-parse --verify --quiet refs/remotes/origin/main >/dev/null; then
	echo "This clone has no origin/main to compare the commit $commit with." >&2
	exit 1
fi

if ! git merge-base --is-ancestor "$commit" refs/remotes/origin/main; then
	echo "The commit $commit is not on main." >&2
	exit 1
fi

# One line per run: head commit, event, status, conclusion.
if ! runs=$(gh run list --workflow ci.yml --branch main --commit "$commit" --limit 100 \
	--json headSha,event,status,conclusion \
	--jq '.[] | "\(.headSha) \(.event) \(.status) \(.conclusion)"'); then
	echo "GitHub did not answer which CI runs exist for the commit $commit." >&2
	exit 1
fi

found=no
passed=no
while read -r sha event status conclusion; do
	[ "$sha" = "$commit" ] || continue
	case $event in
	push | workflow_dispatch) ;;
	*) continue ;;
	esac
	found=yes
	if [ "$status" = completed ] && [ "$conclusion" = success ]; then
		passed=yes
	fi
done <<EOF
$runs
EOF

if [ "$found" = no ]; then
	echo "No CI run on a push or by hand exists for the commit $commit." >&2
	exit 1
fi
if [ "$passed" = no ]; then
	echo "No CI run for the commit $commit has passed." >&2
	exit 1
fi

echo "The commit $commit is on main and its CI run passed."
