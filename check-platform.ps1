# check-platform.ps1
# Quick status check of every service, docker infra, and the kind cluster —
# without starting or restarting anything. Safe to run anytime.

$services = [ordered]@{
    8080 = "gateway"; 8081 = "auth"; 8082 = "tenant"; 8083 = "project"
    8084 = "audit"; 8085 = "cluster"; 8086 = "deployment"; 8087 = "secrets"
    8088 = "domain"; 8089 = "notification"; 8090 = "provisioning"
    8091 = "build"; 8092 = "observability"
}

Write-Host "=== Backend services ===" -ForegroundColor Cyan
foreach ($port in $services.Keys) {
    $name = $services[$port]
    try {
        $r = Invoke-WebRequest -Uri "http://localhost:$port/healthz" -UseBasicParsing -TimeoutSec 3
        Write-Host ("{0,-15} :{1,-5} {2}" -f $name, $port, $r.StatusCode) -ForegroundColor Green
    } catch {
        Write-Host ("{0,-15} :{1,-5} DOWN" -f $name, $port) -ForegroundColor Red
    }
}

Write-Host "`n=== Docker infra ===" -ForegroundColor Cyan
docker ps --filter "name=bdsplatform" --format "table {{.Names}}\t{{.Status}}"

Write-Host "`n=== kind cluster (test-cluster-4) ===" -ForegroundColor Cyan
try {
    kubectl --context kind-bds -n platform-agent get pods 2>$null
} catch {
    Write-Host "kind cluster unreachable" -ForegroundColor Red
}

Write-Host "`n=== Current lease holder ===" -ForegroundColor Cyan
kubectl --context kind-bds -n platform-agent get lease platform-agent-leader -o jsonpath='{.spec.holderIdentity}' 2>$null
Write-Host ""