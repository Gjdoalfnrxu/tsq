package bridge

import (
	"testing"
)

// TestV1ManifestAvailableCount checks the expected number of available classes.
func TestV1ManifestAvailableCount(t *testing.T) {
	m := V1Manifest()
	// v2: 28 original + 5 promoted from unavailable + 12 new v2 = 45
	// But some relations share bridge classes. Count: 28 + 17 = 45
	// v3 Phase 3c: +2 bridge classes (Type, SymbolTypeBinding) = 84
	// v3 Phase 3d: +1 bridge class (NonTaintableType) = 85
	if got := len(m.Available); got != 85 {
		t.Errorf("expected 85 available classes, got %d", got)
	}
}

// TestV1ManifestUnavailableCount checks the expected number of unavailable classes.
func TestV1ManifestUnavailableCount(t *testing.T) {
	m := V1Manifest()
	// v2 Phase 2c: all classes now available (TaintTracking moved from unavailable)
	if got := len(m.Unavailable); got != 0 {
		t.Errorf("expected 0 unavailable classes, got %d", got)
	}
}

// TestAllRelationsCovered verifies every schema relation is accounted for.
func TestAllRelationsCovered(t *testing.T) {
	m := V1Manifest()
	covered, missing := m.AllRelationsCovered()
	if !covered {
		t.Errorf("manifest does not cover all schema relations; missing: %v", missing)
	}
}

// TestCheckQueryNoWarnings verifies that importing available classes produces no warnings.
func TestCheckQueryNoWarnings(t *testing.T) {
	m := V1Manifest()
	warnings := m.CheckQuery([]string{"ASTNode", "Function", "Call"})
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d", len(warnings))
	}
}

// TestAvailableClassesHaveFiles verifies every available class references a .qll file.
func TestAvailableClassesHaveFiles(t *testing.T) {
	m := V1Manifest()
	for _, a := range m.Available {
		if a.File == "" {
			t.Errorf("available class %q has no file", a.Name)
		}
		if a.Relation == "" {
			t.Errorf("available class %q has no relation", a.Name)
		}
	}
}

// TestUnavailableClassesHaveReasons verifies every unavailable class has a reason.
func TestUnavailableClassesHaveReasons(t *testing.T) {
	m := V1Manifest()
	for _, u := range m.Unavailable {
		if u.Reason == "" {
			t.Errorf("unavailable class %q has no reason", u.Name)
		}
		if u.VersionTarget == "" {
			t.Errorf("unavailable class %q has no version target", u.Name)
		}
	}
}

// TestManifestAvailableNamesUnique verifies no duplicate available class names.
func TestManifestAvailableNamesUnique(t *testing.T) {
	m := V1Manifest()
	seen := make(map[string]bool)
	for _, a := range m.Available {
		if seen[a.Name] {
			t.Errorf("duplicate available class name: %q", a.Name)
		}
		seen[a.Name] = true
	}
}
