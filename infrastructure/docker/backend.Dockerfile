# syntax=docker/dockerfile:1.7
#
# Builds either Go entrypoint from backend/:
#   docker build -f infrastructure/docker/backend.Dockerfile --build-arg TARGET=api    backend
#   docker build -f infrastructure/docker/backend.Dockerfile --build-arg TARGET=worker backend

ARG GO_VERSION=1.26.11

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath

# Dependencies first for layer caching.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG TARGET=api
ARG VERSION=dev
ARG COMMIT=none
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build \
      -ldflags "-s -w \
        -X github.com/equiprent/backend/internal/version.Version=${VERSION} \
        -X github.com/equiprent/backend/internal/version.Commit=${COMMIT}" \
      -o /out/app ./cmd/${TARGET}

# Minimal runtime: no shell, no package manager, runs as non-root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app"]
