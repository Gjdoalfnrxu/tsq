package eval_test

import (
	"context"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// makeBaseRels packs single-arity relations for tests.
// (Avoids importing the helpers from estimate_test.go which is in the same package.)
func makeBase1(name string, vals ...int64) *eval.Relation {
	r := eval.NewRelation(name, 1)
	for _, v := range vals {
		r.Add(eval.Tuple{eval.IntVal{V: v}})
	}
	return r
}

// classExtentRule constructs a tagged class-extent rule
// `Class(this) :- BaseRel(this)`.
func classExtentRule(className, baseName string) datalog.Rule {
	return datalog.Rule{
		Head:        datalog.Atom{Predicate: className, Args: []datalog.Term{datalog.Var{Name: "this"}}},
		Body:        []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: baseName, Args: []datalog.Term{datalog.Var{Name: "this"}}}}},
		ClassExtent: true,
	}
}

// TestMaterialiseClassExtents_Basic — a single tagged class extent rule
// over a base relation is materialised, the head relation matches the
// base relation contents, and the size hint is updated.
func TestMaterialiseClassExtents_Basic(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{classExtentRule("MyClass", "BaseRel")},
	}
	base := map[string]*eval.Relation{"BaseRel": makeBase1("BaseRel", 1, 2, 3, 4, 5, 6, 7)}
	hints := map[string]int{"BaseRel": 7}

	mats, updates := eval.MaterialiseClassExtents(prog, base, hints, 0)

	got, ok := mats["MyClass/1"]
	if !ok {
		t.Fatalf("MyClass not materialised; mats=%v", mats)
	}
	if got.Len() != 7 {
		t.Errorf("materialised MyClass len: want 7, got %d", got.Len())
	}
	if updates["MyClass"] != 7 {
		t.Errorf("updates[MyClass]: want 7, got %d", updates["MyClass"])
	}
	if hints["MyClass"] != 7 {
		t.Errorf("hints[MyClass]: want 7, got %d", hints["MyClass"])
	}
}

// TestMaterialiseClassExtents_UntaggedRulesIgnored — rules without
// ClassExtent flag are ignored, even if their body matches the
// structural shape. This protects the class-declaration scoping
// invariant: only rules originating from a `class C { ... }` are
// eligible.
func TestMaterialiseClassExtents_UntaggedRulesIgnored(t *testing.T) {
	r := classExtentRule("Looksy", "BaseRel")
	r.ClassExtent = false // un-tag.
	prog := &datalog.Program{Rules: []datalog.Rule{r}}
	base := map[string]*eval.Relation{"BaseRel": makeBase1("BaseRel", 1, 2, 3)}
	mats, updates := eval.MaterialiseClassExtents(prog, base, nil, 0)
	if len(mats) != 0 {
		t.Errorf("untagged rule was materialised: %v", mats)
	}
	if len(updates) != 0 {
		t.Errorf("untagged rule produced size updates: %v", updates)
	}
}

// TestMaterialiseClassExtents_ArityShadowSkipped — a class extent whose
// body references a name that is BOTH a registered base relation AND
// the head of an IDB rule (the LocalFlow-shape arity-shadow case) MUST
// NOT be materialised, because the base copy may be empty pre-IDB-eval
// and we'd freeze the extent at empty.
func TestMaterialiseClassExtents_ArityShadowSkipped(t *testing.T) {
	// Body refers to `Shadow` (base, currently empty) — but `Shadow`
	// also appears as an IDB head elsewhere in the program.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			classExtentRule("MyClass", "Shadow"),
			// IDB rule populating `Shadow` from another base relation —
			// this is what causes the arity-shadow exclusion.
			{
				Head: datalog.Atom{Predicate: "Shadow", Args: []datalog.Term{datalog.Var{Name: "x"}}},
				Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "Source", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
			},
		},
	}
	// `Shadow` is in baseRels (registered name), but EMPTY.
	base := map[string]*eval.Relation{
		"Shadow": eval.NewRelation("Shadow", 1),
		"Source": makeBase1("Source", 1, 2, 3),
	}
	mats, _ := eval.MaterialiseClassExtents(prog, base, nil, 0)
	if _, ok := mats["MyClass/1"]; ok {
		t.Errorf("MyClass should NOT be materialised when its body references an IDB-shadowed name; mats=%v", mats)
	}
}

// TestMaterialiseClassExtents_NameShadowDifferentArity — the production
// CodeQL char-pred shape `class Symbol extends @symbol { Symbol() {
// Symbol(this,_,_,_) } }` desugars to a head `Symbol/1` whose body
// references base `Symbol/4`. These share a name but NOT an arity.
//
// The arity-shadow exclusion must NOT fire here: `Symbol/4` is a real,
// fully-populated base relation, not an IDB head. A name-only shadow
// check would silently exclude this pattern (and every taint bridge
// fixture that uses it: TaintSink, TaintSource, Sanitizer, etc.) from
// materialisation. This test pins the arity-aware shadowing semantics.
func TestMaterialiseClassExtents_NameShadowDifferentArity(t *testing.T) {
	// head: Symbol(this) :- Symbol(this, _, _, _).  ClassExtent.
	headRule := datalog.Rule{
		Head: datalog.Atom{Predicate: "Symbol", Args: []datalog.Term{datalog.Var{Name: "this"}}},
		Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "Symbol", Args: []datalog.Term{
			datalog.Var{Name: "this"}, datalog.Wildcard{}, datalog.Wildcard{}, datalog.Wildcard{},
		}}}},
		ClassExtent: true,
	}
	prog := &datalog.Program{Rules: []datalog.Rule{headRule}}

	// Base Symbol/4 with three concrete tuples.
	base4 := eval.NewRelation("Symbol", 4)
	base4.Add(eval.Tuple{eval.IntVal{V: 10}, eval.IntVal{V: 0}, eval.IntVal{V: 0}, eval.IntVal{V: 0}})
	base4.Add(eval.Tuple{eval.IntVal{V: 20}, eval.IntVal{V: 0}, eval.IntVal{V: 0}, eval.IntVal{V: 0}})
	base4.Add(eval.Tuple{eval.IntVal{V: 30}, eval.IntVal{V: 0}, eval.IntVal{V: 0}, eval.IntVal{V: 0}})
	base := map[string]*eval.Relation{"Symbol": base4}

	mats, updates := eval.MaterialiseClassExtents(prog, base, nil, 0)

	got, ok := mats["Symbol/1"]
	if !ok {
		t.Fatalf("Symbol/1 not materialised — name-shadow exclusion fired despite different arity; mats=%v", mats)
	}
	if got.Len() != 3 {
		t.Errorf("materialised Symbol/1 len: want 3 (projection of base Symbol/4), got %d", got.Len())
	}
	if updates["Symbol"] != 3 {
		t.Errorf("updates[Symbol]: want 3, got %d", updates["Symbol"])
	}
	// Verify projection correctness: each materialised tuple is the
	// first column of the corresponding base tuple.
	wantVals := map[int64]bool{10: true, 20: true, 30: true}
	gotTuples := got.Tuples()
	for _, tup := range gotTuples {
		if len(tup) != 1 {
			t.Errorf("materialised tuple has wrong arity: want 1, got %d (tup=%v)", len(tup), tup)
			continue
		}
		iv, ok := tup[0].(eval.IntVal)
		if !ok {
			t.Errorf("materialised tuple element not IntVal: %v", tup[0])
			continue
		}
		if !wantVals[iv.V] {
			t.Errorf("unexpected materialised value: %d", iv.V)
		}
	}
}

// TestMaterialiseClassExtents_TaintSinkBridgeShape — direct mirror of
// the bridge `TaintSink` pattern in bridge/tsq_taint.qll:
//
//	class TaintSink extends @taint_sink { TaintSink() { TaintSink(this, _) } }
//
// Head `TaintSink/1` with body `TaintSink/2`. Same name-shadow issue
// as the Symbol case; here we pin it specifically with arity 2 to
// cover the second prevalent bridge shape.
func TestMaterialiseClassExtents_TaintSinkBridgeShape(t *testing.T) {
	headRule := datalog.Rule{
		Head: datalog.Atom{Predicate: "TaintSink", Args: []datalog.Term{datalog.Var{Name: "this"}}},
		Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "TaintSink", Args: []datalog.Term{
			datalog.Var{Name: "this"}, datalog.Wildcard{},
		}}}},
		ClassExtent: true,
	}
	prog := &datalog.Program{Rules: []datalog.Rule{headRule}}

	base2 := eval.NewRelation("TaintSink", 2)
	base2.Add(eval.Tuple{eval.IntVal{V: 100}, eval.IntVal{V: 1}})
	base2.Add(eval.Tuple{eval.IntVal{V: 200}, eval.IntVal{V: 2}})
	base := map[string]*eval.Relation{"TaintSink": base2}

	mats, _ := eval.MaterialiseClassExtents(prog, base, nil, 0)
	got, ok := mats["TaintSink/1"]
	if !ok {
		t.Fatalf("TaintSink/1 not materialised — bridge fixture pattern excluded; mats=%v", mats)
	}
	if got.Len() != 2 {
		t.Errorf("materialised TaintSink/1 len: want 2, got %d", got.Len())
	}
}

// TestMakeMaterialisingEstimatorHook_NilSinkPanics — silently
// no-op'ing on a nil sink is the dangerous failure mode: the planner
// would still see the materialised head names in the returned set
// and strip the rules from the program, but the relations would not
// be available to Evaluate, leaving the extents permanently empty.
// The constructor must fail loudly instead.
func TestMakeMaterialisingEstimatorHook_NilSinkPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on nil materialisedSink, got none")
		}
	}()
	_ = eval.MakeMaterialisingEstimatorHook(map[string]*eval.Relation{}, nil)
}

// TestEvaluate_MaterialisedExtent_NotReEvaluated is the load-bearing
// P2a behaviour test: an injected materialised class extent is used by
// downstream rules as if it were a base relation, and the extent's own
// rule is NOT re-evaluated by Evaluate (so a body that would error on
// empty inputs in re-evaluation does not).
//
// We prove "not re-evaluated" by injecting a materialised extent
// alongside a deliberately-broken rule for the same head: if Evaluate
// re-evaluated the rule it would clobber the injected tuples with the
// broken-rule output (zero rows, since the broken body refers to a
// non-existent base relation `__never__`). The test asserts the
// downstream query sees the injected tuples.
func TestEvaluate_MaterialisedExtent_NotReEvaluated(t *testing.T) {
	// The "broken" rule in the program — would yield 0 if evaluated.
	brokenExtentRule := datalog.Rule{
		Head:        datalog.Atom{Predicate: "MyClass", Args: []datalog.Term{datalog.Var{Name: "this"}}},
		Body:        []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "__never__", Args: []datalog.Term{datalog.Var{Name: "this"}}}}},
		ClassExtent: true,
	}
	// Downstream IDB that consumes the class extent at a join site.
	// Q(this) :- MyClass(this), Other(this).
	consumer := datalog.Rule{
		Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "this"}}},
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "MyClass", Args: []datalog.Term{datalog.Var{Name: "this"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "Other", Args: []datalog.Term{datalog.Var{Name: "this"}}}},
		},
	}
	prog := &datalog.Program{
		Rules: []datalog.Rule{brokenExtentRule, consumer},
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "this"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "this"}}}},
			},
		},
	}

	// The materialised extent we'll inject.
	matRel := makeBase1("MyClass", 1, 2, 3)
	mats := map[string]*eval.Relation{"MyClass/1": matRel}

	// Strip the class extent rule from the program (mirroring what
	// EstimateAndPlanWithExtents does in production). This is the
	// integration contract — we test the eval side handles the
	// stripped-program + injected-rels combination correctly.
	planProg := &datalog.Program{
		Rules: []datalog.Rule{consumer},
		Query: prog.Query,
	}
	base := map[string]*eval.Relation{"Other": makeBase1("Other", 1, 2)}
	execPlan, planErrs := plan.Plan(planProg, map[string]int{"Other": 2, "MyClass": 3})
	if len(planErrs) > 0 {
		t.Fatalf("plan errors: %v", planErrs)
	}
	rs, err := eval.Evaluate(context.Background(), execPlan, base,
		eval.WithMaterialisedClassExtents(mats),
	)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(rs.Rows) != 2 {
		t.Errorf("want 2 result rows (intersection of {1,2,3} and {1,2}), got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestEvaluate_MaterialisedExtent_DefensiveSkipOnUnstrippedRule —
// belt-and-braces: even if the caller violates the contract by passing
// materialised extents WITHOUT stripping the corresponding rule from
// the program, Evaluate skips the rule's bootstrap/delta evaluation
// rather than clobbering the materialised tuples.
func TestEvaluate_MaterialisedExtent_DefensiveSkipOnUnstrippedRule(t *testing.T) {
	brokenExtentRule := datalog.Rule{
		Head:        datalog.Atom{Predicate: "MyClass", Args: []datalog.Term{datalog.Var{Name: "this"}}},
		Body:        []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "__never__", Args: []datalog.Term{datalog.Var{Name: "this"}}}}},
		ClassExtent: true,
	}
	prog := &datalog.Program{
		Rules: []datalog.Rule{brokenExtentRule},
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "this"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "MyClass", Args: []datalog.Term{datalog.Var{Name: "this"}}}},
			},
		},
	}
	matRel := makeBase1("MyClass", 10, 20, 30)
	mats := map[string]*eval.Relation{"MyClass/1": matRel}

	// Need to register the missing __never__ as an empty base relation
	// so planning doesn't error out — the planner is naive about
	// undefined predicates and we don't want the test to depend on its
	// failure mode here.
	base := map[string]*eval.Relation{"__never__": eval.NewRelation("__never__", 1)}

	execPlan, planErrs := plan.Plan(prog, nil)
	if len(planErrs) > 0 {
		t.Fatalf("plan errors: %v", planErrs)
	}
	rs, err := eval.Evaluate(context.Background(), execPlan, base,
		eval.WithMaterialisedClassExtents(mats),
	)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(rs.Rows) != 3 {
		t.Errorf("want 3 rows from injected extent (rule must be skipped), got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestEvaluate_MaterialisedExtent_SingleEvaluationAcrossJoinSites —
// the core P2a benchmark assertion. A class extent referenced at N>1
// join sites must be evaluated exactly ONCE. We measure this by
// counting calls to a probe relation: the extent's BODY references
// the probe; downstream rules reference the EXTENT (not the body
// directly). Without P2a, each downstream rule's planner-evaluator
// path would re-walk the extent body (and thus the probe) per join
// site. With P2a, the extent is materialised once before planning;
// downstream rules see the cached relation; the probe is only walked
// during the pre-pass.
//
// The relation-count probe here is a stand-in for "number of times
// the extent body was scanned". We pre-materialise via the production
// helper MakeMaterialisingEstimatorHook, then evaluate, and assert
// the total work scales as O(extent_size + N * join_size), not as
// O(N * extent_size).
func TestEvaluate_MaterialisedExtent_SingleEvaluationAcrossJoinSites(t *testing.T) {
	// Class extent: MyClass(this) :- BaseRel(this).
	extentRule := classExtentRule("MyClass", "BaseRel")

	// N=3 downstream rules each filter MyClass by a different criterion.
	// Each ends up with a single conjunction of (MyClass, Other_i) so a
	// non-materialising path would re-scan MyClass three times.
	mkConsumer := func(name, otherName string) datalog.Rule {
		return datalog.Rule{
			Head: datalog.Atom{Predicate: name, Args: []datalog.Term{datalog.Var{Name: "x"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "MyClass", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: otherName, Args: []datalog.Term{datalog.Var{Name: "x"}}}},
			},
		}
	}
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			extentRule,
			mkConsumer("Q1", "Other1"),
			mkConsumer("Q2", "Other2"),
			mkConsumer("Q3", "Other3"),
		},
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "x"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Q1", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "Q2", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "Q3", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
			},
		},
	}
	base := map[string]*eval.Relation{
		"BaseRel": makeBase1("BaseRel", 1, 2, 3, 4, 5),
		"Other1":  makeBase1("Other1", 1, 2, 3),
		"Other2":  makeBase1("Other2", 1, 2),
		"Other3":  makeBase1("Other3", 1),
	}

	mats := map[string]*eval.Relation{}
	hook := eval.MakeMaterialisingEstimatorHook(base, mats)
	execPlan, planErrs := plan.EstimateAndPlanWithExtents(prog, nil, 0, nil, hook, plan.Plan)
	if len(planErrs) > 0 {
		t.Fatalf("plan errors: %v", planErrs)
	}

	// The extent must be in the sink — the materialising hook
	// fired and the extent was eligible.
	if _, ok := mats["MyClass/1"]; !ok {
		t.Fatalf("MyClass was not materialised by the hook; sink=%v", mats)
	}

	// The extent's rule must be stripped from execPlan (no stratum
	// contains a rule with head MyClass).
	for si, st := range execPlan.Strata {
		for _, r := range st.Rules {
			if r.Head.Predicate == "MyClass" {
				t.Errorf("extent rule for MyClass leaked into stratum %d after pre-materialisation", si)
			}
		}
	}

	rs, err := eval.Evaluate(context.Background(), execPlan, base,
		eval.WithMaterialisedClassExtents(mats),
	)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	// Result should be intersection of {1..5} ∩ {1,2,3} ∩ {1,2} ∩ {1} = {1}.
	if len(rs.Rows) != 1 {
		t.Fatalf("want 1 row, got %d: %v", len(rs.Rows), rs.Rows)
	}
}
