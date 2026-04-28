# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies (Alpine uses apk)
RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o auth-service \
    ./main.go

# Run stage
FROM alpine:3.20

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create log directory
RUN mkdir -p /apps/logs

# Copy binary from builder
COPY --from=builder /app/payment-gateway .

# Copy config if exists
COPY --from=builder /app/config ./config

EXPOSE 8080 50051

CMD ["./payment-gateway"]