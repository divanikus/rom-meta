param(
    [Parameter(Mandatory=$true)][ValidatePattern('^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$')][string]$Version,
    [string]$OutputDirectory,
    [ValidateSet('all', 'rom-meta', 'prepare-variants')][string]$Program = 'all',
    [ValidateSet('windows-amd64', 'linux-amd64', 'linux-arm64')][string[]]$Platform = @('windows-amd64', 'linux-amd64', 'linux-arm64')
)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$releaseVersion = $Version.TrimStart('v')
$Program = $Program.ToLowerInvariant()
if (!$OutputDirectory) {
    $defaultOutput = if ($Program -eq 'prepare-variants') { "bin/releases/prepare-variants/$releaseVersion" } else { "bin/releases/$releaseVersion" }
    $OutputDirectory = Join-Path $repoRoot $defaultOutput
}
$releaseRoot = [IO.Path]::GetFullPath($OutputDirectory)
if (Test-Path -LiteralPath $releaseRoot) { throw "Output already exists: $releaseRoot" }
$previousGOOS, $previousGOARCH, $previousCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
Push-Location $repoRoot
try {
    $dirty = @(git status --porcelain)
    if ($LASTEXITCODE -ne 0 -or $dirty.Count -gt 0) { throw 'Build release bundles from a clean committed working tree.' }
    $revision = (git rev-parse --short=12 HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot identify source revision.' }
    New-Item -ItemType Directory -Path $releaseRoot | Out-Null
    $publicManifest = Get-Content -Raw -LiteralPath (Join-Path $repoRoot 'packaging/public-files.json') | ConvertFrom-Json
    if ($publicManifest.schema_version -ne 1) { throw 'Unknown public file manifest.' }
    $trackedDocs = @($publicManifest.files | Where-Object { $_ -in @('README.md', 'LICENSE', 'CONTRIBUTING.md', 'AGENTS.md', 'go.mod', 'go.sum') -or $_.StartsWith('docs/') -or $_.StartsWith('examples/') })
    $trackedDocs += @(git ls-files 'examples/romm-importer/*')
    if ($LASTEXITCODE -ne 0) { throw 'Cannot enumerate public examples.' }
    $trackedDocs = @($trackedDocs | Sort-Object -Unique)
    $checksums = @()
    $targets = @($Platform | Select-Object -Unique | ForEach-Object {
        $parts = $_.ToLowerInvariant() -split '-', 2
        @{ OS = $parts[0]; Arch = $parts[1] }
    })
    $programs = if ($Program -eq 'all') { @('rom-meta', 'prepare-variants') } else { @($Program) }
    foreach ($currentProgram in $programs) {
        $entryPoint = if ($currentProgram -eq 'prepare-variants') { './scripts/prepare-variants' } else { './cmd/rom-meta' }
        $packageStem = if ($currentProgram -eq 'prepare-variants') { 'prepare-variants' } else { 'rommeta' }
        foreach ($target in $targets) {
            $targetOS, $targetArch = $target.OS, $target.Arch
            $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $targetOS, $targetArch, '0'
            $moduleRows = @(go list -deps -f '{{with .Module}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}' $entryPoint | Where-Object { $_ } | Sort-Object -Unique)
            if ($LASTEXITCODE -ne 0) { throw 'Cannot resolve dependency licenses.' }
            $packageName = "$packageStem-$releaseVersion-$targetOS-$targetArch"
            $packageRoot = Join-Path $releaseRoot $packageName
            New-Item -ItemType Directory -Path $packageRoot | Out-Null
            foreach ($relative in $trackedDocs) {
                $destination = Join-Path $packageRoot $relative
                New-Item -ItemType Directory -Force -Path (Split-Path -Parent $destination) | Out-Null
                Copy-Item -LiteralPath (Join-Path $repoRoot $relative) -Destination $destination
            }
            $licenseRoot = Join-Path $packageRoot 'third-party-licenses'
            New-Item -ItemType Directory -Path $licenseRoot | Out-Null
            $notices = @('Dependency licenses for this build:', '')
            $toolchainRoot = (go env GOROOT).Trim()
            if ($LASTEXITCODE -ne 0) { throw 'Cannot locate Go runtime license.' }
            $goNotices = Join-Path $licenseRoot 'go-runtime'
            New-Item -ItemType Directory -Path $goNotices | Out-Null
            Copy-Item -LiteralPath (Join-Path $toolchainRoot 'LICENSE') -Destination $goNotices
            if (Test-Path -LiteralPath (Join-Path $toolchainRoot 'PATENTS')) { Copy-Item -LiteralPath (Join-Path $toolchainRoot 'PATENTS') -Destination $goNotices }
            $notices += 'Go runtime - third-party-licenses/go-runtime/'
            if ($currentProgram -eq 'rom-meta') {
                $swaggerNotices = Join-Path $licenseRoot 'swagger-ui-dist'
                New-Item -ItemType Directory -Path $swaggerNotices | Out-Null
                foreach ($notice in @('LICENSE', 'NOTICE', 'UPSTREAM.txt', 'swagger-ui-bundle.js.LICENSE.txt')) { Copy-Item -LiteralPath (Join-Path $repoRoot "internal/httpdocs/static/$notice") -Destination $swaggerNotices }
                $notices += 'Swagger UI - third-party-licenses/swagger-ui-dist/'
            }
            foreach ($row in $moduleRows) {
                $parts = $row -split '\|', 3
                if ($parts[0] -eq 'rommeta') { continue }
                if ($parts.Count -ne 3 -or !$parts[2]) { throw "Missing module source: $($parts[0])" }
                $moduleLicenses = @(Get-ChildItem -LiteralPath $parts[2] -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE)(\.|$|-)' })
                if (!$moduleLicenses.Count) { throw "Review dependency license before distribution: $($parts[0])" }
                $moduleFolder = ($parts[0] + '@' + $parts[1]) -replace '/', '_'
                $destination = Join-Path $licenseRoot $moduleFolder
                New-Item -ItemType Directory -Path $destination | Out-Null
                foreach ($license in $moduleLicenses) { Copy-Item -LiteralPath $license.FullName -Destination $destination }
                $notices += "$($parts[0]) $($parts[1]) - third-party-licenses/$moduleFolder/"
            }
            $notices | Set-Content -LiteralPath (Join-Path $packageRoot 'THIRD-PARTY-NOTICES.txt') -Encoding utf8
            if ($currentProgram -eq 'prepare-variants') {
                $run = if ($targetOS -eq 'windows') { '.\prepare-variants.exe' } else { './prepare-variants' }
                $buildNotes = @(
                    "RomMeta variant preparation helper $releaseVersion",
                    "Source revision: $revision",
                    "Platform: $targetOS/$targetArch",
                    '',
                    'Standalone console program. Go and the source checkout are not required to run it.',
                    'Windows: open PowerShell in this extracted folder.',
                    'Linux: run chmod +x ./prepare-variants after extracting.',
                    "Run $run --help",
                    '',
                    'Download/setup instructions and the full workflow: docs/prepare-variants.md',
                    "Example: $run --catalog /catalog-snapshot --sources /snapshots --discover /work/review",
                    "Review: $run --review /work/review",
                    "Preview: $run --workspace /work/review --out /work/prepared",
                    'Repeat the preview command with --apply to create a separate staging catalog.'
                )
                $buildNotes | Set-Content -LiteralPath (Join-Path $packageRoot 'README.txt') -Encoding utf8
            } else {
                $buildNotes = @("RomMeta $releaseVersion", "Source revision: $revision", "Platform: $targetOS/$targetArch", 'Linux: after extracting ZIP, run chmod +x ./rom-meta.', 'Run ./rom-meta --version (Windows: .\rom-meta.exe --version).', 'See README.md and docs/README.md for guides and release notes.')
            }
            $buildNotes | Set-Content -LiteralPath (Join-Path $packageRoot 'BUILD.txt') -Encoding utf8
            $binaryName = if ($targetOS -eq 'windows') { "$currentProgram.exe" } else { $currentProgram }
            $linkFlags = '-s -w'
            if ($currentProgram -eq 'rom-meta') { $linkFlags += " -X rommeta/internal/appinfo.Version=$releaseVersion+$revision" }
            & go build -trimpath -ldflags $linkFlags -o (Join-Path $packageRoot $binaryName) $entryPoint
            if ($LASTEXITCODE -ne 0) { throw "Build failed for $targetOS/$targetArch" }
            $archive = Join-Path $releaseRoot "$packageName.zip"
            Compress-Archive -LiteralPath $packageRoot -DestinationPath $archive
            $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant()
            $checksums += "$hash  $packageName.zip"
            Write-Output $archive
        }
    }
    $checksums | Set-Content -LiteralPath (Join-Path $releaseRoot 'SHA256SUMS') -Encoding ascii
    Write-Output (Join-Path $releaseRoot 'SHA256SUMS')
} finally {
    $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $previousGOOS, $previousGOARCH, $previousCGO
    Pop-Location
}
