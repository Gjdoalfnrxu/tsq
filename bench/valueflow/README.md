# `bench/valueflow/` — Phase D cross-corpus measurement harness

**Status:** MVP — Phase D PR7. Landed with bridge PRs PR1 (#205), PR2
(#206), PR6 (#207) green on `main`. Unblocks plan-PR7 (R1–R4 shape-
predicate deletion) by providing a reproducible measurement surface.

## What this is

A scripted driver that extracts a TypeScript / JavaScript corpus,
runs a fixed set of value-flow bridge queries against it, and emits
per-predicate row-count CSVs plus a wall-time log. Two runs produce
a diff; that diff is the §4 keep-or-revert signal.

Modelled on `andryo@fungoid.xyz:~/janky-bench/` — run_NNN directory
per run, CSV artefact per corpus, wall-time in a manifest, all
git-tracked in this repo so the commit SHA that produced the numbers
is aligned with the numbers.

## Quickstart

```bash
# One corpus, current HEAD, fresh run directory
./bench_run.sh run_042 local_fixtures

# All corpora from corpora.yaml
./bench_run.sh run_042 --all

# Diff two runs
python3 compare.py results/run_001 results/run_042
```

Outputs land in `results/run_042/`:

```
results/run_042/
├── manifest.yaml           # SHA, date, corpora, tsq version, wall times
├── local_fixtures.csv      # predicate,fixture,row_count,wall_ms
├── local_fixtures.log      # raw extract + query stdout/stderr
└── ...                     # one .csv + .log per corpus
```

## CSV schema

```
predicate,fixture,row_count,wall_ms
```

- `predicate` — short label for the bridge query (e.g.
  `mayResolveToRec`, `ExpressHandlerArgUse`, `mayResolveTo_dataflow`).
  Defined in `bench_run.sh` `QUERIES` table.
- `fixture` — corpus identifier from `corpora.yaml`.
- `row_count` — wc -l of the CSV alert output, minus header.
- `wall_ms` — milliseconds for the query (not extraction; extraction
  is amortised across queries within a run).

The intent is that two runs on the same corpus with different tsq
heads produce CSVs whose row-level diff is the measurable signal.

## Corpora

Listed in `corpora.yaml`. Initial set:

- **local_fixtures** — `testdata/projects/` — fast, deterministic,
  small (dozens of files). The CI-style gate.
- **mastodon** — `audiograb@100.80.10.45:~/setstate-bench/corpus/mastodon/`
  — React + cross-file imports + the historic planner-stress
  reference. ~200MB. SSH fetch expected.

New corpora: append to `corpora.yaml` with `name`, `path` (local or
`user@host:path`), and a brief `notes` field.

## What the harness CAN detect

- **Row-count regressions** — a bridge predicate returning fewer (or
  more) rows between two runs at the same corpus. This catches the
  common "did this refactor silently drop alerts" question.
- **Wall-time regressions** — coarse; 2x+ shifts are real, <2x is
  noise on non-pinned hardware.
- **Predicate added / removed** between runs — `compare.py` lists
  them explicitly rather than silently ignoring.
- **Empty-output regressions** — the MVP sanity check fails the run
  if every predicate for a corpus returns zero rows (symptom of a
  broken extraction, not a real measurement).

## What the harness CANNOT detect

- **Precision regressions where the row count stays the same** — if
  a refactor replaces true positive A with false positive B, the
  row count is identical and the harness is silent. The `diff` mode
  of `compare.py` goes row-by-row; that shows file+line differences,
  but only insofar as the queries project file+line. Adding a query
  that doesn't project locations defeats this check.
- **Plan-shape regressions** — cap-hit / planner-explosion signal
  lives in tsq's stderr ("cap @ step N" log lines); the MVP does
  not parse this. Follow-up: ingest stderr plan logs into
  `manifest.yaml` as a list.
- **Shortcut / wrong-path passes** — a predicate that happens to
  return the right row count via a wrong shortcut is indistinguishable
  from the right answer. Harness is not a correctness oracle; use
  fixture-level expected tables for that.
- **maxrss** — not yet captured in the MVP. `/usr/bin/time -v` stub
  wired but not populated into the CSV; follow-up.

This list is deliberate: better to document the blind spots than
claim coverage we don't have.

## Determinism

Bridge queries are deterministic on a given extraction. Extraction
is deterministic on a given tsq binary. CSV ordering is pinned via
`sort -t, -k1,1 -k2,2 -k3,3n` after the raw output. If a run shows
unexplained row-count jitter between two invocations at the same
SHA, investigate the tsq extraction side before blaming the harness
(see §12.3 in the plan doc).

## Regression guard

`bench/valueflow/tests/test_compare.py` runs as a smoke test:
- Two synthetic run directories, expected diff output.
- Degenerate cases: missing run, zero-row predicate, predicate
  added/removed between runs.
- Mutation probe: breaking `compare.py`'s delta computation causes
  the test to fail.

`bench_run.sh` exits non-zero if:
- Every predicate for a corpus returns `row_count == 0`. Sanity
  check against "silent full collapse."
- The manifest ends up missing required fields.

These are deliberately loose — they catch total-failure modes, not
subtle regressions. Subtlety is the reviewer's job.

## See also

- `docs/design/valueflow-phase-d-plan.md` — the plan. §3–§5 specify
  the matrix and the harness contract; §4 the keep criteria.
- `docs/design/valueflow-layer.md` — the parent value-flow design.
- `RUNS.md` — log of every run, with commentary.
