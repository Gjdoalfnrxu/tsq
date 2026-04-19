package stats

// JoinPair declares an FK-like relation/column pair for which the
// stats compute pass should produce a JoinStats entry. See plan §1.2
// item 3.
//
// The v1 list is intentionally empty. The plan calls out CallArg/Call,
// Parameter/Function, Contains parent/child as candidates; today's
// schema doesn't yet have CallArg with the shape the plan assumes,
// and the planner consumer (PR2) is not yet wired to use the entries.
// Subsequent PRs in Phase B will populate this list as the recursive
// estimator for mayResolveTo lands.
//
// Adding a pair here is the only step needed to opt a relation in:
// the compute pass picks it up automatically and the sidecar grows
// by one block (~80 bytes).
type JoinPair struct {
	LeftRel  string
	LeftCol  int
	RightRel string
	RightCol int
}

// JoinPaired is the global declaration table.
var JoinPaired = []JoinPair{
	// Empty in v1. Examples for PR2/PR3:
	//   {LeftRel: "Parameter", LeftCol: 0, RightRel: "Function", RightCol: 0},
	//   {LeftRel: "Contains",  LeftCol: 0, RightRel: "Contains",  RightCol: 1},
}
