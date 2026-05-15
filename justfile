_default:
    @just --list

# start the dev server
web:
    go run .

# start the dev server on this worktree's deterministic port
web-wt:
    ADDR=:$(wt step eval '{{{{ branch | hash_port }}') go run .

# compile a binary into bin/
build:
    go build -ldflags "-X main.gitSHA=$(git rev-parse --short HEAD)" -o bin/scribblepass .

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
