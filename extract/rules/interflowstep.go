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
// `InterFlowStep(from, to)` union folds the four kinds into one relation;
// `FlowStep` stitches that together with PR2's `LocalFlowStep` so PR4's
// recursive `mayResolveTo` can close over a single relation.
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
// # The four kinds
//
//   - ifsCallArgToParam — call-arg → callee parameter via direct CallTarget.
//     Same wiring as ParamBinding (extract/rules/valueflow.go), expressed
//     here as a step edge for the closure rather than a 4-arity binding row.
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
	out := make([]datalog.Rule, 0, 4+4+2)
	out = append(out, ifsRules()...)
	out = append(out, interFlowStepUnion()...)
	out = append(out, flowStepUnion()...)
	return out
}

// ifsRules returns the four per-kind rules. Each emits its named IDB head
// and is consumed by interFlowStepUnion.
func ifsRules() []datalog.Rule {
	return []datalog.Rule{
		// ifsCallArgToParam(from, to) :-
		//     CallTarget(call, fn),
		//     CallArg(call, idx, from),
		//     Parameter(fn, idx, _, _, paramSym, _),
		//     ExprMayRef(to, paramSym).
		//
		// Direct same-module call: argument expression flows to in-callee
		// references of the bound parameter. Mirrors lfsParamBind via the
		// pre-joined ParamBinding rel, but without the carve-outs (spread /
		// rest / destructured) — those are silently included here. The
		// closure in PR4 will route through ParamBinding-aware lfsParamBind
		// for the carve-outs that matter; ifsCallArgToParam is the
		// inter-procedural breadth-first analogue and intentionally less
		// filtered to keep PR4's cross-call story uniform.
		rule("ifsCallArgToParam",
			[]datalog.Term{v("from"), v("to")},
			pos("CallTarget", v("call"), v("fn")),
			mustNamedLiteral("CallArg", map[string]datalog.Term{
				"call":    v("call"),
				"idx":     v("idx"),
				"argNode": v("from"),
			}),
			mustNamedLiteral("Parameter", map[string]datalog.Term{
				"fn":  v("fn"),
				"idx": v("idx"),
				"sym": v("paramSym"),
			}),
			pos("ExprMayRef", v("to"), v("paramSym")),
		),

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

// interFlowStepUnion folds the four ifs* IDB heads into one
// `InterFlowStep(from, to)` relation. Same per-branch lifting shape as
// `localFlowStepUnion` (the #166 disjunction-poisoning workaround):
// multiple-rule union, never inline `or`.
func interFlowStepUnion() []datalog.Rule {
	kinds := []string{
		"ifsCallArgToParam",
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
