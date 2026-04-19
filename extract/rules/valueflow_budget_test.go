package rules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract"
	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestParamBindingBudget enforces the value-flow Phase A row-count budget gate
// from the plan §7.3 — ParamBinding must not exceed 5x CallArg row count on
// representative fixtures.
//
// The gate exists specifically to catch RTA blow-up: ParamBinding's rule
// consumes both CallTarget AND CallTargetRTA (one disjunct each in
// valueflow.go), and CallTargetRTA can produce many candidate fns per call
// site. Without the RTA disjunct the gate would be decorative — empirical
// ratios on these fixtures all sit under 0.2x. With RTA wired in, the gate
// is the design's load-bearing contract for the multiplicative cost.
//
// If the gate ever fires, the design choice is to drop CallTargetRTA from
// the rule and document the precision loss (plan §7.3 / §1.2 carve-outs).
//
// Also surfaces per-fixture row counts for CallTargetRTA, ExprValueSource
// and AssignExpr so PR review can sanity-check the new EDB rels.
func TestParamBindingBudget(t *testing.T) {
	// Pick a small set of representative fixtures from testdata/projects.
	// Skip if the working directory isn't the repo root (in CI with -short the
	// subset still exercises the budget gate; standalone benches use the
	// dedicated cmd or a manual CLI run).
	repoRoot := findRepoRoot(t)
	if repoRoot == "" {
		// The budget gate is the only thing keeping ParamBinding's
		// CallTarget ∪ CallTargetRTA blow-up honest. Silently skipping when
		// run outside the repo root means CI could pass without the gate
		// ever firing — make this a hard failure instead.
		t.Fatal("repo root not found from CWD; budget gate cannot run")
	}

	fixtures := []string{
		"react-component",
		"react-usestate",
		"react-usestate-context-alias",
		"react-usestate-context-alias-r3",
		"react-usestate-prop-alias",
		"async-patterns",
		"destructuring",
		"imports",
		"full-ts-project",
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(repoRoot, "testdata", "projects", name)
			if _, err := os.Stat(dir); err != nil {
				t.Skipf("fixture not present: %s", dir)
			}
			counts := extractAndCount(t, dir)

			t.Logf("%-40s %s", name+":",
				formatCounts(counts))

			// Budget gate from plan §7.3: ParamBinding ≤ 5x CallArg.
			if counts["CallArg"] > 0 {
				ratio := float64(counts["ParamBinding"]) / float64(counts["CallArg"])
				if ratio > 5.0 {
					t.Errorf("budget gate: ParamBinding (%d) > 5x CallArg (%d) — ratio %.2f",
						counts["ParamBinding"], counts["CallArg"], ratio)
				}
			}

			// Sanity: ExprValueSource should generally be on the order of Node
			// row count divided by ~10–50 (small fraction of all AST nodes are
			// value-source kinds). Loose upper bound: ExprValueSource ≤ Node.
			if counts["ExprValueSource"] > counts["Node"] {
				t.Errorf("ExprValueSource (%d) exceeds Node count (%d) — bug in walker",
					counts["ExprValueSource"], counts["Node"])
			}
		})
	}
}

// TestCallTargetCrossModuleNonZero is a regression guard for the Phase C PR1
// CallTargetCrossModule rule. The budget test above only logs per-fixture
// counts; if a future change broke the rule body or column semantics drifted,
// the rule could silently emit zero rows on every fixture and CI would still
// pass. This test asserts that at least one of the cross-module-shaped
// fixtures (imports, destructuring, full-ts-project) yields a non-trivial
// row count. Floor is intentionally low to avoid brittleness — a true
// regression would zero out all three.
func TestCallTargetCrossModuleNonZero(t *testing.T) {
	repoRoot := findRepoRoot(t)
	if repoRoot == "" {
		t.Fatal("repo root not found from CWD; regression guard cannot run")
	}

	fixtures := []string{"imports", "destructuring", "full-ts-project"}
	total := 0
	present := 0
	for _, name := range fixtures {
		dir := filepath.Join(repoRoot, "testdata", "projects", name)
		if _, err := os.Stat(dir); err != nil {
			t.Logf("fixture not present: %s", dir)
			continue
		}
		present++
		counts := extractAndCount(t, dir)
		t.Logf("%s: CallTargetCrossModule=%d", name, counts["CallTargetCrossModule"])
		total += counts["CallTargetCrossModule"]
	}
	if present == 0 {
		t.Fatal("no cross-module fixtures present")
	}
	if total < 5 {
		t.Errorf("CallTargetCrossModule regression: sum across %v = %d, want >= 5", fixtures, total)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

func extractAndCount(t *testing.T, projectDir string) map[string]int {
	t.Helper()
	database := db.NewDB()
	walker := extract.NewTypeAwareWalker(database)
	backend := &extract.TreeSitterBackend{}
	if err := walker.Run(context.Background(), backend, extract.ProjectConfig{RootDir: projectDir}); err != nil {
		t.Fatalf("walker.Run: %v", err)
	}
	backend.Close()

	counts := map[string]int{}
	for _, name := range []string{"Node", "CallArg", "Parameter", "ExprValueSource", "AssignExpr", "Assign"} {
		if r := database.Relation(name); r != nil {
			counts[name] = r.Tuples()
		}
	}

	// Evaluate ParamBinding via system rules.
	baseRels := dbToRelations(database)
	pbCount, err := evalCount(baseRels, "ParamBinding", 4)
	if err != nil {
		t.Fatalf("eval ParamBinding: %v", err)
	}
	counts["ParamBinding"] = pbCount

	ctCount, err := evalCount(baseRels, "CallTarget", 2)
	if err != nil {
		t.Fatalf("eval CallTarget: %v", err)
	}
	counts["CallTarget"] = ctCount

	rtaCount, err := evalCount(baseRels, "CallTargetRTA", 2)
	if err != nil {
		t.Fatalf("eval CallTargetRTA: %v", err)
	}
	counts["CallTargetRTA"] = rtaCount

	xmodCount, err := evalCount(baseRels, "CallTargetCrossModule", 2)
	if err != nil {
		t.Fatalf("eval CallTargetCrossModule: %v", err)
	}
	counts["CallTargetCrossModule"] = xmodCount
	return counts
}

func dbToRelations(database *db.DB) map[string]*eval.Relation {
	out := map[string]*eval.Relation{}
	for _, def := range schema.Registry {
		r := database.Relation(def.Name)
		if r == nil {
			out[def.Name] = eval.NewRelation(def.Name, def.Arity())
			continue
		}
		er := eval.NewRelation(def.Name, def.Arity())
		for i := 0; i < r.Tuples(); i++ {
			row := make(eval.Tuple, def.Arity())
			for c := 0; c < def.Arity(); c++ {
				if def.Columns[c].Type == schema.TypeString {
					s, _ := r.GetString(database, i, c)
					row[c] = eval.StrVal{V: s}
				} else {
					v, _ := r.GetInt(i, c)
					row[c] = eval.IntVal{V: int64(v)}
				}
			}
			er.Add(row)
		}
		out[def.Name] = er
	}
	return out
}

func evalCount(baseRels map[string]*eval.Relation, pred string, arity int) (int, error) {
	terms := make([]datalog.Term, arity)
	for i := range terms {
		terms[i] = datalog.Var{Name: "x" + string(rune('0'+i))}
	}
	query := &datalog.Query{
		Select: terms,
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: terms}},
		},
	}
	prog := &datalog.Program{Rules: AllSystemRules(), Query: query}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		return 0, errs[0]
	}
	rs, err := eval.Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		return 0, err
	}
	return len(rs.Rows), nil
}

func formatCounts(c map[string]int) string {
	keys := []string{"Node", "CallArg", "Parameter", "CallTarget", "CallTargetRTA", "CallTargetCrossModule", "ParamBinding", "ExprValueSource", "AssignExpr", "Assign"}
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(itoa(c[k]))
		b.WriteString(" ")
	}
	return b.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	buf := make([]byte, 0, 12)
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
