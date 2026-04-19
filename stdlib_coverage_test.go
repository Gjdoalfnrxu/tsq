package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/bridge"
)

// stdlibCoverageAllowlist lists manifest classes that are NOT expected to appear
// in any testdata/compat/*.ql file, with justification for each.
//
// When adding a new class to the manifest, either:
//   - add a query in testdata/compat/ that references it, OR
//   - add an entry here explaining why coverage is deferred.
var stdlibCoverageAllowlist = map[string]string{
	// Internal structural relations — used implicitly by other classes, not
	// referenced directly in user-facing queries.
	"ASTNode":           "base structural class; queries use concrete subclasses",
	"File":              "structural; referenced implicitly via getFile()",
	"Contains":          "structural parent-child relation; not queried directly",
	"SchemaVersion":     "internal versioning relation",
	"ExtractError":      "internal error reporting; not a query target",
	"Symbol":            "base symbol class; queries use DataFlow::Node",
	"FunctionSymbol":    "internal; queries use Function",
	"TypeFromLib":       "internal type resolution plumbing",
	"ExprMayRef":        "internal expression-reference link",
	"ExprIsCall":        "internal expression classification",
	"SymInFunction":     "internal symbol scoping relation",
	"ResolvedType":      "internal type resolution; queries use Type",
	"SymbolType":        "internal type binding; queries use SymbolTypeBinding",
	"SymbolTypeBinding": "internal type binding plumbing",
	"NonTaintableType":  "internal sanitizer detection; not queried directly",

	// v3 Phase 17: type-fact relations — structural type information for advanced queries.
	"TypeInfo":             "type metadata; queried via Type class methods",
	"TypeMember":           "tsgo-enriched; requires type resolution for memberTypeId",
	"UnionMember":          "union constituents; queried via UnionType.getMember()",
	"IntersectionMember":   "intersection constituents; queried via IntersectionType.getMember()",
	"GenericInstantiation": "generic type args; queried via GenericType.getInstantiation()",
	"TypeAlias":            "type alias resolution; queried via TypeAlias.getAliasedType()",
	"TypeParameter":        "generic type params; queried via GenericDecl.getTypeParameter()",

	// Call/parameter detail relations — used implicitly by Function/Call.
	"Parameter":             "accessed via Function.getAParameter(), not standalone",
	"ParameterRest":         "parameter modifier; not queried directly",
	"ParameterOptional":     "parameter modifier; not queried directly",
	"ParameterDestructured": "parameter modifier; ParamBinding carve-out flag; not queried directly",
	"ParamIsFunctionType":   "parameter type info; not queried directly",
	"CallArg":               "accessed via Call methods; not queried directly",
	"CallArgSpread":         "call argument modifier; not queried directly",
	"Call":                  "implicit via compat bridges; coverage_probe.ql added",
	"Assign":                "assignment relation; coverage_probe.ql added",
	"FieldRead":             "field access; coverage_probe.ql added",
	"FieldWrite":            "field write; coverage_probe.ql added",
	"Await":                 "await expression; coverage_probe.ql added",
	"Cast":                  "type cast; coverage_probe.ql added",
	"DestructureField":      "destructuring; coverage_probe.ql added",
	"ArrayDestructure":      "array destructuring; coverage_probe.ql added",
	"DestructureRest":       "destructure rest; coverage_probe.ql added",
	"ObjectLiteralField":    "object literal field; covered by react context-alias integration test (round-2)",
	"ObjectLiteralSpread":   "object literal spread element; covered by react context-alias integration test (round-3)",
	"JsxElement":            "JSX; coverage_probe.ql added",
	"JsxAttribute":          "JSX attribute; coverage_probe.ql added",
	"ImportBinding":         "import binding; coverage_probe.ql added",
	"ExportBinding":         "export binding; coverage_probe.ql added",

	// Call graph relations — internal plumbing.
	"CallCalleeSym":       "internal call graph edge",
	"CallResultSym":       "internal call graph edge",
	"CallTarget":          "internal call graph resolution",
	"CallTargetRTA":       "internal RTA call graph",
	"Instantiated":        "internal instantiation tracking",
	"MethodDeclDirect":    "internal method resolution",
	"MethodDeclInherited": "internal inherited method resolution",

	// Type system relations — queried indirectly.
	"ClassDecl":        "coverage_probe.ql added",
	"InterfaceDecl":    "coverage_probe.ql added",
	"Implements":       "coverage_probe.ql added",
	"Extends":          "coverage_probe.ql added",
	"MethodDecl":       "coverage_probe.ql added",
	"MethodCall":       "coverage_probe.ql added",
	"NewExpr":          "coverage_probe.ql added",
	"ExprType":         "coverage_probe.ql added",
	"TypeDecl":         "coverage_probe.ql added",
	"Type":             "coverage_probe.ql added",
	"ReturnStmt":       "coverage_probe.ql added",
	"FunctionContains": "coverage_probe.ql added",
	"ReturnSym":        "coverage_probe.ql added",
	"ExprInFunction":   "internal expression scoping for taint analysis",

	// Dataflow relations — queried indirectly via DataFlow module.
	"LocalFlow":     "internal dataflow plumbing",
	"LocalFlowStar": "internal dataflow plumbing",

	// Summary relations — internal interprocedural plumbing.
	"ParamToReturn":      "internal summary relation",
	"ParamToCallArg":     "internal summary relation",
	"ParamToFieldWrite":  "internal summary relation",
	"ParamToSink":        "internal summary relation",
	"SourceToReturn":     "internal summary relation",
	"CallReturnToReturn": "internal summary relation",

	// Composition relations — internal.
	"InterFlow": "internal interprocedural composition",
	"FlowStar":  "used in custom_config.ql but as bare predicate, not class reference",

	// Value-flow Phase A grounded base relations — populated for the
	// non-recursive mayResolveTo predicate added in Phase A PR3 (no QL
	// consumers ship in PR1 / PR2).
	"ExprValueSource": "value-flow Phase A grounded base; QL consumer arrives in PR3",
	"AssignExpr":      "value-flow Phase A grounded base; QL consumer arrives in PR3",
	"ParamBinding":    "value-flow Phase A grounded base; QL consumer arrives in PR3",

	// Value-flow Phase C PR1: pre-joined cross-module call target. PR3
	// landed the QL class wrapper in `tsq_callgraph.qll`. The system
	// rule's first user is the `ifsRetToCall` step (Phase C PR3); the
	// recursive `mayResolveTo` consumer arrives in PR4.
	"CallTargetCrossModule": "value-flow Phase C; QL class wrapper landed in PR3",

	// Value-flow Phase C PR2: intra-procedural step union. Populated as a
	// system rule (extract/rules/localflowstep.go); QL consumer ships in
	// Phase C PR4 (`mayResolveTo`).
	"LocalFlowStep": "value-flow Phase C step layer; QL consumer arrives in Phase C PR4",

	// Value-flow Phase C PR3: inter-procedural step union and the
	// top-level `FlowStep` union (LocalFlowStep ∪ InterFlowStep).
	// Populated as system rules (extract/rules/interflowstep.go); QL
	// consumer ships in Phase C PR4 (`mayResolveTo`).
	"InterFlowStep": "value-flow Phase C step layer; QL consumer arrives in Phase C PR4",
	"FlowStep":      "value-flow Phase C step layer; QL consumer arrives in Phase C PR4",

	// Framework model relations.
	"ExpressHandler": "coverage_probe.ql added",

	// DOM stubs — framework-specific, not queried directly in compat tests.
	"DOM::Element":        "DOM element class; queried via DOM security queries",
	"DOM::InnerHtmlWrite": "DOM innerHTML sink; internal taint plumbing",
	"DOM::DocumentWrite":  "document.write sink; internal taint plumbing",
	"DOM::AttributeWrite": "DOM attribute write; internal taint plumbing",

	// Crypto/logging stubs — not queried directly in compat tests.
	"CryptographicOperation": "crypto operation detection; queried via security queries",
	"CleartextLogging":       "cleartext logging sink; queried via security queries",
	"SensitiveDataExpr":      "abstract sensitive data; user-extensible",

	// C1: Template literal extraction
	"TemplateLiteral":    "template literal structural extraction; not queried directly",
	"TemplateElement":    "template string fragment; not queried directly",
	"TemplateExpression": "template interpolation; not queried directly",

	// C2: Enum declaration extraction
	"EnumDecl":   "enum declaration; not queried directly in compat tests",
	"EnumMember": "enum member; not queried directly in compat tests",

	// C5: Optional chaining and nullish coalescing
	"OptionalChain":     "optional chaining expression; not queried directly",
	"NullishCoalescing": "nullish coalescing expression; not queried directly",

	// C3: Decorator extraction
	"Decorator": "decorator structural extraction; not queried directly in compat tests",

	// C4: Namespace/module declaration extraction
	"NamespaceDecl":   "namespace declaration; not queried directly in compat tests",
	"NamespaceMember": "namespace member; not queried directly in compat tests",

	// C6: TypeScript type guards and assertion functions
	"TypeGuard": "type guard/assertion function; not queried directly in compat tests",

	// HTTP abstraction layer stubs — framework-agnostic HTTP classes.
	"HTTP::RequestHandler": "abstract HTTP handler; queried via framework-specific handlers",
	"HTTP::ServerRequest":  "server request parameter; queried via framework queries",
	"HTTP::ResponseBody":   "response body taint sink; queried via XSS queries",

	// IO stubs — database and filesystem access.
	"DatabaseAccess":   "database access sink; queried via SQL/NoSQL injection queries",
	"FileSystemAccess": "stub — real detection deferred; requires fs import tracking",

	// RegExp stubs — regex literal and term analysis.
	"RegExpLiteral": "stub — regex literal analysis deferred; requires AST regex parsing",
	"RegExpTerm":    "stub — regex term analysis deferred; requires regex parse tree",

	// A3: Additional taint/flow step materializers — populated by Configuration overrides.
	"AdditionalTaintStep": "materializer from TaintTracking::Configuration.isAdditionalTaintStep",
	"AdditionalFlowStep":  "materializer from DataFlow::Configuration.isAdditionalFlowStep",

	// Taint relations — some used in queries but as predicates, not class refs.
	"TaintSink":     "internal taint plumbing",
	"TaintSource":   "used as predicate in queries, not as class",
	"Sanitizer":     "internal taint plumbing",
	"TaintedSym":    "internal taint plumbing",
	"TaintedField":  "internal taint plumbing",
	"SanitizedEdge": "internal taint plumbing",

	// DataFlow module classes — covered via DataFlow:: prefix.
	"DataFlow::PathNode": "used implicitly in PathGraph queries",

	// TaintTracking module.
	"TaintTracking::Configuration": "covered in custom_config.ql via DataFlow::Configuration",

	// Security modules — partially covered.
	"Xss::XssSource":                           "covered implicitly; XssSink is the query target",
	"SqlInjection::SqlInjectionSource":         "covered implicitly; SqlInjectionSink is the query target in find_sqli.ql",
	"CommandInjection::CommandInjectionSource": "coverage_probe.ql added",
	"CommandInjection::CommandInjectionSink":   "coverage_probe.ql added",
	"PathTraversal::PathTraversalSource":       "coverage_probe.ql added",
	"PathTraversal::PathTraversalSink":         "coverage_probe.ql added",
}

// TestCompatStdlibCoverage ensures every class in the bridge manifest is either
// referenced by at least one compat query or explicitly allowlisted.
//
// This prevents new bridge classes from being added without corresponding test
// coverage or a documented reason for deferral.
func TestCompatStdlibCoverage(t *testing.T) {
	manifest := bridge.V1Manifest()

	// Collect all class names from the manifest.
	var allClasses []string
	for _, c := range manifest.Available {
		allClasses = append(allClasses, c.Name)
	}
	if len(allClasses) == 0 {
		t.Fatal("manifest has no available classes — something is wrong")
	}

	// Read all .ql files in testdata/compat/.
	qlFiles, err := filepath.Glob("testdata/compat/*.ql")
	if err != nil {
		t.Fatalf("glob ql files: %v", err)
	}

	// Concatenate all query content for searching.
	var allContent strings.Builder
	for _, f := range qlFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		allContent.Write(data)
		allContent.WriteByte('\n')
	}
	content := allContent.String()

	// Check each manifest class.
	var uncovered []string
	for _, className := range allClasses {
		// Check if the class name appears anywhere in query content.
		// For namespaced classes like "DataFlow::Node", search for the full name.
		if strings.Contains(content, className) {
			continue
		}

		// Not found in queries — check the allowlist.
		if _, ok := stdlibCoverageAllowlist[className]; ok {
			continue
		}

		uncovered = append(uncovered, className)
	}

	if len(uncovered) > 0 {
		t.Errorf("manifest classes not covered by any testdata/compat/*.ql query and not in allowlist:\n")
		for _, c := range uncovered {
			t.Errorf("  - %s", c)
		}
		t.Errorf("\nEither add a query that references the class or add it to stdlibCoverageAllowlist with justification.")
	}

	// Verify no allowlist entries are stale (class no longer in manifest).
	manifestSet := make(map[string]bool, len(allClasses))
	for _, c := range allClasses {
		manifestSet[c] = true
	}
	for name := range stdlibCoverageAllowlist {
		if !manifestSet[name] {
			t.Errorf("stale allowlist entry %q — class no longer in manifest; remove it", name)
		}
	}

	t.Logf("coverage matrix: %d manifest classes, %d covered by queries, %d allowlisted",
		len(allClasses), len(allClasses)-len(uncovered)-countAllowlisted(allClasses, content), countAllowlisted(allClasses, content))
}

// countAllowlisted returns how many manifest classes are in the allowlist (not directly covered).
func countAllowlisted(allClasses []string, content string) int {
	count := 0
	for _, c := range allClasses {
		if !strings.Contains(content, c) {
			if _, ok := stdlibCoverageAllowlist[c]; ok {
				count++
			}
		}
	}
	return count
}
