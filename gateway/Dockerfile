# syntax=docker/dockerfile:1.19
# pg-gateway image: built with `cargo auditable` (dependency list embedded for scanners), final stage
# distroless cc (glibc, no shell), non-root. Base images are pinned by digest; Renovate updates them.
# Build context: the gateway/ directory.

FROM docker.io/library/rust:1.98.1-trixie@sha256:a8a5f0a1e5fe7dfe1d352591e4a1c7dd2c08fd70475cae872cf3458ba0df0546 AS build
WORKDIR /src
# cargo-auditable is pinned; --locked uses its published lockfile.
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    cargo install --locked cargo-auditable@0.7.1
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN --mount=type=cache,target=/usr/local/cargo/registry --mount=type=cache,target=/src/target \
    cargo auditable build --release --locked && \
    install -D -m 0555 target/release/pg-gateway /out/pg-gateway

FROM gcr.io/distroless/cc-debian13:nonroot@sha256:54df941ed0d06a1bd95ef5e0ce391fd8d9f94b64782dc9a60062727849ee3f97
ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.source="https://github.com/sayanf22/dbaas" \
      org.opencontainers.image.title="pg-gateway" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY --from=build --chown=65532:65532 /out/pg-gateway /pg-gateway
USER 65532:65532
ENTRYPOINT ["/pg-gateway"]
