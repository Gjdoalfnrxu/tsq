package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// InterFlowStepRules returns the system Datalog rules for the value-flow
// Phase C PR3 inter-procedural step layer (see
// docs/design/valueflow-phase-c-plan.md §1.4) plus the top-level
// `FlowStep(from, to)` union (`LocalFlowStep ∪ InterFlowStep`).
//
// Each `ifs*` predicate models one syntactic carrier of a single value-flow
// edge that crosses a function or module boundary. The top-level
// `InterFlowStep(from, to)` union folds the kinds into one relation;
// `FlowStep` stitches that together with PR2's `LocalFlowStep` so PR4's
// recursive `mayResolveTo` can close over a single relation.
//
// Scope note: intra-call arg→param is `lfsParamBind`'s job (PR2, with
// proper carve-outs for spread/rest/destructured slots); inter-procedural
// call edges here are only the cross-module/RTA ones. An earlier draft of
// PR3 also shipped `ifsCallArgToParam` as an unfiltered same-module
// arg→param edge — it was strictly subsumed by `lfsParamBind` on the
// common path and emitted nothing useful for destructured slots, so it
// was deleted before merge (PR3 review F2).
//
// # Path erasure (PR3 vs plan §1.4)
//
// Plan §1.4 sketches each step at arity (from, to, path); PR2 already shipped
// path-erased and PR3 follows suit. Field sensitivity is PR5's
// access-path composition layer. Each `ifs*` rule below ships arity-2.
//
// # IDB heads for testability
//
// Same posture as PR2: each `ifs*` is its own named IDB head so the
// per-kind regression guard in valueflow_budget_test.go can assert
// non-zero rows on real fixtures (PR1 / PR2 outcome precedent — log-only
// counts are not a guard, and floor=1 misses partial regressions).
//
// # The three kinds
//
//   - ifsRetToCall — callee `return e` → call-site expression, where the
//     callee resolved through one import/export hop. Consumes
//     `CallTargetCrossModule` (PR1) — the placeholder bridge wrapper added
//     in PR1 lands its first user here.
//   - ifsImportExport — symbol-level bridge: a reference to an imported
//     local sym flows from the matching exported sym in the source module.
//     Name-keyed only; over-bridges on name collisions (same posture as
//     `CallTargetCrossModule` and the bridge's `importedFunctionSymbol`).
//   - ifsCallTargetRTA — return-to-call edge resolved via RTA (Rapid Type
//     Analysis) for method dispatch the static CallTarget can't pin down.
//     Strictly weaker than `ifsRetToCall`'s same-module
//     `lfsReturnToCallSite` PR2 sibling; ships as a separate kind so RTA
//     blow-up can be measured (and disabled) independently.
//
// # No recursion
//
// PR3 still ships zero recursion. The closure into `mayResolveTo` is PR4.
// Bridge authors can manually depth-unroll over `FlowStep` in the interim.
func InterFlowStepRules() []datalog.Rule {
	// Capacity = ifsRules + interFlowStepUnion (one branch per kind) +
	// flowStepUnion (2 branches). Derived from the kind count to avoid
	// magic per-component numbers that drift when a kind is added.
	kinds := 3
	out := make([]datalog.Rule, 0, kinds+kinds+2)
	out = append(out, ifsRules()...)
	out = append(out, interFlowStepUnion()...)
	out = append(out, flowStepUnion()...)
	return out
}

// ifsRules returns the per-kind rules. Each emits its named IDB head
// and is consumed by interFlowStepUnion.
func ifsRules() []datalog.Rule {
	return []datalog.Rule{
		// ifsRetToCall(from, to) :-
		//     CallTargetCrossModule(call, fn),
		//     ReturnStmt(fn, _, from),
		//     ExprIsCall(to, call).
		//
		// Cross-module return-to-call edge. Consumes PR1's pre-joined
		// `CallTargetCrossModule` so the closure body avoids the
		// CallCalleeSym × ImportBinding × ExportBinding × FunctionSymbol
		// 4-table join at every step. This is the rule the PR1 bridge
		// comment in tsq_callgraph.qll points at — PR3 lands the class
		// wrapper alongside this consumer.
		//
		// PR4 follow-up: rare local-collision case where a function is both
		// directly defined AND imported under an alias in the same module
		// could surface a duplicate edge against `lfsReturnToCallSite` on
		// the same `(from, to)` pair. Out of scope for PR3 (set semantics
		// dedupe at the union); flagged for PR4 fixture coverage.
		rule("ifsRetToCall",
			[]datalog.Term{v("from"), v("to")},
			pos("CallTargetCrossModule", v("call"), v("fn")),
			mustNamedLiteral("ReturnStmt", map[string]datalog.Term{
				"fnId":       v("fn"),
				"returnExpr": v("from"),
			}),
			pos("ExprIsCall", v("to"), v("call")),
		),

		// ifsImportExport(from, to) :-
		//     ImportBinding(localSym, _, name),
		//     ExportBinding(name, exportedSym, _),
		//     ExprMayRef(from, exportedSym),
		//     ExprMayRef(to, localSym).
		//
		// Symbol-level bridge: any expression in the source module that
		// references the exported sym flows to any expression in the
		// importing module that references the matching local sym. Name-
		// keyed; same over-bridging caveat as CallTargetCrossModule
		// (plan §3.2 / §4.1 — fixing requires a real module resolver).
		//
		// Plan §1.4 sketched `to` as the local sym id; corrected here to
		// match plan §1.1's expression-ID contract for `from`/`to` (two
		// `ExprMayRef` joins). PR4 must verify this kind doesn't dominate
		// the closure on Mastodon — cardinality grows with import-side
		// reference frequency.
		rule("ifsImportExport",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("ImportBinding", map[string]datalog.Term{
				"localSym":     v("localSym"),
				"importedName": v("name"),
			}),
			mustNamedLiteral("ExportBinding", map[string]datalog.Term{
				"exportedName": v("name"),
				"localSym":     v("exportedSym"),
			}),
			pos("ExprMayRef", v("from"), v("exportedSym")),
			pos("ExprMayRef", v("to"), v("localSym")),
		),

		// ifsCallTargetRTA(from, to) :-
		//     CallTargetRTA(call, fn),
		//     ReturnStmt(fn, _, from),
		//     ExprIsCall(to, call).
		//
		// Method dispatch via RTA when the static CallTarget can't pin a
		// unique fn. Same shape as lfsReturnToCallSite but consumes the
		// noisier RTA target relation. Kept distinct from
		// lfsReturnToCallSite so per-fixture row counts can show whether
		// RTA is contributing (and let a future precision-dial flag
		// disable just this kind without touching the same-module path).
		rule("ifsCallTargetRTA",
			[]datalog.Term{v("from"), v("to")},
			pos("CallTargetRTA", v("call"), v("fn")),
			mustNamedLiteral("ReturnStmt", map[string]datalog.Term{
				"fnId":       v("fn"),
				"returnExpr": v("from"),
			}),
			pos("ExprIsCall", v("to"), v("call")),
		),
	}
}

// interFlowStepUnion folds the ifs* IDB heads into one
// `InterFlowStep(from, to)` relation. Same per-branch lifting shape as
// `localFlowStepUnion` (the #166 disjunction-poisoning workaround):
// multiple-rule union, never inline `or`.
func interFlowStepUnion() []datalog.Rule {
	kinds := []string{
		"ifsRetToCall",
		"ifsImportExport",
		"ifsCallTargetRTA",
	}
	out := make([]datalog.Rule, 0, len(kinds))
	head := []datalog.Term{v("from"), v("to")}
	for _, k := range kinds {
		out = append(out, rule("InterFlowStep", head, pos(k, v("from"), v("to"))))
	}
	return out
}

// flowStepUnion stitches PR2's LocalFlowStep and PR3's InterFlowStep into
// the single top-level `FlowStep(from, to)` relation per plan §1.1. PR4's
// recursive mayResolveTo will close over this; bridge authors that want a
// non-recursive 1-hop view can consume it directly.
//
// Two-rule union, same #166-safe shape: each branch is a literal call to
// one named IDB head, never an inline `or` of two predicates.
func flowStepUnion() []datalog.Rule {
	head := []datalog.Term{v("from"), v("to")}
	return []datalog.Rule{
		rule("FlowStep", head, pos("LocalFlowStep", v("from"), v("to"))),
		rule("FlowStep", head, pos("InterFlowStep", v("from"), v("to"))),
	}
}
