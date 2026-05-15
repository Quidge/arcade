_default:
    @just --list

# start the dev server
web:
    go run .

# compile a binary into bin/
build:
    go build -o bin/scribblepass .

# format all Go files in place
fmt:
    gofmt -w .

# run golangci-lint
lint:
    golangci-lint run ./...

# run the test suite
test:
    go test ./...

# sync go.mod/go.sum to actual imports
tidy:
    go mod tidy

# run fmt, lint, test, and tidy
check: fmt lint test tidy
