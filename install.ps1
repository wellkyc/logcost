$ErrorActionPreference = 'Stop'

$repo = 'wellkyc/logcost'
$dir = if ($env:LOGCOST_INSTALL_DIR) { $env:LOGCOST_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'logcost\bin' }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'x86_64' }
$archive = "logcost-windows-$arch.zip"
$base = "https://github.com/$repo/releases/latest/download"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ('logcost-' + [guid]::NewGuid())

[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
New-Item -ItemType Directory -Force $tmp | Out-Null
try {
    Write-Host "Downloading $base/$archive"
    $zip = Join-Path $tmp $archive
    $sums = Join-Path $tmp 'SHA256SUMS'
    Invoke-WebRequest "$base/$archive" -OutFile $zip -UseBasicParsing
    Invoke-WebRequest "$base/SHA256SUMS" -OutFile $sums -UseBasicParsing

    $expected = Get-Content $sums | ForEach-Object {
        $parts = $_ -split '\s+'
        if ($parts.Count -ge 2 -and $parts[1] -eq $archive) { $parts[0] }
    } | Select-Object -First 1
    $actual = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
    if (-not $expected -or $expected.ToLower() -ne $actual) {
        throw "checksum mismatch for $archive, not installing"
    }
    Write-Host 'Checksum OK'

    Expand-Archive $zip $tmp -Force
    New-Item -ItemType Directory -Force $dir | Out-Null
    Copy-Item (Join-Path $tmp 'logcost.exe') $dir -Force
}
finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
Write-Host "Installed logcost to $dir\logcost.exe"

if (-not $env:LOGCOST_NO_MODIFY_PATH) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $userPath) { $userPath = '' }
    if (($userPath -split ';') -notcontains $dir) {
        $newPath = (($userPath.TrimEnd(';'), $dir) | Where-Object { $_ }) -join ';'
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Write-Host "Added $dir to your PATH. Open a new terminal and run: logcost"
    }
}
