#!/usr/bin/env bash
# Git pre-commit hook (15-security-and-reliability.md §9): blocks a commit that would add a secret.
#   1. Trivy's secret scanner over the files staged for this commit;
#   2. hack/check-secrets.sh (unencrypted Kubernetes Secrets, secret-looking NEXT_PUBLIC_ names).
# Install once per clone with `task hooks` (links .git/hooks/pre-commit to this file).
# Requires: trivy, yq, git. Reads the git index only; safe to re-run. A finding means: remove the secret
# from the commit and rotate it (a committed secret is compromised even if the commit is amended).
set -Eeuo pipefail
trap 'echo "pre-commit: failed at line $LINENO" >&2' ERR

root="$(git rev-parse --show-toplevel)"
# Copy of the staged files; global so the EXIT trap removes it on every exit path (it may hold a secret).
work=""

# main: copies the staged blobs to a temp dir, scans them with Trivy, then runs check-secrets.sh.
main() {
  local file
  work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT
  # Scan the staged content (what will actually be committed), not the working tree.
  while IFS= read -r -d '' file; do
    mkdir -p "${work}/$(dirname "$file")"
    git show ":${file}" >"${work}/${file}"
  done < <(git diff --cached --name-only --diff-filter=ACMR -z)
  if ! trivy fs -q --scanners secret --exit-code 1 "$work"; then
    echo "pre-commit: secret found in staged files; remove it and rotate it" >&2
    return 1
  fi
  bash "${root}/hack/check-secrets.sh"
}

main "$@"
