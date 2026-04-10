package eval

import (
	"fmt"
	"strconv"
)

// Compare evaluates a comparison between two Values.
// Handles "=", "!=", "<", ">", "<=", ">=".
// IntVal vs IntVal: numeric comparison.
// StrVal vs StrVal: lexicographic comparison.
// Mixed types: "!=" returns true, "=" returns false, others return error.
func Compare(op string, left, right Value) (bool, error) {
	switch l := left.(type) {
	case IntVal:
		switch r := right.(type) {
		case IntVal:
			return compareInts(op, l.V, r.V)
		case StrVal:
			return mixedTypeComparison(op)
		default:
			return false, fmt.Errorf("unsupported value type %T", right)
		}
	case StrVal:
		switch r := right.(type) {
		case StrVal:
			return compareStrings(op, l.V, r.V)
		case IntVal:
			return mixedTypeComparison(op)
		default:
			return false, fmt.Errorf("unsupported value type %T", right)
		}
	default:
		return false, fmt.Errorf("unsupported value type %T", left)
	}
}

func compareInts(op string, l, r int64) (bool, error) {
	switch op {
	case "=":
		return l == r, nil
	case "!=":
		return l != r, nil
	case "<":
		return l < r, nil
	case ">":
		return l > r, nil
	case "<=":
		return l <= r, nil
	case ">=":
		return l >= r, nil
	default:
		return false, fmt.Errorf("unknown comparison operator %q", op)
	}
}

func compareStrings(op string, l, r string) (bool, error) {
	switch op {
	case "=":
		return l == r, nil
	case "!=":
		return l != r, nil
	case "<":
		return l < r, nil
	case ">":
		return l > r, nil
	case "<=":
		return l <= r, nil
	case ">=":
		return l >= r, nil
	default:
		return false, fmt.Errorf("unknown comparison operator %q", op)
	}
}

func mixedTypeComparison(op string) (bool, error) {
	switch op {
	case "=":
		return false, nil
	case "!=":
		return true, nil
	default:
		return false, fmt.Errorf("cannot apply %q to values of different types", op)
	}
}

// Arithmetic evaluates an arithmetic operation between two Values.
// Handles "+", "-", "*", "/", "%".
// IntVal only for numeric ops; StrVal "+" as concatenation.
func Arithmetic(op string, left, right Value) (Value, error) {
	if op == "+" {
		// Allow string concatenation.
		ls, lok := left.(StrVal)
		rs, rok := right.(StrVal)
		if lok && rok {
			return StrVal{V: ls.V + rs.V}, nil
		}
	}

	l, lok := left.(IntVal)
	r, rok := right.(IntVal)
	if !lok || !rok {
		return nil, fmt.Errorf("arithmetic operator %q requires IntVal operands (got %T, %T)", op, left, right)
	}

	switch op {
	case "+":
		return IntVal{V: l.V + r.V}, nil
	case "-":
		return IntVal{V: l.V - r.V}, nil
	case "*":
		return IntVal{V: l.V * r.V}, nil
	case "/":
		if r.V == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return IntVal{V: l.V / r.V}, nil
	case "%":
		if r.V == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		return IntVal{V: l.V % r.V}, nil
	default:
		return nil, fmt.Errorf("unknown arithmetic operator %q", op)
	}
}

// ValueToString returns a human-readable string for a Value.
func ValueToString(v Value) string {
	switch vv := v.(type) {
	case IntVal:
		return strconv.FormatInt(vv.V, 10)
	case StrVal:
		return vv.V
	default:
		return fmt.Sprintf("%v", v)
	}
}
