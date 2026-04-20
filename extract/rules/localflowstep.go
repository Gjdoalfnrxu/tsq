package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// LocalFlowStepRules returns the system Datalog rules for the value-flow
// Phase C PR2 intra-procedural step layer (see
// docs/design/valueflow-phase-c-plan.md §1.3).
//
// Each `lfs*` predicate models one syntactic carrier of a single intra-
// procedural value-flow edge from `from` (the value source expression) to
// `to` (the consuming expression). The top-level `LocalFlowStep(from, to)`
// union folds the eleven kinds into one relation that PR3's `flowStep`
// will compose with `interFlowStep`, and that PR4's recursive
// `mayResolveTo` will close over.
//
// # Path erasure (PR2 vs plan §1.3)
//
// Plan §1.3 sketches each step at arity (from, to, path). The `path`
// column is the field-sensitivity carrier and is intentionally deferred
// to PR5 (`feat(valueflow): field-sensitive access-path composition`).
// PR2 ships path-erased: arity-2 heads, no `pathCompose`, no field-name
// matching on `lfsFieldRead` / `lfsFieldWrite` / `lfsObjectLiteralStore`.
// Per-kind rules that the plan keys by field name (destructure, field
// read/write, object-literal store) still emit one `LocalFlowStep` row
// per (from, to) pair regardless of field name — the over-approximation
// PR5 tightens by reintroducing path matching.
//
// This is the same posture the §4.3 contract calls out: the layer is a
// refutation tool. PR2's path-erased version produces strictly more
// edges than PR5's path-sensitive version; PR5 narrows but never adds.
//
// # IDB heads for testability
//
// Each `lfs*` is its own named IDB head (not registered in the schema —
// the planner accepts unregistered IDB names; only `mustNamedLiteral` /
// schema consumers care about registration). Keeping them named lets
// the budget test in valueflow_budget_test.go evaluate per-kind row
// counts on real fixtures and assert the plan §8.1 per-step-kind unit
// invariant: every kind that the fixture corpus actually exercises
// produces non-zero rows. The path through the union (`LocalFlowStep`)
// would mask a per-kind regression where one kind silently emits zero
// while another picks up the slack.
//
// # No-recursion, no-inter
//
// PR2 deliberately ships no recursion (PR4) and no inter-procedural
// step kinds (PR3). `lfsReturnToCallSite` IS local in the §1.3 sense
// (same-module return-to-call edge) — the cross-module variant lives
// in PR3's `ifsRetToCall` against `CallTargetCrossModule`.
func LocalFlowStepRules() []datalog.Rule {
	// Capacity: N per-kind rules + N union branches. Current N = 12
	// (11 PR2 kinds + `lfsJsxPropBind` from PR8 / #202 Gap A).
	out := make([]datalog.Rule, 0, 24)
	out = append(out, lfsRules()...)
	out = append(out, localFlowStepUnion()...)
	return out
}

// lfsRules returns the twelve per-kind rules (11 PR2 kinds plus
// `lfsJsxPropBind` from PR8 / #202 Gap A). Each emits its named IDB
// head and is consumed by localFlowStepUnion.
func lfsRules() []datalog.Rule {
	return []datalog.Rule{
		// lfsAssign(from, to) :- Assign(_, from, sym), ExprMayRef(to, sym).
		// Reassignment edge: the rhs of `x = expr` flows to every later
		// expression that references `x`. Path-insensitive in PR2.
		rule("lfsAssign",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("Assign", map[string]datalog.Term{
				"rhsExpr": v("from"),
				"lhsSym":  v("sym"),
			}),
			pos("ExprMayRef", v("to"), v("sym")),
		),

		// lfsVarInit(from, to) :- VarDecl(_, sym, from, _), ExprMayRef(to, sym).
		// `const x = expr` (or `let`/`var`) flows expr to references of x.
		rule("lfsVarInit",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("VarDecl", map[string]datalog.Term{
				"sym":      v("sym"),
				"initExpr": v("from"),
			}),
			pos("ExprMayRef", v("to"), v("sym")),
		),

		// lfsParamBind(from, to) :- ParamBinding(_, _, paramSym, from),
		//                           ExprMayRef(to, paramSym).
		// Call-arg flows to references of the bound parameter inside the
		// callee body. Carve-outs handled by ParamBinding rule
		// (extract/rules/valueflow.go).
		rule("lfsParamBind",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("ParamBinding", map[string]datalog.Term{
				"paramSym": v("paramSym"),
				"argExpr":  v("from"),
			}),
			pos("ExprMayRef", v("to"), v("paramSym")),
		),

		// lfsReturnToCallSite(from, to) :- ReturnStmt(fn, _, from),
		//     CallTarget(call, fn), ExprIsCall(to, call).
		// Function return value flows back to the call-site expression.
		// Same-module via CallTarget; cross-module via CallTargetCrossModule
		// is PR3's `ifsRetToCall`.
		rule("lfsReturnToCallSite",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("ReturnStmt", map[string]datalog.Term{
				"fnId":       v("fn"),
				"returnExpr": v("from"),
			}),
			pos("CallTarget", v("call"), v("fn")),
			pos("ExprIsCall", v("to"), v("call")),
		),

		// lfsDestructureField(from, to) :- DestructureField(parent, _, _, bindSym, _),
		//     parent = from, ExprMayRef(to, bindSym).
		// Object-destructuring binding `const { foo } = obj` flows the
		// destructured parent expression to references of the bound name.
		// Path-erased PR2: drops the source-field constraint, so a binding
		// to ANY field of `parent` flows from `parent`. PR5 narrows via
		// the access-path composition.
		rule("lfsDestructureField",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("DestructureField", map[string]datalog.Term{
				"parent":  v("from"),
				"bindSym": v("bindSym"),
			}),
			pos("ExprMayRef", v("to"), v("bindSym")),
		),

		// lfsArrayDestructure(from, to) :- ArrayDestructure(parent, _, bindSym),
		//     parent = from, ExprMayRef(to, bindSym).
		// `const [x, y] = arr` — same shape as DestructureField, idx-keyed.
		// Path-erased: idx not constrained.
		rule("lfsArrayDestructure",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("ArrayDestructure", map[string]datalog.Term{
				"parent":  v("from"),
				"bindSym": v("bindSym"),
			}),
			pos("ExprMayRef", v("to"), v("bindSym")),
		),

		// lfsObjectLiteralStore(from, to) :- ObjectLiteralField(to, _, from).
		// `{ foo: x }` flows x into the object-literal expression. Path-
		// erased: field-name not constrained.
		rule("lfsObjectLiteralStore",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("ObjectLiteralField", map[string]datalog.Term{
				"parent":    v("to"),
				"valueExpr": v("from"),
			}),
		),

		// lfsSpreadElement(from, to) :- ObjectLiteralSpread(to, from).
		// `{ ...rest }` — the spread source carries (path-erased) all its
		// fields into the enclosing object expression. Schema is arity-2
		// (parent, valueExpr); plan §1.3 sketches an extra idx column we
		// don't have, which only mattered for the path version anyway.
		rule("lfsSpreadElement",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("ObjectLiteralSpread", map[string]datalog.Term{
				"parent":    v("to"),
				"valueExpr": v("from"),
			}),
		),

		// lfsFieldRead(from, to) :- FieldRead(to, baseSym, _), ExprMayRef(from, baseSym).
		// `obj.foo` reads from any expression that may write `obj`. Path-
		// erased: reads of ANY field flow from ANY expression carrying the
		// base symbol. Strictly over-approximate vs PR5; documented (plan
		// §4.3 contract — refutation tool only in PR2).
		rule("lfsFieldRead",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("FieldRead", map[string]datalog.Term{
				"expr":    v("to"),
				"baseSym": v("baseSym"),
			}),
			pos("ExprMayRef", v("from"), v("baseSym")),
		),

		// lfsFieldWrite(from, to) :- FieldWrite(_, baseSym, _, from),
		//     ExprMayRef(to, baseSym).
		// `obj.foo = expr` flows expr to expressions referencing obj. Same
		// path-erasure caveat as lfsFieldRead.
		rule("lfsFieldWrite",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("FieldWrite", map[string]datalog.Term{
				"baseSym": v("baseSym"),
				"rhsExpr": v("from"),
			}),
			pos("ExprMayRef", v("to"), v("baseSym")),
		),

		// lfsAwait(from, to) :- Await(to, from).
		// `await e` is treated as `e` (plan §5 / §1.3). Schema rel is named
		// `Await` (not `AwaitExpr` as the plan sketches).
		rule("lfsAwait",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("Await", map[string]datalog.Term{
				"expr":      v("to"),
				"innerExpr": v("from"),
			}),
		),

		// lfsJsxPropBind(from, to) :-
		//     JsxAttribute(elem, propName, from),
		//     JsxElement(elem, _, tagSym),
		//     FunctionSymbol(tagSym, fn),
		//     Parameter(fn, _, _, paramNode, _, _),
		//     ParamDestructurePattern(paramNode, patternNode),
		//     DestructureField(patternNode, propName, _, bindSym, _),
		//     ExprMayRef(to, bindSym).
		//
		// Value-flow Phase C PR8 (#202 Gap A): bridges a JSX prop's
		// value-expression at the element site to destructured-parameter uses
		// inside the component body. Field-sensitive on the prop name —
		// `DestructureField`'s `sourceField` column matches `JsxAttribute`'s
		// `name`, so `<Inner value={cfg}/>` only flows `cfg` to the
		// `value` binding inside `function Inner({value})`, not to siblings.
		//
		// Scope:
		//   - Same-module call target via `FunctionSymbol`. The cross-module
		//     variant composes via `ifsImportExport` on the tagSym ref in
		//     `MayResolveToRec` already — PR8 deliberately does not double-
		//     count it here.
		//   - Object-destructure only (the common React-FC-props shape).
		//     Array-destructure of JSX props is syntactically possible but
		//     non-idiomatic; if it shows up in Mastodon row counts, split
		//     out a sibling rule keyed on `ArrayDestructure`.
		//   - Path-erased (PR2 posture): the field match on source name is
		//     a structural precondition, not a `pathCompose` carrier. PR5's
		//     access-path layer is where field-sensitivity gets tracked
		//     through the closure proper.
		//
		// Overlap: none with `lfsParamBind`. `lfsParamBind` carves out
		// destructured slots via `not ParameterDestructured(fn, idx)` on
		// `ParamBinding` — the precise path this rule fills.
		rule("lfsJsxPropBind",
			[]datalog.Term{v("from"), v("to")},
			mustNamedLiteral("JsxAttribute", map[string]datalog.Term{
				"element":   v("elem"),
				"name":      v("propName"),
				"valueExpr": v("wrapper"),
			}),
			// `JsxAttribute.valueExpr` points at the JsxExpression `{…}`
			// punctuation wrapper (see tsq_react.qll's Provider-value
			// path). `JsxExpressionInner` bridges the wrapper to the
			// inner semantic expression so `from` anchors at the actual
			// value-carrier (e.g. the `cfg` identifier in
			// `<Inner value={cfg} />`).
			pos("JsxExpressionInner", v("wrapper"), v("from")),
			mustNamedLiteral("JsxElement", map[string]datalog.Term{
				"id":     v("elem"),
				"tagSym": v("tagSym"),
			}),
			pos("FunctionSymbol", v("tagSym"), v("fn")),
			mustNamedLiteral("Parameter", map[string]datalog.Term{
				"fn":        v("fn"),
				"paramNode": v("paramNode"),
			}),
			pos("ParamDestructurePattern", v("paramNode"), v("patternNode")),
			mustNamedLiteral("DestructureField", map[string]datalog.Term{
				"parent":      v("patternNode"),
				"sourceField": v("propName"),
				"bindSym":     v("bindSym"),
			}),
			pos("ExprMayRef", v("to"), v("bindSym")),
		),
	}
}

// localFlowStepUnion folds the eleven lfs* IDB heads into the single
// `LocalFlowStep(from, to)` relation. Eleven rules sharing one head is
// the desugared form of the plan §1.3 top-level `or` — and per the
// plan's own caveat, the `or` shape is exactly what triggered #166
// (disjunction poisoning) prior to the per-branch lifting fix.
// Multiple-rule union is the load-bearing safe form.
func localFlowStepUnion() []datalog.Rule {
	kinds := []string{
		"lfsAssign",
		"lfsVarInit",
		"lfsParamBind",
		"lfsReturnToCallSite",
		"lfsDestructureField",
		"lfsArrayDestructure",
		"lfsObjectLiteralStore",
		"lfsSpreadElement",
		"lfsFieldRead",
		"lfsFieldWrite",
		"lfsAwait",
		"lfsJsxPropBind",
	}
	out := make([]datalog.Rule, 0, len(kinds))
	head := []datalog.Term{v("from"), v("to")}
	for _, k := range kinds {
		out = append(out, rule("LocalFlowStep", head, pos(k, v("from"), v("to"))))
	}
	return out
}
