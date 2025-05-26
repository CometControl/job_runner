# Run Job Runner

# Parse command line arguments
param (
    [string]$ConfigFile = "config.json",
    [string]$HttpAddr = "",
    [int]$HttpPort = 0
)

# Build the arguments string
$arguments = "--config=$ConfigFile"
if ($HttpAddr -ne "") {
    $arguments += " --http.addr=$HttpAddr"
}
if ($HttpPort -ne 0) {
    $arguments += " --http.port=$HttpPort"
}

Write-Host "Starting Job Runner..." -ForegroundColor Green
Start-Process -NoNewWindow -FilePath ".\job_runner.exe" -ArgumentList $arguments

Write-Host "Server is running at http://localhost:8080" -ForegroundColor Cyan
Write-Host "Press Ctrl+C to stop the server" -ForegroundColor Yellow
