$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
New-Item -ItemType Directory -Force data | Out-Null

$go = Get-Command go -ErrorAction SilentlyContinue
$goExe = if ($go) { $go.Source } else { 'C:\Program Files\Go\bin\go.exe' }

"$(Get-Date -Format s) building dashboard" | Out-File -Append -Encoding utf8 data\startup.log
& $goExe build -o dashboard.exe ./cmd/dashboard 2>&1 | Out-File -Append -Encoding utf8 data\startup.log
if ($LASTEXITCODE -ne 0) {
    "$(Get-Date -Format s) build failed, starting the previous build" | Out-File -Append -Encoding utf8 data\startup.log
}
if (-not (Test-Path dashboard.exe)) {
    exit 1
}

& .\dashboard.exe @args
exit $LASTEXITCODE
