GOOGLEAPIS_PATH := $(shell go list -m -f '{{.Dir}}' github.com/googleapis/googleapis)

proto:
	mkdir -p proto/paymentpb
	protoc \
		-I proto \
		-I $(GOOGLEAPIS_PATH) \
		--go_out=proto/paymentpb \
		--go-grpc_out=proto/paymentpb \
		--grpc-gateway_out=proto/paymentpb \
		proto/payment.proto

run:
	go run main.go

build:
	go build -o bin/payment-gateway .

wire:
	cd wire && wire

.PHONY: wire proto run build