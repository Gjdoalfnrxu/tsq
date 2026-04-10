package eval

import (
	"fmt"
	"strings"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// Aggregate evaluates a planned aggregate and returns the result relation.
// The result relation is named agg.ResultRelation and contains
// (groupKey..., aggregatedValue) tuples.
func Aggregate(agg plan.PlannedAggregate, rels map[string]*Relation) *Relation {
	// Compute bindings over the aggregate body using raw literals (no planner ordering).
	bindings := evalLiterals(agg.Agg.Body, rels)

	// Determine which column holds the aggregated value.
	aggVar := agg.Agg.Var
	aggExpr := agg.Agg.Expr // may be nil (count doesn't need it)

	// Group bindings by groupBy key.
	type group struct {
		key    []Value
		values []Value // the aggregated values within this group
	}
	groupMap := make(map[string]*group)
	groupOrder := []string{} // preserve insertion order

	for _, b := range bindings {
		// Build group key.
		key := make([]Value, len(agg.GroupByVars))
		valid := true
		for i, gv := range agg.GroupByVars {
			v, ok := b[gv.Name]
			if !ok {
				valid = false
				break
			}
			key[i] = v
		}
		if !valid {
			continue
		}
		gk := tupleKey(Tuple(key))

		// Get the aggregated value.
		var aggVal Value
		if aggExpr != nil {
			v, ok := lookupTerm(aggExpr, b)
			if !ok {
				// Try using the aggregate variable directly.
				v, ok = b[aggVar]
				if !ok {
					continue
				}
			}
			aggVal = v
		} else {
			// For count: use the aggregate variable binding.
			v, ok := b[aggVar]
			if !ok {
				continue
			}
			aggVal = v
		}

		g, exists := groupMap[gk]
		if !exists {
			g = &group{key: key}
			groupMap[gk] = g
			groupOrder = append(groupOrder, gk)
		}
		g.values = append(g.values, aggVal)
	}

	// Compute aggregate per group.
	arity := len(agg.GroupByVars) + 1
	result := NewRelation(agg.ResultRelation, arity)

	for _, gk := range groupOrder {
		g := groupMap[gk]
		aggResult, err := computeAggregate(agg.Agg.Func, g.values, agg.Agg.Separator)
		if err != nil {
			// Skip groups where aggregation fails (e.g., mixed types).
			continue
		}
		t := make(Tuple, arity)
		copy(t, g.key)
		t[arity-1] = aggResult
		result.Add(t)
	}

	return result
}

// computeAggregate applies the named aggregate function to a list of Values.
func computeAggregate(fn string, vals []Value, separator string) (Value, error) {
	if len(vals) == 0 {
		switch fn {
		case "count":
			return IntVal{V: 0}, nil
		case "strictcount", "strictsum":
			// strict variants return no result for empty sets
			return nil, fmt.Errorf("aggregate %q over empty set", fn)
		default:
			return nil, fmt.Errorf("aggregate %q over empty set", fn)
		}
	}

	switch fn {
	case "count":
		return IntVal{V: int64(len(vals))}, nil

	case "strictcount":
		return IntVal{V: int64(len(vals))}, nil

	case "min":
		result, err := asInt64(vals[0])
		if err != nil {
			return nil, fmt.Errorf("min: %w", err)
		}
		for _, v := range vals[1:] {
			iv, err := asInt64(v)
			if err != nil {
				return nil, fmt.Errorf("min: %w", err)
			}
			if iv < result {
				result = iv
			}
		}
		return IntVal{V: result}, nil

	case "max":
		result, err := asInt64(vals[0])
		if err != nil {
			return nil, fmt.Errorf("max: %w", err)
		}
		for _, v := range vals[1:] {
			iv, err := asInt64(v)
			if err != nil {
				return nil, fmt.Errorf("max: %w", err)
			}
			if iv > result {
				result = iv
			}
		}
		return IntVal{V: result}, nil

	case "sum":
		var sum int64
		for _, v := range vals {
			iv, err := asInt64(v)
			if err != nil {
				return nil, fmt.Errorf("sum: %w", err)
			}
			sum += iv
		}
		return IntVal{V: sum}, nil

	case "strictsum":
		var sum int64
		for _, v := range vals {
			iv, err := asInt64(v)
			if err != nil {
				return nil, fmt.Errorf("strictsum: %w", err)
			}
			sum += iv
		}
		return IntVal{V: sum}, nil

	case "avg":
		var sum int64
		for _, v := range vals {
			iv, err := asInt64(v)
			if err != nil {
				return nil, fmt.Errorf("avg: %w", err)
			}
			sum += iv
		}
		return IntVal{V: sum / int64(len(vals))}, nil

	case "concat":
		return concatValues(vals, separator), nil

	case "rank":
		// Ordinal rank — return position in group (1-indexed).
		// For a simple implementation, rank is just the count of values seen.
		// Full rank-within-group-by-order is more complex; this is the v1 approximation.
		return IntVal{V: int64(len(vals))}, nil

	default:
		return nil, fmt.Errorf("unknown aggregate function %q", fn)
	}
}

// concatValues concatenates string representations of values with a separator.
func concatValues(vals []Value, sep string) StrVal {
	parts := make([]string, len(vals))
	for i, v := range vals {
		switch sv := v.(type) {
		case StrVal:
			parts[i] = sv.V
		case IntVal:
			parts[i] = fmt.Sprintf("%d", sv.V)
		default:
			parts[i] = fmt.Sprintf("%v", v)
		}
	}
	return StrVal{V: strings.Join(parts, sep)}
}

func asInt64(v Value) (int64, error) {
	iv, ok := v.(IntVal)
	if !ok {
		return 0, fmt.Errorf("expected IntVal, got %T (%v)", v, v)
	}
	return iv.V, nil
}

// evalLiterals evaluates a sequence of Datalog literals (from an aggregate
// body) naively, returning all resulting bindings.
// This mirrors evalJoinSteps but works on []datalog.Literal directly,
// without planner-ordered JoinSteps.
func evalLiterals(lits []datalog.Literal, rels map[string]*Relation) []binding {
	current := []binding{make(binding)}
	for _, lit := range lits {
		if len(current) == 0 {
			return nil
		}
		if lit.Cmp != nil {
			current = applyComparison(lit.Cmp, current)
		} else if lit.Agg != nil {
			// Nested aggregate in body — skip for v1.
		} else if lit.Positive {
			current = applyPositive(lit.Atom, rels, current)
		} else {
			current = applyNegative(lit.Atom, rels, current)
		}
	}
	return current
}
