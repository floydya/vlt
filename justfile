set dotenv-load := false

# List available recipes.
default:
    @just --list

# Apply canonical Go formatting.
fmt:
    gofmt -w .

# Verify formatting without changing files.
fmt-check:
    #!/usr/bin/env sh
    set -eu
    files="$(gofmt -l .)"
    if [ -n "$files" ]; then
        printf '%s\n' 'The following files need gofmt:' "$files" >&2
        exit 1
    fi

# Run all tests.
test:
    go test ./...

# Run tests with the race detector.
test-race:
    go test -race ./...

# Run Go static analysis.
vet:
    go vet ./...

# Build the local vlt executable.
build:
    go build ./cmd/vlt

# Run all local quality gates.
check: fmt-check test test-race vet build

# Verify compilation for all initial-release targets.
build-all:
    GOOS=linux GOARCH=amd64 go build ./cmd/vlt
    GOOS=darwin GOARCH=amd64 go build ./cmd/vlt
    GOOS=windows GOARCH=amd64 go build ./cmd/vlt
