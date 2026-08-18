#Requires -Version 5.1
$ErrorActionPreference = 'Stop'

$Repository = 'gustmrg/timesheet-cli'
$Binary = 'timesheet.exe'
$InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\timesheet'
$ApiRoot = "https://api.github.com/repos/$Repository"

switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { $Arch = 'amd64' }
    'ARM64' { $Arch = 'arm64' }
    default { Write-Error "unsupported architecture: $env:PROCESSOR_ARCHITECTURE (supported: amd64, arm64)"; exit 1 }
}

$TmpDir = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $TmpDir | Out-Null
try {
    $Release = Invoke-RestMethod -Uri "$ApiRoot/releases/latest" -Headers @{ 'User-Agent' = 'timesheet-cli-installer' }
    $Version = $Release.tag_name -replace '^v', ''
    if (-not $Version) { throw 'could not determine the latest release version' }

    $Archive = "timesheet-cli_${Version}_windows_${Arch}.zip"
    Write-Host "Downloading timesheet-cli v$Version for windows/$Arch..."

    function Download-Asset($Name, $Destination) {
        $Asset = $Release.assets | Where-Object { $_.name -eq $Name } | Select-Object -First 1
        if (-not $Asset) { throw "release asset was not found: $Name" }
        Invoke-WebRequest -Uri $Asset.browser_download_url -OutFile $Destination
    }

    Download-Asset $Archive (Join-Path $TmpDir $Archive)
    $ChecksumsFile = Join-Path $TmpDir 'checksums.txt'
    Download-Asset 'checksums.txt' $ChecksumsFile

    $Expected = Get-Content $ChecksumsFile |
        Where-Object { $_ -match "\s$([regex]::Escape($Archive))$" } |
        ForEach-Object { ($_ -split '\s+')[0] } |
        Select-Object -First 1
    if (-not $Expected) { throw 'archive checksum was not found' }

    $Actual = (Get-FileHash -Algorithm SHA256 (Join-Path $TmpDir $Archive)).Hash.ToLowerInvariant()
    if ($Actual -ne $Expected.ToLowerInvariant()) { throw 'checksum verification failed' }

    Expand-Archive -Path (Join-Path $TmpDir $Archive) -DestinationPath $TmpDir -Force
    $Extracted = Join-Path $TmpDir $Binary
    if (-not (Test-Path $Extracted)) { throw "release archive did not contain $Binary" }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item $Extracted (Join-Path $InstallDir $Binary) -Force

    $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($UserPath -split ';') -notcontains $InstallDir) {
        [Environment]::SetEnvironmentVariable('Path', "$UserPath;$InstallDir", 'User')
        Write-Host "Added $InstallDir to your user PATH (restart your terminal to pick it up)."
    }

    Write-Host "Installed timesheet v$Version to $(Join-Path $InstallDir $Binary)"

    $SkillUrl = "https://raw.githubusercontent.com/$Repository/v$Version/skills/timesheet-cli/SKILL.md"

    try {
        $AgentsAnswer = Read-Host 'Install the timesheet-cli agent skill globally to ~/.agents/skills? [y/N]'
    }
    catch {
        $AgentsAnswer = ''
    }
    if ($AgentsAnswer -match '^(y|yes)$') {
        $Dest = Join-Path $HOME '.agents\skills\timesheet-cli'
        try {
            New-Item -ItemType Directory -Path $Dest -Force | Out-Null
            Invoke-WebRequest -Uri $SkillUrl -OutFile (Join-Path $Dest 'SKILL.md')
            Write-Host "Installed skill to $(Join-Path $Dest 'SKILL.md')"
        }
        catch {
            Write-Warning "could not install skill to $Dest"
        }
    }

    try {
        $ClaudeAnswer = Read-Host 'Install the timesheet-cli agent skill for Claude to ~/.claude/skills? [y/N]'
    }
    catch {
        $ClaudeAnswer = ''
    }
    if ($ClaudeAnswer -match '^(y|yes)$') {
        $Dest = Join-Path $HOME '.claude\skills\timesheet-cli'
        try {
            New-Item -ItemType Directory -Path $Dest -Force | Out-Null
            Invoke-WebRequest -Uri $SkillUrl -OutFile (Join-Path $Dest 'SKILL.md')
            Write-Host "Installed skill to $(Join-Path $Dest 'SKILL.md')"
        }
        catch {
            Write-Warning "could not install skill to $Dest"
        }
    }
}
finally {
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}
