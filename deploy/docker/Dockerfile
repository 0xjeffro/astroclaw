# syntax=docker/dockerfile:1.7

# Multi-stage build: golang for compilation, distroless static for runtime.
# The runtime image ships only the binary and the seed assets it reads at
# startup, so the attack surface is tiny (no shell, no package manager,
# runs as nonroot by default).

FROM golang:1.26 AS build
WORKDIR /src

# Prime the module cache first so unrelated source edits reuse the layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the rest and build a static binary.
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" \
        -o /out/astroclaw-server ./deploy/docker/server

# Runtime: distroless static. Contains ca-certificates + tzdata + nonroot
# user, nothing else. https://github.com/GoogleContainerTools/distroless
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /

# Seed assets read at startup / by future migrate step.
COPY skills/     /skills/
COPY migrations/ /migrations/

# The server reads STORAGE_URL to open its blob backend. Default to a
# local directory so a bare `docker run` with a mounted /data volume
# works without extra config. compose can override to s3://, mem://, etc.
ENV STORAGE_URL=file:///data/skills

COPY --from=build /out/astroclaw-server /astroclaw-server

EXPOSE 8080
ENTRYPOINT ["/astroclaw-server"]
