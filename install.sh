#!/bin/sh
# Download a verified standalone manager and hand all decisions to that binary.
set -eu

repository=belevtsev/agent-workflows
fail() { printf 'agent-workflows: %s\n' "$*" >&2; exit 1; }

# These paths are deliberately offline and precede tool and filesystem access.
for argument in "$@"; do
    case $argument in
        --dry-run|--dry-run=1|--dry-run=t|--dry-run=T|--dry-run=true|--dry-run=TRUE|--dry-run=True)
            printf '%s\n' '{"dry_run":true,"environment":{"kind":"standalone","status":"deferred"},"next_step":"Run the bootstrap without --dry-run to prepare a verified standalone manager","validation":"deferred"}'
            exit 0
            ;;
        --help|-h|help)
            printf '%s\n' 'cw — Agent workflows' '' 'Usage: install.sh [install|setup|update|status|version|rollback|recover|uninstall] [options]' '' 'Downloads one verified standalone release, then runs its native installer.' 'Run without arguments for interactive setup when attached to a terminal.' 'Options include --dry-run, --json, --non-interactive, --preset recommended|balanced,' '  --settings PATH, --config-dir PATH, --home PATH, --codex-home PATH,' '  --claude-home PATH, --state-dir PATH, --host HOST,' '  --shell auto|bash|zsh|none, --adopt-personal-skills and --forget.' 'Help and bootstrap dry runs make no downloads or persistent writes.'
            exit 0
            ;;
    esac
done

case $(uname -s) in Darwin) target_os=darwin ;; Linux) target_os=linux ;; *) fail 'native cw supports macOS and Linux' ;; esac
case $(uname -m) in arm64|aarch64) target_arch=arm64 ;; x86_64|amd64) target_arch=amd64 ;; *) fail 'native cw supports arm64 and amd64' ;; esac
command -v curl >/dev/null 2>&1 || fail 'curl is required to download the standalone manager'
command -v tar >/dev/null 2>&1 || fail 'tar is required to unpack the standalone manager'
command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 || fail 'sha256sum or shasum is required to verify the standalone manager'

# Resolve latest exactly once. Both asset requests below use this immutable tag
# URL rather than two independently moving latest/download requests.
latest_url=$(curl --fail --silent --show-error --location --head --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 60 --output /dev/null --write-out '%{url_effective}' "https://github.com/$repository/releases/latest") || fail 'cannot resolve a published standalone release'
case $latest_url in "https://github.com/$repository/releases/tag/"*) tag=${latest_url#"https://github.com/$repository/releases/tag/"} ;; *) fail 'latest release resolved to an unexpected URL' ;; esac
valid_tag=$(printf '%s\n' "$tag" | sed -E -n '/^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/p')
[ -n "$tag" ] && [ "$valid_tag" = "$tag" ] && [ "${#tag}" -le 128 ] || fail 'latest release is not a stable semantic version'

umask 077
temporary=$(mktemp -d "${TMPDIR:-/tmp}/cw-bootstrap.XXXXXXXX") || fail 'cannot create private standalone staging directory'
case $temporary in /*) ;; *) fail 'standalone staging requires an absolute temporary directory' ;; esac
[ -d "$temporary" ] && [ ! -L "$temporary" ] || fail 'standalone staging directory is unsafe'
cleanup() { [ -z "$temporary" ] || rm -rf "$temporary"; }
trap cleanup EXIT
child=
interrupted() {
    code=$1
    trap '' INT TERM
    if [ -n "$child" ]; then
        kill -TERM "$child" 2>/dev/null || :
        wait "$child" 2>/dev/null || :
    fi
    exit "$code"
}
trap 'interrupted 130' INT
trap 'interrupted 143' TERM
asset=cw_${target_os}_${target_arch}.tar.gz
base=https://github.com/$repository/releases/download/$tag
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 120 --max-filesize 67108864 "$base/$asset" -o "$temporary/$asset" || fail 'standalone manager archive download failed'
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 60 --max-filesize 65536 "$base/SHA256SUMS" -o "$temporary/SHA256SUMS" || fail 'standalone manager checksum download failed'
expected=$(sed -n "s/^\([0-9a-f]\{64\}\)  cw_${target_os}_${target_arch}\.tar\.gz\$/\1/p" "$temporary/SHA256SUMS")
[ "${#expected}" -eq 64 ] || fail 'release checksums omit or duplicate the requested platform'
if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$temporary/$asset")
else actual=$(shasum -a 256 "$temporary/$asset")
fi
actual=${actual%% *}
[ "$actual" = "$expected" ] || fail 'standalone manager archive checksum mismatch'
members=$(tar -tzf "$temporary/$asset") || fail 'standalone manager archive is invalid'
executables=$(printf '%s\n' "$members" | sed -n '/^cw$/p')
[ "$executables" = cw ] || fail 'standalone manager archive has no unique cw executable'
# Only this named member is written. Archive paths never become filesystem paths.
tar -xOzf "$temporary/$asset" cw > "$temporary/cw" || fail 'standalone manager archive has no readable cw executable'
chmod 700 "$temporary/cw"
identity=$("$temporary/cw" version 2>/dev/null) || fail 'downloaded manager cannot report its native identity'
identified=$(printf '%s\n' "$identity" | sed -n 's/^{"arch":"\([^"]*\)","built_at":"[^"]\{1,\}","os":"\([^"]*\)","revision":"\([0-9a-f]\{40\}\)","version":"\([A-Za-z0-9._+-]\{1,\}\)"}$/\1 \2 \3 \4/p')
case $identified in "$target_arch $target_os "*" $tag") identified_revision=${identified#"$target_arch $target_os "}; identified_revision=${identified_revision%" $tag"} ;; *) fail 'downloaded manager version or platform differs from the pinned release' ;; esac
[ "${#identified_revision}" -eq 40 ] || fail 'downloaded manager source revision is invalid'
protocol=$("$temporary/cw" --manager-protocol 2>/dev/null) || fail 'downloaded manager cannot report its capability'
[ "$protocol" = cw-manager-v10 ] || fail 'published release does not support standalone installation; use a release with cw-manager-v10'
bundle=$("$temporary/cw" --bundle-identity 2>/dev/null) || fail 'downloaded manager cannot validate its embedded resources'
bundle_fields=$(printf '%s\n' "$bundle" | sed -n 's/^{"version":"\([A-Za-z0-9._+-]\{1,\}\)","revision":"\([0-9a-f]\{40\}\)","os":"\([^"]*\)","arch":"\([^"]*\)","base_digest":"\([0-9a-f]\{64\}\)"}$/\1 \2 \3 \4 \5/p')
case $bundle_fields in "$tag $identified_revision $target_os $target_arch "*) bundle_digest=${bundle_fields#"$tag $identified_revision $target_os $target_arch "} ;; *) fail 'embedded bundle identity differs from the downloaded manager' ;; esac
[ "${#bundle_digest}" -eq 64 ] || fail 'downloaded manager has no valid embedded payload digest'
"$temporary/cw" --verify-release "$tag" >/dev/null || fail 'downloaded manager source differs from its pinned release tag'

# Keep the private executable alive until its native transaction completes, then
# remove only our stage. The child owns installation and command activation.
rm -f "$temporary/$asset" "$temporary/SHA256SUMS"
if [ -t 1 ] && [ -t 2 ] && ( : </dev/tty ) 2>/dev/null; then
    "$temporary/cw" "$@" </dev/tty &
    child=$!
else
    # Without job control, POSIX shells such as dash give an asynchronous command
    # /dev/null as stdin before its own redirections, so pass the caller's stdin
    # (or /dev/null when it is closed) on fd 3. Keep fd 3 open until the shell
    # exits: closing this copy of the script's own input crashes macOS bash 3.2
    # with SIGTRAP when the script arrives on stdin (curl ... | sh).
    if (: 3<&0) 2>/dev/null; then exec 3<&0; else exec 3</dev/null; fi
    "$temporary/cw" "$@" <&3 3<&- &
    child=$!
fi
status=0
wait "$child" || status=$?
child=
exit "$status"
