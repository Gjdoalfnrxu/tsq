# 14 — Adversarial review checklist

## Scope

Compat-plan item 4c says "adversarial review on every PR (existing
process)." The process exists but is not written down. This PR adds
a formal checklist to `CONTRIBUTING.md` (or a new file
`docs/ADVERSARIAL-REVIEW.md`) that every PR reviewer can run
through, and references it from the PR template.

Docs-only PR. No code changes.

## Dependencies

None.

## Files to change

- `/tmp/tsq/CONTRIBUTING.md` — add a "Adversarial review checklist"
  section with the items below, or link to a new doc file.
- `/tmp/tsq/docs/ADVERSARIAL-REVIEW.md` (new, if the checklist is
  long enough to warrant splitting) — full checklist.
- `/tmp/tsq/.github/pull_request_template.md` (new or extend) —
  add a "confirmed adversarial review" checkbox.

## Checklist contents (draft)

The reviewer works through these questions for every PR:

1. **Panics.** Grep the diff for `panic(`, `log.Fatal`, `os.Exit`.
   Each must have a justifying comment OR be removed.
2. **Error handling.** Every `err :=` pair must check or explicitly
   drop with `_ =`. No silent `_, _ = ...`.
3. **Concurrency.** Any new goroutine: is the exit path clear? Any
   shared map/slice: is it guarded? Any channel: can it deadlock on
   close?
4. **Resource leaks.** Every `os.Open`, `exec.Command`, `db.Open`,
   `net.Dial` must have a matching `defer Close()`.
5. **Test gaming.** Does the PR's test actually exercise the change,
   or does it pass vacuously? Specifically:
   - Negative controls present?
   - Does removing the production change cause the test to fail?
   - Are assertions specific (`got != expected`) or loose (`len > 0`)?
6. **Benchmark overfitting.** Does the PR only improve a specific
   benchmark at the cost of real-world paths? Check the change
   against the v2 integration suite.
7. **Schema changes.** If `extract/db` schema version bumped, is
   the reader backward-compat path in place? Tested?
8. **QL semantics.** For `ql/` changes, does the change respect
   CodeQL's documented behaviour, or does it invent new semantics?
   Cite the doc URL in the PR if unsure.
9. **Clean-room discipline.** For `bridge/compat_*.qll` changes,
   is the diff clearly paraphrased from public docs, not copied
   from CodeQL's source?
10. **Deps.** Any new `go.mod` entry — is the licence compatible?
    Any replace directive? Pinned version?

## Implementation steps

1. Draft the checklist above into the chosen location.
2. Add a PR template that references the checklist and includes a
   "Adversarial review: yes/no/N/A" line.
3. Update the existing CONTRIBUTING.md to point at the checklist.
4. Open PR, self-review against the checklist as a smoke test.

## Test strategy

Docs PR; no automated tests. Verification:
- CONTRIBUTING renders on GitHub correctly.
- PR template appears when opening a new PR.
- The checklist items are actionable (someone reading them cold
  can execute them in <5 minutes).

## Acceptance criteria

- [ ] Checklist committed.
- [ ] PR template references it.
- [ ] CONTRIBUTING cross-links it.
- [ ] No code changes.

## Risks and open questions

- Over-specified checklists become bureaucratic. Keep it to ~10
  items. Aggressively prune items the existing codebase already
  enforces via linters.
- PR template may already exist — check `.github/`. Extend, don't
  replace.
- Adversarial review without a tool is advisory; real enforcement
  happens in CI. This PR does not add CI gates.

## Out of scope

- CI automation for the checklist.
- Per-item enforcement (linters, grep hooks).
- Automated adversarial review via subagent — that is a process
  decision outside the repo.
