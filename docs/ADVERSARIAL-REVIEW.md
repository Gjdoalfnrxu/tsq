# Adversarial Review Checklist

Every PR to tsq must pass this checklist before merge. Each item is designed to be executable in under a minute by someone with no prior context on the change.

---

## 1. Panics

Grep the diff for `panic(`, `log.Fatal`, `os.Exit`. Each occurrence must have a comment justifying why it is acceptable (e.g., truly unrecoverable init failure). If no justification exists, it must be replaced with error propagation.

## 2. Error Handling

Every `err :=` assignment in the diff must be followed by an `if err != nil` check or explicitly discarded with `_ =`. No unchecked errors.

## 3. Concurrency

Any new goroutine must have a clear exit path (context cancellation, channel close, or WaitGroup). Shared maps and slices must be guarded by a mutex or accessed only from a single goroutine. Channels must not be able to deadlock — verify send/receive pairs and buffer sizes.

## 4. Resource Leaks

Every `os.Open`, `exec.Command` (with `.Start()`), `db.Open`, `net.Dial`, or similar resource acquisition in the diff must have a matching `defer Close()` (or equivalent cleanup). Check that the defer is in the correct scope.

## 5. Test Gaming

For each new or modified test, verify:
- The test actually exercises the production code being changed.
- Negative controls are present (test fails when expected-bad input is given).
- Removing the production change causes the test to fail.
- Assertions are specific — no bare `err == nil` without checking the returned value.

## 6. Benchmark Overfitting

If the PR improves a specific benchmark, check whether the optimization is general-purpose or whether it only speeds up the benchmark's exact input shape at the cost of real-world code paths.

## 7. Schema Changes

If `extract/` or database schema versions are bumped:
- Is there a migration path from the previous version?
- Can the old schema still be read (backward compatibility)?
- Is the version constant incremented, not just the schema definition?

## 8. QL Semantics

For changes under `ql/`:
- Does the change respect documented CodeQL behaviour?
- Are predicates tested with both positive and negative examples?
- Do `.ql` test expectations match what CodeQL would actually produce?

## 9. Clean-Room Discipline

For changes under `bridge/compat_*.qll`:
- Is the diff paraphrased from public documentation, not copied from CodeQL source?
- Can the author point to a public doc URL justifying the implementation?

## 10. Dependencies

For any new entry in `go.mod`:
- Is the licence compatible (MIT, Apache-2.0, BSD — flag anything else)?
- Are there `replace` directives? If so, are they temporary and documented?
- Is the version pinned (no floating tags or `latest`)?
