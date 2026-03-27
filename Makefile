.PHONY: build test install setup vet

build:
	go build -o zoekt-vanzelf ./cmd/zoekt-vanzelf

test:
	go test ./...

install:
	go install ./cmd/zoekt-vanzelf

setup: install
	./install.sh

vet:
	go vet ./...
