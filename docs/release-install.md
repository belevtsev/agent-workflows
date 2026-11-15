# Release installation

**Prepared for `v3.0.0-beta.2`; this Gatecub release is not published yet.**
Use the [source installation](../README.md#install) until it is published. The commands below
describe the future release and are exercised against local archives in CI. They do not claim
that these download URLs already exist. The previously published `v3.0.0-beta.1` contains `cw`
artifacts and is not a Gatecub binary release.

## Install the prepared release

**From a release, without Go.** Each archive is checked against `SHA256SUMS` before it is
unpacked. Gatecub needs Git. The release prepares Linux, macOS and Windows archives for amd64
and arm64.

Linux and macOS:

```sh
set -eu
tag=v3.0.0-beta.2
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
base=https://github.com/belevtsev/gatecub/releases/download/$tag
archive=gatecub_${tag}_${os}_${arch}.tar.gz
curl -fsSLO "$base/$archive"
curl -fsSLO "$base/SHA256SUMS"
awk -v archive="$archive" '$2 == archive { print }' SHA256SUMS | (sha256sum -c - 2>/dev/null || shasum -a 256 -c -) &&
  tar -xzf "$archive" gatecub &&
  mkdir -p "$HOME/.local/bin" && mv gatecub "$HOME/.local/bin/gatecub"
```

Windows, in PowerShell:

```powershell
$ErrorActionPreference = "Stop"
$tag = "v3.0.0-beta.2"
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$base = "https://github.com/belevtsev/gatecub/releases/download/$tag"
$zip = "gatecub_${tag}_windows_${arch}.zip"
Invoke-WebRequest "$base/$zip" -OutFile $zip
Invoke-WebRequest "$base/SHA256SUMS" -OutFile SHA256SUMS
$checksumLines = @(Select-String -Path SHA256SUMS -Pattern (" " + [regex]::Escape($zip) + "$"))
if ($checksumLines.Count -ne 1) { throw "SHA256SUMS must list $zip exactly once" }
$want = $checksumLines[0].Line.Split(" ")[0]
if ((Get-FileHash $zip -Algorithm SHA256).Hash -ne $want) { throw "the checksum of $zip does not match" }
Expand-Archive $zip -DestinationPath "$env:LOCALAPPDATA\gatecub" -Force
```

Put the directory containing `gatecub` on `PATH` and open a new shell. On Linux and macOS that
directory is `$HOME/.local/bin`; on Windows it is `%LOCALAPPDATA%\gatecub`. Run `gatecub version`
to check the version and revision. The hooks use only absolute entries of `PATH`; see
[hooks](hooks.md#when-gatecub-is-not-on-path).

## What CI checks

The archive job builds all six archives, an SBOM for each, and `SHA256SUMS` from the current
commit. It publishes no release. The install jobs read the recipes above and replace only
their download base URL with a local server. With Go removed from `PATH`, they install and
check the version and revision, then repeat with a damaged archive. The damaged archive still
unpacks, so the checksum must reject it before installation.

The release workflow permits publication only from `belevtsev/gatecub` and requires the
requested tag to match both recipes. After publication, its install jobs use the public URLs
without substitution. Preparing these files does not create a tag or publish a release.
