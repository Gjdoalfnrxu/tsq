package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// MergeSystemRules returns a new Program that contains both the user-written
// rules (from prog) and the given system rules. The original program is not
// modified. If prog is nil, a program containing only the system rules is returned.
func MergeSystemRules(prog *datalog.Program, systemRules []datalog.Rule) *datalog.Program {
	if prog == nil {
		return &datalog.Program{
			Rules: append([]datalog.Rule(nil), systemRules...),
		}
	}
	merged := &datalog.Program{
		Query: prog.Query,
		Rules: make([]datalog.Rule, 0, len(prog.Rules)+len(systemRules)),
	}
	merged.Rules = append(merged.Rules, systemRules...)
	merged.Rules = append(merged.Rules, prog.Rules...)
	return merged
}
