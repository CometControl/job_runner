#!/bin/bash
# Test script for Job Runner

# Navigate to the project root
cd $(dirname "$0")/../..

# Install dependencies
echo -e "\033[32mInstalling dependencies...\033[0m"
go mod download

# Run the tests
echo -e "\033[32mRunning tests...\033[0m"
go test -v ./...

# If you want to run a specific test
# go test -v ./db -run TestSQLiteConnection
