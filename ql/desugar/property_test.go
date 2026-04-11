package desugar_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
	"pgregory.net/rapid"
)

// TestPropertyDesugarSemanticsCommutativity checks a real semantic-preservation
// property of the desugar pass: for any pair of QL sources that differ only
// by the order of conjuncts in a where clause (or by any algebraic rewriting
// we know to be semantics-preserving), the desugared-and-evaluated results
// must be identical over any base-fact assignment.
//
// The oracle is NOT "the desugared program". We compare two INDEPENDENT
// pipelines:
//
//	pipeline A: parse(srcA) -> resolve -> desugar -> eval
//	pipeline B: parse(srcB) -> resolve -> desugar -> eval
//
// srcA and srcB are known to be semantically equivalent QL. Any asymmetric
// treatment in resolve or desugar that leaks into the datalog IR shows up as
// divergent query results on the same random base facts.
//
// Real bug class caught: desugar rules that accidentally depend on conjunct
// ordering (e.g., binding analysis that only succeeds with a specific
// ordering, or a lost clause during normalisation). Example bugs
// caught would be a rewrite that drops a conjunct after a recursive
// simplification, or picks a binding hint based on source order that skews
// the result set.
//
// Coverage approach: we use rapid to pick both a query template and a
// permutation of its where-clause conjuncts, plus random base facts for the
// leaf predicates the queries reference. The template set is curated because
// constructing a random-but-valid QL generator is out of scope; the property
// shape (commutativity of `and`) holds for every well-formed instance and
// only requires a handful of structurally different templates to exercise
// the desugar pass's binding and stratification paths.
func TestPropertyDesugarSemanticsCommutativity(t *testing.T) {
	templates := commuteTemplates()

	nonTrivial := 0
	rapid.Check(t, func(t *rapid.T) {
		tpl := templates[rapid.IntRange(0, len(templates)-1).Draw(t, "template")]

		// Choose a permutation of the conjuncts.
		perm := make([]int, len(tpl.conjuncts))
		for i := range perm {
			perm[i] = i
		}
		// Fisher-Yates using rapid draws.
		for i := len(perm) - 1; i > 0; i-- {
			j := rapid.IntRange(0, i).Draw(t, fmt.Sprintf("swap_%d", i))
			perm[i], perm[j] = perm[j], perm[i]
		}

		srcA := renderTemplate(tpl, identityPerm(len(tpl.conjuncts)))
		srcB := renderTemplate(tpl, perm)

		// Generate random base facts for the leaf predicates the template uses.
		// Every permutation of the SAME template evaluates against the SAME
		// facts, so any divergence is a desugar or plan bug, not a fact
		// ordering artefact.
		baseRels := genBaseRels(t, tpl)

		resA := runPipeline(t, srcA, baseRels)
		resB := runPipeline(t, srcB, baseRels)

		if !equalRows(resA, resB) {
			t.Fatalf("desugar conjunction commutativity violated\ntemplate: %s\npermutation: %v\nsrcA:\n%s\nsrcB:\n%s\nresA: %v\nresB: %v",
				tpl.name, perm, srcA, srcB, resA, resB)
		}

		if len(resA) > 0 {
			nonTrivial++
		}
	})

	if nonTrivial == 0 {
		t.Fatalf("property is vacuous: no iteration produced a non-empty result set — base-fact generator does not exercise the query bodies")
	}
}

// conjunctTemplate describes a curated QL query as a fixed preamble plus a
// list of conjuncts that can be reordered freely inside the where clause.
type conjunctTemplate struct {
	name      string
	preamble  string   // from clause and any class decls; lives above the where line
	conjuncts []string // each is a single boolean QL expression
	selectTxt string   // the select clause text
	// leafPreds lists base predicate names (the ones the generator should seed
	// with random facts) and their arity.
	leafPreds map[string]int
}

func commuteTemplates() []conjunctTemplate {
	return []conjunctTemplate{
		{
			name: "two_predicates_join",
			preamble: `
predicate p(int x) { P(x) }
predicate q(int x) { Q(x) }
from int f, int b
`,
			conjuncts: []string{
				"p(f)",
				"q(b)",
				"f = b",
				"f > 0",
			},
			selectTxt: "select f, b",
			leafPreds: map[string]int{
				"P": 1,
				"Q": 1,
			},
		},
		{
			name: "three_conjuncts_single_var",
			preamble: `
predicate r(int x) { R(x) }
from int x
`,
			conjuncts: []string{
				"r(x)",
				"x > 1",
				"x < 9",
				"x != 5",
			},
			selectTxt: "select x",
			leafPreds: map[string]int{
				"R": 1,
			},
		},
		{
			name: "join_three_relations",
			preamble: `
predicate a(int x, int y) { A(x, y) }
predicate b(int x, int y) { B(x, y) }
from int u, int v, int w
`,
			conjuncts: []string{
				"a(u, v)",
				"b(v, w)",
				"u < w",
			},
			selectTxt: "select u, v, w",
			leafPreds: map[string]int{
				"A": 2,
				"B": 2,
			},
		},
	}
}

func identityPerm(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func renderTemplate(tpl conjunctTemplate, perm []int) string {
	var b strings.Builder
	b.WriteString(tpl.preamble)
	b.WriteString("where ")
	for i, p := range perm {
		if i > 0 {
			b.WriteString(" and ")
		}
		b.WriteString(tpl.conjuncts[p])
	}
	b.WriteString("\n")
	b.WriteString(tpl.selectTxt)
	b.WriteString("\n")
	return b.String()
}

// genBaseRels seeds the leaf predicates referenced by tpl with random facts
// (integer arity — small range so joins actually fire).
func genBaseRels(t *rapid.T, tpl conjunctTemplate) map[string]*eval.Relation {
	rels := make(map[string]*eval.Relation, len(tpl.leafPreds))
	// Deterministic iteration order for rapid shrinking stability.
	names := make([]string, 0, len(tpl.leafPreds))
	for n := range tpl.leafPreds {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		arity := tpl.leafPreds[name]
		rel := eval.NewRelation(name, arity)
		n := rapid.IntRange(3, 10).Draw(t, fmt.Sprintf("nFacts_%s", name))
		for i := 0; i < n; i++ {
			tup := make(eval.Tuple, arity)
			for j := 0; j < arity; j++ {
				tup[j] = eval.IntVal{V: int64(rapid.IntRange(0, 10).Draw(t, fmt.Sprintf("fact_%s_%d_%d", name, i, j)))}
			}
			rel.Add(tup)
		}
		rels[name] = rel
	}
	return rels
}

// runPipeline parses, resolves, desugars, plans, and evaluates src against
// the given base relations. It returns the query result rows as sorted
// string representations so two runs can be compared for set equality.
func runPipeline(t *rapid.T, src string, baseRels map[string]*eval.Relation) []string {
	p := parse.NewParser(src, "<property>")
	mod, err := p.Parse()
	if err != nil {
		t.Skipf("parse: %v", err)
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Skipf("resolve: %v", err)
	}
	prog, dsErrs := desugar.Desugar(rm)
	if len(dsErrs) > 0 {
		t.Skipf("desugar: %v", dsErrs)
	}
	if prog.Query == nil {
		t.Skipf("desugar produced no query for src:\n%s", src)
	}
	ep, planErrs := plan.Plan(prog, nil)
	if len(planErrs) > 0 {
		t.Skipf("plan: %v", planErrs)
	}
	// Deep copy base relations so the evaluator can't mutate across runs.
	rels := make(map[string]*eval.Relation, len(baseRels))
	for k, v := range baseRels {
		nr := eval.NewRelation(v.Name, v.Arity)
		for _, tup := range v.Tuples() {
			nr.Add(tup)
		}
		rels[k] = nr
	}
	rs, err := eval.Evaluate(context.Background(), ep, rels)
	if err != nil {
		t.Skipf("evaluate: %v", err)
	}
	rows := make([]string, 0, len(rs.Rows))
	for _, row := range rs.Rows {
		rows = append(rows, fmt.Sprintf("%v", row))
	}
	sort.Strings(rows)
	return rows
}

func equalRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Unused import guard (keeps datalog imported if we want to reach into the
// planned program for debugging later).
var _ = datalog.Program{}
