#!/bin/sh
# Bootstrap a verified native manager, then hand all installation decisions to it.
set -eu

checkout=$(CDPATH='' cd -P "$(dirname "$0")" && pwd)
binary=$checkout/.bin/cw
repository=belevtsev/agent-workflows

fail() { printf 'agent-workflows: %s\n' "$*" >&2; exit 1; }
case $(uname -s) in Darwin) target_os=darwin ;; Linux) target_os=linux ;; *) fail 'native cw supports macOS and Linux' ;; esac
case $(uname -m) in arm64|aarch64) target_arch=arm64 ;; x86_64|amd64) target_arch=amd64 ;; *) fail 'native cw supports arm64 and amd64' ;; esac

identify_binary() {
    [ -f "$1" ] && [ ! -L "$1" ] && [ -x "$1" ] || return 1
    identity=$("$1" version 2>/dev/null) || return 1
    identified=$(printf '%s\n' "$identity" | sed -n 's/^{"arch":"\([^"]*\)","built_at":"[^"]\{1,\}","os":"\([^"]*\)","revision":"\([0-9a-f]*\)","version":"[A-Za-z0-9._+-]\{1,\}"}$/\1 \2 \3/p')
    case $identified in "$target_arch $target_os "*) identified_revision=${identified#"$target_arch $target_os "} ;; *) return 1 ;; esac
    [ "${#identified_revision}" -eq 40 ] || return 1
    case $identified_revision in *[!0-9a-f]*) return 1 ;; esac
}

if [ -L "$checkout/.bin" ] || { [ -e "$checkout/.bin" ] && [ ! -d "$checkout/.bin" ]; }; then fail '.bin is occupied or symlinked'; fi
identify_manager_capabilities() {
    protocol=$("$1" --manager-protocol 2>/dev/null) || return 1
    [ "$protocol" = cw-manager-v6 ] || return 1
    policy=$("$1" --model-policy-capability 2>/dev/null) || return 1
    [ "$policy" = cw-model-policy-v2-dual-choice ] || return 1
    defaults_capability=$("$1" --codex-defaults-capability 2>/dev/null) || return 1
    [ "$defaults_capability" = cw-codex-defaults-v1 ] || return 1
    openai_decisions_capability=$("$1" --openai-decisions-capability 2>/dev/null) || return 1
    [ "$openai_decisions_capability" = cw-openai-decisions-v1 ] || return 1
    three_adviser_capability=$("$1" --three-adviser-capability 2>/dev/null) || return 1
    [ "$three_adviser_capability" = cw-three-adviser-v1 ]
}

if [ -e "$binary" ] || [ -L "$binary" ]; then
    identify_binary "$binary" || fail 'unidentified .bin/cw; refusing to replace it'
    if identify_manager_capabilities "$binary"; then
        exec "$binary" --source "$checkout" "$@"
    fi
fi

# Only cold-cache gates live in the bootstrap. Argument validation, command
# dispatch, Git, builds, updates and all installation mutations belong to Go.
for argument in "$@"; do
    case $argument in
        --dry-run|--dry-run=1|--dry-run=t|--dry-run=T|--dry-run=true|--dry-run=TRUE|--dry-run=True)
            printf '%s\n' '{"dry_run":true,"environment":{"kind":"native_go","status":"deferred"},"next_step":"Run ./install.sh to prepare the native manager","validation":"deferred"}'
            exit 0
            ;;
        --help|-h|help)
            printf '%s\n' 'cw — Agent workflows' '' 'Usage: ./install.sh [install|setup|update|status|validate|version|help|rollback|recover|uninstall] [options]' '' 'install (setup alias) activates validated local HEAD; update fetches origin/main.' 'status and validate inspect locally; rollback, recover and uninstall manage owned state.' 'Options: --dry-run, --source PATH, --home PATH, --codex-home PATH, --state-dir PATH,' '         --shell auto|bash|zsh|none, --migrate-from PATH, --typesafe-legacy[=PATH], --no-checkout,' '         --adopt-personal-skills (install/setup only)' 'Mutations apply by default. Cold dry runs make no downloads or persistent writes.'
            exit 0
            ;;
    esac
done

command -v curl >/dev/null 2>&1 || fail 'curl is required to bootstrap the native manager'
command -v tar >/dev/null 2>&1 || fail 'tar is required to bootstrap the native manager'
command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 || fail 'sha256sum or shasum is required to verify the native manager'
lock=$checkout/.bin.lock
mkdir "$lock" 2>/dev/null || fail 'another native manager bootstrap holds .bin.lock; wait and retry'
temporary=
cleanup() { [ -z "$temporary" ] || rm -rf "$temporary"; rmdir "$lock" 2>/dev/null || :; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if [ -L "$checkout/.bin" ] || { [ -e "$checkout/.bin" ] && [ ! -d "$checkout/.bin" ]; }; then fail '.bin is occupied or symlinked'; fi
mkdir -p "$checkout/.bin"
if [ -e "$binary" ] || [ -L "$binary" ]; then
    identify_binary "$binary" || fail 'unidentified .bin/cw; refusing to replace it'
    if identify_manager_capabilities "$binary"; then
        cleanup; trap - EXIT INT TERM
        exec "$binary" --source "$checkout" "$@"
    fi
fi
temporary=$(mktemp -d "$checkout/.bin/.bootstrap.XXXXXXXX")
asset=cw_${target_os}_${target_arch}.tar.gz
base=https://github.com/$repository/releases/latest/download
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 120 --max-filesize 134217728 "$base/$asset" -o "$temporary/$asset" || fail 'native manager download failed; use a released native manager to bootstrap'
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 60 --max-filesize 65536 "$base/SHA256SUMS" -o "$temporary/SHA256SUMS" || fail 'native manager checksum download failed'
expected=$(sed -n "s/^\([0-9a-f]\{64\}\)  cw_${target_os}_${target_arch}\.tar\.gz\$/\1/p" "$temporary/SHA256SUMS")
[ "${#expected}" -eq 64 ] || fail 'release checksum manifest is missing or duplicates the requested asset'
if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$temporary/$asset")
else actual=$(shasum -a 256 "$temporary/$asset")
fi
actual=${actual%% *}
[ "$actual" = "$expected" ] || fail 'native manager archive checksum mismatch'
# Only the named member is written; archive paths never become filesystem paths.
tar -xOzf "$temporary/$asset" cw > "$temporary/cw" || fail 'release archive has no readable cw binary'
chmod 755 "$temporary/cw"
identify_binary "$temporary/cw" || fail 'release asset is not an identifiable native manager for this platform'
identify_manager_capabilities "$temporary/cw" || fail 'released native manager does not support the required bootstrap capabilities cw-manager-v6, cw-model-policy-v2-dual-choice, cw-codex-defaults-v1, cw-openai-decisions-v1, and cw-three-adviser-v1'
mv "$temporary/cw" "$binary"
cleanup
trap - EXIT INT TERM
exec "$binary" --source "$checkout" "$@"
