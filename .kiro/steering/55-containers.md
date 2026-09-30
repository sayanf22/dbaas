---
inclusion: fileMatch
fileMatchPattern: '**/*Dockerfile*'
---

# Container image rules (Dockerfiles, built with Docker Buildx)

- MUST: multi-stage builds. The builder stage compiles; the final stage contains only the binary, CA certificates and required static files. No compilers, shells, package managers or source in the final image.
- MUST: final images are distroless on the same Debian release as the builder (Debian 13 "trixie", so glibc matches): `gcr.io/distroless/static-debian13:nonroot` for pure-Go binaries, `gcr.io/distroless/cc-debian13:nonroot` for the gateway (Rust against glibc) and for cgo binaries.
- MUST: every `FROM` pins a digest (`image:tag@sha256:…`); Renovate updates digests. No `latest` tags.
- MUST: run as a non-root numeric user (`USER 65532:65532`); files the process needs are owned by it and read-only; no setuid/setgid binaries.
- MUST: build secrets only through `RUN --mount=type=secret` or `--mount=type=ssh`. Never pass secrets in `ARG`, `ENV`, `COPY`, or a layer that is later deleted.
- MUST: `ENTRYPOINT`/`CMD` in exec form, so the binary is PID 1 and receives SIGTERM.
- MUST: `COPY`, not `ADD` (except a remote download with `--checksum`).
- MUST: a `.dockerignore` excludes `.git`, local env files, build outputs and test data.
- MUST: builder stages that install OS packages use one `RUN` with `apt-get update && apt-get install -y --no-install-recommends <pinned versions>` and delete the apt lists; any `RUN` with a pipe sets `SHELL ["/bin/bash", "-o", "pipefail", "-c"]` first.
- MUST: Go binaries build with `CGO_ENABLED=0` unless the package needs cgo (only `pg_query_go` users), `-trimpath`, and `-ldflags="-s -w -X …version"`; the gateway builds with `cargo auditable build --release --locked`.
- MUST: images build natively for `linux/amd64` (GitHub `ubuntu-24.04` runners) and `linux/arm64` (`ubuntu-24.04-arm`), are merged into one manifest list and pushed to ghcr.io by digest; third-party images the cell uses are mirrored there by digest too.
- MUST: images carry OCI labels (`org.opencontainers.image.source`, `.revision`, `.version`), an SBOM (syft) and a cosign signature; Trivy blocks critical vulnerabilities.
- MUST: hadolint passes with no ignored rules unless the ignore carries a reason comment.
- MUST: images hold no configuration or secrets; everything comes from the environment or mounted files at run time.
- SHOULD: order layers from least to most frequently changing (dependency download before source copy) and use BuildKit cache mounts for Go and Cargo caches.
