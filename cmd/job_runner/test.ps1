# Test script for Job Runner

# Navigate to the project root
cd $PSScriptRoot\..\..

# Install dependencies 
Write-Host "Installing dependencies..." -ForegroundColor Green
go mod download

# Run the tests
Write-Host "Running tests..." -ForegroundColor Green
# $env:CGO_ENABLED=1 # Reverted this change for now
go test .\... -v

# If you want to run a specific test
# go test -v ./db -run TestSQLiteConnection
