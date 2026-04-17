package eval

import (
	"regexp"
	"sync"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// TestCachedRegexp_ReuseSameInstance verifies that two calls with the same
// pattern return the same *regexp.Regexp pointer (cache hit).
func TestCachedRegexp_ReuseSameInstance(t *testing.T) {
	regexCache = sync.Map{} // isolate from other tests
	re1, err := cachedRegexp("^foo.*$")
	if err != nil {
		t.Fatal(err)
	}
	re2, err := cachedRegexp("^foo.*$")
	if err != nil {
		t.Fatal(err)
	}
	if re1 != re2 {
		t.Fatalf("expected same *regexp.Regexp instance on cache hit, got distinct pointers")
	}
}

// TestCachedRegexp_DistinctPatterns verifies that different patterns get
// different compiled instances.
func TestCachedRegexp_DistinctPatterns(t *testing.T) {
	regexCache = sync.Map{}
	a, err := cachedRegexp("^a$")
	if err != nil {
		t.Fatal(err)
	}
	b, err := cachedRegexp("^b$")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("distinct patterns should yield distinct compiled regexps")
	}
}

// TestCachedRegexp_BadPatternNotCached: invalid patterns return an error
// and must NOT pollute the cache (so a future correct call still works).
func TestCachedRegexp_BadPatternNotCached(t *testing.T) {
	regexCache = sync.Map{}
	_, err := cachedRegexp("[")
	if err == nil {
		t.Fatalf("expected compile error for [")
	}
	if _, ok := regexCache.Load("["); ok {
		t.Fatalf("invalid pattern should not be inserted into cache")
	}
}

// TestCachedRegexp_ConcurrentSamePattern hammers the cache with goroutines
// all asking for the same pattern. Designed to surface races under -race
// and to confirm LoadOrStore deduplication.
func TestCachedRegexp_ConcurrentSamePattern(t *testing.T) {
	regexCache = sync.Map{}
	const G = 64
	var wg sync.WaitGroup
	results := make([]*regexp.Regexp, G)
	wg.Add(G)
	for i := 0; i < G; i++ {
		i := i
		go func() {
			defer wg.Done()
			re, err := cachedRegexp("^concurrent_(\\d+)$")
			if err != nil {
				t.Errorf("goroutine %d: unexpected err %v", i, err)
				return
			}
			results[i] = re
		}()
	}
	wg.Wait()
	for i := 1; i < G; i++ {
		if results[i] != results[0] {
			t.Fatalf("goroutine %d got a different *regexp.Regexp than goroutine 0", i)
		}
	}
}

// TestCachedRegexp_ConcurrentDistinctPatterns runs different patterns in
// parallel — combined with -race this surfaces sync.Map misuse.
func TestCachedRegexp_ConcurrentDistinctPatterns(t *testing.T) {
	regexCache = sync.Map{}
	patterns := []string{"^a", "^b", "^c", "^d", "^e", "^f", "^g", "^h"}
	var wg sync.WaitGroup
	for _, p := range patterns {
		for i := 0; i < 16; i++ {
			wg.Add(1)
			p := p
			go func() {
				defer wg.Done()
				if _, err := cachedRegexp(p); err != nil {
					t.Errorf("pattern %q: %v", p, err)
				}
			}()
		}
	}
	wg.Wait()
	for _, p := range patterns {
		if _, ok := regexCache.Load(p); !ok {
			t.Errorf("expected pattern %q to be cached", p)
		}
	}
}

// TestRegexpMatch_BehaviourUnchanged: end-to-end smoke that the cached
// builtin returns the same results as the original would.
func TestRegexpMatch_BehaviourUnchanged(t *testing.T) {
	regexCache = sync.Map{}
	atom := datalog.Atom{
		Predicate: "__builtin_string_regexpMatch",
		Args: []datalog.Term{
			datalog.Var{Name: "x"},
			datalog.StringConst{Value: "^foo"},
		},
	}
	bindings := []binding{
		{"x": StrVal{V: "foobar"}}, // matches
		{"x": StrVal{V: "barfoo"}}, // doesn't match
		{"x": StrVal{V: "foo"}},    // matches
	}
	out := ApplyBuiltin(atom, bindings)
	if len(out) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(out), out)
	}
}
