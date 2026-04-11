package db_test

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
	"pgregory.net/rapid"
)

// TestPropertyEncodeDecodeRoundtrip verifies that for any sequence of AddTuple
// calls against any registered relation, Encode -> ReadDB produces a DB that
// is tuple-wise identical to the original. This is an end-to-end format check:
// it catches regressions in column ordering, length prefixes, string interning,
// padding, or any other byte-level format drift between writer and reader.
//
// The oracle is "structural equality with the source DB", not "the decoded DB
// parses" — we compare every tuple column-by-column using the schema.
func TestPropertyEncodeDecodeRoundtrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Pick a non-empty subset of registry relations to exercise.
		// Use a named generator so rapid can shrink the subset.
		if len(schema.Registry) == 0 {
			t.Skip("registry empty — nothing to test")
		}

		// Generate a flag per registered relation deciding whether to populate it.
		use := make([]bool, len(schema.Registry))
		anySelected := false
		for i := range schema.Registry {
			use[i] = rapid.Bool().Draw(t, fmt.Sprintf("use_%s", schema.Registry[i].Name))
			if use[i] {
				anySelected = true
			}
		}
		if !anySelected {
			// Force at least one relation so the test exercises the writer/reader path.
			idx := rapid.IntRange(0, len(schema.Registry)-1).Draw(t, "forceIdx")
			use[idx] = true
		}

		srcDB := db.NewDB()

		// For each selected relation, generate a small batch of random tuples.
		// Keep counts low so the corpus remains fast but still produces non-empty
		// columns (a vacuous generator is forbidden by the property-test brief).
		for i, def := range schema.Registry {
			if !use[i] {
				continue
			}
			rel := srcDB.Relation(def.Name)
			nTuples := rapid.IntRange(1, 6).Draw(t, fmt.Sprintf("nTuples_%s", def.Name))
			for tu := 0; tu < nTuples; tu++ {
				vals := make([]interface{}, len(def.Columns))
				for ci, col := range def.Columns {
					switch col.Type {
					case schema.TypeInt32, schema.TypeEntityRef:
						// Small range keeps shrinking useful.
						vals[ci] = int32(rapid.IntRange(-1000, 1000).Draw(
							t, fmt.Sprintf("int_%s_%s_%d", def.Name, col.Name, tu)))
					case schema.TypeString:
						// Include empty and non-empty strings; include potentially
						// shared strings so interning is exercised both ways.
						vals[ci] = rapid.SampledFrom([]string{
							"", "a", "b", "ab", "hello", "world", "αβ", "tab\there",
							strconv.Itoa(tu),
						}).Draw(t, fmt.Sprintf("str_%s_%s_%d", def.Name, col.Name, tu))
					default:
						t.Fatalf("unhandled column type %v on %s.%s", col.Type, def.Name, col.Name)
					}
				}
				if err := rel.AddTuple(srcDB, vals...); err != nil {
					t.Fatalf("AddTuple(%s): %v", def.Name, err)
				}
			}
		}

		// Encode -> Decode
		var buf bytes.Buffer
		if err := srcDB.Encode(&buf); err != nil {
			t.Fatalf("Encode: %v", err)
		}
		data := buf.Bytes()
		decoded, err := db.ReadDB(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("ReadDB: %v", err)
		}

		// Compare every registry relation tuple-by-tuple. We use sorted serialised
		// tuples so legitimate row reorderings (which the format does not promise
		// to preserve anyway) do not cause spurious failures — but any actual
		// value-level drift fails immediately.
		for _, def := range schema.Registry {
			srcRel := srcDB.Relation(def.Name)
			gotRel := decoded.Relation(def.Name)

			srcTuples := dumpTuples(t, srcDB, srcRel, def)
			gotTuples := dumpTuples(t, decoded, gotRel, def)
			sort.Strings(srcTuples)
			sort.Strings(gotTuples)

			if len(srcTuples) != len(gotTuples) {
				t.Fatalf("relation %s: tuple count mismatch: src=%d decoded=%d\n  src: %s\n  got: %s",
					def.Name, len(srcTuples), len(gotTuples),
					strings.Join(srcTuples, "|"), strings.Join(gotTuples, "|"))
			}
			for k := range srcTuples {
				if srcTuples[k] != gotTuples[k] {
					t.Fatalf("relation %s row %d mismatch:\n  src: %q\n  got: %q",
						def.Name, k, srcTuples[k], gotTuples[k])
				}
			}
		}
	})
}

// dumpTuples serialises each tuple in rel as "col0|col1|..." using the schema
// to dispatch per-column accessors. Mirrors compat_test.go:serializeTuples.
func dumpTuples(t *rapid.T, database *db.DB, rel *db.Relation, def schema.RelationDef) []string {
	n := rel.Tuples()
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		parts := make([]string, 0, len(def.Columns))
		for j, col := range def.Columns {
			switch col.Type {
			case schema.TypeInt32, schema.TypeEntityRef:
				v, err := rel.GetInt(i, j)
				if err != nil {
					t.Fatalf("GetInt(%s,%d,%d): %v", def.Name, i, j, err)
				}
				parts = append(parts, strconv.FormatInt(int64(v), 10))
			case schema.TypeString:
				v, err := rel.GetString(database, i, j)
				if err != nil {
					t.Fatalf("GetString(%s,%d,%d): %v", def.Name, i, j, err)
				}
				// Escape pipe to avoid parse ambiguity in error output.
				parts = append(parts, strings.ReplaceAll(v, "|", `\|`))
			}
		}
		out = append(out, strings.Join(parts, "|"))
	}
	return out
}
