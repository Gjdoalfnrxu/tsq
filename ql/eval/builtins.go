package eval

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// builtinFunc evaluates a built-in predicate against a set of bindings.
// It takes the atom (with args) and current bindings, and returns extended bindings.
type builtinFunc func(atom datalog.Atom, bindings []binding) []binding

// builtinRegistry maps __builtin_* predicate names to their Go implementations.
var builtinRegistry = map[string]builtinFunc{
	"__builtin_string_length":      builtinStringLength,
	"__builtin_string_toUpperCase": builtinStringToUpperCase,
	"__builtin_string_toLowerCase": builtinStringToLowerCase,
	"__builtin_string_trim":        builtinStringTrim,
	"__builtin_string_indexOf":     builtinStringIndexOf,
	"__builtin_string_substring":   builtinStringSubstring,
	"__builtin_string_charAt":      builtinStringCharAt,
	"__builtin_string_replaceAll":  builtinStringReplaceAll,
	"__builtin_string_matches":     builtinStringMatches,
	"__builtin_string_regexpMatch": builtinStringRegexpMatch,
	"__builtin_string_toInt":       builtinStringToInt,
	"__builtin_string_toString":    builtinStringToString,
	"__builtin_string_splitAt":     builtinStringSplitAt,

	// D1: regex/string builtins
	"__builtin_string_regexpFind":       builtinStringRegexpFind,
	"__builtin_string_regexpReplaceAll": builtinStringRegexpReplaceAll,
	"__builtin_string_prefix":           builtinStringPrefix,
	"__builtin_string_suffix":           builtinStringSuffix,

	// D3: integer builtins
	"__builtin_int_abs":           builtinIntAbs,
	"__builtin_int_bitAnd":        builtinIntBitAnd,
	"__builtin_int_bitOr":         builtinIntBitOr,
	"__builtin_int_bitXor":        builtinIntBitXor,
	"__builtin_int_bitShiftLeft":  builtinIntBitShiftLeft,
	"__builtin_int_bitShiftRight": builtinIntBitShiftRight,
}

// IsBuiltin returns true if the predicate name is a registered builtin.
func IsBuiltin(pred string) bool {
	_, ok := builtinRegistry[pred]
	return ok
}

// ApplyBuiltin evaluates a builtin predicate against the given bindings.
func ApplyBuiltin(atom datalog.Atom, bindings []binding) []binding {
	fn, ok := builtinRegistry[atom.Predicate]
	if !ok {
		return nil
	}
	return fn(atom, bindings)
}

// helper: resolve a string argument from the binding
func resolveStringArg(arg datalog.Term, b binding) (string, bool) {
	v, ok := lookupTerm(arg, b)
	if !ok {
		return "", false
	}
	sv, ok := v.(StrVal)
	if !ok {
		return "", false
	}
	return sv.V, true
}

// helper: resolve an int argument from the binding
func resolveIntArg(arg datalog.Term, b binding) (int64, bool) {
	v, ok := lookupTerm(arg, b)
	if !ok {
		return 0, false
	}
	iv, ok := v.(IntVal)
	if !ok {
		return 0, false
	}
	return iv.V, true
}

// helper: bind or check the result variable
func bindResult(arg datalog.Term, b binding, val Value) (binding, bool) {
	existing, ok := lookupTerm(arg, b)
	if ok {
		// Already bound — check equality
		eq, err := Compare("=", existing, val)
		if err != nil || !eq {
			return nil, false
		}
		return b, true
	}
	// Bind the variable
	if v, isVar := arg.(datalog.Var); isVar && v.Name != "_" {
		nb := b.clone()
		nb[v.Name] = val
		return nb, true
	}
	return b, true
}

// __builtin_string_length(this, result) — result = len(this)
func builtinStringLength(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 2 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		result := IntVal{V: int64(len(s))}
		nb, ok := bindResult(atom.Args[1], b, result)
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_toUpperCase(this, result)
func builtinStringToUpperCase(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 2 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		nb, ok := bindResult(atom.Args[1], b, StrVal{V: strings.ToUpper(s)})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_toLowerCase(this, result)
func builtinStringToLowerCase(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 2 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		nb, ok := bindResult(atom.Args[1], b, StrVal{V: strings.ToLower(s)})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_trim(this, result)
func builtinStringTrim(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 2 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		nb, ok := bindResult(atom.Args[1], b, StrVal{V: strings.TrimSpace(s)})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_indexOf(this, arg, result)
func builtinStringIndexOf(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 3 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		sub, ok := resolveStringArg(atom.Args[1], b)
		if !ok {
			continue
		}
		idx := strings.Index(s, sub)
		nb, ok := bindResult(atom.Args[2], b, IntVal{V: int64(idx)})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_substring(this, start, end, result)
func builtinStringSubstring(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 4 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		start, ok := resolveIntArg(atom.Args[1], b)
		if !ok {
			continue
		}
		end, ok := resolveIntArg(atom.Args[2], b)
		if !ok {
			continue
		}
		if start < 0 || end < start || int(end) > len(s) {
			continue
		}
		nb, ok := bindResult(atom.Args[3], b, StrVal{V: s[start:end]})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_charAt(this, idx, result)
func builtinStringCharAt(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 3 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		idx, ok := resolveIntArg(atom.Args[1], b)
		if !ok {
			continue
		}
		if idx < 0 || int(idx) >= len(s) {
			continue
		}
		nb, ok := bindResult(atom.Args[2], b, StrVal{V: string(s[idx])})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_replaceAll(this, old, new, result)
func builtinStringReplaceAll(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 4 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		old, ok := resolveStringArg(atom.Args[1], b)
		if !ok {
			continue
		}
		newStr, ok := resolveStringArg(atom.Args[2], b)
		if !ok {
			continue
		}
		nb, ok := bindResult(atom.Args[3], b, StrVal{V: strings.ReplaceAll(s, old, newStr)})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_matches(this, pattern) — predicate, no result
func builtinStringMatches(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 2 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		pattern, ok := resolveStringArg(atom.Args[1], b)
		if !ok {
			continue
		}
		// CodeQL matches uses glob-like patterns: % = any sequence, _ = any char
		// Convert to filepath.Match pattern: % → *, _ → ?
		globPat := strings.ReplaceAll(pattern, "%", "*")
		globPat = strings.ReplaceAll(globPat, "_", "?")
		matched, err := filepath.Match(globPat, s)
		if err != nil || !matched {
			continue
		}
		out = append(out, b)
	}
	return out
}

// __builtin_string_regexpMatch(this, pattern) — predicate, no result
func builtinStringRegexpMatch(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 2 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		pattern, ok := resolveStringArg(atom.Args[1], b)
		if !ok {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		if re.MatchString(s) {
			out = append(out, b)
		}
	}
	return out
}

// __builtin_string_toInt(this, result)
func builtinStringToInt(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 2 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		val, err := strconv.Atoi(s)
		if err != nil {
			continue
		}
		nb, ok := bindResult(atom.Args[1], b, IntVal{V: int64(val)})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_toString(this, result)
func builtinStringToString(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 2 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		nb, ok := bindResult(atom.Args[1], b, StrVal{V: s})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_splitAt(this, index, result) — result = this[index:]
// Predicate fails (no result) if index < 0 or index > len(this).
func builtinStringSplitAt(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 3 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		idx, ok := resolveIntArg(atom.Args[1], b)
		if !ok {
			continue
		}
		// Out-of-range: predicate fails (no result row).
		if idx < 0 || int(idx) > len(s) {
			continue
		}
		nb, ok := bindResult(atom.Args[2], b, StrVal{V: s[idx:]})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// ---- D1: String/regex builtins ----

// __builtin_string_regexpFind(this, pattern, index, offset, result)
// Find the index-th match of pattern starting at offset; return the match string.
func builtinStringRegexpFind(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 5 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		pattern, ok := resolveStringArg(atom.Args[1], b)
		if !ok {
			continue
		}
		index, ok := resolveIntArg(atom.Args[2], b)
		if !ok {
			continue
		}
		offset, ok := resolveIntArg(atom.Args[3], b)
		if !ok {
			continue
		}
		if offset < 0 || int(offset) > len(s) {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		matches := re.FindAllString(s[offset:], -1)
		if index < 0 || int(index) >= len(matches) {
			continue
		}
		nb, ok := bindResult(atom.Args[4], b, StrVal{V: matches[index]})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_regexpReplaceAll(this, pattern, replacement, result)
func builtinStringRegexpReplaceAll(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 4 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		pattern, ok := resolveStringArg(atom.Args[1], b)
		if !ok {
			continue
		}
		replacement, ok := resolveStringArg(atom.Args[2], b)
		if !ok {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		nb, ok := bindResult(atom.Args[3], b, StrVal{V: re.ReplaceAllString(s, replacement)})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_prefix(this, n, result) — return first n characters
func builtinStringPrefix(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 3 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		n, ok := resolveIntArg(atom.Args[1], b)
		if !ok {
			continue
		}
		if n < 0 || int(n) > len(s) {
			continue
		}
		nb, ok := bindResult(atom.Args[2], b, StrVal{V: s[:n]})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_string_suffix(this, n, result) — return last n characters
func builtinStringSuffix(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 3 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		s, ok := resolveStringArg(atom.Args[0], b)
		if !ok {
			continue
		}
		n, ok := resolveIntArg(atom.Args[1], b)
		if !ok {
			continue
		}
		if n < 0 || int(n) > len(s) {
			continue
		}
		nb, ok := bindResult(atom.Args[2], b, StrVal{V: s[len(s)-int(n):]})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// ---- D3: Integer builtins ----

// __builtin_int_abs(this, result)
func builtinIntAbs(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 2 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		n, ok := resolveIntArg(atom.Args[0], b)
		if !ok {
			continue
		}
		if n < 0 {
			n = -n
		}
		nb, ok := bindResult(atom.Args[1], b, IntVal{V: n})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_int_bitAnd(this, other, result)
func builtinIntBitAnd(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 3 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		a, ok := resolveIntArg(atom.Args[0], b)
		if !ok {
			continue
		}
		other, ok := resolveIntArg(atom.Args[1], b)
		if !ok {
			continue
		}
		nb, ok := bindResult(atom.Args[2], b, IntVal{V: a & other})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_int_bitOr(this, other, result)
func builtinIntBitOr(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 3 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		a, ok := resolveIntArg(atom.Args[0], b)
		if !ok {
			continue
		}
		other, ok := resolveIntArg(atom.Args[1], b)
		if !ok {
			continue
		}
		nb, ok := bindResult(atom.Args[2], b, IntVal{V: a | other})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_int_bitXor(this, other, result)
func builtinIntBitXor(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 3 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		a, ok := resolveIntArg(atom.Args[0], b)
		if !ok {
			continue
		}
		other, ok := resolveIntArg(atom.Args[1], b)
		if !ok {
			continue
		}
		nb, ok := bindResult(atom.Args[2], b, IntVal{V: a ^ other})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_int_bitShiftLeft(this, n, result)
func builtinIntBitShiftLeft(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 3 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		a, ok := resolveIntArg(atom.Args[0], b)
		if !ok {
			continue
		}
		n, ok := resolveIntArg(atom.Args[1], b)
		if !ok || n < 0 {
			continue
		}
		nb, ok := bindResult(atom.Args[2], b, IntVal{V: a << uint(n)})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// __builtin_int_bitShiftRight(this, n, result) — arithmetic right shift
func builtinIntBitShiftRight(atom datalog.Atom, bindings []binding) []binding {
	if len(atom.Args) != 3 {
		return nil
	}
	var out []binding
	for _, b := range bindings {
		a, ok := resolveIntArg(atom.Args[0], b)
		if !ok {
			continue
		}
		n, ok := resolveIntArg(atom.Args[1], b)
		if !ok || n < 0 {
			continue
		}
		nb, ok := bindResult(atom.Args[2], b, IntVal{V: a >> uint(n)})
		if ok {
			out = append(out, nb)
		}
	}
	return out
}

// FormatBuiltinError formats a diagnostic when a builtin is called with wrong arity.
func FormatBuiltinError(pred string, got int) string {
	return fmt.Sprintf("builtin %s called with %d args", pred, got)
}
