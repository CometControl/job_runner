#!/bin/bash
# Run Job Runner

# Default values
CONFIG_FILE="config.json"
HTTP_ADDR=""
HTTP_PORT=0

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  key="$1"
  case $key in
    --config)
      CONFIG_FILE="$2"
      shift
      shift
      ;;
    --http.addr)
      HTTP_ADDR="$2"
      shift
      shift
      ;;
    --http.port)
      HTTP_PORT="$2"
      shift
      shift
      ;;
    *)
      shift
      ;;
  esac
done

# Build the arguments string
ARGS="--config=${CONFIG_FILE}"
if [ -n "$HTTP_ADDR" ]; then
  ARGS="$ARGS --http.addr=${HTTP_ADDR}"
fi
if [ "$HTTP_PORT" -ne 0 ]; then
  ARGS="$ARGS --http.port=${HTTP_PORT}"
fi

echo -e "\033[32mStarting Job Runner...\033[0m"
./job_runner $ARGS &

echo -e "\033[36mServer is running at http://localhost:8080\033[0m"
echo -e "\033[33mPress Ctrl+C to stop the server\033[0m"

# Wait for Ctrl+C
trap "kill $!; exit" INT
wait
