package stats

import (
	"fmt"
	"io"
	"sort"
	"time"
)

// Inspect writes a human-readable dump of s to w. If relFilter is
// non-empty, only that one relation is shown.
func Inspect(w io.Writer, s *Schema, relFilter string) {
	if s == nil {
		fmt.Fprintln(w, "<nil schema>")
		return
	}
	fmt.Fprintf(w, "tsq stats sidecar (format v%d)\n", s.FormatVersion)
	fmt.Fprintf(w, "  EDB hash: %x\n", s.EDBHash)
	fmt.Fprintf(w, "  built at: %s\n", s.BuiltAt.Format(time.RFC3339))
	fmt.Fprintf(w, "  relations: %d\n", len(s.Rels))
	fmt.Fprintf(w, "  joins:     %d\n", len(s.Joins))
	fmt.Fprintln(w)

	names := make([]string, 0, len(s.Rels))
	for n := range s.Rels {
		if relFilter == "" || n == relFilter {
			names = append(names, n)
		}
	}
	sort.Strings(names)

	for _, n := range names {
		r := s.Rels[n]
		fmt.Fprintf(w, "rel %s (arity=%d, rows=%d)\n", r.Name, r.Arity, r.RowCount)
		for _, c := range r.Cols {
			fmt.Fprintf(w, "  col[%d]: NDV=%d nullFrac=%.4f topK=%d hist=%d\n",
				c.Pos, c.NDV, c.NullFrac, len(c.TopK), len(c.HistBuckets))
			if relFilter != "" {
				for i, t := range c.TopK {
					fmt.Fprintf(w, "    topK[%d]: value=%d count=%d\n", i, t.Value, t.Count)
					if i >= 9 {
						fmt.Fprintf(w, "    ... (%d more)\n", len(c.TopK)-i-1)
						break
					}
				}
				for i, b := range c.HistBuckets {
					fmt.Fprintf(w, "    bucket[%d]: [%d, %d] count=%d\n", i, b.Lo, b.Hi, b.Count)
					if i >= 7 {
						fmt.Fprintf(w, "    ... (%d more)\n", len(c.HistBuckets)-i-1)
						break
					}
				}
			}
		}
	}

	if len(s.Joins) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "joins:")
		for _, j := range s.Joins {
			fmt.Fprintf(w, "  %s.col%d <-> %s.col%d  selectivity=%.6g distinctMatches=%d\n",
				j.LeftRel, j.LeftCol, j.RightRel, j.RightCol, j.Selectivity, j.DistinctMatches)
		}
	}
}
