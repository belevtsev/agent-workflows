# install-test.ps1 -Tag TAG -Revision REVISION [-Archives DIRECTORY]
#
# Follows the first install instruction of README.md for Windows, on a PATH
# that holds no go, in the PowerShell that runs this script, and exits 0 only
# when `cw version` then prints TAG and REVISION.
#
# With -Archives, a directory that holds the archives and SHA256SUMS, the
# instruction downloads from this machine in place of the release; nothing
# else in it is changed. The instruction is then followed a second time with
# one byte of the archive changed, and must fail and install nothing.
#
# Without -Archives the instruction runs exactly as the README gives it, so it
# needs the published release.
#
# It needs python to serve the archives. Run it in the root of the repository.
param(
    [Parameter(Mandatory = $true)][string]$Tag,
    [Parameter(Mandatory = $true)][string]$Revision,
    [string]$Archives = ""
)
$ErrorActionPreference = "Stop"
$port = 18719
$work = Join-Path ([IO.Path]::GetTempPath()) ("cw-install-" + [guid]::NewGuid())
New-Item -ItemType Directory $work | Out-Null

# The instruction is the first powershell block after the line that starts it.
$instruction = @()
$seen = $false
$inside = $false
foreach ($line in Get-Content README.md) {
    if ($line.StartsWith("**From a release, without Go.**")) { $seen = $true }
    elseif ($seen -and -not $inside -and $line -eq '```powershell') { $inside = $true }
    elseif ($inside -and $line -eq '```') { break }
    elseif ($inside) { $instruction += $line }
}
if (-not ($instruction -contains ('$tag = "' + $Tag + '"'))) {
    throw "The first install instruction of README.md for Windows does not install $Tag."
}
if (-not ($instruction | Where-Object { $_.StartsWith('$base = "https://') })) {
    throw 'The first install instruction of README.md for Windows has no line that starts with $base = "https://.'
}

# A PATH without go.
$env:Path = ($env:Path -split ";" | Where-Object { $_ -and -not (Test-Path (Join-Path $_ "go.exe")) }) -join ";"
if (Get-Command go -ErrorAction SilentlyContinue) { throw "go is still on PATH." }

# Follow NAME: runs the instruction in $work\NAME.ps1 in a new PowerShell of
# the same kind, with a LOCALAPPDATA of its own, and returns its exit code.
function Follow([string]$name) {
    $downloads = Join-Path $work "downloads-$name"
    New-Item -ItemType Directory $downloads | Out-Null
    $saved = $env:LOCALAPPDATA
    $env:LOCALAPPDATA = Join-Path $work "local-$name"
    New-Item -ItemType Directory $env:LOCALAPPDATA | Out-Null
    Push-Location $downloads
    try {
        & (Get-Process -Id $PID).Path -NoProfile -ExecutionPolicy Bypass -File (Join-Path $work "$name.ps1") | Out-Host
        return $LASTEXITCODE
    }
    finally {
        Pop-Location
        $env:LOCALAPPDATA = $saved
    }
}

$server = $null
try {
    if ($Archives -eq "") {
        Set-Content (Join-Path $work "good.ps1") $instruction
    }
    else {
        $arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
        $zip = "cw_${Tag}_windows_${arch}.zip"
        foreach ($name in "good", "damaged") {
            New-Item -ItemType Directory (Join-Path $work $name) | Out-Null
            Copy-Item (Join-Path $Archives $zip), (Join-Path $Archives "SHA256SUMS") (Join-Path $work $name)
        }
        # One byte in the middle of the damaged copy gets another value.
        $damaged = Join-Path $work "damaged\$zip"
        $bytes = [IO.File]::ReadAllBytes($damaged)
        $at = [int]($bytes.Length / 2)
        $bytes[$at] = ($bytes[$at] + 1) % 256
        [IO.File]::WriteAllBytes($damaged, $bytes)

        $server = Start-Process python -PassThru -WindowStyle Hidden -ArgumentList `
            "-m", "http.server", "$port", "--bind", "127.0.0.1", "--directory", "`"$work`""
        $ready = $false
        foreach ($try in 1..50) {
            try {
                Invoke-WebRequest "http://127.0.0.1:$port/good/SHA256SUMS" -UseBasicParsing | Out-Null
                $ready = $true
                break
            }
            catch { Start-Sleep -Milliseconds 200 }
        }
        if (-not $ready) { throw "The local server for the archives did not start." }
        foreach ($name in "good", "damaged") {
            $changed = $instruction | ForEach-Object {
                if ($_.StartsWith('$base = ')) { '$base = "http://127.0.0.1:' + $port + '/' + $name + '"' } else { $_ }
            }
            Set-Content (Join-Path $work "$name.ps1") $changed
        }
    }

    if ((Follow "good") -ne 0) { throw "The install instruction failed." }
    $printed = & (Join-Path $work "local-good\cw\cw.exe") version
    $printed | Out-Host
    if (-not ($printed -contains "cw $Tag")) { throw "cw version does not print the tag $Tag." }
    if (-not ($printed -contains "revision $Revision")) { throw "cw version does not print the revision $Revision." }

    if ($Archives -ne "") {
        if ((Follow "damaged") -eq 0) { throw "The install instruction succeeded with a damaged archive." }
        if (Test-Path (Join-Path $work "local-damaged\cw")) { throw "cw was installed from a damaged archive." }
        "A damaged archive was refused and nothing was installed." | Out-Host
    }
}
finally {
    if ($server) { Stop-Process -Id $server.Id -Force -ErrorAction SilentlyContinue }
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
