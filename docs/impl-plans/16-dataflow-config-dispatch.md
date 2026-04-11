# Plan 16: DataFlow/TaintTracking Configuration dispatch

## Scope

Make `DataFlow::Configuration.hasFlow(source, sink)` (and the TaintTracking equivalent) actually consult the user's `isSource`/`isSink`/`isBarrier` overrides on their `Configuration` subclass, instead of ignoring the subclass and returning the global `LocalFlowStar` / `TaintAlert` extent. This is the compat-stack fix that turns DataFlow and TaintTracking from API-shape stubs into working flow engines for user-defined configurations.

Does NOT deliver: changes to the underlying flow engine (`LocalFlowStar`, `TaintAlert`, `extract/rules/taint.go`). This plan is purely about the bridge-level predicate so the user's class extent filters the result set correctly.

## Dependencies

None strictly; lands cleanly on current main. Should land before plans 07–09 (XSS/SQLi/custom-config goldens) because those goldens can't pass until Configuration dispatch works end-to-end.

## Context

Current state on main (as of this plan):

```ql
// bridge/compat_dataflow.qll:71-75
predicate hasFlow(Node source, Node sink) {
    exists(int fnId |
        LocalFlowStar(fnId, source, sink)
    )
}
```

This is a module-level predicate, not a method on `Configuration`, and it never mentions `this.isSource(source)` or `this.isSink(sink)`. The same shape appears in `bridge/compat_tainttracking.qll:34-45`. A user who writes:

```ql
class MyConfig extends DataFlow::Configuration {
    override predicate isSource(Node n) { ... }
    override predicate isSink(Node n) { ... }
}

from MyConfig cfg, Node src, Node sink
where cfg.hasFlow(src, sink)
select src, sink
```

...gets every pair of nodes linked by `LocalFlowStar`, not the ones their `isSource`/`isSink` would select. That's silently wrong.

## Files to change

- `bridge/compat_dataflow.qll` — rework `hasFlow` to be a **method on `Configuration`** that conjoins the user's `isSource(source)` and `isSink(sink)` with the underlying flow relation.
- `bridge/compat_tainttracking.qll` — same shape, but uses `TaintAlert`/the taint transitive closure instead of `LocalFlowStar`, and additionally negates `isBarrier` / `isSanitizer` as appropriate.
- `bridge/compat_dataflow_test.go` / `bridge/compat_tainttracking_test.go` — extend existing test helpers with an end-to-end case where a subclass overrides `isSource`/`isSink` and the result set is a proper subset of the global extent.

## Implementation steps

1. Read `bridge/compat_dataflow.qll` end to end to see how `Configuration` and `Node` are currently defined.
2. Move `hasFlow` inside the `Configuration` class (or make it a method that takes `this` implicitly), and update its body to:
   ```ql
   predicate hasFlow(Node source, Node sink) {
       this.isSource(source) and
       this.isSink(sink) and
       exists(int fnId | LocalFlowStar(fnId, source, sink))
   }
   ```
3. Do the same for `hasFlowPath` (currently identical shape, same bug).
4. Repeat in `compat_tainttracking.qll`, and additionally add `not this.isBarrier(source) and not this.isBarrier(sink)` (or wherever barrier semantics should land — check CodeQL docs for exact positioning).
5. Add a bridge test that declares a `Configuration` subclass with non-trivial `isSource`/`isSink` and asserts the result is a strict subset of the global `LocalFlowStar` extent.
6. Run existing `bridge/compat_*_test.go` to confirm they still pass (some may need to switch from module-level `hasFlow` calls to subclass-method calls).

## Test strategy

- `TestDataFlowConfigSubclassFiltersSource` in `bridge/compat_dataflow_test.go` — declare a subclass whose `isSource` returns only nodes with a specific name; assert `hasFlow` results all have that source.
- `TestDataFlowConfigSubclassFiltersSink` — mirror, on the sink side.
- `TestTaintTrackingConfigBarrier` — declare a subclass with a non-trivial `isBarrier`; assert flows through the barrier are excluded.
- A regression assertion: an empty-override subclass (`isSource = none()`) returns zero rows.

## Acceptance criteria

- [ ] `hasFlow` is a method on `Configuration` (or equivalent); the body references `this.isSource`/`this.isSink`.
- [ ] The new tests pass.
- [ ] `bridge/compat_dataflow_test.go`, `bridge/compat_tainttracking_test.go`, and any query goldens under `testdata/` that referenced module-level `hasFlow` still pass (migrate call sites if needed).
- [ ] `go test ./...` green.

## Risks and open questions

- The engine may not currently dispatch `this.<predicate>()` through a subclass override in all cases. Verify with a smoke test before writing the bridge code. If the engine has a gap here, that's a bigger problem than this plan can solve and should be raised as its own engine-side plan.
- CodeQL's exact `isBarrier` / `isSanitizer` semantics in taint tracking differ from straight dataflow — confirm against docs before coding step 4.
- Performance: inlining the `isSource`/`isSink` filters into the closure evaluation may change planner behaviour. Monitor test runtimes; if significant, carve out a follow-up for planner hints.

## Out of scope

- Changes to `LocalFlowStar` / `TaintAlert` / the underlying flow engine.
- New flow-step kinds beyond what CodeQL's base classes expose.
- Path-query output formatting (`PathGraph`, SARIF hops) — that's plan 10 territory.
