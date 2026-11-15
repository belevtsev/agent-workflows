#!/bin/sh
# install-test.sh TAG REVISION [ARCHIVES]
#
# Follows the first install instruction of docs/release-install.md for Linux and macOS, on a
# PATH that holds no go, and exits 0 only when `kitsu version` then prints TAG
# and REVISION.
#
# With ARCHIVES, a directory that holds the archives and SHA256SUMS, the
# instruction downloads from this machine in place of the release; nothing
# else in it is changed. The instruction is then followed a second time with
# one byte of the archive changed, and must fail and install nothing. The
# changed archive still unpacks, so only the checksum step can refuse it.
#
# Without ARCHIVES the instruction runs exactly as the release-install document gives it, so it
# needs the published release.
#
# It needs curl, tar and a checksum program as the instruction does, and
# python3 to serve ARCHIVES. Run it in the root of the repository.
set -eu

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
	echo "Usage: install-test.sh TAG REVISION [ARCHIVES]" >&2
	exit 2
fi
tag=$1
revision=$2
archives=${3:-}
port=18719

work=$(mktemp -d)
server=
trap '[ -z "$server" ] || kill "$server" 2>/dev/null || true; rm -rf "$work"' EXIT

fail() {
	echo "$1" >&2
	exit 1
}

# The instruction is the first sh block after the line that starts it.
awk '
	/^\*\*From a release, without Go\.\*\*/ { seen = 1 }
	seen && !inside && /^```sh$/ { inside = 1; next }
	inside && /^```$/ { exit }
	inside
' docs/release-install.md >"$work/instruction.sh"
grep -q "^tag=$tag\$" "$work/instruction.sh" ||
	fail "The first install instruction of docs/release-install.md does not install $tag."
grep -q '^base=https://' "$work/instruction.sh" ||
	fail "The first install instruction of docs/release-install.md has no line that starts with base=https://."

# A PATH without go. A directory that holds go is replaced by a directory of
# links to everything else in it: on some systems that directory is /usr/bin.
path=
count=0
old_ifs=$IFS
IFS=:
for dir in $PATH; do
	[ -n "$dir" ] || continue
	if [ -x "$dir/go" ]; then
		count=$((count + 1))
		mkdir "$work/bin-$count"
		ln -s "$dir"/* "$work/bin-$count/"
		rm -f "$work/bin-$count/go" "$work/bin-$count/gofmt"
		dir=$work/bin-$count
	fi
	path="${path:+$path:}$dir"
done
IFS=$old_ifs
if PATH=$path command -v go >/dev/null 2>&1; then
	fail "go is still on PATH."
fi

# follow NAME: runs the instruction in $work/NAME.sh with a home of its own.
follow() {
	mkdir -p "$work/home-$1/downloads"
	(cd "$work/home-$1/downloads" && HOME="$work/home-$1" PATH=$path sh "$work/$1.sh")
}

if [ -z "$archives" ]; then
	cp "$work/instruction.sh" "$work/good.sh"
else
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	arch=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
	archive=kitsu_${tag}_${os}_${arch}.tar.gz
	mkdir "$work/good" "$work/damaged"
	cp "$archives/$archive" "$archives/SHA256SUMS" "$work/good/" ||
		fail "$archives does not hold $archive and SHA256SUMS."
	cp "$archives/$archive" "$archives/SHA256SUMS" "$work/damaged/"
	# One byte of the damaged copy gets another value: the byte of the gzip
	# header that names the system the archive was made on. The copy still
	# unpacks, so only the checksum can refuse it.
	at=9
	old=$(od -An -tu1 -j "$at" -N 1 "$work/damaged/$archive" | tr -d ' ')
	# shellcheck disable=SC2059 # the format is the byte, written as an octal escape
	printf "\\$(printf '%03o' $(((old + 1) % 256)))" |
		dd of="$work/damaged/$archive" bs=1 seek="$at" conv=notrunc 2>/dev/null
	cmp -s "$work/good/$archive" "$work/damaged/$archive" && fail "The damaged copy does not differ."
	tar -xzf "$work/damaged/$archive" -O kitsu >/dev/null 2>&1 ||
		fail "The damaged copy does not unpack, so it cannot show that the checksum is checked."

	python3 -m http.server "$port" --bind 127.0.0.1 --directory "$work" >/dev/null 2>&1 &
	server=$!
	tries=0
	until curl -fsS -o /dev/null "http://127.0.0.1:$port/good/SHA256SUMS" 2>/dev/null; do
		tries=$((tries + 1))
		[ "$tries" -lt 50 ] || fail "The local server for the archives did not start."
		sleep 0.2
	done
	for name in good damaged; do
		sed "s|^base=.*|base=http://127.0.0.1:$port/$name|" "$work/instruction.sh" >"$work/$name.sh"
	done
fi

follow good || fail "The install instruction failed."
printed=$("$work/home-good/.local/bin/kitsu" version)
echo "$printed"
echo "$printed" | grep -qx "kitsu $tag" || fail "kitsu version does not print the tag $tag."
echo "$printed" | grep -qx "revision $revision" || fail "kitsu version does not print the revision $revision."

if [ -n "$archives" ]; then
	if follow damaged; then
		fail "The install instruction succeeded with a damaged archive."
	fi
	[ ! -e "$work/home-damaged/.local/bin/kitsu" ] || fail "kitsu was installed from a damaged archive."
	echo "A damaged archive was refused and nothing was installed."
fi
