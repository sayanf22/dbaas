# Efficient, safe dev loop (all work on this PC)

The local loop must stay fast, never hang, and never put the owner's Windows PC at risk. Details: `plan/05-dev-environment-and-downloads.md` §6.

## Safety on the owner's PC
- MUST: never delete, overwrite or move anything outside this repository on Windows, and never delete user data anywhere (files, folders, WSL distros, Docker volumes that aren't the local cell's, browser or app data). Generated, disposable items inside the repo or the agent's own temp dirs (`%TEMP%\dbc*`) may be cleaned up.
- MUST: no command that can damage the machine or its data: disk/partition tools, registry edits, `wsl --unregister`, driver or BIOS changes, disabling Defender/firewall, mass permission changes, `git push --force`, `git reset --hard`, `git clean -f`.
- MUST: back up any user-level config file before changing it (copy next to it with a dated suffix) and say where the backup is.
- MUST: prefer reversible actions; when an action is hard to undo or touches data outside the repo, explain it and ask first. When unsure whether something is safe, don't do it: ask.
- MUST: restarting local tools the work depends on (Docker Desktop, WSL, the k3d cell) is allowed; say so in the summary.

## Speed
- MUST: every command has a timeout; long jobs run in the background with a bounded wait.
- MUST: no `sleep` longer than 30 s. Poll with short intervals and an upper bound, and fail fast on terminal error states (`CrashLoopBackOff`, `ImagePullBackOff`, `Failed` Jobs, `Error` conditions).
- MUST: never block on an interactive prompt. In WSL set `GIT_TERMINAL_PROMPT=0` and close stdin (`</dev/null`) for tools that may wait on it (pnpm); push with Windows git, which holds the credentials.
- MUST: don't recreate the local cell unless its profile or K3s configuration changed; roll out manifest changes with `task cell:sync`.
- MUST: run Trivy `fs` on a copy of the repo on the Linux filesystem; on `/mnt/d` it times out.
- MUST: check CI status at intervals of 2 minutes or more.
- MUST: from PowerShell, write a bash script file and run it with `wsl -d Ubuntu-24.04 -u dev -- bash -l <file>`; never inline multi-line bash, and never write and run the script in the same parallel batch.
- MUST: run independent work in parallel (subagents, parallel test packages); serialize only real dependencies.

## Verify in proportion to risk
Run the checks that can catch a mistake in what changed, not every check every time. CI still runs on every push (path-filtered, free), so local runs only need what CI can't do or what saves a CI round trip.

| Change | Local checks |
|---|---|
| Docs, comments, steering, plan text only | none; re-read the change (links, tables, numbers) |
| One small, low-risk edit (a constant, a message, a config value with no code path change) | the one linter or test that covers that file |
| Code in one package/crate/script | that package's tests + its linter (`go test ./pkg/...`, `cargo test -p`, `shellcheck file`) |
| Cross-package code, a dependency bump, generated code | the language's full lint + tests (`task lint` part for that language, `go test -race ./...`) |
| `deploy/` manifests | `bash hack/check-manifests.sh`, then `task cell:sync` if the cell runs |
| K3s config, cell scripts, operator/gateway behaviour, anything security-relevant (auth, RBAC, NetworkPolicy, secrets, SQL safety) | the above + `task cell:test`; the reference shape (`PROFILE=reference`) only when the K3s configuration changed |
| End of a build-plan step | the step's full "Done when" set, once |

- MUST: security-relevant changes never take the shortcut; when unsure which row applies, use the stricter one.
- MUST: a check that fails is fixed or explained, never skipped silently; say which checks ran and which didn't.
