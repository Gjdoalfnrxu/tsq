# Bench run log

One entry per `bench_run.sh` invocation, most recent first. Format:

```
## run_NNN — YYYY-MM-DD — <label>

- **SHA:** <git sha>
- **Corpora:** <list>
- **Outcome:** <OK / PARTIAL / FAIL>
- **Notes:** <one-two sentences>
```

Numbers live in `results/run_NNN/manifest.yaml` + `<corpus>.csv`.
This file is the human-legible index — grep here to find the SHA
for a given run; then `git show <sha>` for the binary's provenance.

---

## run_001 — 2026-04-20 — Phase D PR7 baseline

- **SHA:** `506a7d5` (post Phase D PR1/PR2/PR6, pre plan-PR7).
- **Corpora:** `local_fixtures` (in-tree `testdata/projects/`).
- **Outcome:** OK — sanity check passes, four predicates all non-zero.
- **Wall:** extract 1959 ms; queries 1889 / 2023 / 755 / 2072 ms.

### Baseline row counts

| Predicate | Corpus | Rows | Wall (ms) |
|---|---|---|---|
| `mayResolveToRec` | local_fixtures | 481 | 1889 |
| `mayResolveTo_all` | local_fixtures | 695 | 2023 |
| `mayResolveTo_dataflow` | local_fixtures | 481 | 755 |
| `resolvesToFunctionDirect` | local_fixtures | 49 | 2072 |

### Notes

- MVP run for the Phase D harness. Proves the driver + query set
  produce non-zero, reproducible numbers against the current main
  tip. Future plan-PR7 runs diff against this.
- Note: `mayResolveToRec` and `mayResolveTo_dataflow` both return
  481 rows — consistent with the parity test wired in Phase D PR1
  (the `tsq_dataflow` wrapper exposes the same system relation).
  `mayResolveTo_all` at 695 includes non-recursive rows that the
  `_located` projection filters out (location-projectable endpoints
  only); ratio ~1.44 is within the known range documented in
  `bridge/tsq_valueflow.qll` comments.
- Mastodon corpus is reachable at
  `audiograb@100.80.10.45:~/setstate-bench/corpus/mastodon` (~200MB).
  Not included in run_001 to keep the PR's MVP small and determin-
  istic. First Mastodon run will be run_002, opened against the
  plan-PR7 branch so the diff is meaningful.
- Hardware: Planky's VM. Not the Mastodon perf-gate hardware
  (`fungoid.xyz`); wall-time numbers here are intentionally informa-
  tional, not gating.
