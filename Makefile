.PHONY: build test install vet

build:
	go build -o zoekt-vanzelf ./cmd/zoekt-vanzelf

test:
	go test ./...

install:
	go install ./cmd/zoekt-vanzelf

vet:
	go vet ./...
