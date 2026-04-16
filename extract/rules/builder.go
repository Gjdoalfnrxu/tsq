package rules

import (
	"fmt"

	"github.com/Gjdoalfnrxu/tsq/extract/schema"
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// NamedLiteral builds a positive datalog.Literal for the given relation using
// a map of column-name → term instead of positional arguments. Column ordering
// is resolved at call time via schema.Lookup, so the caller is insulated from
// schema column reordering.
//
// Every column not present in cols is substituted with a Wildcard. This makes
// it safe to name only the columns that are semantically meaningful in the rule
// body and leave the rest as wildcards.
//
// Returns an error if the relation is not registered in the schema, or if any
// key in cols is not a valid column name for that relation.
//
// Example:
//
//	lit, err := NamedLiteral("Assign", map[string]datalog.Term{
//	    "rhsExpr": v("rhsExpr"),
//	    "lhsSym":  v("lhsSym"),
//	    // lhsNode is not needed — becomes Wildcard
//	})
func NamedLiteral(pred string, cols map[string]datalog.Term) (datalog.Literal, error) {
	def, ok := schema.Lookup(pred)
	if !ok {
		return datalog.Literal{}, fmt.Errorf("NamedLiteral: unknown relation %q", pred)
	}

	// Validate: every key in cols must name a real column.
	colIndex := make(map[string]int, len(def.Columns))
	for i, col := range def.Columns {
		colIndex[col.Name] = i
	}
	for name := range cols {
		if _, ok := colIndex[name]; !ok {
			return datalog.Literal{}, fmt.Errorf("NamedLiteral: relation %q has no column %q", pred, name)
		}
	}

	// Build args in schema-defined order; fill missing columns with Wildcard.
	args := make([]datalog.Term, len(def.Columns))
	for i, col := range def.Columns {
		if t, ok := cols[col.Name]; ok {
			args[i] = t
		} else {
			args[i] = datalog.Wildcard{}
		}
	}

	return datalog.Literal{
		Positive: true,
		Atom:     datalog.Atom{Predicate: pred, Args: args},
	}, nil
}

// mustNamedLiteral is like NamedLiteral but panics on error. It is safe to use
// in package-level rule constructors that run at startup with known-good inputs.
func mustNamedLiteral(pred string, cols map[string]datalog.Term) datalog.Literal {
	lit, err := NamedLiteral(pred, cols)
	if err != nil {
		panic(err)
	}
	return lit
}
