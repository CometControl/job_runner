# Build script for Job Runner

# Navigate to the project root
cd $PSScriptRoot\..\..

# Install dependencies 
Write-Host "Installing dependencies..." -ForegroundColor Green
go mod download

# Build the application
Write-Host "Building Job Runner..." -ForegroundColor Green
go build -o cmd\job_runner\job_runner.exe .\cmd\job_runner

if ($LASTEXITCODE -eq 0) {
    Write-Host "Build successful! The executable is: cmd\job_runner\job_runner.exe" -ForegroundColor Green
    Write-Host "Run with: .\cmd\job_runner\job_runner.exe --config=cmd\job_runner\config.json" -ForegroundColor Yellow
} else {
    Write-Host "Build failed with exit code $LASTEXITCODE" -ForegroundColor Red
}
