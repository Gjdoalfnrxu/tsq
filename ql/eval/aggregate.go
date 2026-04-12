package eval

import (
	"fmt"
	"sort"
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

	if agg.Agg.Func == "rank" {
		// Rank is multi-tuple: for each group, sort values by the ordering
		// expression and emit one tuple per value with its 1-indexed ordinal
		// position. Uses dense ranking (no gaps on ties).
		for _, gk := range groupOrder {
			g := groupMap[gk]
			if len(g.values) == 0 {
				continue
			}
			ranks := computeRank(g.values)
			for _, r := range ranks {
				t := make(Tuple, arity)
				copy(t, g.key)
				t[arity-1] = IntVal{V: r}
				result.Add(t)
			}
		}
		return result
	}

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
		// Rank is handled as a multi-tuple aggregate in Aggregate().
		// This path should not be reached; if it is, return an error.
		return nil, fmt.Errorf("rank aggregate must be handled via computeRank, not computeAggregate")

	default:
		return nil, fmt.Errorf("unknown aggregate function %q", fn)
	}
}

// computeRank sorts values and returns their 1-indexed ordinal positions.
// Uses dense ranking: tied values share the same rank, and the next distinct
// value gets rank+1 (no gaps). For example, [10, 20, 20, 30] yields
// [1, 2, 2, 3]. Values are sorted ascending by their natural order
// (int < int, string < string lexicographically). Mixed types are sorted
// with ints before strings.
func computeRank(vals []Value) []int64 {
	type indexed struct {
		val Value
		idx int
	}
	items := make([]indexed, len(vals))
	for i, v := range vals {
		items[i] = indexed{val: v, idx: i}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return valueLess(items[i].val, items[j].val)
	})

	// Assign dense ranks: each new distinct value gets the next rank.
	ranks := make([]int64, len(vals))
	var currentRank int64 = 1
	for i, item := range items {
		if i > 0 && !valueEqual(items[i-1].val, item.val) {
			currentRank++
		}
		ranks[item.idx] = currentRank
	}
	return ranks
}

// valueLess returns true if a < b for ordering purposes.
func valueLess(a, b Value) bool {
	switch av := a.(type) {
	case IntVal:
		switch bv := b.(type) {
		case IntVal:
			return av.V < bv.V
		case StrVal:
			return true // ints sort before strings
		}
	case StrVal:
		switch bv := b.(type) {
		case IntVal:
			return false // strings sort after ints
		case StrVal:
			return av.V < bv.V
		}
	}
	return false
}

// valueEqual returns true if a and b are the same value.
func valueEqual(a, b Value) bool {
	switch av := a.(type) {
	case IntVal:
		if bv, ok := b.(IntVal); ok {
			return av.V == bv.V
		}
	case StrVal:
		if bv, ok := b.(StrVal); ok {
			return av.V == bv.V
		}
	}
	return false
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
