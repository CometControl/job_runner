FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install gcc for CGO
RUN apk add --no-cache gcc musl-dev

# Copy go.mod file
COPY go.mod ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags="-linkmode external -extldflags -static" -o cmd/job_runner/job_runner ./cmd/job_runner

# Use a smaller image for the final stage
FROM alpine:3.19

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/cmd/job_runner/job_runner .
COPY --from=builder /app/cmd/job_runner/config.json .

# Create a non-root user and set permissions
RUN adduser -D -u 1000 appuser && \
    chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Expose the port
EXPOSE 8080

# Set the entrypoint
ENTRYPOINT ["/app/job_runner"]
CMD ["--config=/app/config.json"]
