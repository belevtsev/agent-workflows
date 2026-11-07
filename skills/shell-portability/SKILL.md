---
name: shell-portability
description: Write, review, or run shell scripts and installers that must work under sh, dash, busybox, macOS bash 3.2, bash 5, and curl | sh.
---

# Shell portability

Use this for any `.sh` file, `#!` script, installer, CI step, Dockerfile `RUN`, or shell string built by a program. A script is only portable to the shells it has actually run under. Reading it is not enough: a reviewer who reads bash fluently will not see that dash rejects `[[`, or that bash 3.2 dies on an empty array under `set -u`.

## Decide the target first

The interpreter is whatever runs the file, not what the `#!` line says. `sh install.sh`, `bash install.sh` and `curl … | sh` all ignore the shebang.

| Invocation or host | Real interpreter |
| --- | --- |
| macOS `/bin/sh` | bash 3.2.57 in POSIX mode: accepts arrays and `[[`, so it hides bashisms |
| macOS `/bin/bash` | bash 3.2.57 |
| Debian, Ubuntu `/bin/sh`; GitHub Linux runners | dash |
| Alpine, distroless debug, BusyBox images | BusyBox `ash`: accepts `[[`, `function`, `${v:0:3}` and `pipefail`, so it also hides most bashisms |
| `curl -fsSL URL \| sh` | the user's `sh`: any of the above, with the script on stdin |
| `#!/usr/bin/env bash` | bash 3.2 on stock macOS, bash 5 on Linux and Homebrew |

Pick one:
- **`#!/bin/sh` and POSIX only.** Required for anything run as `| sh`, from `sh file`, in Alpine, or by tools that call `/bin/sh -c` (Make, Dockerfile `RUN`, Go `exec.Command("sh", "-c", …)`, cron).
- **`#!/usr/bin/env bash` and bash 3.2.** For developer scripts that need arrays. Bash 4+ features crash on stock macOS. Document the invocation as `bash script.sh`, and don't ship it as `| sh`.
- **Bash 4+ only** when the README says so and the script checks `BASH_VERSINFO` at the top with a clear error.

## POSIX sh rules

dash rejects most of these with a syntax error, and silently mishandles `$RANDOM` (empty) and `echo -e` (prints `-e`). macOS `/bin/sh` and BusyBox `ash` accept most of them, so a script that passes there can still break under dash. Run dash to find them:

| Avoid | Use |
| --- | --- |
| `[[ … ]]`, `==` in `[` | `[ … ]` with `=`, or `case` for patterns |
| arrays, `${#a[@]}` | `set -- "$@" item` for one list, or newline-separated text |
| `function f {` | `f() {` |
| `source file` | `. ./file` |
| `${v:0:3}`, `${v/a/b}`, `${v,,}` | `cut`, `sed`, `tr`, or `case` |
| `<(cmd)`, `<<<` | a temp file or a pipe |
| `echo -e`, `echo -n` | `printf '%s\n' "$v"`; dash's `echo` expands `\t` itself |
| `$RANDOM`, `$EPOCHSECONDS`, `$BASH_SOURCE` | `od` on `/dev/urandom`, `date +%s`, an argument |
| `set -o pipefail` | check each stage; dash on Debian 12, Ubuntu 24.04 and macOS rejects it and the script stops there |
| `which` | `command -v` |
| `read` | `IFS= read -r line`, with `|| [ -n "$line" ]` to keep a last line that has no newline |

`local` is not POSIX, but dash, BusyBox and bash support it; it only works inside a function. Use it for those targets, never as `local x=$(cmd)` (see below).

Tools differ too: `sed -i` (GNU) vs `sed -i ''` (BSD), `date -d`, `stat -c` vs `stat -f`, `readlink -f` (absent before macOS 12.3), `grep -P`, `find -printf`, `xargs -r`, `base64 -d` vs `-D`, and `mktemp -t`. Write to a temp file and `mv` it instead of `sed -i`. Use `mktemp -d "${TMPDIR:-/tmp}/name.XXXXXX"`, which GNU, BSD and BusyBox all accept.

## Bash 3.2 on macOS

Bash scripts must still run on bash 3.2 unless they declare otherwise. Not in 3.2: `declare -A`, `mapfile`/`readarray`, `${v,,}`/`${v^^}`, `|&`, `&>>`, `;&`, `declare -g`, `[[ -v name ]]`, `wait -n`, `shopt -s inherit_errexit`, and negative array subscripts.

**The empty array crash.** Under `set -u`, bash before 4.4 treats `"${a[@]}"` of an empty array as unbound and exits:

```sh
set -u
args=()
cmd "${args[@]}"              # bash 3.2: "args[@]: unbound variable", exit 1
cmd ${args[@]+"${args[@]}"}   # every bash: expands to nothing when empty
```

Bash 5 accepts the first form, so it passes in Linux CI and crashes on a Mac.

## Errexit, pipelines, and status

`set -e` is a safety net with holes. Assume every one of these runs on:
- A function called as an `if` or `while` condition, after `!`, or anywhere in an `&&` or `||` list except the last command runs with `set -e` off for its whole body: `f || die` ignores failures inside `f`.
- `local x=$(cmd)`, `export x=$(cmd)` and `readonly x=$(cmd)` return the builtin's status, so a failing `cmd` is ignored. Declare first, then assign: `local x; x=$(cmd)`. A plain `x=$(cmd)` does exit.
- A pipeline's status is its last command's, so `build | tee build.log` succeeds when `build` fails. Download to a file and check it before you use it, rather than piping a download into the next command.
- `exit` inside `$(…)` leaves only the subshell. In bash, `$(…)` doesn't inherit `set -e` either, outside POSIX mode or `inherit_errexit`.
- `cond && action` as the last line of a function returns 1 when `cond` is false, which kills the caller. End with `if` or add `return 0`.
- `((i++))` with `i=0` exits under bash 5 and continues under bash 3.2. Use `i=$((i + 1))`.

Check a status explicitly when it matters: `cmd || die "cmd failed"`.

## Quoting

Quote every expansion: `"$var"`, `"$(cmd)"`, `"$@"`. An unquoted variable splits on spaces and expands `*` and `[x]`. That matters most for paths built from `$HOME`, `$TMPDIR` or user input, which may contain both. Pass `--` before a value that might start with `-`. Leave a variable unquoted only on purpose, in a `case` word or an assignment.

When a program runs a shell, never paste values into the script text. Pass them as positional parameters:

```go
// The script stays constant, and dir arrives as $1, so spaces, quotes and $(…)
// in it stay data. Never fmt.Sprintf a value into the script text.
cmd := exec.CommandContext(ctx, "sh", "-c", `cd -- "$1" && make`, "sh", dir)
```

Better still, run the program directly: `exec.CommandContext(ctx, "make", "-C", dir)`.

## curl | sh

When the script arrives on stdin:
- `$0` is `sh`, so `dirname "$0"` and `$BASH_SOURCE` don't locate anything. Download sibling files by URL.
- Any command that reads stdin eats the rest of the script: `read`, `ssh`, `docker run -i`, a bare `cat`, a prompt. Redirect `</dev/null`, or read answers from `/dev/tty` only after `[ -t 1 ] && [ -r /dev/tty ]`, with a non-interactive default.
- A dropped connection runs a truncated script. Put everything in functions and call `main "$@"` on the last line, so nothing runs until the whole file has arrived.
- `curl` without `-f` pipes an HTML error page into `sh`. Use `curl -fsSL`, and verify downloaded artifacts with a checksum.
- Pass arguments with `curl -fsSL URL | sh -s -- --flag`.

## Traps, temp files, and cleanup

```sh
tmp=$(mktemp -d "${TMPDIR:-/tmp}/tool.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
trap 'exit 1' HUP INT TERM
```

bash runs the EXIT trap when a signal kills it. dash doesn't, so the second `trap` turns the signal into an `exit` that runs cleanup. Single-quote trap bodies so variables expand when the trap runs. Replace an installed file by writing a temp file in the same directory and renaming it with `mv -f`, so a failure never leaves half a binary in place.

## Installer template

This skeleton runs unchanged under every shell in the table above, by path, from stdin, and through a pipe. A repository test executes it under each available shell.

<!-- installer-template -->
```sh
#!/bin/sh
# Installs tool from a release. Safe for: curl -fsSL https://example.com/install.sh | sh
set -eu

log() { printf '%s\n' "$*" >&2; }
die() {
	log "error: $*"
	exit 1
}

fetch() { # fetch URL FILE
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1" || die "download failed: $1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1" || die "download failed: $1"
	else
		die "need curl or wget"
	fi
}

sha256() { # sha256 FILE; reads stdin, so the file name is never escaped
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum <"$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 <"$1" | cut -d ' ' -f 1
	else
		die "need sha256sum or shasum"
	fi
}

main() {
	base_url=${TOOL_BASE_URL:-https://example.com/tool/releases/latest/download}
	install_dir=${TOOL_INSTALL_DIR:-$HOME/.local/bin}

	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	machine=$(uname -m)
	case $os in
	darwin | linux) ;;
	*) die "unsupported OS: $os" ;;
	esac
	case $machine in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) die "unsupported architecture: $machine" ;;
	esac
	archive="tool_${os}_${arch}.tar.gz"

	tmp=$(mktemp -d "${TMPDIR:-/tmp}/tool-install.XXXXXX")
	staged=
	trap 'rm -rf "$tmp"; [ -z "$staged" ] || rm -f "$staged"' EXIT
	trap 'exit 1' HUP INT TERM

	fetch "$base_url/$archive" "$tmp/$archive"
	fetch "$base_url/SHA256SUMS" "$tmp/SHA256SUMS"
	want=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/SHA256SUMS")
	[ -n "$want" ] || die "no checksum for $archive"
	got=$(sha256 "$tmp/$archive")
	[ "$got" = "$want" ] || die "checksum mismatch for $archive"

	tar -xzf "$tmp/$archive" -C "$tmp" tool
	mkdir -p "$install_dir"
	staged="$install_dir/.tool.$$"
	cp "$tmp/tool" "$staged"
	chmod 755 "$staged"
	mv -f "$staged" "$install_dir/tool" # same directory, so the rename is atomic
	staged=
	log "installed $install_dir/tool"
}

main "$@"
```

## Run it under every target

Before handing off a changed script, run it under each shell its users have, in each way they invoke it. [Running under each target](references/run-targets.md) has the exact commands for macOS and Linux containers, a stdin and no-tty harness, and a Go test pattern. The short version:

```sh
shellcheck -s sh install.sh          # static check; it misses the bash 3.2 runtime crashes
/bin/dash install.sh                 # macOS ships dash: the fastest bashism check
/bin/sh < install.sh                 # macOS sh is bash 3.2, script on stdin
docker run --rm -v "$PWD:/w:ro" debian:12-slim sh /w/install.sh   # dash as /bin/sh
docker run --rm -v "$PWD:/w:ro" alpine:3.20 sh /w/install.sh      # BusyBox ash
docker run --rm -v "$PWD:/w:ro" bash:5.2 bash /w/install.sh       # bash 5
```

The container commands need what the script needs: slim images have no `curl`, and the template needs a `TOOL_BASE_URL` to download from. [Running under each target](references/run-targets.md) shows both.

In a review, a script that nobody ran under its targets is unverified. Say which shells and invocation modes were run, and name the ones that were not.
