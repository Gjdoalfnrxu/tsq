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
// representative fixtures. If this gate fires, the design choice is to drop
// CallTargetRTA and document the precision loss (see plan §7.3 / §1.2 carve-outs).
//
// Also surfaces the per-fixture row counts for ExprValueSource and AssignExpr
// so PR review can sanity-check the new EDB rels haven't blown up.
func TestParamBindingBudget(t *testing.T) {
	// Pick a small set of representative fixtures from testdata/projects.
	// Skip if the working directory isn't the repo root (in CI with -short the
	// subset still exercises the budget gate; standalone benches use the
	// dedicated cmd or a manual CLI run).
	repoRoot := findRepoRoot(t)
	if repoRoot == "" {
		t.Skip("repo root not found from CWD; skipping budget gate")
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
	keys := []string{"Node", "CallArg", "Parameter", "CallTarget", "ParamBinding", "ExprValueSource", "AssignExpr", "Assign"}
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
