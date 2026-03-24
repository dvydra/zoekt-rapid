.PHONY: build test install vet

build:
	go build -o zoekt-rapid ./cmd/zoekt-rapid

test:
	go test ./...

install:
	go install ./cmd/zoekt-rapid

vet:
	go vet ./...
