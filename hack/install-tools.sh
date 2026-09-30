#!/usr/bin/env bash
# Installs the project's pinned CLI tools that aren't Ubuntu packages into ~/.local/bin (no sudo).
# Requires: bash, curl, tar, sha256sum, Go (for `go install`), network access.
# Input: tools.versions at the repo root; optional tool names as arguments install only those
# (CI's Rust job: `install-tools.sh cargo-deny cargo-nextest`, no Go needed).
# Safe to re-run: a tool already at its pinned version is skipped.
# Integrity: Go tools are verified by the Go checksum database (sum.golang.org); release archives are
# checked against the SHA-256 their project publishes before anything is installed.
set -Eeuo pipefail
trap 'echo "install-tools: failed at line $LINENO" >&2' ERR

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin_dir="${HOME}/.local/bin"
gh="https://github.com"

# load_versions: exports KEY=value pairs from tools.versions (comments and blank lines ignored).
load_versions() {
  local key value
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" == \#* ]] && continue
    printf -v "$key" '%s' "${value%$'\r'}"
  done <"${repo_root}/tools.versions"
}

# Tools requested on the command line; empty means all.
selected=()

# wanted <binary>: true when no tools were named or <binary> is one of them.
wanted() {
  local name
  [[ ${#selected[@]} -eq 0 ]] && return 0
  for name in "${selected[@]}"; do [[ "$name" == "$1" ]] && return 0; done
  return 1
}

# have <binary> <version>: true when <binary> is on PATH and its version output mentions <version>.
have() {
  local out
  out="$("$1" --version 2>&1 || "$1" version 2>&1 || true)"
  [[ "$out" == *"$2"* ]]
}

# go_tool <binary> <module@version>: builds with `go install`, which checks sum.golang.org.
go_tool() {
  local name="$1" mod="$2"
  wanted "$name" || return 0
  if have "$name" "${mod##*@v}"; then echo "ok   $name"; return; fi
  GOBIN="$bin_dir" go install "$mod"
  echo "new  $name ${mod##*@}"
}

# archive_tool <binary> <version> <archive-url> <checksum-url> [member]: verifies, extracts, installs.
# The checksum file is either a list ("<sha>  <name>") or a single "<sha>" for one archive.
archive_tool() {
  local name="$1" version="$2" url="$3" sums_url="$4" member="${5:-$1}" work file want
  wanted "$name" || return 0
  if have "$name" "$version"; then echo "ok   $name"; return; fi
  work="$(mktemp -d)"
  file="${work}/$(basename "$url")"
  curl -fsSL -o "$file" "$url"
  want="$(curl -fsSL "$sums_url" | awk -v n="$(basename "$url")" 'NF == 1 { print $1; exit } { f = $2; sub(/^\*/, "", f); sub(/^.*\//, "", f) } f == n { print $1; exit }')"
  [[ -n "$want" ]] || { echo "install-tools: no checksum for $(basename "$url")" >&2; rm -rf "$work"; return 1; }
  echo "${want}  ${file}" | sha256sum -c --quiet -
  tar -xzf "$file" -C "$work"
  install -m 0755 "$(find "$work" -type f -name "$member" | head -n 1)" "${bin_dir}/${name}"
  rm -rf "$work"
  echo "new  $name $version"
}

main() {
  selected=("$@")
  load_versions
  mkdir -p "$bin_dir"
  export PATH="${bin_dir}:${PATH}"

  go_tool age          "filippo.io/age/cmd/age@v${AGE}"
  go_tool age-keygen   "filippo.io/age/cmd/age-keygen@v${AGE}"
  go_tool yq           "github.com/mikefarah/yq/v4@v${YQ}"
  go_tool goose        "github.com/pressly/goose/v3/cmd/goose@v${GOOSE}"
  go_tool oapi-codegen "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v${OAPI_CODEGEN}"
  go_tool govulncheck  "golang.org/x/vuln/cmd/govulncheck@v${GOVULNCHECK}"
  go_tool sqlc         "github.com/sqlc-dev/sqlc/cmd/sqlc@v${SQLC}"   # cgo: needs build-essential

  archive_tool flux "$FLUX" \
    "${gh}/fluxcd/flux2/releases/download/v${FLUX}/flux_${FLUX}_linux_amd64.tar.gz" \
    "${gh}/fluxcd/flux2/releases/download/v${FLUX}/flux_${FLUX}_checksums.txt"
  archive_tool kubectl-cnpg "$CNPG_PLUGIN" \
    "${gh}/cloudnative-pg/cloudnative-pg/releases/download/v${CNPG_PLUGIN}/kubectl-cnpg_${CNPG_PLUGIN}_linux_x86_64.tar.gz" \
    "${gh}/cloudnative-pg/cloudnative-pg/releases/download/v${CNPG_PLUGIN}/cnpg-${CNPG_PLUGIN}-checksums.txt"
  archive_tool cargo-deny "$CARGO_DENY" \
    "${gh}/EmbarkStudios/cargo-deny/releases/download/${CARGO_DENY}/cargo-deny-${CARGO_DENY}-x86_64-unknown-linux-musl.tar.gz" \
    "${gh}/EmbarkStudios/cargo-deny/releases/download/${CARGO_DENY}/cargo-deny-${CARGO_DENY}-x86_64-unknown-linux-musl.tar.gz.sha256"
  archive_tool cargo-nextest "$CARGO_NEXTEST" \
    "${gh}/nextest-rs/nextest/releases/download/cargo-nextest-${CARGO_NEXTEST}/cargo-nextest-${CARGO_NEXTEST}-x86_64-unknown-linux-gnu.tar.gz" \
    "${gh}/nextest-rs/nextest/releases/download/cargo-nextest-${CARGO_NEXTEST}/cargo-nextest-${CARGO_NEXTEST}-x86_64-unknown-linux-gnu.sha256"
}

main "$@"
