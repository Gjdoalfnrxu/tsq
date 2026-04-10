package eval

import (
	"context"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

func sc(s string) datalog.StringConst { return datalog.StringConst{Value: s} }

// TestBuiltinStringLength evaluates __builtin_string_length.
func TestBuiltinStringLength(t *testing.T) {
	// Fact: Data("hello"), Data("ab")
	data := makeRelation("Data", 1, StrVal{V: "hello"}, StrVal{V: "ab"})
	baseRels := map[string]*Relation{"Data": data}

	// Rule: Result(s, n) :- Data(s), __builtin_string_length(s, n).
	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("s"), v("n")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_length", v("s"), v("n")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("s"), v("n")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("s"), v("n"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rs.Rows))
	}

	// Check results
	for _, row := range rs.Rows {
		s := row[0].(StrVal).V
		n := row[1].(IntVal).V
		if s == "hello" && n != 5 {
			t.Errorf("len(\"hello\") = %d, want 5", n)
		}
		if s == "ab" && n != 2 {
			t.Errorf("len(\"ab\") = %d, want 2", n)
		}
	}
}

// TestBuiltinStringToUpperCase evaluates __builtin_string_toUpperCase.
func TestBuiltinStringToUpperCase(t *testing.T) {
	data := makeRelation("Data", 1, StrVal{V: "hello"})
	baseRels := map[string]*Relation{"Data": data}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("s"), v("u")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_toUpperCase", v("s"), v("u")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("u")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("s"), v("u"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rs.Rows))
	}
	if rs.Rows[0][0].(StrVal).V != "HELLO" {
		t.Errorf("toUpperCase(\"hello\") = %q, want \"HELLO\"", rs.Rows[0][0].(StrVal).V)
	}
}

// TestBuiltinStringToLowerCase evaluates __builtin_string_toLowerCase.
func TestBuiltinStringToLowerCase(t *testing.T) {
	data := makeRelation("Data", 1, StrVal{V: "HELLO"})
	baseRels := map[string]*Relation{"Data": data}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("r")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_toLowerCase", v("s"), v("r")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("r")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("r"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0][0].(StrVal).V != "hello" {
		t.Errorf("expected \"hello\", got %v", rs.Rows)
	}
}

// TestBuiltinStringTrim evaluates __builtin_string_trim.
func TestBuiltinStringTrim(t *testing.T) {
	data := makeRelation("Data", 1, StrVal{V: "  hello  "})
	baseRels := map[string]*Relation{"Data": data}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("r")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_trim", v("s"), v("r")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("r")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("r"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0][0].(StrVal).V != "hello" {
		t.Errorf("expected \"hello\", got %v", rs.Rows)
	}
}

// TestBuiltinStringIndexOf evaluates __builtin_string_indexOf.
func TestBuiltinStringIndexOf(t *testing.T) {
	data := makeRelation("Data", 1, StrVal{V: "hello world"})
	baseRels := map[string]*Relation{"Data": data}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("idx")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_indexOf", v("s"), sc("world"), v("idx")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("idx")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("idx"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0][0].(IntVal).V != 6 {
		t.Errorf("expected indexOf=6, got %v", rs.Rows)
	}
}

// TestBuiltinStringMatches evaluates __builtin_string_matches (glob pattern).
func TestBuiltinStringMatches(t *testing.T) {
	data := makeRelation("Data", 1,
		StrVal{V: "hello"},
		StrVal{V: "world"},
		StrVal{V: "help"},
	)
	baseRels := map[string]*Relation{"Data": data}

	// Match strings starting with "hel" using glob: hel%
	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("s")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_matches", v("s"), sc("hel%")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("s")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("s"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 2 {
		t.Fatalf("expected 2 matches for hel%%, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestBuiltinStringRegexpMatch evaluates __builtin_string_regexpMatch.
func TestBuiltinStringRegexpMatch(t *testing.T) {
	data := makeRelation("Data", 1,
		StrVal{V: "foo123"},
		StrVal{V: "bar"},
		StrVal{V: "baz456"},
	)
	baseRels := map[string]*Relation{"Data": data}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("s")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_regexpMatch", v("s"), sc("[0-9]+")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("s")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("s"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 2 {
		t.Fatalf("expected 2 regex matches, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestBuiltinStringToInt evaluates __builtin_string_toInt.
func TestBuiltinStringToInt(t *testing.T) {
	data := makeRelation("Data", 1, StrVal{V: "42"})
	baseRels := map[string]*Relation{"Data": data}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("n")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_toInt", v("s"), v("n")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("n")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("n"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0][0].(IntVal).V != 42 {
		t.Errorf("expected 42, got %v", rs.Rows)
	}
}

// TestBuiltinStringSubstring evaluates __builtin_string_substring.
func TestBuiltinStringSubstring(t *testing.T) {
	data := makeRelation("Data", 1, StrVal{V: "hello world"})
	baseRels := map[string]*Relation{"Data": data}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("r")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_substring", v("s"), ic(0), ic(5), v("r")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("r")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("r"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0][0].(StrVal).V != "hello" {
		t.Errorf("expected \"hello\", got %v", rs.Rows)
	}
}

// TestBuiltinStringReplaceAll evaluates __builtin_string_replaceAll.
func TestBuiltinStringReplaceAll(t *testing.T) {
	data := makeRelation("Data", 1, StrVal{V: "aabaa"})
	baseRels := map[string]*Relation{"Data": data}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("r")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_replaceAll", v("s"), sc("a"), sc("x"), v("r")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("r")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("r"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0][0].(StrVal).V != "xxbxx" {
		t.Errorf("expected \"xxbxx\", got %v", rs.Rows)
	}
}

// TestBuiltinStringCharAt evaluates __builtin_string_charAt.
func TestBuiltinStringCharAt(t *testing.T) {
	data := makeRelation("Data", 1, StrVal{V: "hello"})
	baseRels := map[string]*Relation{"Data": data}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("r")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_charAt", v("s"), ic(1), v("r")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("r")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("r"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0][0].(StrVal).V != "e" {
		t.Errorf("expected \"e\", got %v", rs.Rows)
	}
}

// TestBuiltinStringToString evaluates __builtin_string_toString (identity).
func TestBuiltinStringToString(t *testing.T) {
	data := makeRelation("Data", 1, StrVal{V: "hello"})
	baseRels := map[string]*Relation{"Data": data}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{{
			Rules: []plan.PlannedRule{{
				Head: head("Result", v("r")),
				JoinOrder: []plan.JoinStep{
					positiveStep("Data", v("s")),
					positiveStep("__builtin_string_toString", v("s"), v("r")),
				},
			}},
		}},
		Query: &plan.PlannedQuery{
			Select:    []datalog.Term{v("r")},
			JoinOrder: []plan.JoinStep{positiveStep("Result", v("r"))},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0][0].(StrVal).V != "hello" {
		t.Errorf("expected \"hello\", got %v", rs.Rows)
	}
}

// TestBuiltinIsBuiltin verifies IsBuiltin for known and unknown predicates.
func TestBuiltinIsBuiltin(t *testing.T) {
	if !IsBuiltin("__builtin_string_length") {
		t.Error("expected __builtin_string_length to be a builtin")
	}
	if IsBuiltin("notABuiltin") {
		t.Error("expected notABuiltin to NOT be a builtin")
	}
}
