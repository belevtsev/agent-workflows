# Running a script under each target

Run the script the way its users do: under each shell they have, by path, on stdin, and through a pipe, with no terminal. Record what ran, and say which targets were not run.

## Static checks first

They are fast and catch most syntax problems, but none of them catches the bash 3.2 empty-array crash or behavior that depends on the data:

```sh
shellcheck -s sh install.sh      # POSIX target; use -s bash for bash scripts
shfmt -ln posix -d install.sh    # fails to parse [[, arrays, and other bash syntax
checkbashisms install.sh         # Debian devscripts; looks for bash-only constructs
```

## macOS

| Target | Command | Notes |
| --- | --- | --- |
| `/bin/sh` | `/bin/sh install.sh` | bash 3.2 in POSIX mode by default. Check with `ls -l /private/var/select/sh`. It accepts `[[` and arrays, so it doesn't catch bashisms |
| dash | `/bin/dash install.sh` | Shipped since macOS 10.15. The quickest local bashism check |
| bash 3.2 | `/bin/bash install.sh` | What `#!/usr/bin/env bash` finds on a stock Mac |
| bash 5 | `"$(brew --prefix)/bin/bash" install.sh` | After `brew install bash`; or use the container below |

zsh is not `sh`. Don't count a zsh run as evidence for `sh`.

## Linux containers

Mount the script read-only, so the container can't change it. Slim images lack `curl`: install it when the script downloads anything.

```sh
# dash, as /bin/sh on Debian and Ubuntu (and GitHub's Linux runners)
docker run --rm -v "$PWD:/w:ro" debian:12-slim sh -c \
  'apt-get -qq update && apt-get -qq install -y curl >/dev/null && sh /w/install.sh'

# BusyBox ash, as on Alpine
docker run --rm -v "$PWD:/w:ro" alpine:3.20 sh -c 'apk add -q curl && sh /w/install.sh'

# bash 5, also in POSIX mode
docker run --rm -v "$PWD:/w:ro" bash:5.2 bash /w/install.sh
docker run --rm -v "$PWD:/w:ro" bash:5.2 bash --posix /w/install.sh
```

Add `--platform linux/amd64` to also check the other architecture on an Apple Silicon Mac. Use `-e NAME=value` to pass the variables the script reads. For the installer template, mount a local release directory and point `TOOL_BASE_URL` at it, so the run doesn't depend on a published release:

```sh
docker run --rm -v "$PWD:/w:ro" -v "$PWD/dist:/r:ro" -e TOOL_BASE_URL=file:///r \
  alpine:3.20 sh -c 'apk add -q curl && sh /w/install.sh'
```

A server on the host, such as `python3 -m http.server`, is reachable from Docker Desktop containers as `http://host.docker.internal:8000`.

## Simulate curl | sh

The script must work when it arrives on stdin, when `$0` is `sh`, and when no terminal is attached:

```sh
/bin/sh < install.sh                                  # stdin is the script
cat install.sh | /bin/dash -s -- --flag               # a pipe, with arguments
curl -fsSL "file://$PWD/install.sh" | sh             # through curl itself
curl -fsSL "file://$PWD/install.sh" | sh 2>&1 | cat   # stdout is not a terminal either
```

Don't add `</dev/null` after `| sh`. In bash and dash that redirection replaces the pipe, so `sh` reads an empty script, runs nothing and exits 0. The pipe already means stdin is not a terminal.

To test the real download path, serve a directory: `python3 -m http.server 8000 --directory dist`, then use `http://localhost:8000/install.sh`.

## A matrix in one loop

```sh
for sh in /bin/sh /bin/dash /bin/bash "/bin/bash --posix"; do
  for mode in path stdin pipe; do
    case $mode in
    path) $sh install.sh </dev/null ;;
    stdin) $sh <install.sh ;;
    pipe) cat install.sh | $sh -s ;;
    esac
    printf '%s %s: exit %s\n' "$sh" "$mode" "$?"
  done
done
```

Run the same loop inside each container. Check more than the exit status: the files the script should create, that a failure leaves nothing half-installed, and that the temp directory is empty afterwards (point `TMPDIR` at an empty directory and list it).

## A Go test for a shipped script

A script a repository ships deserves a test that runs it, not just a reviewer. This pattern runs a script under each shell that is installed, with the script on stdin, as `curl | sh` does. Missing shells skip, so the same test covers macOS (bash 3.2, dash) locally and dash and bash 5 in Linux CI. `/bin/bash` is listed on its own because on a Mac the `bash` on `PATH` is often Homebrew's bash 5:

```go
func TestInstallScriptShells(t *testing.T) {
	script, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, shell := range [][]string{{"/bin/sh"}, {"dash"}, {"/bin/bash"}, {"bash"}, {"bash", "--posix"}, {"busybox", "sh"}} {
		t.Run(strings.Join(shell, " "), func(t *testing.T) {
			t.Parallel()
			path, err := exec.LookPath(shell[0])
			if err != nil {
				t.Skipf("%s is not installed", shell[0])
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, path, append(shell[1:], "-s")...)
			cmd.Stdin = bytes.NewReader(script) // no terminal, as with curl | sh
			cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "TMPDIR="+t.TempDir())
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %v\n%s", shell, err, out)
			}
		})
	}
}
```

Assert on effects as well as success: run failure cases (a missing download, a bad checksum) and check that nothing was installed and the temp directory is empty. Use paths with spaces and glob characters such as `dir with space [x]*`, which catch missing quotes.

## CI

GitHub's `macos-*` runners have bash 3.2 as `/bin/sh` and `/bin/bash`, plus `/bin/dash`. The `ubuntu-*` runners have dash as `/bin/sh` and bash 5. Running the test above on both covers four of the five targets. Add an Alpine container job when BusyBox users matter.
