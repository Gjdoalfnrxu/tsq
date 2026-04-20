package schema

func init() {
	// Structural
	RegisterRelation(RelationDef{Name: "File", Version: 1, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "path", Type: TypeString},
		{Name: "contentHash", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "Node", Version: 1, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "file", Type: TypeEntityRef},
		{Name: "kind", Type: TypeString},
		{Name: "startLine", Type: TypeInt32},
		{Name: "startCol", Type: TypeInt32},
		{Name: "endLine", Type: TypeInt32},
		{Name: "endCol", Type: TypeInt32},
	}})
	RegisterRelation(RelationDef{Name: "Contains", Version: 1, Columns: []ColumnDef{
		{Name: "parent", Type: TypeEntityRef},
		{Name: "child", Type: TypeEntityRef},
	}})
	// Symbols
	RegisterRelation(RelationDef{Name: "Symbol", Version: 1, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "declNode", Type: TypeEntityRef},
		{Name: "declFile", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "FunctionSymbol", Version: 1, Columns: []ColumnDef{
		{Name: "sym", Type: TypeEntityRef},
		{Name: "fn", Type: TypeEntityRef},
	}})
	// Functions
	RegisterRelation(RelationDef{Name: "Function", Version: 1, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "isArrow", Type: TypeInt32},
		{Name: "isAsync", Type: TypeInt32},
		{Name: "isGenerator", Type: TypeInt32},
		{Name: "isMethod", Type: TypeInt32},
	}})
	RegisterRelation(RelationDef{Name: "Parameter", Version: 1, Columns: []ColumnDef{
		{Name: "fn", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
		{Name: "name", Type: TypeString},
		{Name: "paramNode", Type: TypeEntityRef},
		{Name: "sym", Type: TypeEntityRef},
		{Name: "typeText", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "ParameterRest", Version: 1, Columns: []ColumnDef{
		{Name: "fn", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
	}})
	RegisterRelation(RelationDef{Name: "ParameterOptional", Version: 1, Columns: []ColumnDef{
		{Name: "fn", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
	}})
	// ParameterDestructured marks parameter slots whose pattern is an
	// ObjectPattern or ArrayPattern (e.g. `function f({a, b}, [x, y])`).
	// Phase A does NOT model destructured-parameter binding (the per-bound-name
	// expansion needs a separate pass — deferred to Phase C). The walker still
	// emits a single Parameter row for the slot (so arity bookkeeping is
	// stable), but the synthesised name is the literal pattern source text and
	// the symbol id is therefore bogus. ParamBinding's rule excludes these
	// slots via `not ParameterDestructured(fn, idx)` to prevent fake bindings.
	RegisterRelation(RelationDef{Name: "ParameterDestructured", Version: 1, Columns: []ColumnDef{
		{Name: "fn", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
	}})
	// ParamDestructurePattern(paramNode, patternNode) — links a destructured
	// parameter slot to the ObjectPattern/ArrayPattern that does the binding.
	// `paramNode` is the Parameter row's paramNode id (which may be a
	// RequiredParameter/OptionalParameter/AssignmentPattern wrapper or, for an
	// unwrapped arrow `({value}) =>`, the pattern itself); `patternNode` is the
	// ObjectPattern/ArrayPattern id that owns the DestructureField rows.
	// Emitted by the walker alongside the ParameterDestructured flag; Phase C
	// PR8 (#202 Gap A) consumes it from `lfsJsxPropBind` to bridge a JSX
	// prop's value-expression to the destructured-param use site inside the
	// component body.
	RegisterRelation(RelationDef{Name: "ParamDestructurePattern", Version: 1, Columns: []ColumnDef{
		{Name: "paramNode", Type: TypeEntityRef},
		{Name: "patternNode", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "ParamIsFunctionType", Version: 1, Columns: []ColumnDef{
		{Name: "fn", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
	}})
	// JsxExpressionInner(wrapperNode, innerNode) — bridges a `{…}` JSX
	// expression punctuation wrapper to its inner semantic expression.
	// Phase C PR8 (#202 Gap A) uses this from `lfsJsxPropBind` so the
	// value-flow layer can compose across the wrapper without forcing the
	// whole bridge stack to relearn it — `JsxAttribute.valueExpr`
	// continues to point at the JsxExpression wrapper so existing
	// consumers (notably `tsq_react.qll`'s Provider-value path, which
	// relies on `Contains` descent) stay untouched.
	RegisterRelation(RelationDef{Name: "JsxExpressionInner", Version: 1, Columns: []ColumnDef{
		{Name: "wrapperNode", Type: TypeEntityRef},
		{Name: "innerNode", Type: TypeEntityRef},
	}})
	// Calls
	RegisterRelation(RelationDef{Name: "Call", Version: 1, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "calleeNode", Type: TypeEntityRef},
		{Name: "arity", Type: TypeInt32},
	}})
	RegisterRelation(RelationDef{Name: "CallArg", Version: 1, Columns: []ColumnDef{
		{Name: "call", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
		{Name: "argNode", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "CallArgSpread", Version: 1, Columns: []ColumnDef{
		{Name: "call", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
	}})
	RegisterRelation(RelationDef{Name: "CallCalleeSym", Version: 1, Columns: []ColumnDef{
		{Name: "call", Type: TypeEntityRef},
		{Name: "sym", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "CallResultSym", Version: 1, Columns: []ColumnDef{
		{Name: "call", Type: TypeEntityRef},
		{Name: "sym", Type: TypeEntityRef},
	}})
	// Variables
	RegisterRelation(RelationDef{Name: "VarDecl", Version: 1, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "sym", Type: TypeEntityRef},
		{Name: "initExpr", Type: TypeEntityRef},
		{Name: "isConst", Type: TypeInt32},
	}})
	RegisterRelation(RelationDef{Name: "Assign", Version: 1, Columns: []ColumnDef{
		{Name: "lhsNode", Type: TypeEntityRef},
		{Name: "rhsExpr", Type: TypeEntityRef},
		{Name: "lhsSym", Type: TypeEntityRef},
	}})
	// Expressions
	RegisterRelation(RelationDef{Name: "ExprMayRef", Version: 1, Columns: []ColumnDef{
		{Name: "expr", Type: TypeEntityRef},
		{Name: "sym", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "ExprIsCall", Version: 1, Columns: []ColumnDef{
		{Name: "expr", Type: TypeEntityRef},
		{Name: "call", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "FieldRead", Version: 1, Columns: []ColumnDef{
		{Name: "expr", Type: TypeEntityRef},
		{Name: "baseSym", Type: TypeEntityRef},
		{Name: "fieldName", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "FieldWrite", Version: 1, Columns: []ColumnDef{
		{Name: "assignNode", Type: TypeEntityRef},
		{Name: "baseSym", Type: TypeEntityRef},
		{Name: "fieldName", Type: TypeString},
		{Name: "rhsExpr", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "Await", Version: 1, Columns: []ColumnDef{
		{Name: "expr", Type: TypeEntityRef},
		{Name: "innerExpr", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "Cast", Version: 1, Columns: []ColumnDef{
		{Name: "expr", Type: TypeEntityRef},
		{Name: "innerExpr", Type: TypeEntityRef},
	}})
	// Destructuring
	RegisterRelation(RelationDef{Name: "DestructureField", Version: 1, Columns: []ColumnDef{
		{Name: "parent", Type: TypeEntityRef},
		{Name: "sourceField", Type: TypeString},
		{Name: "bindName", Type: TypeString},
		{Name: "bindSym", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
	}})
	RegisterRelation(RelationDef{Name: "ArrayDestructure", Version: 1, Columns: []ColumnDef{
		{Name: "parent", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
		{Name: "bindSym", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "DestructureRest", Version: 1, Columns: []ColumnDef{
		{Name: "parent", Type: TypeEntityRef},
		{Name: "bindSym", Type: TypeEntityRef},
	}})
	// Modules
	RegisterRelation(RelationDef{Name: "ImportBinding", Version: 1, Columns: []ColumnDef{
		{Name: "localSym", Type: TypeEntityRef},
		{Name: "moduleSpec", Type: TypeString},
		{Name: "importedName", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "ExportBinding", Version: 1, Columns: []ColumnDef{
		{Name: "exportedName", Type: TypeString},
		{Name: "localSym", Type: TypeEntityRef},
		{Name: "file", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "TypeFromLib", Version: 1, Columns: []ColumnDef{
		{Name: "sym", Type: TypeEntityRef},
		{Name: "libName", Type: TypeString},
	}})
	// Object literals
	// ObjectLiteralField: a field in an object literal expression.
	// For shorthand `{ foo }` the fieldName equals the binding name and
	// valueExpr is the Identifier node (which in turn has an ExprMayRef row).
	// For `{ foo: expr }` the fieldName is the source key and valueExpr is
	// the value-position expression.
	// Used by the React context-alias tracking in `bridge/tsq_react.qll` to
	// look up which symbol a Provider's value object exposes under a given
	// field name. v1 limitations: spread elements (`{ ...rest }`) and
	// computed-key properties are skipped silently.
	RegisterRelation(RelationDef{Name: "ObjectLiteralField", Version: 1, Columns: []ColumnDef{
		{Name: "parent", Type: TypeEntityRef},
		{Name: "fieldName", Type: TypeString},
		{Name: "valueExpr", Type: TypeEntityRef},
	}})
	// ObjectLiteralSpread: a `...expr` element of an object literal.
	// `parent` is the enclosing object expression node id; `valueExpr` is
	// the expression node after the `...` (typically an Identifier).
	// Round-3 of the React context-alias work uses this together with a
	// VarDecl-to-ObjectExpression resolution to compute the union of own
	// fields and spread-contributed fields of a Provider value object.
	RegisterRelation(RelationDef{Name: "ObjectLiteralSpread", Version: 1, Columns: []ColumnDef{
		{Name: "parent", Type: TypeEntityRef},
		{Name: "valueExpr", Type: TypeEntityRef},
	}})
	// JSX
	RegisterRelation(RelationDef{Name: "JsxElement", Version: 1, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "tagNode", Type: TypeEntityRef},
		{Name: "tagSym", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "JsxAttribute", Version: 1, Columns: []ColumnDef{
		{Name: "element", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "valueExpr", Type: TypeEntityRef},
	}})
	// v2: Type-aware relations (structural emission via tree-sitter AST patterns)
	RegisterRelation(RelationDef{Name: "ClassDecl", Version: 2, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "file", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "InterfaceDecl", Version: 2, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "file", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "Implements", Version: 2, Columns: []ColumnDef{
		{Name: "classId", Type: TypeEntityRef},
		{Name: "interfaceId", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "Extends", Version: 2, Columns: []ColumnDef{
		{Name: "childId", Type: TypeEntityRef},
		{Name: "parentId", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "MethodDecl", Version: 2, Columns: []ColumnDef{
		{Name: "classOrIfaceId", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "fnId", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "MethodCall", Version: 2, Columns: []ColumnDef{
		{Name: "callId", Type: TypeEntityRef},
		{Name: "receiverExpr", Type: TypeEntityRef},
		{Name: "methodName", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "NewExpr", Version: 2, Columns: []ColumnDef{
		{Name: "callId", Type: TypeEntityRef},
		{Name: "classId", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "ExprType", Version: 2, Columns: []ColumnDef{
		{Name: "exprId", Type: TypeEntityRef},
		{Name: "typeId", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "TypeDecl", Version: 2, Columns: []ColumnDef{
		{Name: "typeId", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "kind", Type: TypeString},
		{Name: "file", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "ReturnStmt", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "stmtNode", Type: TypeEntityRef},
		{Name: "returnExpr", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "FunctionContains", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "nodeId", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "SymInFunction", Version: 2, Columns: []ColumnDef{
		{Name: "sym", Type: TypeEntityRef},
		{Name: "fnId", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "ExprInFunction", Version: 2, Columns: []ColumnDef{
		{Name: "exprId", Type: TypeEntityRef},
		{Name: "fnId", Type: TypeEntityRef},
	}})

	// v2 Phase C1: Return value symbol (synthetic per-function return symbol)
	RegisterRelation(RelationDef{Name: "ReturnSym", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "sym", Type: TypeEntityRef},
	}})

	// v2 Phase B: call graph derived relations (computed by system Datalog rules)
	RegisterRelation(RelationDef{Name: "CallTarget", Version: 2, Columns: []ColumnDef{
		{Name: "call", Type: TypeEntityRef},
		{Name: "fn", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "CallTargetRTA", Version: 2, Columns: []ColumnDef{
		{Name: "call", Type: TypeEntityRef},
		{Name: "fn", Type: TypeEntityRef},
	}})
	// Value-flow Phase C PR1: pre-joined Call × Import × Export × FunctionSymbol.
	// Bridges a call site whose callee is a name imported from another module to
	// the function definition the export resolves to. Name-only join across the
	// import/export pair (over-bridges on name collisions — same posture as the
	// existing bridge `importedFunctionSymbol` predicate; see
	// docs/design/valueflow-phase-c-plan.md §3.2). Wired by Phase C PR3's
	// `ifsRetToCall` to avoid a 4-table join in the recursive `mayResolveTo`
	// closure body. Populated as a system rule in extract/rules/valueflow.go.
	RegisterRelation(RelationDef{Name: "CallTargetCrossModule", Version: 2, Columns: []ColumnDef{
		{Name: "call", Type: TypeEntityRef},
		{Name: "fn", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "Instantiated", Version: 2, Columns: []ColumnDef{
		{Name: "classId", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "MethodDeclDirect", Version: 2, Columns: []ColumnDef{
		{Name: "classId", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "fn", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "MethodDeclInherited", Version: 2, Columns: []ColumnDef{
		{Name: "childId", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "fn", Type: TypeEntityRef},
	}})

	// v2 Phase C1: intra-procedural dataflow (computed by system Datalog rules)
	RegisterRelation(RelationDef{Name: "LocalFlow", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "srcSym", Type: TypeEntityRef},
		{Name: "dstSym", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "LocalFlowStar", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "srcSym", Type: TypeEntityRef},
		{Name: "dstSym", Type: TypeEntityRef},
	}})

	// v2 Phase C2: function-level summaries (computed by system Datalog rules)
	RegisterRelation(RelationDef{Name: "ParamToReturn", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "paramIdx", Type: TypeInt32},
	}})
	RegisterRelation(RelationDef{Name: "ParamToCallArg", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "paramIdx", Type: TypeInt32},
		{Name: "calleeSym", Type: TypeEntityRef},
		{Name: "argIdx", Type: TypeInt32},
	}})
	RegisterRelation(RelationDef{Name: "ParamToFieldWrite", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "paramIdx", Type: TypeInt32},
		{Name: "fieldName", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "ParamToSink", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "paramIdx", Type: TypeInt32},
		{Name: "sinkKind", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "SourceToReturn", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "sourceKind", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "CallReturnToReturn", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "callId", Type: TypeEntityRef},
	}})

	// v2 Phase C3: inter-procedural composition (computed by system Datalog rules)
	RegisterRelation(RelationDef{Name: "InterFlow", Version: 2, Columns: []ColumnDef{
		{Name: "srcSym", Type: TypeEntityRef},
		{Name: "dstSym", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "FlowStar", Version: 2, Columns: []ColumnDef{
		{Name: "srcSym", Type: TypeEntityRef},
		{Name: "dstSym", Type: TypeEntityRef},
	}})

	// v2 Phase D: taint analysis base relations
	RegisterRelation(RelationDef{Name: "TaintSink", Version: 2, Columns: []ColumnDef{
		{Name: "sinkExpr", Type: TypeEntityRef},
		{Name: "sinkKind", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "TaintSource", Version: 2, Columns: []ColumnDef{
		{Name: "srcExpr", Type: TypeEntityRef},
		{Name: "sourceKind", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "Sanitizer", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "kind", Type: TypeString},
	}})

	// v2 Phase D: taint analysis derived relations
	RegisterRelation(RelationDef{Name: "TaintedSym", Version: 2, Columns: []ColumnDef{
		{Name: "sym", Type: TypeEntityRef},
		{Name: "kind", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "TaintedField", Version: 2, Columns: []ColumnDef{
		{Name: "baseSym", Type: TypeEntityRef},
		{Name: "fieldName", Type: TypeString},
		{Name: "kind", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "SanitizedEdge", Version: 2, Columns: []ColumnDef{
		{Name: "srcSym", Type: TypeEntityRef},
		{Name: "dstSym", Type: TypeEntityRef},
		{Name: "kind", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "TaintAlert", Version: 2, Columns: []ColumnDef{
		{Name: "srcExpr", Type: TypeEntityRef},
		{Name: "sinkExpr", Type: TypeEntityRef},
		{Name: "srcKind", Type: TypeString},
		{Name: "sinkKind", Type: TypeString},
	}})

	// v2 Phase A3: additional taint/flow steps (populated by user Configuration overrides)
	RegisterRelation(RelationDef{Name: "AdditionalTaintStep", Version: 2, Columns: []ColumnDef{
		{Name: "srcSym", Type: TypeEntityRef},
		{Name: "dstSym", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "AdditionalFlowStep", Version: 2, Columns: []ColumnDef{
		{Name: "srcSym", Type: TypeEntityRef},
		{Name: "dstSym", Type: TypeEntityRef},
	}})

	// v2 Phase F: framework-derived relations
	RegisterRelation(RelationDef{Name: "ExpressHandler", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
	}})

	// v3: tsgo-resolved type relations
	RegisterRelation(RelationDef{Name: "ResolvedType", Version: 3, Columns: []ColumnDef{
		{Name: "typeId", Type: TypeEntityRef},
		{Name: "displayName", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "SymbolType", Version: 3, Columns: []ColumnDef{
		{Name: "sym", Type: TypeEntityRef},
		{Name: "typeId", Type: TypeEntityRef},
	}})
	// v3 Phase 3d: non-taintable primitive types (number, boolean, etc.)
	// identified by display name, used by taint analysis as a type-based sanitizer.
	RegisterRelation(RelationDef{Name: "NonTaintableType", Version: 3, Columns: []ColumnDef{
		{Name: "typeId", Type: TypeEntityRef},
	}})

	// v3 Phase 17: type-fact relations (structural + tsgo-enriched)
	// TypeInfo: detailed info about a type (kind: "union", "intersection", "object", "primitive", etc.)
	RegisterRelation(RelationDef{Name: "TypeInfo", Version: 3, Columns: []ColumnDef{
		{Name: "typeId", Type: TypeEntityRef},
		{Name: "kind", Type: TypeString},
		{Name: "displayName", Type: TypeString},
	}})
	// TypeMember: a named member of an object/interface/class type
	RegisterRelation(RelationDef{Name: "TypeMember", Version: 3, Columns: []ColumnDef{
		{Name: "typeId", Type: TypeEntityRef},
		{Name: "memberName", Type: TypeString},
		{Name: "memberTypeId", Type: TypeEntityRef},
	}})
	// UnionMember: constituent type of a union (e.g. string | number)
	RegisterRelation(RelationDef{Name: "UnionMember", Version: 3, Columns: []ColumnDef{
		{Name: "unionTypeId", Type: TypeEntityRef},
		{Name: "memberTypeId", Type: TypeEntityRef},
	}})
	// IntersectionMember: constituent type of an intersection (e.g. A & B)
	RegisterRelation(RelationDef{Name: "IntersectionMember", Version: 3, Columns: []ColumnDef{
		{Name: "intersectionTypeId", Type: TypeEntityRef},
		{Name: "memberTypeId", Type: TypeEntityRef},
	}})
	// GenericInstantiation: a generic type instantiated with type arguments
	RegisterRelation(RelationDef{Name: "GenericInstantiation", Version: 3, Columns: []ColumnDef{
		{Name: "instanceTypeId", Type: TypeEntityRef},
		{Name: "genericTypeId", Type: TypeEntityRef},
		{Name: "argIdx", Type: TypeInt32},
		{Name: "argTypeId", Type: TypeEntityRef},
	}})
	// TypeAlias: a type alias declaration linking alias name to the aliased type
	RegisterRelation(RelationDef{Name: "TypeAlias", Version: 3, Columns: []ColumnDef{
		{Name: "aliasTypeId", Type: TypeEntityRef},
		{Name: "aliasedTypeId", Type: TypeEntityRef},
	}})
	// TypeParameter: a type parameter on a generic declaration
	RegisterRelation(RelationDef{Name: "TypeParameter", Version: 3, Columns: []ColumnDef{
		{Name: "declId", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
		{Name: "name", Type: TypeString},
		{Name: "constraintTypeId", Type: TypeEntityRef},
	}})

	// v2 Phase A (value-flow): grounded base relations for non-recursive mayResolveTo.
	// See docs/design/valueflow-phase-a-plan.md §1.2.

	// ExprValueSource(expr, sourceExpr) — one row per AST expression that is a
	// "value-producing literal at its own location": object literals, array
	// literals, function/arrow expressions, class expressions, primitive
	// literals (string/number/bool/null/undefined/regex/template-without-subs),
	// JSX elements. Identity row: expr == sourceExpr. Provides a grounded base
	// predicate for the planner's trivial-IDB pre-pass to size mayResolveTo.
	// NOT emitted for: identifiers, calls, member access, binary ops, await,
	// `as`/`!`/parenthesised casts (those resolve through other relations).
	RegisterRelation(RelationDef{Name: "ExprValueSource", Version: 2, Columns: []ColumnDef{
		{Name: "expr", Type: TypeEntityRef},
		{Name: "sourceExpr", Type: TypeEntityRef},
	}})

	// AssignExpr(lhsSym, rhsExpr) — symmetric-to-VarDecl projection of Assign.
	// Materialised as a 2-column projection so the planner can key joins on
	// lhsSym directly without dragging Assign's unused lhsNode column through
	// binding inference. Pure projection of Assign; emitted at the same site.
	RegisterRelation(RelationDef{Name: "AssignExpr", Version: 2, Columns: []ColumnDef{
		{Name: "lhsSym", Type: TypeEntityRef},
		{Name: "rhsExpr", Type: TypeEntityRef},
	}})

	// ParamBinding(fn, paramIdx, paramSym, argExpr) — one row per
	// (call site × parameter slot) where fn is the callee function id
	// (resolved via CallTarget), paramIdx is the parameter position, paramSym
	// is the symbol of that parameter inside the callee, and argExpr is the
	// actual-argument expression at the call site. Materialises the
	// CallTarget ⨝ CallArg ⨝ Parameter join once so non-recursive
	// mayResolveTo can consume it as an already-bound base predicate.
	// Computed by the value-flow system Datalog rules (see extract/rules).
	// Carve-outs (NOT emitted in v1): spread args (`f(...rest)`), rest
	// params (`function f(...args)`). Both are silently skipped — adding
	// them needs an array-shape model and is deferred to Phase C.
	RegisterRelation(RelationDef{Name: "ParamBinding", Version: 2, Columns: []ColumnDef{
		{Name: "fn", Type: TypeEntityRef},
		{Name: "paramIdx", Type: TypeInt32},
		{Name: "paramSym", Type: TypeEntityRef},
		{Name: "argExpr", Type: TypeEntityRef},
	}})

	// Value-flow Phase C PR2: intra-procedural value-flow step union.
	// LocalFlowStep(from, to) holds when the runtime value of expression
	// `from` may flow to expression `to` in a single intra-procedural step
	// — assignment, var init, param binding, return-to-call, destructure,
	// object-literal store/spread, field read/write, or await. PR2 ships
	// path-erased (arity-2); PR5 widens to (from, to, path) for field
	// sensitivity. Populated by extract/rules/localflowstep.go as the
	// union of eleven `lfs*` per-kind IDB rules. PR3 adds the symmetric
	// `InterFlowStep` for cross-call/module steps; PR4 closes the union
	// into the recursive `MayResolveTo` predicate. See
	// docs/design/valueflow-phase-c-plan.md §1.3.
	RegisterRelation(RelationDef{Name: "LocalFlowStep", Version: 2, Columns: []ColumnDef{
		{Name: "from", Type: TypeEntityRef},
		{Name: "to", Type: TypeEntityRef},
	}})

	// Value-flow Phase C PR3: inter-procedural value-flow step union.
	// InterFlowStep(from, to) holds when the runtime value of expression
	// `from` may flow to expression `to` in a single inter-procedural step
	// — call-arg → callee parameter, callee return → call site (same- or
	// cross-module), import → export bridge, or RTA-resolved method
	// dispatch. PR3 ships path-erased (arity-2); PR5 widens for field
	// sensitivity. Populated by extract/rules/interflowstep.go as the
	// union of four `ifs*` per-kind IDB rules. See
	// docs/design/valueflow-phase-c-plan.md §1.4.
	RegisterRelation(RelationDef{Name: "InterFlowStep", Version: 2, Columns: []ColumnDef{
		{Name: "from", Type: TypeEntityRef},
		{Name: "to", Type: TypeEntityRef},
	}})

	// Value-flow Phase C PR3: top-level single-step flow union.
	// FlowStep(from, to) is the union of LocalFlowStep ∪ InterFlowStep
	// per plan §1.1 — the relation PR4's recursive `MayResolveTo` will
	// close over. Bridge authors that want a non-recursive 1-hop view of
	// value flow consume this directly (manual depth-unrolling on top of
	// FlowStep is the migration path between PR3 landing and PR4 going
	// live).
	RegisterRelation(RelationDef{Name: "FlowStep", Version: 2, Columns: []ColumnDef{
		{Name: "from", Type: TypeEntityRef},
		{Name: "to", Type: TypeEntityRef},
	}})

	// Value-flow Phase C PR4: recursive may-resolve-to closure.
	// MayResolveTo(v, s) is the transitive closure of FlowStep starting
	// from ExprValueSource — "expression v's runtime value may be the
	// value produced at expression s". Populated by system rules in
	// extract/rules/mayresolveto.go (two heads: ExprValueSource base case
	// + FlowStep-then-MayResolveTo recursive case). Path-erased (arity-2);
	// PR5 widens to (v, s, path) for field sensitivity. Bridge migration
	// (PR6) swaps existing R1–R4 shape predicates for `mayResolveToRec`
	// consumers in tsq_react.qll. See
	// docs/design/valueflow-phase-c-plan.md §1.2.
	RegisterRelation(RelationDef{Name: "MayResolveTo", Version: 2, Columns: []ColumnDef{
		{Name: "v", Type: TypeEntityRef},
		{Name: "s", Type: TypeEntityRef},
	}})

	// Value-flow Phase C PR7: cap-hit diagnostic relation.
	// MayResolveToCapHit(queryId, rulePred, lastDeltaSize) is emitted
	// at most once per top-level query whose `MayResolveTo` stratum
	// fails to converge under DefaultMaxIterations (ql/eval/seminaive.go).
	// Bridge consumers filter on this relation to detect
	// under-approximated results (truncate-don't-crash per plan §5.2).
	//
	// PR7 scope: schema-registered; no caller yet; no evaluator wiring.
	// No emitter populates this relation in any code path shipped with
	// PR7. The schema entry is a forward-declaration so the bridge
	// manifest can carry the name and QL consumers can plan against
	// its shape.
	//
	// Follow-ups:
	//   - Evaluator wiring (emit on *IterationCapError before
	//     truncation): Gjdoalfnrxu/tsq#201
	//   - Behavioural test (forces cap-hit, asserts row materialises):
	//     Gjdoalfnrxu/tsq#200
	//
	// Do NOT rely on read-back of rows from this relation until #201
	// ships.
	//
	// Columns:
	//   queryId       — synthetic id for the emitting top-level query
	//                   (0 for global / unnamed)
	//   rulePred      — predicate name whose stratum hit the cap
	//                   (expected values: "MayResolveTo")
	//   lastDeltaSize — the last-delta-size reported by
	//                   *IterationCapError
	RegisterRelation(RelationDef{Name: "MayResolveToCapHit", Version: 2, Columns: []ColumnDef{
		{Name: "queryId", Type: TypeEntityRef},
		{Name: "rulePred", Type: TypeString},
		{Name: "lastDeltaSize", Type: TypeInt32},
	}})

	// C1: Template literal extraction
	RegisterRelation(RelationDef{Name: "TemplateLiteral", Version: 2, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "tag", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "TemplateElement", Version: 2, Columns: []ColumnDef{
		{Name: "parentId", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
		{Name: "rawText", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "TemplateExpression", Version: 2, Columns: []ColumnDef{
		{Name: "parentId", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
		{Name: "exprId", Type: TypeEntityRef},
	}})

	// C2: Enum declaration extraction
	RegisterRelation(RelationDef{Name: "EnumDecl", Version: 2, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "file", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "EnumMember", Version: 2, Columns: []ColumnDef{
		{Name: "enumId", Type: TypeEntityRef},
		{Name: "memberName", Type: TypeString},
		{Name: "initExpr", Type: TypeEntityRef},
	}})

	// C5: Optional chaining and nullish coalescing
	RegisterRelation(RelationDef{Name: "OptionalChain", Version: 2, Columns: []ColumnDef{
		{Name: "expr", Type: TypeEntityRef},
		{Name: "baseExpr", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "NullishCoalescing", Version: 2, Columns: []ColumnDef{
		{Name: "expr", Type: TypeEntityRef},
		{Name: "lhs", Type: TypeEntityRef},
		{Name: "rhs", Type: TypeEntityRef},
	}})

	// C3: Decorator extraction
	RegisterRelation(RelationDef{Name: "Decorator", Version: 2, Columns: []ColumnDef{
		{Name: "targetId", Type: TypeEntityRef},
		{Name: "decoratorExpr", Type: TypeEntityRef},
	}})

	// C4: Namespace/module declaration extraction
	RegisterRelation(RelationDef{Name: "NamespaceDecl", Version: 2, Columns: []ColumnDef{
		{Name: "id", Type: TypeEntityRef},
		{Name: "name", Type: TypeString},
		{Name: "file", Type: TypeEntityRef},
	}})
	RegisterRelation(RelationDef{Name: "NamespaceMember", Version: 2, Columns: []ColumnDef{
		{Name: "nsId", Type: TypeEntityRef},
		{Name: "memberId", Type: TypeEntityRef},
	}})

	// C6: TypeScript type guards and assertion functions
	RegisterRelation(RelationDef{Name: "TypeGuard", Version: 2, Columns: []ColumnDef{
		{Name: "fnId", Type: TypeEntityRef},
		{Name: "paramIdx", Type: TypeInt32},
		{Name: "narrowedType", Type: TypeString},
	}})

	// Diagnostics
	RegisterRelation(RelationDef{Name: "ExtractError", Version: 1, Columns: []ColumnDef{
		{Name: "file", Type: TypeEntityRef},
		{Name: "nodeStartLine", Type: TypeInt32},
		{Name: "phase", Type: TypeString},
		{Name: "message", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "SchemaVersion", Version: 1, Columns: []ColumnDef{
		{Name: "version", Type: TypeInt32},
	}})
}
