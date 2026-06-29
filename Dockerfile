# Stage 1: Build the Go binary
FROM golang:alpine AS builder

WORKDIR /app

# Copy dependency files and download modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Compile the static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o routerd ./cmd/routerd

# Stage 2: Create a minimal, secure runtime image
FROM alpine:latest

# Install CA certificates to support secure HTTPS/SSE connections to MCP servers/APIs
RUN apk --no-cache add ca-certificates

# Run as a non-privileged user for security
RUN adduser -D -u 10001 router-user
USER router-user

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/routerd /app/routerd

# Expose the default sidecar HTTP port
EXPOSE 8090

# Execute daemon with configuration arguments
ENTRYPOINT ["/app/routerd"]
CMD ["-listen", "0.0.0.0:8090", "-mcp", "/etc/routerd/mcp.toml"]
