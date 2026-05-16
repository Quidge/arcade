# `date -u` instead of `--utc` because BSD/macOS date(1) lacks the long form.
GIT_SHA := `git rev-parse HEAD`
BUILD_TIME := `date -u +%FT%TZ`

_default:
    @just --list

# build with SHA + build-time injection, then serve on this worktree's deterministic port
web: build
    ADDR=:$(wt step eval '{{{{ branch | hash_port }}') ./bin/scribblepass

# compile a binary into bin/
build:
    go build -ldflags "-X main.gitSHA={{GIT_SHA}} -X main.buildTime={{BUILD_TIME}}" -o bin/scribblepass .

# build the docker image for local arch (fast iteration)
docker-build:
    docker build \
        --build-arg GIT_SHA={{GIT_SHA}} \
        --build-arg BUILD_TIME={{BUILD_TIME}} \
        --tag scribblepass:dev \
        .

# run the locally-built image on :8080
docker-run: docker-build
    docker run --rm --publish 8080:8080 scribblepass:dev

# multi-arch build + push to GHCR (requires prior `docker login ghcr.io`)
docker-build-push-ci:
    docker buildx build \
        --platform linux/amd64,linux/arm64 \
        --build-arg GIT_SHA={{GIT_SHA}} \
        --build-arg BUILD_TIME={{BUILD_TIME}} \
        --push \
        --tag ghcr.io/quidge/scribble:sha-{{GIT_SHA}} \
        .

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
