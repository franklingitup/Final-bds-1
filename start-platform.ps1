# start-platform.ps1
# Starts docker-compose infra (if not already up), every backend service, and the
# frontend - each in its own separate, independent PowerShell window.
#
# Usage: powershell -File C:\Users\davit\.cursor\bdsplatform\start-platform.ps1

$repo = "C:\Users\davit\.cursor\bdsplatform"

# Plain array of objects - deliberately NOT a hashtable/[ordered]@{}, since
# PowerShell's OrderedDictionary indexer has repeatedly caused subtle bugs with
# integer keys (ambiguous positional-vs-key lookup). Arrays of objects have no
# such ambiguity.
$services = @(
    [PSCustomObject]@{ Port = 8081; Name = "auth" }
    [PSCustomObject]@{ Port = 8082; Name = "tenant" }
    [PSCustomObject]@{ Port = 8083; Name = "project" }
    [PSCustomObject]@{ Port = 8084; Name = "audit" }
    [PSCustomObject]@{ Port = 8085; Name = "cluster" }
    [PSCustomObject]@{ Port = 8086; Name = "deployment" }
    [PSCustomObject]@{ Port = 8087; Name = "secrets" }
    [PSCustomObject]@{ Port = 8088; Name = "domain" }
    [PSCustomObject]@{ Port = 8089; Name = "notification" }
    [PSCustomObject]@{ Port = 8090; Name = "provisioning" }
    [PSCustomObject]@{ Port = 8091; Name = "build" }
    [PSCustomObject]@{ Port = 8092; Name = "observability" }
)
$gateway = [PSCustomObject]@{ Port = 8080; Name = "gateway" }

Write-Host "=== BDS Platform: starting everything ===" -ForegroundColor Cyan

# --- 1. Infra ---
Write-Host ""
Write-Host "[1/3] Checking docker-compose infra..." -ForegroundColor Yellow
Push-Location $repo
$running = docker compose ps --status running -q 2>$null
if (-not $running) {
    Write-Host "Infra not running - starting it now..."
    docker compose up -d
    Start-Sleep -Seconds 5
} else {
    Write-Host "Infra already running."
}
Pop-Location

function Build-ServiceCommand {
    param([string]$repo, [string]$svc, [int]$port)
    $lines = @(
        "`$host.ui.RawUI.WindowTitle = 'bds: $svc (:$port)'"
        "cd '$repo\backend'"
        "Get-Content '$repo\.env' | ForEach-Object { if (`$_ -match '^\s*([^#][^=]*)=(.*)`$') { [System.Environment]::SetEnvironmentVariable(`$matches[1].Trim(), `$matches[2].Trim()) } }"
        "`$env:HTTP_ADDR = ':$port'"
        "go run ./services/$svc/cmd/server"
    )
    return ($lines -join "; ")
}

# --- 2. Backend services ---
Write-Host ""
Write-Host "[2/3] Launching backend services in separate windows..." -ForegroundColor Yellow
foreach ($s in $services) {
    $cmdString = Build-ServiceCommand -repo $repo -svc $s.Name -port $s.Port
    Start-Process powershell -ArgumentList "-NoExit", "-Command", $cmdString
    Start-Sleep -Milliseconds 300
}

Write-Host "Launching gateway (:8080) last..."
$gatewayCmd = Build-ServiceCommand -repo $repo -svc $gateway.Name -port $gateway.Port
Start-Process powershell -ArgumentList "-NoExit", "-Command", $gatewayCmd

# --- 3. Frontend ---
Write-Host ""
Write-Host "[3/3] Launching frontend..." -ForegroundColor Yellow
$frontendLines = @(
    "`$host.ui.RawUI.WindowTitle = 'bds: frontend (:3000)'"
    "cd '$repo\frontend'"
    "npm run dev"
)
Start-Process powershell -ArgumentList "-NoExit", "-Command", ($frontendLines -join "; ")

Write-Host ""
Write-Host "All windows launched. Waiting 25s before checking health..." -ForegroundColor Cyan
Start-Sleep -Seconds 25

# --- 4. Health check - plain array again, no dictionary lookups at all ---
Write-Host ""
Write-Host "=== Health check ===" -ForegroundColor Cyan
$allServices = @($gateway) + $services
$failed = @()
foreach ($s in ($allServices | Sort-Object Port)) {
    try {
        $r = Invoke-WebRequest -Uri "http://localhost:$($s.Port)/healthz" -UseBasicParsing -TimeoutSec 5
        Write-Host ("{0,-15} :{1,-5} {2}" -f $s.Name, $s.Port, $r.StatusCode) -ForegroundColor Green
    } catch {
        Write-Host ("{0,-15} :{1,-5} DOWN" -f $s.Name, $s.Port) -ForegroundColor Red
        $failed += $s.Name
    }
}

Write-Host ""
if ($failed.Count -gt 0) {
    Write-Host ("NOT healthy yet: " + ($failed -join ", ")) -ForegroundColor Red
    Write-Host "Check each service's own window for compile errors, or re-run check-platform.ps1 in a minute."
} else {
    Write-Host "All 13 services healthy." -ForegroundColor Green
}

Write-Host ""
Write-Host "Frontend: http://localhost:3000"