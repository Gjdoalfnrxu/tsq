package plan

import (
	"fmt"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// edgeKind distinguishes positive and negative (or aggregate) dependencies.
type edgeKind int

const (
	edgePositive edgeKind = iota
	edgeNegative          // negative literal or aggregate dependency
)

type depEdge struct {
	to   string
	kind edgeKind
}

// buildDepGraph builds the predicate dependency graph.
// Returns: adjacency list (from → []depEdge), and the set of all predicates.
func buildDepGraph(rules []datalog.Rule) (map[string][]depEdge, map[string]bool) {
	adj := map[string][]depEdge{}
	preds := map[string]bool{}

	for _, rule := range rules {
		head := rule.Head.Predicate
		preds[head] = true
		if _, ok := adj[head]; !ok {
			adj[head] = nil
		}
		for _, lit := range rule.Body {
			if lit.Cmp != nil {
				continue // comparison — no predicate dependency
			}
			if lit.Agg != nil {
				// Aggregate body predicates are negative-style dependencies.
				for _, bodyLit := range lit.Agg.Body {
					if bodyLit.Cmp != nil || bodyLit.Agg != nil {
						continue
					}
					dep := bodyLit.Atom.Predicate
					preds[dep] = true
					adj[head] = append(adj[head], depEdge{to: dep, kind: edgeNegative})
				}
				continue
			}
			dep := lit.Atom.Predicate
			preds[dep] = true
			kind := edgePositive
			if !lit.Positive {
				kind = edgeNegative
			}
			adj[head] = append(adj[head], depEdge{to: dep, kind: kind})
		}
	}
	return adj, preds
}

// tarjan implements Tarjan's SCC algorithm.
type tarjanState struct {
	index   int
	stack   []string
	onStack map[string]bool
	indices map[string]int
	lowlink map[string]int
	sccs    [][]string
}

func tarjanSCCs(adj map[string][]depEdge, preds map[string]bool) [][]string {
	ts := &tarjanState{
		onStack: map[string]bool{},
		indices: map[string]int{},
		lowlink: map[string]int{},
	}
	for p := range preds {
		if _, visited := ts.indices[p]; !visited {
			ts.strongConnect(p, adj)
		}
	}
	return ts.sccs
}

func (ts *tarjanState) strongConnect(v string, adj map[string][]depEdge) {
	ts.indices[v] = ts.index
	ts.lowlink[v] = ts.index
	ts.index++
	ts.stack = append(ts.stack, v)
	ts.onStack[v] = true

	for _, e := range adj[v] {
		w := e.to
		if _, visited := ts.indices[w]; !visited {
			ts.strongConnect(w, adj)
			if ts.lowlink[w] < ts.lowlink[v] {
				ts.lowlink[v] = ts.lowlink[w]
			}
		} else if ts.onStack[w] {
			if ts.indices[w] < ts.lowlink[v] {
				ts.lowlink[v] = ts.indices[w]
			}
		}
	}

	if ts.lowlink[v] == ts.indices[v] {
		// Pop SCC
		var scc []string
		for {
			w := ts.stack[len(ts.stack)-1]
			ts.stack = ts.stack[:len(ts.stack)-1]
			ts.onStack[w] = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		ts.sccs = append(ts.sccs, scc)
	}
}

// stratify assigns rules to strata in evaluation order (dependencies first).
// Returns strata as slices of rules, ordered so that each stratum's dependencies
// are in earlier strata.
func stratify(rules []datalog.Rule) ([][]datalog.Rule, []error) {
	adj, preds := buildDepGraph(rules)
	sccs := tarjanSCCs(adj, preds)

	// Check for negation within SCCs (unstratifiable).
	// Build a set for each SCC.
	sccOf := map[string]int{} // predicate → SCC index
	for i, scc := range sccs {
		for _, p := range scc {
			sccOf[p] = i
		}
	}

	var errs []error
	for _, scc := range sccs {
		sccSet := map[string]bool{}
		for _, p := range scc {
			sccSet[p] = true
		}
		if len(scc) <= 1 {
			// Self-loop check for single-node SCC.
			p := scc[0]
			for _, e := range adj[p] {
				if e.to == p && e.kind == edgeNegative {
					errs = append(errs, fmt.Errorf("unstratifiable: predicate %s has recursive negation", p))
				}
			}
			continue
		}
		// Multi-node SCC: any negative edge within it is an error.
		for _, p := range scc {
			for _, e := range adj[p] {
				if e.kind == edgeNegative && sccSet[e.to] {
					errs = append(errs, fmt.Errorf("unstratifiable: predicate %s has recursive negation", p))
				}
			}
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}

	// Topological sort of SCCs.
	// Tarjan's output is reverse topological order — dependencies come later in sccs.
	// We want evaluation order: dependencies first, so reverse.
	n := len(sccs)
	ordered := make([][]string, n)
	for i, scc := range sccs {
		ordered[n-1-i] = scc
	}

	// Assign stratum numbers per SCC.
	// Start with the topological order. For negative edges crossing SCC boundaries,
	// ensure the dependency SCC has a strictly lower stratum than the using SCC.
	sccOfPred := map[string]int{}
	for i, scc := range ordered {
		for _, p := range scc {
			sccOfPred[p] = i
		}
	}

	stratum := make([]int, n) // stratum number per ordered SCC index

	// Propagate stratum numbers: for each rule, if the using predicate's SCC
	// has a negative edge to a dependency SCC, the dep must have a stratum
	// strictly less than the using SCC.
	// We do a simple iterative pass.
	changed := true
	for changed {
		changed = false
		for _, rule := range rules {
			head := rule.Head.Predicate
			headSCC := sccOfPred[head]
			for _, lit := range rule.Body {
				if lit.Cmp != nil {
					continue
				}
				if lit.Agg != nil {
					for _, bodyLit := range lit.Agg.Body {
						if bodyLit.Cmp != nil || bodyLit.Agg != nil {
							continue
						}
						depPred := bodyLit.Atom.Predicate
						depSCC := sccOfPred[depPred]
						if depSCC != headSCC && stratum[headSCC] <= stratum[depSCC] {
							stratum[headSCC] = stratum[depSCC] + 1
							changed = true
						}
					}
					continue
				}
				depPred := lit.Atom.Predicate
				depSCC := sccOfPred[depPred]
				if depSCC == headSCC {
					continue
				}
				if !lit.Positive {
					// Negative dependency: dep must be strictly lower.
					if stratum[headSCC] <= stratum[depSCC] {
						stratum[headSCC] = stratum[depSCC] + 1
						changed = true
					}
				} else {
					// Positive dependency: dep must be <= (at most equal).
					if stratum[headSCC] < stratum[depSCC] {
						stratum[headSCC] = stratum[depSCC]
						changed = true
					}
				}
			}
		}
	}

	// Group SCCs by stratum number, then sort by stratum.
	maxStratum := 0
	for _, s := range stratum {
		if s > maxStratum {
			maxStratum = s
		}
	}

	// Build predicate → stratum map.
	predStratum := map[string]int{}
	for i, scc := range ordered {
		for _, p := range scc {
			predStratum[p] = stratum[i]
		}
	}

	// Group rules by stratum.
	stratumRules := make([][]datalog.Rule, maxStratum+1)
	for _, rule := range rules {
		s := predStratum[rule.Head.Predicate]
		stratumRules[s] = append(stratumRules[s], rule)
	}

	// Remove empty strata.
	var result [][]datalog.Rule
	for _, sr := range stratumRules {
		if len(sr) > 0 {
			result = append(result, sr)
		}
	}

	// Predicates that only appear in bodies (base facts) → stratum 0.
	// They don't have rules, so they're implicitly at stratum 0. No action needed.

	return result, nil
}
