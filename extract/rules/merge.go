package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// MergeSystemRules returns a new Program that contains both the user-written
// rules (from prog) and the given system rules. The original program is not
// modified.
func MergeSystemRules(prog *datalog.Program, systemRules []datalog.Rule) *datalog.Program {
	merged := &datalog.Program{
		Query: prog.Query,
		Rules: make([]datalog.Rule, 0, len(prog.Rules)+len(systemRules)),
	}
	merged.Rules = append(merged.Rules, systemRules...)
	merged.Rules = append(merged.Rules, prog.Rules...)
	return merged
}
