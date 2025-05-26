#!/bin/bash
# Build script for Job Runner

# Navigate to the project root
cd $(dirname "$0")/../..

# Install dependencies
echo -e "\033[32mInstalling dependencies...\033[0m"
go mod download

# Build the application
echo -e "\033[32mBuilding Job Runner...\033[0m"
go build -o cmd/job_runner/job_runner ./cmd/job_runner

if [ $? -eq 0 ]; then
    echo -e "\033[32mBuild successful! The executable is: cmd/job_runner/job_runner\033[0m"
    echo -e "\033[33mRun with: ./cmd/job_runner/job_runner --config=cmd/job_runner/config.json\033[0m"
else
    echo -e "\033[31mBuild failed with exit code $?\033[0m"
fi
