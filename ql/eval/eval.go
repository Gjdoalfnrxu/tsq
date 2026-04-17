// Package eval implements semi-naive bottom-up Datalog evaluation.
package eval

import (
	"context"
	"fmt"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// Evaluator loads base facts and evaluates a planned query.
type Evaluator struct {
	execPlan *plan.ExecutionPlan
	factDB   *db.DB
	opts     []Option
}

// NewEvaluator creates an Evaluator that will load base facts from factDB.
// Pass options (WithMaxIterations, WithMaxBindingsPerRule, WithParallel) to
// configure the underlying call to Evaluate.
func NewEvaluator(execPlan *plan.ExecutionPlan, factDB *db.DB, opts ...Option) *Evaluator {
	return &Evaluator{
		execPlan: execPlan,
		factDB:   factDB,
		opts:     opts,
	}
}

// Evaluate runs the evaluation and returns results.
func (e *Evaluator) Evaluate(ctx context.Context) (*ResultSet, error) {
	baseRels, err := loadBaseRelations(e.factDB)
	if err != nil {
		return nil, fmt.Errorf("eval: load base relations: %w", err)
	}
	return Evaluate(ctx, e.execPlan, baseRels, e.opts...)
}

// LoadBaseRelations is the exported alias for loadBaseRelations. It is used
// by the trivial-IDB pre-pass (see EstimateNonRecursiveIDBSizes and its
// caller in cmd/tsq/main.go) which needs base relations BEFORE the main
// Evaluate() call. The pre-pass and Evaluate() then share the same loaded
// map; loading is idempotent but duplicating the work would needlessly read
// the fact DB twice.
func LoadBaseRelations(factDB *db.DB) (map[string]*Relation, error) {
	return loadBaseRelations(factDB)
}

// loadBaseRelations converts a db.DB into a map of eval.Relation objects.
// It iterates all registered schema relations and loads any that are present
// in the DB.
func loadBaseRelations(factDB *db.DB) (map[string]*Relation, error) {
	rels := make(map[string]*Relation)

	for _, def := range schema.Registry {
		dbRel := factDB.Relation(def.Name)
		// db.Relation always returns a (possibly empty) Relation for registered names.
		n := dbRel.Tuples()
		if n == 0 {
			// Create an empty relation so lookups work correctly.
			rels[def.Name] = NewRelation(def.Name, len(def.Columns))
			continue
		}

		rel := NewRelation(def.Name, len(def.Columns))
		for ti := 0; ti < n; ti++ {
			t := make(Tuple, len(def.Columns))
			for ci, colDef := range def.Columns {
				switch colDef.Type {
				case schema.TypeInt32, schema.TypeEntityRef:
					v, err := dbRel.GetInt(ti, ci)
					if err != nil {
						return nil, fmt.Errorf("relation %q tuple %d col %d: %w", def.Name, ti, ci, err)
					}
					t[ci] = IntVal{V: int64(v)}
				case schema.TypeString:
					s, err := dbRel.GetString(factDB, ti, ci)
					if err != nil {
						return nil, fmt.Errorf("relation %q tuple %d col %d: %w", def.Name, ti, ci, err)
					}
					t[ci] = StrVal{V: s}
				default:
					return nil, fmt.Errorf("relation %q col %d: unknown column type %d", def.Name, ci, colDef.Type)
				}
			}
			rel.Add(t)
		}
		rels[def.Name] = rel
	}

	return rels, nil
}
