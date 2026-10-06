# Efficient dev loop (all work on this PC)

The local loop must stay fast and never hang. Details: `plan/05-dev-environment-and-downloads.md` §6.

- MUST: every command has a timeout; long jobs run in the background with a bounded wait.
- MUST: no `sleep` longer than 30 s. Poll with short intervals and an upper bound, and fail fast on terminal error states (`CrashLoopBackOff`, `ImagePullBackOff`, `Failed` Jobs, `Error` conditions).
- MUST: never block on an interactive prompt. In WSL set `GIT_TERMINAL_PROMPT=0`; push with Windows git, which holds the credentials.
- MUST: don't recreate the local cell unless its profile or K3s configuration changed; roll out manifest changes with `task cell:sync`.
- MUST: while iterating, run only the scoped tests and linters for what changed; run `task cell:test` and the full lint set once per step, at the end.
- MUST: run Trivy `fs` on a copy of the repo on the Linux filesystem; on `/mnt/d` it times out.
- MUST: check CI status at intervals of 2 minutes or more.
- MUST: from PowerShell, write a bash script file and run it with `wsl -d Ubuntu-24.04 -u dev -- bash -l <file>`; never inline multi-line bash, and never write and run the script in the same parallel batch.
- MUST: run independent work in parallel (subagents, parallel test packages); serialize only real dependencies.
- MUST: the optional HA compatibility check (`PROFILE=reference` + `TestControlPlaneSurvivesServerLoss`) runs only when the K3s configuration changes, not on every step.
