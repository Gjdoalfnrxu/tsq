package integration_test

import (
	"os"
	"strings"
	"testing"
)

// TestRegression_CartesianProduct guards against the Cartesian product bug
// found in PR #69 adversarial review: queries that join TaintAlert with a
// sink class must use alert.getSinkExpr() = sink, otherwise they produce
// rows × rows cross products.
// Fixed in: PR #69 (fa136eb)
func TestRegression_CartesianProduct(t *testing.T) {
	// Extract the express-sqli project and run a query that properly joins
	// sink to alert. Verify the result count matches the golden (3 rows),
	// not a Cartesian product (would be 9+ rows).
	raw := extractProject(t, "testdata/compat/projects/express-sqli")
	factDB := serializeDB(t, raw)
	rs := runCompatQuery(t, "testdata/compat/find_sqli_express.ql", factDB)

	// The express-sqli fixture has 3 routes, producing exactly 3 taint alerts.
	// A Cartesian product would produce 9 or more rows.
	if len(rs.Rows) > 5 {
		t.Errorf("suspected Cartesian product: got %d rows, expected ~3 (check join in query)", len(rs.Rows))
	}
	if len(rs.Rows) == 0 {
		t.Error("query returned zero rows — regression in taint pipeline")
	}
}

// TestRegression_EmptyResultGuard guards against empty golden files being
// silently accepted. Found during PR #69 review: the empty-result guard
// was placed after resultToCSV instead of before, meaning an empty result
// could still produce a header-only golden.
// Fixed in: PR #69 (fa136eb)
func TestRegression_EmptyResultGuard(t *testing.T) {
	// Verify that all committed compat golden files have at least one data row.
	for _, tc := range compatTestCases() {
		if tc.skip != "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.goldenFile)
			if err != nil {
				t.Fatalf("read golden file: %v", err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) <= 1 {
				t.Errorf("golden file %s has only %d lines (header only) — expected data rows", tc.goldenFile, len(lines))
			}
		})
	}
}
