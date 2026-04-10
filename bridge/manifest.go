// Package bridge provides the QL library files mapping the fact schema to QL-visible predicates.
package bridge

import (
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
)

// CapabilityManifest describes which QL bridge classes are available and which are not.
type CapabilityManifest struct {
	Available   []AvailableClass
	Unavailable []UnavailableClass
}

// AvailableClass is a bridge class that is available in v1.
type AvailableClass struct {
	Name     string // QL class name
	Relation string // underlying schema relation name
	File     string // .qll file containing this class
}

// UnavailableClass is a bridge class that is NOT available in v1.
type UnavailableClass struct {
	Name          string // QL class name
	Reason        string // why it's unavailable
	VersionTarget string // when it's expected to be available
}

// UnavailableWarning is returned by CheckQuery for imports that reference unavailable features.
type UnavailableWarning struct {
	Import        string
	Reason        string
	VersionTarget string
}

// V1Manifest returns the capability manifest for schema v1.
func V1Manifest() *CapabilityManifest {
	return &CapabilityManifest{
		Available: []AvailableClass{
			{Name: "ASTNode", Relation: "Node", File: "tsq_base.qll"},
			{Name: "File", Relation: "File", File: "tsq_base.qll"},
			{Name: "Contains", Relation: "Contains", File: "tsq_base.qll"},
			{Name: "Function", Relation: "Function", File: "tsq_functions.qll"},
			{Name: "Parameter", Relation: "Parameter", File: "tsq_functions.qll"},
			{Name: "ParameterRest", Relation: "ParameterRest", File: "tsq_functions.qll"},
			{Name: "ParameterOptional", Relation: "ParameterOptional", File: "tsq_functions.qll"},
			{Name: "ParamIsFunctionType", Relation: "ParamIsFunctionType", File: "tsq_functions.qll"},
			{Name: "Call", Relation: "Call", File: "tsq_calls.qll"},
			{Name: "CallArg", Relation: "CallArg", File: "tsq_calls.qll"},
			{Name: "CallArgSpread", Relation: "CallArgSpread", File: "tsq_calls.qll"},
			{Name: "VarDecl", Relation: "VarDecl", File: "tsq_variables.qll"},
			{Name: "Assign", Relation: "Assign", File: "tsq_variables.qll"},
			{Name: "ExprMayRef", Relation: "ExprMayRef", File: "tsq_expressions.qll"},
			{Name: "ExprIsCall", Relation: "ExprIsCall", File: "tsq_expressions.qll"},
			{Name: "FieldRead", Relation: "FieldRead", File: "tsq_expressions.qll"},
			{Name: "FieldWrite", Relation: "FieldWrite", File: "tsq_expressions.qll"},
			{Name: "Await", Relation: "Await", File: "tsq_expressions.qll"},
			{Name: "Cast", Relation: "Cast", File: "tsq_expressions.qll"},
			{Name: "DestructureField", Relation: "DestructureField", File: "tsq_expressions.qll"},
			{Name: "ArrayDestructure", Relation: "ArrayDestructure", File: "tsq_expressions.qll"},
			{Name: "DestructureRest", Relation: "DestructureRest", File: "tsq_expressions.qll"},
			{Name: "JsxElement", Relation: "JsxElement", File: "tsq_jsx.qll"},
			{Name: "JsxAttribute", Relation: "JsxAttribute", File: "tsq_jsx.qll"},
			{Name: "ImportBinding", Relation: "ImportBinding", File: "tsq_imports.qll"},
			{Name: "ExportBinding", Relation: "ExportBinding", File: "tsq_imports.qll"},
			{Name: "ExtractError", Relation: "ExtractError", File: "tsq_errors.qll"},
			{Name: "SchemaVersion", Relation: "SchemaVersion", File: "tsq_base.qll"},
		},
		Unavailable: []UnavailableClass{
			{Name: "DataFlow", Reason: "IPA-dependent; requires inter-procedural analysis engine", VersionTarget: "v3"},
			{Name: "TaintTracking", Reason: "IPA-dependent; requires data flow framework", VersionTarget: "v3"},
			{Name: "Symbol", Reason: "relation empty in v1; symbol resolution not yet implemented", VersionTarget: "v2"},
			{Name: "FunctionSymbol", Reason: "relation empty in v1; depends on Symbol", VersionTarget: "v2"},
			{Name: "CallCalleeSym", Reason: "relation empty in v1; depends on Symbol", VersionTarget: "v2"},
			{Name: "CallResultSym", Reason: "relation empty in v1; depends on Symbol", VersionTarget: "v2"},
			{Name: "TypeFromLib", Reason: "relation empty in v1; type resolution not yet implemented", VersionTarget: "v2"},
		},
	}
}

// CheckQuery examines a list of import paths and returns warnings for any
// that reference unavailable bridge features.
func (m *CapabilityManifest) CheckQuery(imports []string) []UnavailableWarning {
	unavailMap := make(map[string]*UnavailableClass, len(m.Unavailable))
	for i := range m.Unavailable {
		unavailMap[m.Unavailable[i].Name] = &m.Unavailable[i]
	}

	var warnings []UnavailableWarning
	for _, imp := range imports {
		if u, ok := unavailMap[imp]; ok {
			warnings = append(warnings, UnavailableWarning{
				Import:        imp,
				Reason:        u.Reason,
				VersionTarget: u.VersionTarget,
			})
		}
	}
	return warnings
}

// AllRelationsCovered returns true if every schema relation has a corresponding
// bridge class (in Available) or a documented exclusion (in Unavailable).
func (m *CapabilityManifest) AllRelationsCovered() (covered bool, missing []string) {
	coveredNames := make(map[string]bool)
	for _, a := range m.Available {
		coveredNames[a.Relation] = true
	}
	for _, u := range m.Unavailable {
		coveredNames[u.Name] = true
	}

	for _, rel := range schema.Registry {
		if !coveredNames[rel.Name] {
			missing = append(missing, rel.Name)
		}
	}
	return len(missing) == 0, missing
}
