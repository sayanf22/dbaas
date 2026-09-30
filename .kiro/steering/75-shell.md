---
inclusion: fileMatch
fileMatchPattern: '**/*.sh'
---

# Shell rules (hack/ scripts, cloud-init helpers)

- MUST: shell is glue only. A script over ~100 lines or with non-trivial logic is rewritten in Go.
- MUST: `#!/usr/bin/env bash` and `set -Eeuo pipefail` on the first lines, with an `ERR` trap that prints the failing line to stderr. `set -e` is skipped inside conditions and `&&`/`||` lists, so critical commands are checked explicitly.
- MUST: ShellCheck passes in CI with no disabled checks unless a `# shellcheck disable=SCxxxx # reason` comment explains it.
- MUST: quote every expansion (`"$var"`, `"${arr[@]}"`); build argument lists in arrays; glob with `./*` so file names can't become options.
- MUST: no `eval`, no aliases, no parsing of `ls` output, no `curl | sh`; downloads verify a pinned checksum before use.
- MUST: functions use `local` variables; the script ends with `main "$@"`.
- MUST: errors go to stderr with a clear message; exit codes are non-zero on failure; temporary files come from `mktemp` and are removed by a `trap … EXIT`.
- MUST: scripts are idempotent (safe to run twice) and never print secrets; secrets are read from files or the environment, never from arguments (they show up in `ps`).
- MUST: every script starts with a header comment: what it does, required tools, inputs, and whether it is safe to re-run.
