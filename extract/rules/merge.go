package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// AllSystemRules returns all system Datalog rules: call graph + local flow + summaries + composition + taint + frameworks + higher-order + value-flow + value-flow local-step + value-flow inter-step + value-flow recursive mayResolveTo.
func AllSystemRules() []datalog.Rule {
	var all []datalog.Rule
	all = append(all, CallGraphRules()...)
	all = append(all, LocalFlowRules()...)
	all = append(all, SummaryRules()...)
	all = append(all, CompositionRules()...)
	all = append(all, TaintRules()...)
	all = append(all, FrameworkRules()...)
	all = append(all, HigherOrderRules()...)
	all = append(all, ValueFlowRules()...)
	all = append(all, LocalFlowStepRules()...)
	all = append(all, InterFlowStepRules()...)
	all = append(all, MayResolveToRules()...)
	return all
}

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
