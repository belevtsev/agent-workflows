<#
.SYNOPSIS
Installs a checksummed Kitsu archive for the current Windows user.
.DESCRIPTION
Downloads the exact Windows archive and SHA256SUMS, or reads them from ArchiveDir.
Requires PowerShell 5.1 or newer. No Go toolchain or administrator access is needed.
ArchiveDir can install a CI build before publication.
.PARAMETER Version
Archive version label. Defaults to KITSU_VERSION. When neither is provided, a local install uses
the single archive tag for this platform. Remote host installs select the latest pair; CLI-only installs use v0.0.1.
.PARAMETER InstallDir
Destination directory. Defaults to KITSU_INSTALL_DIR, then LOCALAPPDATA\kitsu.
.PARAMETER NoModifyPath
Leaves both user PATH and the current session PATH unchanged. Also set by KITSU_NO_MODIFY_PATH=1.
.PARAMETER ArchiveDir
Reads the archive and SHA256SUMS locally, without network access. Defaults to KITSU_ARCHIVE_DIR.
.PARAMETER AgentHost
Installs the matching plugin and runs Kitsu agent setup for codex or claude.
.EXAMPLE
irm https://raw.githubusercontent.com/belevtsev/kitsu/main/install.ps1 | iex
.EXAMPLE
.\install.ps1 -ArchiveDir C:\Downloads\kitsu -InstallDir C:\Tools\kitsu -NoModifyPath
#>
[CmdletBinding()]
param(
    [string]$Version = $env:KITSU_VERSION,
    [string]$InstallDir = $env:KITSU_INSTALL_DIR,
    [switch]$NoModifyPath = ($env:KITSU_NO_MODIFY_PATH -eq '1'),
    [string]$ArchiveDir = $env:KITSU_ARCHIVE_DIR,
    [string]$AgentHost
)

# Keep preferences and helper functions out of an interactive irm | iex session.
& {
    param($ReleaseVersion, $DestinationDir, $LeavePath, $LocalArchives, $SelectedHost, $HostSpecified)
    $ErrorActionPreference = 'Stop'
    Set-StrictMode -Version 2.0

    function Get-KitsuArchitecture {
        if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
            throw 'This installer supports Windows only; use install.sh on Linux or macOS.'
        }
        # SYSTEM is shared across registry views, including x86/x64 emulation
        # on ARM64. Process-level architecture variables can describe an emulator.
        $nativeName = ''
        $machineEnvironment = $null
        try {
            $machineEnvironment = [Microsoft.Win32.Registry]::LocalMachine.OpenSubKey(
                'SYSTEM\CurrentControlSet\Control\Session Manager\Environment')
            if ($null -ne $machineEnvironment) {
                $nativeName = [string]$machineEnvironment.GetValue('PROCESSOR_ARCHITECTURE')
            }
        }
        catch { } # Restricted registry access may still leave a native OS query available.
        finally { if ($null -ne $machineEnvironment) { $machineEnvironment.Dispose() } }
        if ([string]::IsNullOrEmpty($nativeName)) { $nativeName = $env:PROCESSOR_ARCHITEW6432 }
        if ([string]::IsNullOrEmpty($nativeName)) {
            if ($PSVersionTable.PSVersion.Major -lt 7) {
                throw 'Windows native architecture is unavailable; use PowerShell 7 or allow reading the machine environment registry key.'
            }
            try { $nativeName = [string][Runtime.InteropServices.RuntimeInformation]::OSArchitecture }
            catch { throw 'Windows native architecture could not be determined safely.' }
        }
        switch ($nativeName) {
            'AMD64' { return 'amd64' }
            'X64' { return 'amd64' }
            'ARM64' { return 'arm64' }
            default { throw "Unsupported native Windows architecture: $nativeName." }
        }
    }

    function Assert-KitsuDownloadUri([uri]$AssetUri) {
        $secure = $AssetUri.IsAbsoluteUri -and $AssetUri.Scheme -eq 'https'
        $localHttp = $AssetUri.IsAbsoluteUri -and $AssetUri.Scheme -eq 'http' -and $AssetUri.IsLoopback
        if ((-not $secure -and -not $localHttp) -or $AssetUri.UserInfo -or $AssetUri.Fragment) {
            throw 'KITSU_DOWNLOAD_BASE and download redirects must use HTTPS, or HTTP on loopback, without credentials or fragments.'
        }
    }

    function Save-KitsuAsset($Client, [string]$Source, [string]$Name, [string]$OutputFile) {
        if ($null -eq $Client) {
            $sourceFile = Get-Item -LiteralPath (Join-Path $Source $Name) -Force
            if (($sourceFile.Attributes -band ([IO.FileAttributes]::Directory -bor [IO.FileAttributes]::ReparsePoint)) -ne 0) {
                throw "Local release asset $Name must be a regular file."
            }
            [IO.File]::Copy($sourceFile.FullName, $OutputFile, $false)
            return
        }
        $AssetUri = [uri]($Source.TrimEnd('/') + '/' + [uri]::EscapeDataString($Name))
        for ($redirect = 0; $redirect -le 5; $redirect++) {
            Assert-KitsuDownloadUri $AssetUri
            $response = $null
            try {
                $response = $Client.GetAsync($AssetUri).GetAwaiter().GetResult()
                $status = [int]$response.StatusCode
                if ($status -in 301, 302, 303, 307, 308) {
                    if ($null -eq $response.Headers.Location) { throw 'The download redirect has no destination.' }
                    $AssetUri = [uri]::new($AssetUri, $response.Headers.Location)
                    continue
                }
                if (-not $response.IsSuccessStatusCode) {
                    throw "HTTP $status. The Kitsu $ReleaseVersion release or its Windows archive may not be published yet."
                }
                $output = [IO.File]::Open($OutputFile, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write)
                try { $response.Content.CopyToAsync($output).GetAwaiter().GetResult() }
                finally { $output.Dispose() }
                return
            }
            catch {
                throw "Could not download $($AssetUri.Segments[-1]) for Kitsu ${ReleaseVersion}: $($_.Exception.Message) No older cw release will be used."
            }
            finally { if ($null -ne $response) { $response.Dispose() } }
        }
        throw 'The Kitsu download exceeded five redirects.'
    }

    function Get-KitsuSHA256([string]$FilePath) {
        $algorithm = [Security.Cryptography.SHA256]::Create()
        $stream = $null
        try {
            $stream = [IO.File]::OpenRead($FilePath)
            $digest = $algorithm.ComputeHash($stream)
            return [BitConverter]::ToString($digest).Replace('-', '').ToLowerInvariant()
        }
        finally {
            if ($null -ne $stream) { $stream.Dispose() }
            $algorithm.Dispose()
        }
    }

    function Expand-KitsuExecutable([string]$ZipPath, [string]$OutputFile) {
        Add-Type -AssemblyName System.IO.Compression
        $zipStream = [IO.File]::OpenRead($ZipPath)
        $zip = $null
        try {
            $zip = [IO.Compression.ZipArchive]::new($zipStream, [IO.Compression.ZipArchiveMode]::Read, $false)
            $executable = $null
            foreach ($entry in $zip.Entries) {
                $name = $entry.FullName
                if ($name.Contains('\') -or $name.Contains(':') -or $name.StartsWith('/') -or
                        $name -match '(^|/)\.\.?(/|$)') {
                    throw "Unsafe archive entry: $name."
                }
                if ($name -cne 'kitsu.exe') { continue }
                if ($null -ne $executable) { throw 'The archive must contain kitsu.exe exactly once.' }
                $attributes = [int64]$entry.ExternalAttributes
                $unixType = ($attributes -shr 16) -band 0xF000
                $specialType = $unixType -ne 0 -and $unixType -ne 0x8000
                if ($specialType -or ($attributes -band 0x410) -ne 0 -or
                        $entry.Length -le 0 -or $entry.Length -gt 128MB) {
                    throw 'The archive kitsu.exe must be a regular file of at most 128 MiB.'
                }
                $executable = $entry
            }
            if ($null -eq $executable) { throw 'The archive has no regular root kitsu.exe.' }
            $inputStream = $executable.Open()
            try {
                $output = [IO.File]::Open($OutputFile, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write)
                try {
                    $buffer = New-Object byte[] 65536
                    [long]$written = 0
                    while (($read = $inputStream.Read($buffer, 0, $buffer.Length)) -gt 0) {
                        $written += $read
                        if ($written -gt $executable.Length -or $written -gt 128MB) {
                            throw 'The archive executable exceeds its declared or permitted size.'
                        }
                        $output.Write($buffer, 0, $read)
                    }
                    if ($written -ne $executable.Length) { throw 'The archive executable is truncated.' }
                }
                finally { $output.Dispose() }
            }
            finally { $inputStream.Dispose() }
        }
        finally {
            if ($null -ne $zip) { $zip.Dispose() }
            $zipStream.Dispose()
        }
    }

    function Assert-KitsuVersion([string]$Executable) {
        $start = [Diagnostics.ProcessStartInfo]::new()
        $start.FileName = $Executable
        $start.Arguments = 'version'
        $start.UseShellExecute = $false
        $start.CreateNoWindow = $true
        $start.RedirectStandardOutput = $true
        $start.RedirectStandardError = $true
        $process = [Diagnostics.Process]::new()
        try {
            $process.StartInfo = $start
            [void]$process.Start()
            $stdout = $process.StandardOutput.ReadToEndAsync()
            $stderr = $process.StandardError.ReadToEndAsync()
            if (-not $process.WaitForExit(15000)) {
                $process.Kill()
                $process.WaitForExit()
                throw 'The downloaded kitsu.exe did not finish its version check.'
            }
            $versionText = $stdout.GetAwaiter().GetResult()
            [void]$stderr.GetAwaiter().GetResult()
            if ($process.ExitCode -ne 0 -or $versionText -notmatch ('\Akitsu ' + [regex]::Escape($ReleaseVersion) + '(\s|$)')) {
                throw "The archive executable does not report Kitsu $ReleaseVersion."
            }
        }
        finally { $process.Dispose() }
    }

    function Add-KitsuPath([string]$ExistingPath, [string]$Directory) {
        foreach ($part in ($ExistingPath -split ';')) {
            $expanded = [Environment]::ExpandEnvironmentVariables($part.Trim().Trim('"'))
            if ($expanded.TrimEnd('\', '/') -ieq $Directory.TrimEnd('\', '/')) { return $ExistingPath }
        }
        if ([string]::IsNullOrEmpty($ExistingPath)) { return $Directory }
        return $ExistingPath.TrimEnd(';') + ';' + $Directory
    }

    function Assert-KitsuReleaseTag([string]$Tag) {
        if ($Tag -notmatch '\Av[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?(?:\+[0-9A-Za-z][0-9A-Za-z.-]*)?\z') {
            throw 'Version must be a v-prefixed release tag, such as v0.0.1.'
        }
    }

    if ($PSVersionTable.PSVersion -lt [version]'5.1') { throw 'Kitsu requires PowerShell 5.1 or newer.' }
    # Invoke-Expression applies parameter attributes even for omitted arguments.
    if ($HostSpecified -and $SelectedHost -notin 'codex', 'claude') { throw '-AgentHost must be codex or claude.' }
    $isLocal = -not [string]::IsNullOrEmpty($LocalArchives)
    $remoteHost = $SelectedHost -and -not $isLocal
    $officialLatest = $remoteHost -and -not $ReleaseVersion -and -not $env:KITSU_DOWNLOAD_BASE
    if (-not $isLocal -and -not $SelectedHost -and -not $ReleaseVersion) { $ReleaseVersion = 'v0.0.1' }
    if ($ReleaseVersion) { Assert-KitsuReleaseTag $ReleaseVersion }
    $architecture = Get-KitsuArchitecture
    if ([string]::IsNullOrEmpty($DestinationDir)) {
        if ([string]::IsNullOrEmpty($env:LOCALAPPDATA)) { throw 'LOCALAPPDATA is unavailable; supply -InstallDir.' }
        $DestinationDir = Join-Path $env:LOCALAPPDATA 'kitsu'
    }
    $DestinationDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($DestinationDir)
    if (-not $LeavePath -and $DestinationDir.Contains(';')) {
        throw 'InstallDir cannot contain a semicolon when adding it to PATH; use -NoModifyPath for this directory.'
    }
    $archiveSuffix = "_windows_${architecture}.zip"
    $archiveName = ''
    if ($ReleaseVersion) { $archiveName = "kitsu_${ReleaseVersion}${archiveSuffix}" }
    $work = Join-Path ([IO.Path]::GetTempPath()) ('kitsu-install-' + [guid]::NewGuid().ToString('N'))
    [void][IO.Directory]::CreateDirectory($work)
    $stage = $null
    $client = $null
    $handler = $null
    $previousTls = [Net.ServicePointManager]::SecurityProtocol
    try {
        $manifestFile = Join-Path $work 'SHA256SUMS'
        if ($isLocal) {
            $source = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($LocalArchives)
        }
        else {
            $source = $env:KITSU_DOWNLOAD_BASE
            if ([string]::IsNullOrEmpty($source)) { $source = "https://github.com/belevtsev/kitsu/releases/download/$ReleaseVersion" }
            if ($officialLatest) { $source = 'https://github.com/belevtsev/kitsu/releases/latest/download' }
            $baseUri = [uri]$source
            Assert-KitsuDownloadUri $baseUri
            if ($baseUri.Query) { throw 'KITSU_DOWNLOAD_BASE must be a release directory URL without a query.' }
            Add-Type -AssemblyName System.Net.Http
            [Net.ServicePointManager]::SecurityProtocol = $previousTls -bor [Net.SecurityProtocolType]::Tls12
            $handler = [Net.Http.HttpClientHandler]::new()
            $handler.AllowAutoRedirect = $false
            $client = [Net.Http.HttpClient]::new($handler)
            $client.Timeout = [TimeSpan]::FromMinutes(2)
            $client.MaxResponseContentBufferSize = 128MB
        }
        Save-KitsuAsset $client $source 'SHA256SUMS' $manifestFile
        if ((Get-Item -LiteralPath $manifestFile).Length -gt 1MB) { throw 'SHA256SUMS is too large.' }
        $assetClient = $client
        $pluginDir = $source
        if ($officialLatest) {
            $lines = [IO.File]::ReadAllLines($manifestFile)
            $legacyRow = '[ \t]+\*?' + [regex]::Escape("kitsu_v0.0.1${archiveSuffix}") + '$'
            if (($lines -cmatch $legacyRow) -and -not ($lines -cmatch '[ \t]+\*?kitsu-plugin\.zip$')) {
                if (-not (Get-Command gh -CommandType Application -ErrorAction SilentlyContinue)) {
                    throw 'v0.0.1 predates the plugin. Install GitHub CLI, run gh auth login, and rerun this installer.'
                }
                $repo = 'github.com/belevtsev/kitsu'
                $run = & gh run list -R $repo --workflow ci.yml --branch main --event push --status success --limit 1 --json databaseId --jq '.[0].databaseId'
                if ($LASTEXITCODE -ne 0 -or $run -notmatch '^[1-9][0-9]*$') { throw 'Cannot find a successful main build. Check gh auth status and retry.' }
                Write-Host "Release v0.0.1 predates the plugin. Installing the main bundle from CI run $run."
                $source = Join-Path $work 'bundle'
                & gh run download $run -R $repo -n kitsu-archives -D $source
                if ($LASTEXITCODE -ne 0) { throw 'Cannot download the matching CLI and plugin from the selected main build.' }
                $assetClient = $null
                $pluginDir = $source
                [IO.File]::Delete($manifestFile)
                Save-KitsuAsset $null $source 'SHA256SUMS' $manifestFile
                if ((Get-Item -LiteralPath $manifestFile).Length -gt 1MB) { throw 'SHA256SUMS is too large.' }
            }
        }
        $pluginRows = 0
        $pluginHash = ''
        $matchingRows = 0
        $selectedArchive = ''
        $expected = ''
        foreach ($line in [IO.File]::ReadLines($manifestFile)) {
            $row = [regex]::Match($line, '^(\S+)[ \t]+\*?(.+)$')
            if (-not $row.Success) { continue }
            $name = $row.Groups[2].Value
            if ($name -ceq 'kitsu-plugin.zip') {
                $pluginRows++
                $pluginHash = $row.Groups[1].Value
                continue
            }
            if ($archiveName) {
                if ($name -cne $archiveName) { continue }
            }
            elseif ($name.Length -lt (6 + $archiveSuffix.Length) -or
                    -not $name.StartsWith('kitsu_', [StringComparison]::Ordinal) -or
                    -not $name.EndsWith($archiveSuffix, [StringComparison]::Ordinal)) { continue }
            if ($matchingRows -gt 0 -and $name -cne $selectedArchive) {
                throw "SHA256SUMS lists multiple Kitsu archives for windows/$architecture. Specify -Version TAG."
            }
            $selectedArchive = $name
            $matchingRows++
            $expected = $row.Groups[1].Value
        }
        if (-not $archiveName) {
            if ($matchingRows -eq 0) { throw "SHA256SUMS does not list a Kitsu archive for windows/$architecture." }
            $ReleaseVersion = $selectedArchive.Substring(6, $selectedArchive.Length - 6 - $archiveSuffix.Length)
            Assert-KitsuReleaseTag $ReleaseVersion
            $archiveName = $selectedArchive
        }
        if ($matchingRows -ne 1) { throw "SHA256SUMS must list $archiveName exactly once." }
        if ($expected -notmatch '\A[0-9a-fA-F]{64}\z') { throw "Invalid SHA-256 row for $archiveName." }
        $archiveFile = Join-Path $work $archiveName
        if ($officialLatest -and $null -ne $assetClient) {
            $source = "https://github.com/belevtsev/kitsu/releases/download/$ReleaseVersion"
        }
        if ($remoteHost) {
            if ($pluginRows -ne 1 -or $pluginHash -notmatch '^[0-9a-fA-F]{64}$') {
                throw 'SHA256SUMS must list kitsu-plugin.zip exactly once with a valid checksum.'
            }
            if ($null -ne $assetClient) {
                $pluginDir = $work
                Save-KitsuAsset $assetClient $source 'kitsu-plugin.zip' (Join-Path $pluginDir 'kitsu-plugin.zip')
            }
            if ((Get-KitsuSHA256 (Join-Path $pluginDir 'kitsu-plugin.zip')) -ine $pluginHash) {
                throw 'Checksum mismatch for kitsu-plugin.zip.'
            }
        }
        Save-KitsuAsset $assetClient $source $archiveName $archiveFile
        $actual = Get-KitsuSHA256 $archiveFile
        if ($actual -ine $expected) { throw "The checksum of $archiveName does not match SHA256SUMS." }
        $verifiedExe = Join-Path $work 'kitsu.exe'
        Expand-KitsuExecutable $archiveFile $verifiedExe
        Assert-KitsuVersion $verifiedExe

        $oldUserPath = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
        $oldSessionPath = $env:Path
        [void][IO.Directory]::CreateDirectory($DestinationDir)
        $destination = Join-Path $DestinationDir 'kitsu.exe'
        $oldFile = Get-Item -LiteralPath $destination -Force -ErrorAction SilentlyContinue
        if ($null -ne $oldFile -and ($oldFile.Attributes -band ([IO.FileAttributes]::Directory -bor [IO.FileAttributes]::ReparsePoint)) -ne 0) {
            throw 'The existing kitsu.exe destination must be a regular file.'
        }
        $stage = Join-Path $DestinationDir ('.kitsu-install-' + [guid]::NewGuid().ToString('N'))
        [void][IO.Directory]::CreateDirectory($stage)
        $stagedExe = Join-Path $stage 'kitsu.exe'
        $backup = Join-Path $stage 'previous-kitsu.exe'
        [IO.File]::Copy($verifiedExe, $stagedExe, $false)
        $installed = $false
        $userPathChanged = $false
        try {
            if ($null -ne $oldFile) { [IO.File]::Replace($stagedExe, $destination, $backup) }
            else { [IO.File]::Move($stagedExe, $destination) }
            $installed = $true
            if (-not $LeavePath) {
                $newUserPath = Add-KitsuPath $oldUserPath $DestinationDir
                if ($newUserPath -cne $oldUserPath) {
                    [Environment]::SetEnvironmentVariable('Path', $newUserPath, [EnvironmentVariableTarget]::User)
                    $userPathChanged = $true
                }
                $env:Path = Add-KitsuPath $oldSessionPath $DestinationDir
            }
        }
        catch {
            $failure = $_
            try {
                if ($userPathChanged) { [Environment]::SetEnvironmentVariable('Path', $oldUserPath, [EnvironmentVariableTarget]::User) }
                $env:Path = $oldSessionPath
                if ([IO.File]::Exists($backup)) {
                    if ([IO.File]::Exists($destination)) { [IO.File]::Replace($backup, $destination, $null) }
                    else { [IO.File]::Move($backup, $destination) }
                }
                elseif ($installed -and $null -eq $oldFile) { [IO.File]::Delete($destination) }
            }
            catch {
                $preservedStage = $stage
                $stage = $null
                throw "Installation failed: $($failure.Exception.Message) Recovery files remain in ${preservedStage}: $($_.Exception.Message)"
            }
            throw $failure
        }
        Write-Host "Installed Kitsu $ReleaseVersion for windows/$architecture at $destination"
        if ($LeavePath) { Write-Host 'PATH was left unchanged; invoke the installed executable by its full path.' }
        else { Write-Host 'Added the installation directory to your user PATH and this session PATH.' }
        if ($SelectedHost) {
            $SelectedHost = $SelectedHost.ToLowerInvariant()
            try {
                & $destination agent setup --host $SelectedHost --archive-dir $pluginDir
                if ($LASTEXITCODE -ne 0) { throw "Kitsu exited with code $LASTEXITCODE." }
            }
            catch {
                if ($remoteHost) { throw "$SelectedHost setup failed. After fixing the reported problem, rerun the installer with -AgentHost $SelectedHost. $($_.Exception.Message)" }
                $retryExecutable = $destination.Replace("'", "''")
                $retryArchiveDir = $source.Replace("'", "''")
                [Console]::Error.WriteLine("Retry with this command.`n& '$retryExecutable' agent setup --host $SelectedHost --archive-dir '$retryArchiveDir'")
                throw "$SelectedHost setup failed. $($_.Exception.Message)"
            }
        }
    }
    finally {
        if ($null -ne $client) { $client.Dispose() }
        if ($null -ne $handler) { $handler.Dispose() }
        [Net.ServicePointManager]::SecurityProtocol = $previousTls
        if ($null -ne $stage) { Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue }
        Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
    }
} $Version $InstallDir $NoModifyPath.IsPresent $ArchiveDir $AgentHost ($PSBoundParameters.ContainsKey('AgentHost'))
