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
	return v2Manifest()
}

// v2Manifest returns the capability manifest including v2 type-aware classes.
func v2Manifest() *CapabilityManifest {
	return &CapabilityManifest{
		Available: []AvailableClass{
			// v1 base
			{Name: "ASTNode", Relation: "Node", File: "tsq_base.qll"},
			{Name: "File", Relation: "File", File: "tsq_base.qll"},
			{Name: "Contains", Relation: "Contains", File: "tsq_base.qll"},
			{Name: "Function", Relation: "Function", File: "tsq_functions.qll"},
			{Name: "Parameter", Relation: "Parameter", File: "tsq_functions.qll"},
			{Name: "ParameterRest", Relation: "ParameterRest", File: "tsq_functions.qll"},
			{Name: "ParameterOptional", Relation: "ParameterOptional", File: "tsq_functions.qll"},
			{Name: "ParameterDestructured", Relation: "ParameterDestructured", File: "tsq_functions.qll"},
			// Value-flow Phase C PR8 (#202 Gap A): destructured-param slot →
			// pattern-node bridge. Populated by the walker alongside
			// `ParameterDestructured`; consumer is `lfsJsxPropBind` in
			// extract/rules/localflowstep.go. Points at tsq_functions.qll as
			// the planned QL consumer site (bridge class authored alongside
			// the Phase D react-final bridge rollout).
			{Name: "ParamDestructurePattern", Relation: "ParamDestructurePattern", File: "tsq_functions.qll"},
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
			{Name: "ObjectLiteralField", Relation: "ObjectLiteralField", File: "tsq_expressions.qll"},
			{Name: "ObjectLiteralSpread", Relation: "ObjectLiteralSpread", File: "tsq_expressions.qll"},
			{Name: "JsxElement", Relation: "JsxElement", File: "tsq_jsx.qll"},
			{Name: "JsxAttribute", Relation: "JsxAttribute", File: "tsq_jsx.qll"},
			// Value-flow Phase C PR8 (#202 Gap A): JSX `{…}` wrapper →
			// inner expression bridge used by `lfsJsxPropBind`. Points at
			// tsq_jsx.qll as the planned QL consumer site (bridge class
			// authored alongside the Phase D react-final bridge rollout).
			{Name: "JsxExpressionInner", Relation: "JsxExpressionInner", File: "tsq_jsx.qll"},
			{Name: "ImportBinding", Relation: "ImportBinding", File: "tsq_imports.qll"},
			{Name: "ExportBinding", Relation: "ExportBinding", File: "tsq_imports.qll"},
			{Name: "ExtractError", Relation: "ExtractError", File: "tsq_errors.qll"},
			{Name: "SchemaVersion", Relation: "SchemaVersion", File: "tsq_base.qll"},
			// v2: previously empty v1 relations now populated
			{Name: "Symbol", Relation: "Symbol", File: "tsq_symbols.qll"},
			{Name: "FunctionSymbol", Relation: "FunctionSymbol", File: "tsq_symbols.qll"},
			{Name: "CallCalleeSym", Relation: "CallCalleeSym", File: "tsq_calls.qll"},
			{Name: "CallResultSym", Relation: "CallResultSym", File: "tsq_calls.qll"},
			{Name: "TypeFromLib", Relation: "TypeFromLib", File: "tsq_symbols.qll"},
			// v2: new type-aware classes
			{Name: "ClassDecl", Relation: "ClassDecl", File: "tsq_types.qll"},
			{Name: "InterfaceDecl", Relation: "InterfaceDecl", File: "tsq_types.qll"},
			{Name: "Implements", Relation: "Implements", File: "tsq_types.qll"},
			{Name: "Extends", Relation: "Extends", File: "tsq_types.qll"},
			{Name: "MethodDecl", Relation: "MethodDecl", File: "tsq_types.qll"},
			{Name: "MethodCall", Relation: "MethodCall", File: "tsq_types.qll"},
			{Name: "NewExpr", Relation: "NewExpr", File: "tsq_types.qll"},
			{Name: "ExprType", Relation: "ExprType", File: "tsq_types.qll"},
			{Name: "TypeDecl", Relation: "TypeDecl", File: "tsq_types.qll"},
			{Name: "ReturnStmt", Relation: "ReturnStmt", File: "tsq_functions.qll"},
			{Name: "FunctionContains", Relation: "FunctionContains", File: "tsq_functions.qll"},
			{Name: "SymInFunction", Relation: "SymInFunction", File: "tsq_symbols.qll"},
			// v2 Phase B: call graph derived relations
			{Name: "CallTarget", Relation: "CallTarget", File: "tsq_callgraph.qll"},
			{Name: "CallTargetRTA", Relation: "CallTargetRTA", File: "tsq_callgraph.qll"},
			// Value-flow Phase C PR1: cross-module call target. Populated as a
			// system rule (extract/rules/valueflow.go); QL consumer arrives in
			// Phase C PR3 (`ifsRetToCall`). Manifest entry exists now to keep
			// `TestAllRelationsCovered` green.
			{Name: "CallTargetCrossModule", Relation: "CallTargetCrossModule", File: "tsq_callgraph.qll"},
			{Name: "Instantiated", Relation: "Instantiated", File: "tsq_callgraph.qll"},
			{Name: "MethodDeclDirect", Relation: "MethodDeclDirect", File: "tsq_callgraph.qll"},
			{Name: "MethodDeclInherited", Relation: "MethodDeclInherited", File: "tsq_callgraph.qll"},
			// v2 Phase C1: intra-procedural dataflow
			{Name: "ReturnSym", Relation: "ReturnSym", File: "tsq_functions.qll"},
			{Name: "LocalFlow", Relation: "LocalFlow", File: "tsq_dataflow.qll"},
			{Name: "LocalFlowStar", Relation: "LocalFlowStar", File: "tsq_dataflow.qll"},
			// v2 Phase C2: function-level summaries
			{Name: "ParamToReturn", Relation: "ParamToReturn", File: "tsq_summaries.qll"},
			{Name: "ParamToCallArg", Relation: "ParamToCallArg", File: "tsq_summaries.qll"},
			{Name: "ParamToFieldWrite", Relation: "ParamToFieldWrite", File: "tsq_summaries.qll"},
			{Name: "ParamToSink", Relation: "ParamToSink", File: "tsq_summaries.qll"},
			{Name: "SourceToReturn", Relation: "SourceToReturn", File: "tsq_summaries.qll"},
			{Name: "CallReturnToReturn", Relation: "CallReturnToReturn", File: "tsq_summaries.qll"},
			// v2 Phase C3: inter-procedural composition
			{Name: "InterFlow", Relation: "InterFlow", File: "tsq_composition.qll"},
			{Name: "FlowStar", Relation: "FlowStar", File: "tsq_composition.qll"},
			// v2 Phase A (value-flow): grounded base relations for non-recursive
			// mayResolveTo. QL consumers ship in Phase A PR3 (tsq_valueflow.qll);
			// the relations themselves are populated now so downstream PRs can
			// be merged independently. See docs/design/valueflow-phase-a-plan.md.
			{Name: "ExprValueSource", Relation: "ExprValueSource", File: "tsq_expressions.qll"},
			{Name: "AssignExpr", Relation: "AssignExpr", File: "tsq_variables.qll"},
			{Name: "ParamBinding", Relation: "ParamBinding", File: "tsq_calls.qll"},
			// v2 Phase C PR2 (value-flow): intra-procedural step union.
			// Populated by extract/rules/localflowstep.go; QL consumer
			// arrives in Phase C PR6 (bridge migration). Until PR6 ships,
			// no .qll declares this relation — File field points at the
			// PR6 consumption site (tsq_valueflow.qll) so the manifest
			// matches the post-PR6 reality. (PR4 review M2.)
			{Name: "LocalFlowStep", Relation: "LocalFlowStep", File: "tsq_valueflow.qll"},
			// v2 Phase C PR3 (value-flow): inter-procedural step union and
			// the top-level `FlowStep` union (LocalFlowStep ∪ InterFlowStep).
			// Populated by extract/rules/interflowstep.go. QL consumer
			// arrives in Phase C PR6; File field points at PR6 site as
			// above. (PR4 review M2.)
			{Name: "InterFlowStep", Relation: "InterFlowStep", File: "tsq_valueflow.qll"},
			{Name: "FlowStep", Relation: "FlowStep", File: "tsq_valueflow.qll"},
			// v2 Phase C PR4 (value-flow): recursive may-resolve-to closure
			// over FlowStep. Populated by extract/rules/mayresolveto.go;
			// QL consumer is `mayResolveToRec` in tsq_valueflow.qll.
			// Bridge migration to swap R1–R4 shape predicates over to this
			// recursive form is PR6.
			{Name: "MayResolveTo", Relation: "MayResolveTo", File: "tsq_valueflow.qll"},
			// v2 Phase C PR7 (value-flow): cap-hit diagnostic relation.
			// Schema surface only; automatic population from evaluator
			// *IterationCapError events is tracked as follow-up. Per
			// PR4 M2 ("For relations populated by system rules but not
			// yet consumed in any .qll, point File at the planned
			// consumer site"), File points at tsq_valueflow.qll — the
			// target consumer once the evaluator wiring lands.
			{Name: "MayResolveToCapHit", Relation: "MayResolveToCapHit", File: "tsq_valueflow.qll"},
			// v2 Phase F: framework models
			{Name: "ExpressHandler", Relation: "ExpressHandler", File: "tsq_express.qll"},
			// v2 Phase D: taint analysis
			{Name: "TaintSink", Relation: "TaintSink", File: "tsq_taint.qll"},
			{Name: "TaintSource", Relation: "TaintSource", File: "tsq_taint.qll"},
			{Name: "Sanitizer", Relation: "Sanitizer", File: "tsq_taint.qll"},
			{Name: "TaintedSym", Relation: "TaintedSym", File: "tsq_taint.qll"},
			{Name: "TaintedField", Relation: "TaintedField", File: "tsq_taint.qll"},
			{Name: "SanitizedEdge", Relation: "SanitizedEdge", File: "tsq_taint.qll"},
			{Name: "TaintAlert", Relation: "TaintAlert", File: "tsq_taint.qll"},
			// v2 Phase A3: additional taint/flow step materializers
			{Name: "AdditionalTaintStep", Relation: "AdditionalTaintStep", File: "compat_tainttracking.qll"},
			{Name: "AdditionalFlowStep", Relation: "AdditionalFlowStep", File: "compat_dataflow.qll"},
			// v3: tsgo-resolved type relations
			{Name: "ResolvedType", Relation: "ResolvedType", File: "tsq_types.qll"},
			{Name: "SymbolType", Relation: "SymbolType", File: "tsq_types.qll"},
			// v3 Phase 3c: bridge classes for tsgo-resolved types
			{Name: "Type", Relation: "ResolvedType", File: "tsq_types.qll"},
			{Name: "SymbolTypeBinding", Relation: "SymbolType", File: "tsq_types.qll"},
			// v3 Phase 3d: type-based sanitizer detection
			{Name: "NonTaintableType", Relation: "NonTaintableType", File: "tsq_types.qll"},
			// v3 Phase 17: type-fact relations
			{Name: "TypeInfo", Relation: "TypeInfo", File: "tsq_types.qll"},
			{Name: "TypeMember", Relation: "TypeMember", File: "tsq_types.qll"},
			{Name: "UnionMember", Relation: "UnionMember", File: "tsq_types.qll"},
			{Name: "IntersectionMember", Relation: "IntersectionMember", File: "tsq_types.qll"},
			{Name: "GenericInstantiation", Relation: "GenericInstantiation", File: "tsq_types.qll"},
			{Name: "TypeAlias", Relation: "TypeAlias", File: "tsq_types.qll"},
			{Name: "TypeParameter", Relation: "TypeParameter", File: "tsq_types.qll"},
			// Phase F1: expression-in-function scoping
			{Name: "ExprInFunction", Relation: "ExprInFunction", File: "tsq_functions.qll"},
			// C1: Template literal extraction
			{Name: "TemplateLiteral", Relation: "TemplateLiteral", File: "tsq_expressions.qll"},
			{Name: "TemplateElement", Relation: "TemplateElement", File: "tsq_expressions.qll"},
			{Name: "TemplateExpression", Relation: "TemplateExpression", File: "tsq_expressions.qll"},
			// C2: Enum declaration extraction
			{Name: "EnumDecl", Relation: "EnumDecl", File: "tsq_types.qll"},
			{Name: "EnumMember", Relation: "EnumMember", File: "tsq_types.qll"},
			// C5: Optional chaining and nullish coalescing
			{Name: "OptionalChain", Relation: "OptionalChain", File: "tsq_expressions.qll"},
			{Name: "NullishCoalescing", Relation: "NullishCoalescing", File: "tsq_expressions.qll"},
			// C3: Decorator extraction
			{Name: "Decorator", Relation: "Decorator", File: "tsq_types.qll"},
			// C4: Namespace/module declaration extraction
			{Name: "NamespaceDecl", Relation: "NamespaceDecl", File: "tsq_types.qll"},
			{Name: "NamespaceMember", Relation: "NamespaceMember", File: "tsq_types.qll"},
			// C6: TypeScript type guards and assertion functions
			{Name: "TypeGuard", Relation: "TypeGuard", File: "tsq_functions.qll"},
			// v2 Phase 2b: CodeQL-compatible DataFlow module
			{Name: "DataFlow::Node", Relation: "Symbol", File: "compat_dataflow.qll"},
			{Name: "DataFlow::PathNode", Relation: "Symbol", File: "compat_dataflow.qll"},
			// v2 Phase 2c: CodeQL-compatible TaintTracking module
			{Name: "TaintTracking::Configuration", Relation: "Symbol", File: "compat_tainttracking.qll"},
			// v2 Phase 2d: CodeQL-compatible security query libraries
			{Name: "Xss::XssSource", Relation: "Symbol", File: "compat_security_xss.qll"},
			{Name: "Xss::XssSink", Relation: "Symbol", File: "compat_security_xss.qll"},
			{Name: "CommandInjection::CommandInjectionSource", Relation: "Symbol", File: "compat_security_cmdi.qll"},
			{Name: "CommandInjection::CommandInjectionSink", Relation: "Symbol", File: "compat_security_cmdi.qll"},
			{Name: "SqlInjection::SqlInjectionSource", Relation: "Symbol", File: "compat_security_sqli.qll"},
			{Name: "SqlInjection::SqlInjectionSink", Relation: "Symbol", File: "compat_security_sqli.qll"},
			{Name: "PathTraversal::PathTraversalSource", Relation: "Symbol", File: "compat_security_pathtraversal.qll"},
			{Name: "PathTraversal::PathTraversalSink", Relation: "Symbol", File: "compat_security_pathtraversal.qll"},
			// v3 Phase E2: DOM class stubs
			{Name: "DOM::Element", Relation: "JsxElement", File: "compat_dom.qll"},
			{Name: "DOM::InnerHtmlWrite", Relation: "FieldWrite", File: "compat_dom.qll"},
			{Name: "DOM::DocumentWrite", Relation: "MethodCall", File: "compat_dom.qll"},
			{Name: "DOM::AttributeWrite", Relation: "FieldWrite", File: "compat_dom.qll"},
			// v3 Phase E3: Crypto and logging stubs
			{Name: "CryptographicOperation", Relation: "MethodCall", File: "compat_crypto.qll"},
			{Name: "CleartextLogging", Relation: "MethodCall", File: "compat_crypto.qll"},
			{Name: "SensitiveDataExpr", Relation: "Symbol", File: "compat_crypto.qll"},
			// v3 Phase E1: HTTP abstraction layer
			{Name: "HTTP::RequestHandler", Relation: "Symbol", File: "compat_http.qll"},
			{Name: "HTTP::ServerRequest", Relation: "Symbol", File: "compat_http.qll"},
			{Name: "HTTP::ResponseBody", Relation: "TaintSink", File: "compat_http.qll"},
			// v3 Phase E4: IO stubs
			{Name: "DatabaseAccess", Relation: "TaintSink", File: "compat_io.qll"},
			{Name: "FileSystemAccess", Relation: "Symbol", File: "compat_io.qll"},
			// v3 Phase E5: RegExp stubs
			{Name: "RegExpLiteral", Relation: "Symbol", File: "compat_regexp.qll"},
			{Name: "RegExpTerm", Relation: "Symbol", File: "compat_regexp.qll"},
		},
		Unavailable: []UnavailableClass{},
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
