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
	RegisterRelation(RelationDef{Name: "ParamIsFunctionType", Version: 1, Columns: []ColumnDef{
		{Name: "fn", Type: TypeEntityRef},
		{Name: "idx", Type: TypeInt32},
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

	// v2 Phase D placeholders: taint analysis base relations (empty until Phase D)
	RegisterRelation(RelationDef{Name: "TaintSink", Version: 2, Columns: []ColumnDef{
		{Name: "sinkExpr", Type: TypeEntityRef},
		{Name: "sinkKind", Type: TypeString},
	}})
	RegisterRelation(RelationDef{Name: "TaintSource", Version: 2, Columns: []ColumnDef{
		{Name: "srcExpr", Type: TypeEntityRef},
		{Name: "sourceKind", Type: TypeString},
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
