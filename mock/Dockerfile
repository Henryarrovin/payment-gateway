FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o mock-razorpay .

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /app/mock-razorpay .
EXPOSE 8090
CMD ["./mock-razorpay"]