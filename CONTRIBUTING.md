# Contributing to tsq

## Development Workflow

After cloning, run `make setup` to configure the git hooks path. This installs a pre-commit hook that auto-formats and auto-fixes lint issues before each commit.

All changes to tsq must go through a pull request. Direct pushes to `main` are not permitted.

### Branch Naming

`feat/phase-N-<short-description>` for planned phases.
`fix/<short-description>` for bug fixes.

### Worktree Workflow

Each parallel work stream works in its own git worktree:

```bash
git worktree add /tmp/tsq-phase-N feat/phase-N-description
cd /tmp/tsq-phase-N
# do work
git add .
git commit -m "feat: implement X"
git push origin feat/phase-N-description
gh pr create --title "Phase N: description" --body "..."
```

### PR Requirements

Every PR must:
1. Have a description explaining what changed and why
2. Pass CI (all tests green, lint clean)
3. Include a handover document (`HANDOVER-phase-N.md`) for phase completions
4. Be adversarially reviewed by a separate agent before merge

### Adversarial Review

For each PR, a separate review agent runs the [adversarial review checklist](docs/ADVERSARIAL-REVIEW.md). The review must find and report any real problems before the PR merges. The implementing agent fixes issues found, then the reviewer re-checks.

### Performance investigation

`tsq query` exposes three flags for diagnosing slow or memory-hungry queries:

- `--cpu-profile FILE` — writes a CPU profile for the duration of the query.
- `--mem-profile FILE` — writes a heap profile after the query completes (post-GC).
- `--mem-snapshot-dir DIR` — writes a heap profile every 10s while the query runs.

Analyse with `go tool pprof FILE`. The snapshot dir is most useful for catching
eval-time memory blow-ups that complete (or OOM) before the final `--mem-profile`
gets written — see #130 for the real-world OOM that motivated these flags.

### Running Phase C perf gates (Mastodon, local/nightly)

The Phase C value-flow rollout includes a Mastodon wall-time gate (plan
§9.1 — +50% of pre-Phase-C baseline blocks merge). The corpus is NOT
checked into the repo; the gate runs opt-in behind the `bench` build
tag:

```sh
TSQ_MASTODON_CORPUS=/path/to/mastodon \
TSQ_MASTODON_BASELINE_SECONDS=48 \
go test -tags=bench -run TestBench_MastodonPerfGate -timeout 10m .
```

- `TSQ_MASTODON_CORPUS` — absolute path to the Mastodon corpus
  directory (must contain extractable sources).
- `TSQ_MASTODON_BASELINE_SECONDS` — pre-Phase-C baseline in seconds.
  Defaults to 48 per plan §9.1 / wiki §Phase C PR4 outcomes. Re-baseline
  without a code change by setting this env var.

The gate uses a 1.5x multiplier (plan's "blocks-merge" threshold).
Within +50% but > baseline prints as "flag-for-follow-up" and passes.

On fungoid.xyz the corpus is at `~/benchmarks/mastodon`; the
janky-bench workflow (`andryo@fungoid.xyz:~/janky-bench/`) drives the
gate on a nightly/manual cadence. Default CI is unchanged — no Mastodon
run on GitHub Actions (would bloat wall-time and introduce flakiness on
shared runners). When a reliably-provisioned bench runner becomes
available, wire this into a nightly-only workflow.

### Handover Documents

When a phase completes, the implementing agent creates `HANDOVER-phase-N.md` at the repo root describing:
- What was implemented
- What was intentionally left out
- Key decisions and rationale
- Test coverage summary
- Files changed
- Dependencies for downstream phases
- First steps for the next phase
