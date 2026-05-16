# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.26.3 AS builder
WORKDIR /src

ARG GIT_SHA
ARG BUILT_AT
ARG TARGETOS
ARG TARGETARCH

COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY main.go ./
COPY templates ./templates

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
        -trimpath \
        -ldflags="-s -w -X main.gitSHA=${GIT_SHA} -X main.builtAt=${BUILT_AT}" \
        -o /out/scribble \
        .

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=builder /out/scribble /scribble
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/scribble"]
