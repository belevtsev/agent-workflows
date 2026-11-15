# Release installation

Install Kitsu's CLI without a repository checkout or Go. The installer chooses the archive for
your operating system and native architecture, verifies it against `SHA256SUMS`, then replaces
the executable. Linux, macOS, and Windows releases provide amd64 and arm64 builds.

**Prepared for `v3.0.0-beta.2`; this release is not published yet.** The public install commands
need its release assets. To try this build before publication, use the
[CI artifact method](#install-a-ci-artifact-before-publication) below. The older
`v3.0.0-beta.1` release contains `cw`, not a Kitsu executable.

## Install from a release

On macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/belevtsev/kitsu/main/install.sh | sh
```

On Windows, in PowerShell 5.1 or 7:

```powershell
irm https://raw.githubusercontent.com/belevtsev/kitsu/main/install.ps1 | iex
```

These are user installs. The defaults are `$HOME/.local/bin/kitsu` on macOS/Linux and
`%LOCALAPPDATA%\kitsu\kitsu.exe` on Windows. Open a new terminal, then run `kitsu version`.
Kitsu uses Git to work in a repository; Windows hooks use the shell supplied by Git for Windows.

The installer adds its directory to user `PATH` unless you opt out. Rerunning it does not add
duplicate entries. It installs the CLI only; add the [agent plugin](../README.md#for-a-coding-agent)
through your host's marketplace. Provider CLIs and Git are installed separately.

## Install a CI artifact before publication

1. Open a passing run of [CI](https://github.com/belevtsev/kitsu/actions/workflows/ci.yml) and
   download its **kitsu-archives** artifact. Extract that download into a directory of your choice.
2. Keep the contained platform archives intact. The directory must hold `install.sh`,
   `install.ps1`, `SHA256SUMS`, and the `kitsu_<version>_<os>_<arch>` archives.
3. Run the bundled installer with that directory. It verifies the selected local archive and
   uses no network. Neither a clone nor Go is needed.

On macOS or Linux, replace the example path with the directory you extracted:

```sh
sh /path/to/kitsu-archives/install.sh --archive-dir /path/to/kitsu-archives
```

On Windows:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "C:\path\kitsu-archives\install.ps1" -ArchiveDir "C:\path\kitsu-archives"
```

`-ExecutionPolicy Bypass` applies only to that PowerShell process. Open a new terminal and run
`kitsu version`. The artifact is a build of that CI commit, not a published release.
If its filenames use a different version, pass that version explicitly.

## Choose a version or directory

Download the installer once to pass arguments. For example, on macOS/Linux:

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
chosen directory to user `PATH`. Select an existing `v`-prefixed release tag for a public download.
The prepared `v3.0.0-beta.2` still needs the CI artifact route until it is published.

| macOS/Linux flag | PowerShell parameter | Environment default | Purpose |
| --- | --- | --- | --- |
| `--version TAG` | `-Version TAG` | `KITSU_VERSION` | Select the version; the default is `v3.0.0-beta.2`. |
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
login profile among `.bash_profile`, `.bash_login`, and `.profile`; macOS bash creates
`.bash_profile` if none exists. Other supported POSIX shells use `.profile`. Custom installation
directories are resolved to absolute paths before adding them to `PATH`. Windows updates user
`PATH` and the installer process's current `PATH`.

On macOS/Linux, if the installer cannot update startup configuration, it prints the manual
`PATH` step. The verified executable remains installed; follow that step before opening a new
terminal.

Rerun the one-line install command to reinstall or move to the installer's current default
version. Defaults stay pinned until a newer release updates them; the installer does not choose
an arbitrary latest tag. Use the version option to pin a particular release. Reusing a saved
installer keeps that script's default unless you override it.

## Manual archive install

**From a release, without Go.** The following alternative downloads a single archive and checks
its checksum before unpacking it. These URLs become available after publication. Perform the
download in an empty directory.

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
