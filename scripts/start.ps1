$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw "Install Docker Desktop and start it first." }
& (Join-Path $PSScriptRoot "setup.ps1")
docker compose up --build -d --wait
if ($LASTEXITCODE -ne 0) { throw "Startup failed. Run: docker compose logs --tail=100" }
$port = (Get-Content .env | Where-Object { $_ -match '^APP_PORT=' }) -replace '^APP_PORT=', ''
if (-not $port) { $port = '8080' }
Write-Host "SeatBook is ready: http://localhost:$port"
