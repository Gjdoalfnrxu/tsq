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
