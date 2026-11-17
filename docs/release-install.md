# Release installation

Install Kitsu's CLI without a repository checkout or Go. The installer chooses the archive for
your operating system and native architecture, verifies it against `SHA256SUMS`, then replaces
the executable. Linux, macOS, and Windows archives provide amd64 and arm64 builds.

**No GitHub releases or tags are currently published.** All historical releases, tags and
attached assets were removed on 17 November 2026. Use a successful CI artifact or
[build from source](development.md#build-from-source).

Current installers, plugin manifests and CI archives use `v3.0.0-beta.2` as their build label.
That label does not represent an available public release. The next release version and revision
are undecided; [publication](development.md#releases) must align the version metadata and create new assets.

## Install from a release

Run one command. The installer downloads the right binary, checks its checksum, and sets up `PATH`.
The current default is `v3.0.0-beta.2`; its assets must be published before these commands can succeed.

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/belevtsev/kitsu/main/install.sh | sh
```

Windows, in PowerShell 5.1 or 7:

```powershell
irm https://raw.githubusercontent.com/belevtsev/kitsu/main/install.ps1 | iex
```

These are user installs, with no Go toolchain or administrator access required.
The defaults are `$HOME/.local/bin/kitsu` on macOS/Linux and
`%LOCALAPPDATA%\kitsu\kitsu.exe` on Windows. Open a new terminal, then run `kitsu version`.

Kitsu uses Git to work in a repository; Windows hooks use the shell supplied by Git for Windows.

The installer adds its directory to user `PATH` unless you opt out. Rerunning it does not add
duplicate entries. It installs the CLI only; add the [agent plugin](../README.md#for-a-coding-agent)
through your host's marketplace. Provider CLIs and Git are installed separately.

## Check the host and start a task

After adding the [Kitsu plugin](../README.md#for-a-coding-agent), run:

```sh
kitsu agent check --host codex
kitsu task start "Add an order CSV export" --host codex
```

Use `--host claude` for Claude Code. The first command reads local PATH, host, plugin, task
skill, and hook setup without executing the host or changing configuration. Follow any next
steps; on Codex, manually review and trust Kitsu's current definitions in `/hooks`.
The second command explicitly opens the interactive host after local preflight. Confirm
authentication, model availability, hook execution, and skill loading in that session.

For a desktop session or a machine without the host CLI, add `--print` to prepare a prompt to
paste; `--json` also exposes diagnostics and proposed argv. Both modes execute nothing and
create no task record. See [onboarding](cli.md#check-setup-and-start-a-task) for exit statuses and
ready, broken, and unknown evidence. Installing the supporting Git gate is a separate step.

## Choose a version or directory

For a published version or custom directory, download the installer once and pass arguments.
On macOS/Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/belevtsev/kitsu/main/install.sh -o kitsu-install.sh
sh kitsu-install.sh --version v3.0.0-beta.2 --install-dir "$HOME/tools/kitsu" --no-modify-path
```

On Windows:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/belevtsev/kitsu/main/install.ps1 -OutFile "$env:TEMP\kitsu-install.ps1"
powershell -NoProfile -ExecutionPolicy Bypass -File "$env:TEMP\kitsu-install.ps1" -Version v3.0.0-beta.2 -InstallDir "$env:LOCALAPPDATA\tools\kitsu" -NoModifyPath
```

These examples leave `PATH` unchanged. Use the installed executable by its full path, or add the
chosen directory to user `PATH`. The selected version must be published. To install a local
CI build, add `--archive-dir` or `-ArchiveDir` and match its archive filenames.
`-ExecutionPolicy Bypass` applies only to that PowerShell process.

| macOS/Linux flag | PowerShell parameter | Environment default | Purpose |
| --- | --- | --- | --- |
| `--version TAG` | `-Version TAG` | `KITSU_VERSION` | Match the archive version; the current default is `v3.0.0-beta.2`. |
| `--install-dir DIR` | `-InstallDir DIR` | `KITSU_INSTALL_DIR` | Choose the destination directory. |
| `--no-modify-path` | `-NoModifyPath` | `KITSU_NO_MODIFY_PATH=1` | Install without changing user `PATH`. |
| `--archive-dir DIR` | `-ArchiveDir DIR` | `KITSU_ARCHIVE_DIR` | Use local archives and `SHA256SUMS`, with no download. |

Explicit arguments override environment defaults. Unix value options also accept `--flag=value`.
For a mirror or test server, `KITSU_DOWNLOAD_BASE` overrides the complete version-specific asset
directory URL; the installer appends only the archive name and `SHA256SUMS`. It has no command-line
flag. Use HTTPS; HTTP is supported only for loopback test servers. A failed checksum leaves an
already installed executable in place.

Unix help is `sh kitsu-install.sh --help`; PowerShell help is
`Get-Help "$env:TEMP\kitsu-install.ps1" -Full` after downloading as above.

## PATH and updates

The installer updates only user startup configuration on macOS/Linux or user `PATH` on Windows.
Open a new terminal after installation so Git and the agent host inherit the directory as well.
The hooks search only absolute `PATH` entries; see [hooks](hooks.md#when-kitsu-is-not-on-path).

For zsh it updates `${ZDOTDIR:-$HOME}/.zshrc`. For bash it updates `.bashrc` and the first existing
login profile among `.bash_profile`, `.bash_login`, and `.profile`. If none exists, bash creates
`.bash_profile` on macOS or `.profile` on Linux. Other supported POSIX shells use `.profile`. Custom installation
directories are resolved to absolute paths before adding them to `PATH`. Windows updates user
`PATH` and the installer process's current `PATH`.

On macOS/Linux, if the installer cannot update startup configuration, it prints the manual
`PATH` step. The verified executable remains installed; follow that step before opening a new
terminal.

Rerun the one-line command to reinstall or update to the installer's current default version.
Defaults stay pinned until updated in source; the installer does not discover the latest tag.
Reusing a saved installer keeps that script's default unless you override it.

## Install a CI artifact before publication

For contributors trying an unpublished commit:

1. Open a successful run of [CI](https://github.com/belevtsev/kitsu/actions/workflows/ci.yml)
   for the revision you want, then download and extract its **kitsu-archives** artifact.
2. Keep the contained platform archives intact alongside `install.sh`, `install.ps1` and `SHA256SUMS`.
3. Run the bundled installer with that directory. It verifies the selected local archive and uses no network.

On macOS or Linux:

```sh
sh /path/to/kitsu-archives/install.sh --archive-dir /path/to/kitsu-archives
```

On Windows:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "C:\path\kitsu-archives\install.ps1" -ArchiveDir "C:\path\kitsu-archives"
```

Use scripts and archives from the same download. If its filenames use a different version,
pass it with `--version` or `-Version`. To update, download a newer successful artifact and
rerun its bundled installer. Check the revision printed by `kitsu version` against the CI run:
different commits can share the same version label.

## Manual archive install

**For use after publication, without Go.** This alternative downloads a single archive and
checks its checksum before unpacking it. The tag assignments below retain the current
`v3.0.0-beta.2` build label; they are prepared recipes, not available downloads. Update them to
the chosen release version as part of publication. Perform the download in an empty directory.

On macOS or Linux:

```sh
set -eu
tag=v3.0.0-beta.2
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
base=https://github.com/belevtsev/kitsu/releases/download/$tag
archive=kitsu_${tag}_${os}_${arch}.tar.gz
curl -fsSLO "$base/$archive"
curl -fsSLO "$base/SHA256SUMS"
awk -v archive="$archive" '
  $2 == archive { matches++; selected = $0 }
  END { if (matches != 1) exit 1; print selected }
' SHA256SUMS > checksum.txt
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c checksum.txt
else
  shasum -a 256 -c checksum.txt
fi
tar -xzf "$archive" kitsu
mkdir -p "$HOME/.local/bin"
mv kitsu "$HOME/.local/bin/kitsu"
```

On Windows:

```powershell
$ErrorActionPreference = "Stop"
$tag = "v3.0.0-beta.2"
$machineArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = switch ($machineArch) { "ARM64" { "arm64" }; "AMD64" { "amd64" }; default { throw "Unsupported architecture: $machineArch" } }
$base = "https://github.com/belevtsev/kitsu/releases/download/$tag"
$zip = "kitsu_${tag}_windows_${arch}.zip"
Invoke-WebRequest "$base/$zip" -OutFile $zip
Invoke-WebRequest "$base/SHA256SUMS" -OutFile SHA256SUMS
$checksumLines = @(Select-String -Path SHA256SUMS -Pattern (" " + [regex]::Escape($zip) + "$"))
if ($checksumLines.Count -ne 1) { throw "SHA256SUMS must list $zip exactly once" }
$want = $checksumLines[0].Line.Split(" ")[0]
if ((Get-FileHash $zip -Algorithm SHA256).Hash -ne $want) { throw "the checksum of $zip does not match" }
Expand-Archive $zip -DestinationPath "$env:LOCALAPPDATA\kitsu" -Force
```

These manual recipes do not change `PATH`. Add the target directory to user `PATH`, open a new
terminal, and run `kitsu version`. Building the CLI is a separate
[contributor option](development.md#build-from-source).

## What CI checks

CI builds six platform archives, SBOMs, both installers, and `SHA256SUMS` into the
`kitsu-archives` artifact without publishing a release. The Go test harness under
`scenarios/installers` runs downloaded installer scripts with an isolated home and a `PATH`
without Go or Git. It exercises local archives, platform selection, checksum rejection, updates,
and `PATH` behavior. With `KITSU_INSTALL_ARCHIVES`, it also installs the real build archives.
Linux/macOS run the shell installer; Windows runs both PowerShell 5.1 and 7.

After publication, the release workflow tests the actual release installer assets and checks
the installed version and revision. Preparing installers and archives creates no tag or release.
