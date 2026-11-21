# Release installation

Install Kitsu's CLI without a repository checkout or Go. The installer chooses the archive for
your operating system and native architecture, verifies it against `SHA256SUMS`, then replaces
the executable. Linux, macOS, and Windows archives provide amd64 and arm64 builds.

## Install from a release

With Git and Codex or Claude Code installed, run the command for your platform. Replace `codex` with `claude` for Claude Code.

**macOS or Linux**

```sh
(
  installer=$(mktemp) &&
    trap 'rm -f "$installer"' 0 &&
    curl -fsSL https://github.com/belevtsev/kitsu/releases/latest/download/install.sh -o "$installer" &&
    grep -Fq 'to check the installed version.' "$installer" &&
    sh -n "$installer" &&
    sh "$installer" --host codex
)
```

**Windows PowerShell**

```powershell
& ([scriptblock]::Create([Net.WebClient]::new().DownloadString('https://github.com/belevtsev/kitsu/releases/latest/download/install.ps1'))) -AgentHost codex
```

The installer downloads the CLI and matching plugin from the latest public release, verifies both checksums, and registers the plugin with your agent. Downloads are anonymous. On macOS and Linux the command stages the whole installer before running it.

Plugin setup saves the chosen host if your personal default is empty. Run them in a terminal with the host flag omitted to choose Codex, Claude Code, or the CLI alone. For unattended setup, pass the host flag explicitly. To set a default after a v0.0.2 install, run `kitsu config set host codex --global` or substitute `claude`. [Choose a host per role](configured-workflow.md#configure-the-roles)

The default locations are `$HOME/.local/bin/kitsu` on macOS/Linux and `%LOCALAPPDATA%\kitsu\kitsu.exe` on Windows. Open a new terminal and run `kitsu version` for the installed tag and revision. Windows hooks use Git for Windows.

For commands under [Unreleased](../CHANGELOG.md#unreleased), install the matching
CLI and plugin from a [CI build](#install-a-ci-artifact-before-publication).
Check your installed command with `kitsu task --help`.

## Check the host and start a task

After installing, restart Codex or Claude Code so it picks up the plugin and the updated PATH. Open your project’s Git worktree in a fresh session.

- **Codex:** Open `/hooks` and review and trust Kitsu’s hook definitions. In chat, ask “Use Kitsu’s task skill to plan an order CSV export and give me the task ID.”
- **Claude Code:** Enter `/kitsu:task Plan an order CSV export and give me the task ID.`

If your agent cannot find Kitsu, run `kitsu agent check --host codex` in the project’s terminal (use `claude` for Claude Code). This checks the local CLI, plugin, skill, and hook setup and prints any next steps. To launch a new session directly from the terminal, run `kitsu task start "Plan an order CSV export" --until plan --host codex`.

For a desktop session or a machine without the host CLI, add `--print` to prepare a prompt to
paste; `--json` also exposes diagnostics and proposed argv. Both modes execute nothing and
create no task record. See [onboarding](cli.md#check-setup-and-start-a-task) for exit statuses and
ready, broken, and unknown evidence. Installing the supporting Git gate is a separate step.

<a name="add-role-coordination"></a>

## Add role coordination

The installer enables the `kitsu` plugin: the daily task skill, the gate skill and the host hooks. The seven role agents, the managed role workflow, OpenSpec records, role settings and supervised runs are in a second plugin of the same marketplace, `kitsu-team`. It is opt-in and experimental. On two small tasks, one run each, the roles used about 5 and 39 times the tokens of a plain agent ([what an agent spends](benchmarks.md#what-an-agent-spends)).

```sh
codex plugin add kitsu-team@kitsu
```

```sh
claude plugin install kitsu-team@kitsu
```

Restart the host. In Claude Code, start managed work with `/kitsu-team:managed`. In Codex, ask for Kitsu's `managed` skill. Keep the `kitsu` plugin enabled: it supplies the hooks that both plugins use.

## Choose a version or directory

For a published version or custom directory, download the installer once and pass arguments.
On macOS/Linux:

```sh
curl -fsSL https://github.com/belevtsev/kitsu/releases/latest/download/install.sh -o kitsu-install.sh
sh kitsu-install.sh --version v0.0.2 --install-dir "$HOME/tools/kitsu" --no-modify-path
```

On Windows:

```powershell
Invoke-WebRequest https://github.com/belevtsev/kitsu/releases/latest/download/install.ps1 -OutFile "$env:TEMP\kitsu-install.ps1"
powershell -NoProfile -ExecutionPolicy Bypass -File "$env:TEMP\kitsu-install.ps1" -Version v0.0.2 -InstallDir "$env:LOCALAPPDATA\tools\kitsu" -NoModifyPath
```

These examples leave `PATH` unchanged. Use the installed executable by its full path, or add the
chosen directory to user `PATH`. The selected version must be published. To install a local
CI build, use the bundled installer and pass its artifact directory.
`-ExecutionPolicy Bypass` applies only to that PowerShell process.

| macOS/Linux flag | PowerShell parameter | Environment default | Purpose |
| --- | --- | --- | --- |
| `--version TAG` | `-Version TAG` | `KITSU_VERSION` | Pin the requested tag. Remote installs use the latest release by default. |
| `--host codex\|claude` | `-AgentHost codex\|claude` | — | Install the matching plugin and set your first personal default. |
| `--set-default` | `-SetDefault` | — | Change the saved personal default after the selected plugin installs. |
| `--cli-only` | `-CliOnly` | — | Install the preview CLI without prompting or changing the saved host. |
| `--install-dir DIR` | `-InstallDir DIR` | `KITSU_INSTALL_DIR` | Choose the destination directory. |
| `--no-modify-path` | `-NoModifyPath` | `KITSU_NO_MODIFY_PATH=1` | Install without changing user `PATH`. |
| `--archive-dir DIR` | `-ArchiveDir DIR` | `KITSU_ARCHIVE_DIR` | Use local archives and `SHA256SUMS`, with no download. |

Explicit arguments override environment defaults. Unix value options also accept `--flag=value`.
For a mirror or test server, `KITSU_DOWNLOAD_BASE` overrides the complete version-specific asset
directory URL; the installer reads the CLI archive, `SHA256SUMS` and, for host installs, `kitsu-plugin.zip` from that directory. It has no command-line
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

Rerun the host install command to update both components. An explicit version or mirror selects only that source. If host registration fails after the CLI is installed, fix the reported problem and rerun the same command.

## Install a CI artifact before publication

Download **kitsu-archives** from a successful run for the revision you want, extract it, and open a terminal in the extracted directory. Keep the platform archives, `kitsu-plugin.zip`, installers and `SHA256SUMS` together. Install the host CLI you want to use. [CI runs](https://github.com/belevtsev/kitsu/actions/workflows/ci.yml)

With a build whose installer lists the host option, one command installs the platform CLI and registers the local plugin. Choose `codex` or `claude`.

```sh
sh ./install.sh --archive-dir . --host codex
```

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1 -ArchiveDir . -AgentHost codex
```

Omit the host option in an interactive terminal to show the preview chooser. Use `--cli-only` or `-CliOnly` to skip setup explicitly. In a noninteractive shell, omitting both options installs only the CLI. With one matching platform archive in the manifest, the installer can discover a preview tag. When multiple tags match, select one with `--version TAG` or `-Version TAG`. Use scripts and archives from the same download. For an update, rerun the command from the new bundle. Check the revision from `kitsu version` against the CI run because two builds can share a version label.

### Register the matching marketplace

If host setup needs attention after the CLI is installed, fix the reported problem, then run the following from the same extracted bundle. You can also use it to add a second host.

```sh
kitsu agent setup --host claude --archive-dir .
kitsu agent check --host claude
```

Setup verifies the plugin checksum and registers a persistent copy for the selected host. To change your saved default after registering a second host, use `kitsu config set host claude --global`. In a new terminal, run `kitsu agent check --host codex` or `--host claude` from the repository where you work. Review Codex hook trust in `/hooks`. Once ready, start a task with `kitsu task start "Add an order CSV export" --host codex`. [Start or resume a daily task](task-harness.md)

<details>
<summary>Installing an older preview whose installer lacks the host option</summary>

Run the bundled installer with the exact tag shown in its native archive filename, for example `sh ./install.sh --archive-dir . --version TAG` or `.\install.ps1 -ArchiveDir . -Version TAG`. Then register the matching plugin manually.

Choose a build containing `kitsu-plugin.zip` and keep the extracted marketplace in a permanent directory. From the unpacked artifact directory, check the plugin against the bundled manifest and extract it. Use a fresh destination for each build.

On macOS or Linux, set `plugin_dir` to your destination:

```sh
(
  set -eu
  plugin_dir="$HOME/.local/share/kitsu/preview-plugins"
  test ! -e "$plugin_dir/kitsu-marketplace"
  line=$(awk '$2 == "kitsu-plugin.zip" { n++; row=$0 } END { if (n!=1) exit 1; print row }' SHA256SUMS)
  if command -v sha256sum >/dev/null; then
    echo "$line" | sha256sum -c -
  else
    echo "$line" | shasum -a 256 -c -
  fi
  mkdir -p "$plugin_dir"
  unzip -q kitsu-plugin.zip -d "$plugin_dir"
)
```

On Windows, in PowerShell:

```powershell
$pluginDir = Join-Path $env:LOCALAPPDATA "kitsu/preview-plugins"
if (Test-Path "$pluginDir/kitsu-marketplace") { throw "Choose a fresh destination" }
$lines = @(Select-String -Path SHA256SUMS -Pattern '^[0-9a-f]{64}  kitsu-plugin\.zip$')
if ($lines.Count -ne 1) { throw "Expected one plugin checksum" }
if ((Get-FileHash kitsu-plugin.zip).Hash -ne $lines[0].Line.Split(" ")[0]) { throw "Plugin checksum mismatch" }
Expand-Archive kitsu-plugin.zip -DestinationPath $pluginDir
```

Register the absolute path to the extracted marketplace with your chosen host. Replace `/path/to/kitsu-marketplace` with that directory. The local marketplace supplies the `kitsu` plugin with both skills and the host hook definitions, and the opt-in [`kitsu-team` plugin](#add-role-coordination) with the role instructions.

```sh
codex plugin marketplace add /path/to/kitsu-marketplace
codex plugin add kitsu@kitsu
```

```sh
claude plugin marketplace add /path/to/kitsu-marketplace
claude plugin install kitsu@kitsu
```

Open a new terminal in your working repository. Run `kitsu task open --help` to confirm the daily CLI is on PATH, then `kitsu agent check --host codex` or `--host claude`. In Codex review the definitions in `/hooks` to enable trusted execution. [Start or resume a daily task](task-harness.md)

</details>

## Manual archive install

This alternative downloads a single release archive and checks its checksum before unpacking it.
Perform the download in an empty directory. The one-line installers above also configure `PATH`.

On macOS or Linux:

```sh
set -eu
tag=v0.0.2
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
$tag = "v0.0.2"
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

The preview supporting delegated coding and pinned commit ranges used task-skill protocol 2. The source build with configurable daily review requires task-skill protocol 3. Update both the CLI and the registered marketplace when upgrading from the earlier daily preview. Existing task ledgers can still be resumed.

## Verify release provenance

For releases made by the protected **Release** workflow, you can use GitHub CLI to check that
`SHA256SUMS` came from a build on Kitsu's `main`. Run this in the directory where you downloaded
the manifest and the assets you want to verify.

```sh
gh attestation verify SHA256SUMS --repo belevtsev/kitsu \
  --signer-workflow belevtsev/kitsu/.github/workflows/release.yml --source-ref refs/heads/main
```

Match the verification output's source commit with the commit for the selected release tag.
Use the SHA-256 digest in the verified manifest to check the downloaded installer, archive or
plugin before executing or extracting it. Earlier releases such as `v0.0.2` used the previous
workflow and do not have this manifest attestation.
[GitHub CLI attestation verification](https://cli.github.com/manual/gh_attestation_verify)

## What CI checks

CI builds six platform archives, their SBOMs, a matching host plugin, both installers, and `SHA256SUMS` into the
`kitsu-archives` artifact without publishing a release. The Go test harness under
`scenarios/installers` runs downloaded installer scripts with an isolated home and a `PATH`
without Go or Git. It exercises local archives, platform selection, checksum rejection, updates,
and `PATH` behavior. With `KITSU_INSTALL_ARCHIVES`, it also installs the real build archives. In a separate disposable Git repository the installed CLI opens a plan, records it, extends the task to implementation, resumes the plan in a new invocation and verifies a real output file.
It also resumes verify-only tasks on passing and failing staged patches and checks that HEAD, the index and developer files survive. Fixture settings exercise plugin diagnostics even when host executables are absent. A separate subtest detects installed native clients, registers the local marketplace in a fresh profile and previews a task handoff. The log identifies the client version or the reason the native check was skipped. Linux and macOS use the shell installer. Windows runs both PowerShell 5.1 and 7.

For release validation, dispatch the **CI** workflow at the candidate revision with `verify_native_hosts` enabled. The installer jobs provision pinned native Codex and Claude Code clients. They require a result from each host on amd64 and arm64 for Linux, macOS and Windows, including Windows PowerShell 5.1 and 7. The expected matrix contains 16 shell/host combinations. A missing client or missing row fails the requested evidence run. Each `kitsu-native-install-<runner>` artifact contains `results.json` and the Go test log. Check the reported revision, executable and plugin hashes, fresh install, reinstall and resumed task before claiming that combination. Normal CI can skip a client it lacks. [Workflow and pinned versions](../.github/workflows/ci.yml)

The release workflow tests the exact publication bundle on all six platforms and checks the
installed version and revision before publishing. See [releases](development.md#releases).
