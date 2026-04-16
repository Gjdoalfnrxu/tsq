package rules

// builder.go — named-column helper for constructing datalog.Literal values.
//
// Problem: the rule files in extract/rules/*.go construct datalog.Literal with
// hardcoded positional arguments that must match the column order defined in
// extract/schema/relations.go. With 93 relations and ~120 coupling points across
// 7 rule files, any schema column reorder produces silent wrong results with no
// compile-time or runtime error.
//
// Solution: the colPos / posLit / negLit helpers accept a map of
// column-name→term, look up the schema to determine correct position, and
// assemble the []datalog.Term slice in the right order. Unspecified columns
// default to w() (Wildcard). Unknown column names panic at program startup so
// bugs surface immediately rather than producing silent incorrect output.
//
// Usage:
//
//	// Old (positional — fragile):
//	pos("Assign", w(), v("rhsExpr"), v("lhsSym"))
//
//	// New (named — safe):
//	posLit("Assign", cols{"rhsExpr": v("rhsExpr"), "lhsSym": v("lhsSym")})
//
// The named form documents intent, is resilient to column reordering, and fails
// loudly on typos (panic at init/test time via the registry lookup).

import (
	"fmt"

	"github.com/Gjdoalfnrxu/tsq/extract/schema"
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// cols is a convenience alias for the column-name→term map.
type cols = map[string]datalog.Term

// colPos builds a []datalog.Term for relation relName using namedCols.
// Columns absent from namedCols are filled with Wildcard. Panics if relName
// is not registered in schema.Registry or if any key in namedCols is not a
// valid column name for that relation.
func colPos(relName string, namedCols cols) []datalog.Term {
	def, ok := schema.Lookup(relName)
	if !ok {
		panic(fmt.Sprintf("rules: colPos: unknown relation %q", relName))
	}

	// Validate all keys in namedCols are real column names.
	colIndex := make(map[string]int, len(def.Columns))
	for i, col := range def.Columns {
		colIndex[col.Name] = i
	}
	for colName := range namedCols {
		if _, ok := colIndex[colName]; !ok {
			panic(fmt.Sprintf("rules: colPos: relation %q has no column %q", relName, colName))
		}
	}

	// Build positional args; unspecified positions → Wildcard.
	args := make([]datalog.Term, len(def.Columns))
	for i := range args {
		args[i] = datalog.Wildcard{}
	}
	for colName, term := range namedCols {
		args[colIndex[colName]] = term
	}
	return args
}

// posLit constructs a positive datalog.Literal for relName with named columns.
// Equivalent to pos(relName, colPos(relName, namedCols)...) but self-documenting.
func posLit(relName string, namedCols cols) datalog.Literal {
	return datalog.Literal{
		Positive: true,
		Atom:     datalog.Atom{Predicate: relName, Args: colPos(relName, namedCols)},
	}
}

// negLit constructs a negative datalog.Literal for relName with named columns.
func negLit(relName string, namedCols cols) datalog.Literal {
	return datalog.Literal{
		Positive: false,
		Atom:     datalog.Atom{Predicate: relName, Args: colPos(relName, namedCols)},
	}
}
