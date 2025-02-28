FROM golang:1.21.6-alpine3.19 AS builder

# Install build dependencies
RUN apk add --no-cache git build-base

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN go build -o inferoute-client ./cmd

# Use a smaller base image for the final image
FROM alpine:3.19.1

# Install required packages
RUN apk add --no-cache ca-certificates curl jq nginx supervisor bash

# Install NGROK
RUN curl -Lo /tmp/ngrok.zip https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-linux-amd64.zip && \
    unzip -o /tmp/ngrok.zip -d /usr/local/bin && \
    rm -f /tmp/ngrok.zip && \
    chmod +x /usr/local/bin/ngrok

# Set working directory
WORKDIR /app

# Copy the compiled binary from the builder stage
COPY --from=builder /app/inferoute-client /app/inferoute-client

# Copy config.yaml.example
COPY config.yaml.example /app/config.yaml.example

# Create directories for NGINX
RUN mkdir -p /run/nginx

# Create docker directory and copy configuration files
RUN mkdir -p /app/docker
COPY docker/nginx.conf /etc/nginx/http.d/default.conf
COPY docker/supervisord.conf /etc/supervisord.conf
COPY docker/start.sh /app/start.sh
COPY docker/ngrok_start.sh /app/docker/ngrok_start.sh
COPY docker/inferoute_start.sh /app/docker/inferoute_start.sh

# Make scripts executable
RUN chmod +x /app/start.sh /app/docker/ngrok_start.sh /app/docker/inferoute_start.sh

# Create a non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
RUN chown -R appuser:appgroup /app /run/nginx

# Expose ports
EXPOSE 8080 4040

# Switch to non-root user for better security
# Note: We need to keep root for now as nginx and supervisord require it
# USER appuser

# Set entrypoint
ENTRYPOINT ["/app/start.sh"] 