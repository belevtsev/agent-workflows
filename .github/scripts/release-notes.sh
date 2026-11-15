#!/bin/sh
# release-notes.sh TAG
#
# Prints the notes of the release TAG: the section of CHANGELOG.md whose
# heading starts with "## [<version>]", where <version> is TAG without its
# leading v. While the changelog has no such section, it prints the section
# "## [Unreleased]". It exits 1 when that section holds no text.
#
# A link to a file of this repository becomes a link to that file at TAG, and
# an image a link to the raw file: a release page would resolve a relative
# link against its own address. Only the forms [text](target) and
# ![text](target) are known. A section with another form of link that would
# stay relative is refused: a reference link, a target in angle brackets, or
# a target without a scheme that starts with www.
#
# Run it in the root of the repository.
set -eu

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
	echo "Usage: release-notes.sh TAG" >&2
	exit 2
fi
tag=$1
repository=${GITHUB_REPOSITORY:-belevtsev/gatecub}

# section NAME: the lines between the heading "## [NAME]" and the next
# heading, or the link definitions at the end of the file. A line inside a
# fenced block is no heading.
section() {
	awk -v head="## [$1]" '
		/^(```|~~~)/ { fence = !fence }
		!fence && index($0, head) == 1 { inside = 1; next }
		inside && !fence && (/^## \[/ || /^\[[^]]*\]: /) { exit }
		inside
	' CHANGELOG.md
}

name=Unreleased
if awk -v head="## [${tag#v}]" 'index($0, head) == 1 { found = 1 } END { exit !found }' CHANGELOG.md; then
	name=${tag#v}
fi
notes=$(section "$name")
if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
	echo "CHANGELOG.md has no text under ## [$name]." >&2
	exit 1
fi
if printf '%s\n' "$notes" | grep -qE '\]\[|\]\(<|\]\(www\.'; then
	echo "CHANGELOG.md has, under ## [$name], a link that the release page could not resolve:" >&2
	printf '%s\n' "$notes" | grep -nE '\]\[|\]\(<|\]\(www\.' >&2
	exit 1
fi
printf '%s\n' "$notes" | sed -E \
	-e "s#!\[([^]]*)\]\(([A-Za-z0-9_.][^):]*)\)#![\1](https://github.com/$repository/raw/$tag/\2)#g" \
	-e "s#\]\(([A-Za-z0-9_.][^):]*)\)#](https://github.com/$repository/blob/$tag/\1)#g"
