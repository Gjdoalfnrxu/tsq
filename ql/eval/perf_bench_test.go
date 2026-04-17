package eval

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// BenchmarkApplyPositive_Filter measures the clone-skip fast path: a
// step with bound args and no free vars (pure filter). Sub-change (b)
// targets this path.
//
// Setup: A(x) has N rows; B(x) is a 1-row filter. Per input binding the
// inner loop emits 1 output row, sharing the input map (no clone with
// the optimisation; one map alloc per row without it).
func BenchmarkApplyPositive_Filter(b *testing.B) {
	const N = 1000
	A := NewRelation("A", 1)
	for i := 0; i < N; i++ {
		A.Add(Tuple{IntVal{int64(i)}})
	}
	// B contains every value of A — every binding survives, exercising
	// the full filter throughput without rejection short-circuits.
	B := NewRelation("B", 1)
	for i := 0; i < N; i++ {
		B.Add(Tuple{IntVal{int64(i)}})
	}
	rels := RelsOf(A, B)

	rule := plan.PlannedRule{
		Head: datalog.Atom{Predicate: "H", Args: []datalog.Term{datalog.Var{Name: "x"}}},
		JoinOrder: []plan.JoinStep{
			{Literal: datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
			{Literal: datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Rule(rule, rels, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkApplyPositive_Join measures the standard binding-extension
// case (free vars > 0). The clone-skip optimisation should NOT regress
// this path — it goes through the same else branch as before.
func BenchmarkApplyPositive_Join(b *testing.B) {
	const N = 200
	// Edge has N edges forming chains; 2-hop join produces O(N) results.
	E := NewRelation("Edge", 2)
	for i := 0; i < N; i++ {
		E.Add(Tuple{IntVal{int64(i)}, IntVal{int64(i + 1)}})
	}
	rels := RelsOf(E)

	rule := plan.PlannedRule{
		Head: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}},
		JoinOrder: []plan.JoinStep{
			{Literal: datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}}},
			{Literal: datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}}},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Rule(rule, rels, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRegexBuiltin_Cold measures regex builtin throughput when the
// cache is empty on every iteration — i.e. forces a fresh
// regexp.Compile each time. This is the worst case for the cache (it
// adds a sync.Map miss + LoadOrStore overhead on top of the compile).
func BenchmarkRegexBuiltin_Cold(b *testing.B) {
	const N = 100
	bindings := make([]binding, N)
	for i := 0; i < N; i++ {
		bindings[i] = binding{"x": StrVal{V: fmt.Sprintf("foobar-%d", i)}}
	}
	atom := datalog.Atom{
		Predicate: "__builtin_string_regexpMatch",
		Args: []datalog.Term{
			datalog.Var{Name: "x"},
			datalog.StringConst{Value: "^foo"},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		regexCache = sync.Map{} // reset so every iteration is cold
		_ = ApplyBuiltin(atom, bindings)
	}
}

// BenchmarkRegexBuiltin_Hot measures the steady-state case: cache is
// warm from a prior call, every binding row in this call is a cache
// hit. This is the realistic case for any query that uses regex on a
// relation of more than one row.
func BenchmarkRegexBuiltin_Hot(b *testing.B) {
	const N = 100
	bindings := make([]binding, N)
	for i := 0; i < N; i++ {
		bindings[i] = binding{"x": StrVal{V: fmt.Sprintf("foobar-%d", i)}}
	}
	atom := datalog.Atom{
		Predicate: "__builtin_string_regexpMatch",
		Args: []datalog.Term{
			datalog.Var{Name: "x"},
			datalog.StringConst{Value: "^foo"},
		},
	}

	regexCache = sync.Map{}
	_ = ApplyBuiltin(atom, bindings) // warm

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ApplyBuiltin(atom, bindings)
	}
}
