# pvman installer for Windows.
#
# Downloads the prebuilt release archive rather than compiling from source, so
# no Go toolchain is needed and nothing else on the machine is touched.
#
# The binary goes to %LOCALAPPDATA%\pvman\bin -- deliberately *outside* the Go
# bin directory. internal/update tells its two install channels apart by looking
# at the directory the running binary lives in, and a binary here must take the
# release-binary path so `pvman --update` never reaches for a Go toolchain that
# this installer did not install.
#
# Environment variables:
#   PV_MAN_VERSION      install this tag instead of the latest one (e.g. 0.7.0)
#   PV_MAN_INSTALL_DIR  install somewhere else; also skips the PATH edit
$ErrorActionPreference = "Stop"

Add-Type -AssemblyName System.Net.Http -ErrorAction SilentlyContinue
Add-Type -AssemblyName System.IO.Compression.FileSystem -ErrorAction SilentlyContinue

$Repo = "tkzzzzzz6/pvman"
$Releases = "https://github.com/$Repo/releases"
$InstallDir = if ($env:PV_MAN_INSTALL_DIR) { $env:PV_MAN_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "pvman\bin" }
$Bin = Join-Path $InstallDir "pvman.exe"
$Legacy = Join-Path $env:USERPROFILE "go\bin\pvman.exe"
$Total = 3

function Write-Step($Number, $Text) {
    Write-Host ""
    Write-Host "[$Number/$Total] $Text"
}

function Write-Info($Text) {
    Write-Host "      $Text" -ForegroundColor DarkGray
}

function Write-Note($Text) {
    Write-Warning "pvman: $Text"
}

function Fail($Text) {
    throw "pvman: $Text"
}

# Get-LatestTag asks GitHub which release is newest by reading the redirect that
# /releases/latest answers with. This deliberately avoids the REST API: that
# endpoint is rate-limited to 60 requests an hour for anonymous callers, and an
# installer that fails on a rate limit fails for no good reason.
function Get-LatestTag {
    $handler = [System.Net.Http.HttpClientHandler]::new()
    $handler.AllowAutoRedirect = $false
    $client = [System.Net.Http.HttpClient]::new($handler)
    try {
        $response = $client.GetAsync("$Releases/latest").GetAwaiter().GetResult()
        if (-not $response.Headers.Location) { return $null }
        $location = $response.Headers.Location.ToString()
        $index = $location.LastIndexOf("/tag/")
        if ($index -lt 0) { return $null }
        return $location.Substring($index + 5)
    }
    finally {
        $client.Dispose()
    }
}

# Get-ReleaseFile streams a URL to disk. It does the streaming by hand rather
# than calling Invoke-WebRequest because that cmdlet renders its progress bar
# through the very same channel Write-Progress uses, and on PowerShell 5.1 that
# rendering dominates the download time for large files. Pass -Quiet for small
# metadata files, where a progress bar is more output than the file has bytes.
function Get-ReleaseFile {
    param([string]$Url, [string]$Destination, [string]$Label, [switch]$Quiet)

    $handler = [System.Net.Http.HttpClientHandler]::new()
    $client = [System.Net.Http.HttpClient]::new($handler)
    $client.Timeout = [TimeSpan]::FromMinutes(10)
    try {
        $response = $client.GetAsync($Url, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead).GetAwaiter().GetResult()
        $response.EnsureSuccessStatusCode() | Out-Null

        $total = $response.Content.Headers.ContentLength
        $source = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
        $target = [System.IO.File]::Create($Destination)
        try {
            $buffer = New-Object byte[] 81920
            $done = 0L
            $lastPercent = -1
            while (($read = $source.Read($buffer, 0, $buffer.Length)) -gt 0) {
                $target.Write($buffer, 0, $read)
                $done += $read
                if ($total -gt 0 -and -not $Quiet) {
                    $percent = [int](100 * $done / $total)
                    if ($percent -ne $lastPercent) {
                        $status = "{0}% ({1:N1} of {2:N1} MB)" -f $percent, ($done / 1MB), ($total / 1MB)
                        Write-Progress -Activity $Label -Status $status -PercentComplete $percent
                        $lastPercent = $percent
                    }
                }
            }
        }
        finally {
            $target.Dispose()
            $source.Dispose()
        }
    }
    finally {
        $client.Dispose()
        if (-not $Quiet) { Write-Progress -Activity $Label -Completed }
    }
}

# Get-InstalledVersion reports the version of an existing pvman without ever
# running it. Asking a binary for its own version is not safe here: --version
# only exists from v0.6.2 on, and older builds ignore their arguments and
# launch the full-screen TUI, which would seize the terminal or hang an
# unattended install. `go version -m` only reads the file.
function Get-InstalledVersion {
    param([string]$Path)

    if (-not (Test-Path -LiteralPath $Path)) { return $null }
    $go = Get-Command go -ErrorAction SilentlyContinue
    if (-not $go) { return $null }

    $version = $null
    $buildInfo = @(& $go.Source version -m $Path 2>$null)
    foreach ($line in $buildInfo) {
        $fields = @($line -split "\s+" | Where-Object { $_ })
        # The mod line carries the full module path ("github.com/owner/repo"),
        # not the bare owner/repo that $Repo holds, so compare on the suffix.
        if ($fields.Count -ge 3 -and $fields[0] -eq "mod" -and ($fields[1] -eq $Repo -or $fields[1] -like "*/$Repo")) {
            $version = $fields[2]
            break
        }
    }

    if (-not $version -or $version -eq "(devel)") {
        # Fallback for builds that were not made with -trimpath. Official
        # releases are built with it, and the toolchain then omits the -ldflags
        # build setting entirely, so main.version is simply not recorded in
        # them -- this branch is for local builds only.
        foreach ($line in $buildInfo) {
            if ($line -match "main\.version=([0-9][^`" ]*)") {
                $version = $Matches[1]
                break
            }
        }
    }

    if (-not $version -or $version -eq "(devel)") { return $null }
    return $version.TrimStart("v")
}

switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
    "X64" { $GoArch = "amd64" }
    "Arm64" { $GoArch = "arm64" }
    default { Fail "unsupported Windows architecture: $_" }
}

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("pvman-install-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

try {
    $previous = Get-InstalledVersion $Bin

    Write-Step 1 "Resolving the version to install"
    if ($env:PV_MAN_VERSION) {
        $tag = "v" + ($env:PV_MAN_VERSION -replace '^v', '')
        Write-Info "pinned by PV_MAN_VERSION: $tag"
    }
    else {
        $tag = Get-LatestTag
        if (-not $tag) { Fail "could not determine the latest release; pin one with PV_MAN_VERSION=0.7.0" }
        Write-Info "latest release: $tag"
    }
    $version = $tag.TrimStart("v")
    $archive = "pvman_${version}_windows_${GoArch}.zip"

    Write-Step 2 "Downloading pvman $version for windows/$GoArch"
    $archivePath = Join-Path $tempDir $archive
    Get-ReleaseFile "$Releases/download/$tag/$archive" $archivePath "Downloading $archive"
    $sumsPath = Join-Path $tempDir "checksums.txt"
    Get-ReleaseFile "$Releases/download/$tag/checksums.txt" $sumsPath "Downloading checksums.txt" -Quiet

    $want = $null
    foreach ($line in Get-Content $sumsPath) {
        $fields = @($line -split "\s+" | Where-Object { $_ })
        if ($fields.Count -eq 2 -and $fields[1] -eq $archive) {
            $want = $fields[0]
            break
        }
    }
    if (-not $want) { Fail "checksums.txt has no entry for $archive" }
    $got = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash
    if ($got -ne $want.ToUpperInvariant()) { Fail "checksum mismatch for $archive" }
    Write-Info "sha256 verified"

    Write-Step 3 "Installing to $InstallDir"
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $staged = Join-Path $tempDir "pvman.exe"
    $zip = [System.IO.Compression.ZipFile]::OpenRead($archivePath)
    try {
        $entry = $zip.Entries | Where-Object { $_.Name -eq "pvman.exe" } | Select-Object -First 1
        if (-not $entry) { Fail "$archive did not contain a pvman.exe binary" }
        [System.IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $staged, $true)
    }
    finally {
        $zip.Dispose()
    }
    Move-Item -LiteralPath $staged -Destination $Bin -Force
    Write-Info "wrote $Bin"

    if (-not $env:PV_MAN_INSTALL_DIR) {
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        $entries = @($userPath -split ";" | Where-Object { $_ })
        if ($entries -notcontains $InstallDir) {
            $entries += $InstallDir
            [Environment]::SetEnvironmentVariable("Path", ($entries -join ";"), "User")
            Write-Info "added $InstallDir to the user PATH"
        }
    }

    if ($Legacy -ne $Bin -and (Test-Path -LiteralPath $Legacy)) {
        Write-Note "an older pvman is still installed at $Legacy"
        if (@([Environment]::GetEnvironmentVariable("Path", "User") -split ";" | Where-Object { $_ }) -contains (Split-Path -Parent $Legacy)) {
            Write-Info "your user PATH lists that directory before $InstallDir, so a"
            Write-Info "bare 'pvman' still runs the old one."
        }
        Write-Info "remove it with: Remove-Item -Force '$Legacy'"
    }

    # Older installers also pulled down a Go toolchain and a go1.26.1 wrapper.
    # pvman does not use either any more, but the wording stops short of calling
    # them junk: on many machines go1.26.1.cmd is the official golang.org/dl
    # wrapper, which other projects may still be relying on.
    $oldToolchain = Join-Path $env:LOCALAPPDATA "pvman\go1.26.1"
    if (Test-Path -LiteralPath $oldToolchain) {
        Write-Note "pvman no longer needs a Go toolchain"
        Write-Info "safe to remove if nothing else uses it:"
        Write-Info "Remove-Item -Recurse -Force '$oldToolchain'"
    }
    $oldWrapper = Join-Path $env:USERPROFILE "go\bin\go1.26.1.cmd"
    if (Test-Path -LiteralPath $oldWrapper) {
        Write-Note "pvman no longer needs the go1.26.1 wrapper"
        Write-Info "safe to remove if nothing else uses it:"
        Write-Info "Remove-Item -Force '$oldWrapper'"
    }

    Write-Host ""
    if ($previous -and $previous -ne $version) {
        Write-Host "pvman $previous -> $version installed at $Bin"
    }
    else {
        Write-Host "pvman $version installed at $Bin"
    }
    if (-not $env:PV_MAN_INSTALL_DIR) {
        Write-Host "Open a new terminal, then run: pvman"
    }
}
finally {
    if (Test-Path -LiteralPath $tempDir) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force
    }
}
