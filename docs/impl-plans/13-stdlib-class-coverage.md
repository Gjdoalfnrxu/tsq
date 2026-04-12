# 13 — CodeQL stdlib class coverage matrix

## Scope

Compat-plan item 4c says "every CodeQL standard library class gets at
least one test." This PR implements that as a single coverage matrix:
a test that enumerates every class exported by the compat bridge
files (`bridge/compat_*.qll`) and asserts that at least one query in
`testdata/compat/` (or a dedicated stub query) references it.

Two possible failure modes the matrix catches:
1. A class is added to the bridge but never exercised.
2. A class is removed/renamed in the bridge and silently breaks
   queries downstream.

## Dependencies

- Plan 06 (harness exists, so a coverage test can slot into the same
  package).
- Plans 07–10 (goldens exist so the "referenced by a test" check
  is meaningful).

## Files to change

- `/tmp/tsq/bridge/manifest.go` — read only; the manifest already
  enumerates available classes (see `compat_test.go` reference to
  `available classes 84 -> 85`). If not, extend it.
- `/tmp/tsq/compat_test.go` — add `TestCompatStdlibCoverage`:
  - Load the bridge manifest.
  - Parse every `testdata/compat/*.ql` and collect referenced
    class names.
  - Assert that every class in the manifest appears in at least
    one query — or is in an allowlist of "intentionally unused
    for now" classes.
- `/tmp/tsq/testdata/compat/coverage_probe.ql` (new, optional) —
  a query that touches classes not covered elsewhere, so the
  matrix can be green without hand-writing 85 fixture queries.

## Implementation steps

1. Read `bridge/manifest.go` and confirm it exposes a list of
   available class names per compat file. If it doesn't, add a
   `ListCompatClasses()` helper returning `[]string`.
2. In `compat_test.go`, add `TestCompatStdlibCoverage`:
   - Walk `testdata/compat/*.ql`.
   - For each file, parse it (using `ql/parse`) and collect every
     `TypeRef` that mentions a CodeQL compat name.
   - Diff against the manifest.
   - Fail with the list of uncovered classes.
3. If the diff is non-empty (likely — 85 classes is a lot), add
   `coverage_probe.ql` that just does `from <Class> c select c` for
   each uncovered class. This keeps the matrix meaningful without
   80 separate files.
4. Add an explicit allowlist in the test for classes that must
   remain uncovered (e.g., internal helpers). Default: empty.

## Test strategy

- `compat_test.go::TestCompatStdlibCoverage` passes.
- Removing a class from the manifest causes the test to fail
  because the probe query no longer resolves.
- Adding a class and failing to include it in the probe causes
  the test to fail with a clear "uncovered: [ClassName]" message.

## Acceptance criteria

- [ ] Coverage test passes against every class currently in the
  manifest.
- [ ] `coverage_probe.ql` exists (if needed) and parses cleanly.
- [ ] Allowlist is empty OR has entries with inline justification
  comments.
- [ ] Manifest helper method (if added) is exported cleanly.

## Risks and open questions

- Parsing `.ql` files in a test is brittle — consider instead
  grepping the text for class names. Faster and simpler. Prefer
  grep unless it produces false positives.
- "Covered by reference" is a weak form of coverage. A real test
  would assert each class can actually match at least one tuple
  in a fixture project. That is a much bigger lift — defer.
- Adding a class to the bridge should then require a matching
  probe update in the same PR. Document this in CONTRIBUTING
  (covered by plan 14).

## Out of scope

- Per-class assertion that tuples flow through.
- Generating queries automatically from the manifest.
- Coverage metrics beyond the matrix.
