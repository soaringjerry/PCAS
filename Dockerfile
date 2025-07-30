# Production Dockerfile
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the entire project
COPY . .

# Build the binaries
RUN make build

# Final stage - minimal runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -g '' pcas

# Set working directory
WORKDIR /app

# Copy binaries from builder
COPY --from=builder /app/bin/pcas /app/bin/pcas
COPY --from=builder /app/bin/pcasctl /app/bin/pcasctl

# Copy configuration files
COPY policy.yaml .

# Change ownership
RUN chown -R pcas:pcas /app

# Create and set permissions for the data directory
RUN mkdir /data && chown pcas:pcas /data

# Switch to non-root user
USER pcas

# Expose gRPC port
EXPOSE 50051

# Run the server
CMD ["/app/bin/pcas", "serve"]