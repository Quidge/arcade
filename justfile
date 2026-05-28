# `date -u` instead of `--utc` because BSD/macOS date(1) lacks the long form.
GIT_SHA := `git rev-parse HEAD`
BUILT_AT := `date -u +%FT%TZ`

_default:
    @just --list

# build with SHA + build-time injection, then serve on this worktree's deterministic port
web: build
    ADDR=:$(wt step eval '{{{{ branch | hash_port }}') ./bin/arcade

# compile a binary into bin/
build:
    go build -ldflags "-X main.gitSHA={{GIT_SHA}} -X main.builtAt={{BUILT_AT}}" -o bin/arcade .

# build the docker image for local arch (fast iteration)
docker-build:
    docker build \
        --build-arg GIT_SHA={{GIT_SHA}} \
        --build-arg BUILT_AT={{BUILT_AT}} \
        --tag arcade:dev \
        .

# run the locally-built image on :8080
docker-run: docker-build
    docker run --rm --publish 8080:8080 arcade:dev

# multi-arch build + push to GHCR (requires prior `docker login ghcr.io`)
docker-build-push-ci:
    docker buildx build \
        --platform linux/amd64,linux/arm64 \
        --build-arg GIT_SHA={{GIT_SHA}} \
        --build-arg BUILT_AT={{BUILT_AT}} \
        --push \
        --tag ghcr.io/quidge/arcade:sha-{{GIT_SHA}} \
        .

# format all Go files in place
fmt:
    gofmt -w .

# run golangci-lint
lint:
    golangci-lint run ./...

# run unit tests only
test-unit:
    go test ./...

# run integration tests only
test-integration:
    go test -tags=integration ./tests/integration/...

# run end-to-end UI tests (Playwright)
test-e2e:
    SCRIBBLE_E2E_PORT=$(wt step eval '{{{{ (branch ~ "---e2e") | hash_port }}') pnpm test:e2e

# run all Go test tiers (e2e is invoked separately; see test-e2e)
test-all: test-unit test-integration

# sync go.mod/go.sum to actual imports
tidy:
    go mod tidy

# run fmt, lint, all test tiers, and tidy
check: fmt lint test-all tidy
