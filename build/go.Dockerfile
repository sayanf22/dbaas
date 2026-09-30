# syntax=docker/dockerfile:1.19
# One image recipe for every Go service (plan/04 §1): `--build-arg BINARY=control-api` selects cmd/<BINARY>.
# Builder compiles a static binary; the final stage is distroless (no shell, no package manager), non-root.
# Base images are pinned by digest; Renovate updates them.

FROM docker.io/library/golang:1.27.1-trixie@sha256:3b77fc618ec235a1ab412de7737f120dd507c57e8d87de4cbb7994fb94275ed5 AS build
WORKDIR /src
# Dependencies first so the module download layer is reused while only source changes.
COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG BINARY
ARG VERSION=dev
# CGO off: none of these binaries links pg_query_go yet; the sqlsafety users will switch to the cc base (55-containers.md).
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    test -n "${BINARY}" && \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X github.com/sayanf22/dbaas/internal/platform.version=${VERSION}" \
      -o /out/app "./cmd/${BINARY}"

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG BINARY
ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.source="https://github.com/sayanf22/dbaas" \
      org.opencontainers.image.title="${BINARY}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY --from=build --chown=65532:65532 --chmod=0555 /out/app /app
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app"]
