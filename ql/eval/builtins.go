package eval

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// regexCache memoises compiled regexp.Regexp by pattern string. All three
// regex builtins (regexpMatch, regexpFind, regexpReplaceAll) compile the
// same pattern on every invocation per binding row in their hot loop —
// a query like `regexpMatch(x, "^foo.*bar$")` over N bindings paid N
// regexp.Compile calls before this cache.
//
// sync.Map fits the read-mostly access pattern: after the first row of a
// query, every subsequent row hits the read-only map with no locking.
// Concurrency safety is provided by sync.Map itself; *regexp.Regexp is
// documented as safe for concurrent use by multiple goroutines.
//
// We do NOT cache compile errors — a malformed pattern is rare and cheap
// to re-fail on; caching errors would also keep their (possibly large)
// error messages alive forever.
//
// Unbounded growth: pattern strings come from user queries. We only insert
// into the cache when the pattern arg is a compile-time StringConst — i.e.
// fixed by the query text itself, bounded by query size. Per-binding
// dynamic patterns (a Var resolved from a relation) bypass the cache and
// compile directly via regexp.Compile, so a query that derives patterns
// from N rows cannot blow the cache to N entries. See the per-builtin
// call sites below for the routing.
var regexCache sync.Map // map[string]*regexp.Regexp

// cachedRegexp returns a compiled *regexp.Regexp for pattern, reusing a
// previously compiled instance if one exists. Returns the same (re, err)
// shape as regexp.Compile. Only call this when the pattern is bounded
// (compile-time constant in the query) — see compileRegexp for the
// routing helper that enforces this.
func cachedRegexp(pattern string) (*regexp.Regexp, error) {
	if v, ok := regexCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	// LoadOrStore: if a concurrent caller already inserted, use theirs.
	// Discarding our compiled re is cheap; consistency matters more.
	if actual, loaded := regexCache.LoadOrStore(pattern, re); loaded {
		return actual.(*regexp.Regexp), nil
	}
	return re, nil
}

// compileRegexp routes compilation through the cache only when the pattern
// is a compile-time constant in the query (datalog.StringConst). For any
// other term shape — typically a Var resolved from a binding row — the
// pattern is data-driven and may be distinct per row; caching it would
// give the cache an attacker-controllable / data-controllable size. In
// that case we compile directly without inserting into the cache.
func compileRegexp(arg datalog.Term, pattern string) (*regexp.Regexp, error) {
	if _, isConst := arg.(datalog.StringConst); isConst {
		return cachedRegexp(pattern)
	}
	return regexp.Compile(pattern)
}

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

// helper: bind or check the result variable.
//
// Invariant (shared with applyPositive's clone-skip fast path): callers
// MUST NOT mutate a binding map they don't own. The clone-skip in
// applyPositive shares the same `binding` (a Go map) across multiple
// output rows when a step has no free variables; bindResult therefore
// clones before writing a new variable, never mutates `b` in place.
// Any future helper that writes into a binding map must do the same.
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
		re, err := compileRegexp(atom.Args[1], pattern)
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
		re, err := compileRegexp(atom.Args[1], pattern)
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
		re, err := compileRegexp(atom.Args[1], pattern)
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
			if n == math.MinInt64 {
				continue // overflow: abs(MinInt64) not representable in int64
			}
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
