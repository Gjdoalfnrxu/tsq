package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// frameworkBaseRels returns the base relations needed for framework rules evaluation,
// including all relations from the full system rule set.
func frameworkBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
	base := taintBaseRels(nil)
	// Add framework-specific base relations
	base["FieldRead"] = eval.NewRelation("FieldRead", 4)
	base["FieldWrite"] = eval.NewRelation("FieldWrite", 5)
	base["Function"] = eval.NewRelation("Function", 6)
	base["JsxElement"] = eval.NewRelation("JsxElement", 3)
	base["JsxAttribute"] = eval.NewRelation("JsxAttribute", 3)
	base["ImportBinding"] = eval.NewRelation("ImportBinding", 3)
	base["ExportBinding"] = eval.NewRelation("ExportBinding", 3)
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// TestExpressHandler_FromAppGet tests that ExpressHandler is derived from app.get(path, handler).
func TestExpressHandler_FromAppGet(t *testing.T) {
	// app.get("/path", handler) → MethodCall(call=500, recv=_, "get"), CallArg(500, 1, cbExpr=600),
	// ExprMayRef(600, cbSym=60), FunctionSymbol(60, fn=7).
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"MethodCall":     makeRel("MethodCall", 3, iv(500), iv(400), sv("get")),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(1), iv(600)),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(600), iv(60)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn")},
		Body:   []datalog.Literal{pos("ExpressHandler", v("fn"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(7)) {
		t.Errorf("expected ExpressHandler(7), got %v", rs.Rows)
	}
}

// TestExpressSource_ReqQuery tests that req.query produces a TaintSource.
func TestExpressSource_ReqQuery(t *testing.T) {
	// Express handler fn=7, req param sym=10 at idx 0.
	// FieldRead(expr=100, reqSym=10, "query")
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		// Handler detection: app.post("/path", handler)
		"MethodCall":     makeRel("MethodCall", 3, iv(500), iv(400), sv("post")),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(1), iv(600)),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(600), iv(60), iv(100), iv(10)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
		"Parameter":      makeRel("Parameter", 6, iv(7), iv(0), sv("req"), iv(80), iv(10), sv("")),
		"FieldRead":      makeRel("FieldRead", 4, iv(100), iv(10), sv("query"), sv(".query")),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("expr"), v("kind")},
		Body:   []datalog.Literal{pos("TaintSource", v("expr"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(100), sv("http_input")) {
		t.Errorf("expected TaintSource(100, http_input) from req.query, got %v", rs.Rows)
	}
}

// TestExpressSink_ResSend tests that res.send(data) produces a TaintSink.
func TestExpressSink_ResSend(t *testing.T) {
	// Express handler fn=7, res param sym=20 at idx 1.
	// res.send(data): MethodCall(call=700, recv=750, "send"), ExprMayRef(750, 20),
	// CallArg(700, 0, argExpr=800).
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		// Handler detection
		"MethodCall": makeRel("MethodCall", 3,
			iv(500), iv(400), sv("get"), // app.get
			iv(700), iv(750), sv("send"), // res.send
		),
		"CallArg": makeRel("CallArg", 3,
			iv(500), iv(1), iv(600), // handler callback
			iv(700), iv(0), iv(800), // send argument
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(600), iv(60), // callback expr → cbSym
			iv(750), iv(20), // recv expr → resSym
		),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
		"Parameter":      makeRel("Parameter", 6, iv(7), iv(1), sv("res"), iv(81), iv(20), sv("")),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("expr"), v("kind")},
		Body:   []datalog.Literal{pos("TaintSink", v("expr"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(800), sv("xss")) {
		t.Errorf("expected TaintSink(800, xss) from res.send, got %v", rs.Rows)
	}
}

// TestNodeSink_ChildProcessExec tests that child_process.exec(cmd) produces a command_injection sink.
func TestNodeSink_ChildProcessExec(t *testing.T) {
	// exec(cmd): CallCalleeSym(call=900, execSym=90), FunctionSymbol(90, execFn=9),
	// Function(9, "exec", 0, 0, 0, 0), CallArg(900, 0, argExpr=950).
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(900), iv(90)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(90), iv(9)),
		"Function":       makeRel("Function", 6, iv(9), sv("exec"), iv(0), iv(0), iv(0), iv(0)),
		"CallArg":        makeRel("CallArg", 3, iv(900), iv(0), iv(950)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("expr"), v("kind")},
		Body:   []datalog.Literal{pos("TaintSink", v("expr"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(950), sv("command_injection")) {
		t.Errorf("expected TaintSink(950, command_injection) from exec(), got %v", rs.Rows)
	}
}

// TestReactSink_DangerouslySetInnerHTML tests that dangerouslySetInnerHTML produces an XSS sink.
func TestReactSink_DangerouslySetInnerHTML(t *testing.T) {
	// <div dangerouslySetInnerHTML={expr} />
	// JsxAttribute(elem=1000, "dangerouslySetInnerHTML", valueExpr=1050)
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"JsxAttribute": makeRel("JsxAttribute", 3, iv(1000), sv("dangerouslySetInnerHTML"), iv(1050)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("expr"), v("kind")},
		Body:   []datalog.Literal{pos("TaintSink", v("expr"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(1050), sv("xss")) {
		t.Errorf("expected TaintSink(1050, xss) from dangerouslySetInnerHTML, got %v", rs.Rows)
	}
}

// TestFrameworkRulesValidate verifies all framework rules pass validation.
func TestFrameworkRulesValidate(t *testing.T) {
	for i, r := range FrameworkRules() {
		errs := plan.ValidateRule(r)
		if len(errs) > 0 {
			t.Errorf("rule %d (%s) validation errors: %v", i, r.Head.Predicate, errs)
		}
	}
}

// TestFrameworkRulesStratify verifies framework rules stratify with all system rules.
func TestFrameworkRulesStratify(t *testing.T) {
	prog := &datalog.Program{Rules: AllSystemRules()}
	_, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("all system rules (including frameworks) failed to plan: %v", errs)
	}
}

// TestFrameworkRuleCount verifies the expected number of framework rules.
func TestFrameworkRuleCount(t *testing.T) {
	rules := FrameworkRules()
	// Count breakdown:
	// Express: 6 handler + 3 source + 1 sink = 10
	// Node.js sinks: exec=1, dangerouslySetInnerHTML=1, heuristic .query()=1 = 3
	// HTTP: 2 handler + 3 source + 2 sink = 7
	// Koa: 1 handler + 1 ctx.query + 2 ctx.request.{body,query} + 1 sink = 5
	// Fastify: 7 handler + 3 source + 1 sink = 11
	// Lambda: 1 handler + 1 source = 2
	// Next.js: 1 handler + 2 source + 2 sink = 5
	// Database: 4 mods × 2 methods = 8, mongoose 3, sequelize 1 = 12
	// Sanitizers: XSS 6 + SQL 2 + CMD 2 = 10
	// Total: 10 + 3 + 7 + 5 + 11 + 2 + 5 + 12 + 10 = 65
	expected := 65
	if len(rules) != expected {
		t.Errorf("expected %d framework rules, got %d", expected, len(rules))
	}
}

// ─── B1: Node HTTP handler tests ─────────────────────────────────────────

func TestHttpHandler_CreateServer(t *testing.T) {
	// http.createServer(callback)
	// ImportBinding(calleeSym=50, "http", "createServer"),
	// CallCalleeSym(call=500, calleeSym=50),
	// CallArg(500, 0, cbExpr=600), ExprMayRef(600, cbSym=60), FunctionSymbol(60, fn=7).
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"ImportBinding":  makeRel("ImportBinding", 3, iv(50), sv("http"), sv("createServer")),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(500), iv(50)),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(0), iv(600)),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(600), iv(60)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn")},
		Body:   []datalog.Literal{pos("HttpHandler", v("fn"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(7)) {
		t.Errorf("expected HttpHandler(7), got %v", rs.Rows)
	}
}

func TestHttpSource_ReqUrl(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"ImportBinding":  makeRel("ImportBinding", 3, iv(50), sv("http"), sv("createServer")),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(500), iv(50)),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(0), iv(600)),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(600), iv(60)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
		"Parameter":      makeRel("Parameter", 6, iv(7), iv(0), sv("req"), iv(80), iv(10), sv("")),
		"FieldRead":      makeRel("FieldRead", 4, iv(100), iv(10), sv("url"), sv(".url")),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("expr"), v("kind")},
		Body:   []datalog.Literal{pos("TaintSource", v("expr"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(100), sv("http_input")) {
		t.Errorf("expected TaintSource(100, http_input) from req.url, got %v", rs.Rows)
	}
}

// ─── B2: Koa tests ──────────────────────────────────────────────────────

func TestKoaSource_CtxQuery(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"MethodCall":     makeRel("MethodCall", 3, iv(500), iv(400), sv("use")),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(0), iv(600)),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(600), iv(60)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
		"Parameter":      makeRel("Parameter", 6, iv(7), iv(0), sv("ctx"), iv(80), iv(10), sv("")),
		"FieldRead":      makeRel("FieldRead", 4, iv(100), iv(10), sv("query"), sv(".query")),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("expr"), v("kind")},
		Body:   []datalog.Literal{pos("TaintSource", v("expr"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(100), sv("http_input")) {
		t.Errorf("expected TaintSource(100, http_input) from ctx.query, got %v", rs.Rows)
	}
}

func TestKoaSink_CtxBodyAssign(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"MethodCall":     makeRel("MethodCall", 3, iv(500), iv(400), sv("use")),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(0), iv(600)),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(600), iv(60)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
		"Parameter":      makeRel("Parameter", 6, iv(7), iv(0), sv("ctx"), iv(80), iv(10), sv("")),
		"FieldWrite":     makeRel("FieldWrite", 5, iv(200), iv(10), sv("body"), iv(300), sv(".body")),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("expr"), v("kind")},
		Body:   []datalog.Literal{pos("TaintSink", v("expr"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(300), sv("xss")) {
		t.Errorf("expected TaintSink(300, xss) from ctx.body assignment, got %v", rs.Rows)
	}
}

// ─── B3: Fastify tests ─────────────────────────────────────────────────

func TestFastifyHandler_Post(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"MethodCall":     makeRel("MethodCall", 3, iv(500), iv(400), sv("post")),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(1), iv(600)),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(600), iv(60)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn")},
		Body:   []datalog.Literal{pos("FastifyHandler", v("fn"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(7)) {
		t.Errorf("expected FastifyHandler(7), got %v", rs.Rows)
	}
}

// ─── B4: Lambda tests ──────────────────────────────────────────────────

func TestLambdaHandler_ExportHandler(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"ExportBinding":  makeRel("ExportBinding", 3, sv("handler"), iv(50), iv(1)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(50), iv(7)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn")},
		Body:   []datalog.Literal{pos("LambdaHandler", v("fn"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(7)) {
		t.Errorf("expected LambdaHandler(7), got %v", rs.Rows)
	}
}

func TestLambdaSource_EventField(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"ExportBinding":  makeRel("ExportBinding", 3, sv("handler"), iv(50), iv(1)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(50), iv(7)),
		"Parameter":      makeRel("Parameter", 6, iv(7), iv(0), sv("event"), iv(80), iv(10), sv("")),
		"FieldRead":      makeRel("FieldRead", 4, iv(100), iv(10), sv("body"), sv(".body")),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("expr"), v("kind")},
		Body:   []datalog.Literal{pos("TaintSource", v("expr"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(100), sv("http_input")) {
		t.Errorf("expected TaintSource(100, http_input) from event field, got %v", rs.Rows)
	}
}

// ─── B5: Next.js tests ─────────────────────────────────────────────────

func TestNextjsHandler_DefaultExport(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"ExportBinding":  makeRel("ExportBinding", 3, sv("default"), iv(50), iv(1)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(50), iv(7)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn")},
		Body:   []datalog.Literal{pos("NextjsHandler", v("fn"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(7)) {
		t.Errorf("expected NextjsHandler(7), got %v", rs.Rows)
	}
}

// ─── B6: Database driver tests ──────────────────────────────────────────

func TestDatabaseSink_PgQuery(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"ImportBinding": makeRel("ImportBinding", 3, iv(50), sv("pg"), sv("default")),
		"MethodCall":    makeRel("MethodCall", 3, iv(500), iv(400), sv("query")),
		"ExprMayRef":    makeRel("ExprMayRef", 2, iv(400), iv(50)),
		"CallArg":       makeRel("CallArg", 3, iv(500), iv(0), iv(800)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("expr"), v("kind")},
		Body:   []datalog.Literal{pos("TaintSink", v("expr"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(800), sv("sql")) {
		t.Errorf("expected TaintSink(800, sql) from pg .query(), got %v", rs.Rows)
	}
}

func TestDatabaseSink_MongooseFind(t *testing.T) {
	// Mongoose uses heuristic: any .find() call is a NoSQL sink
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"MethodCall": makeRel("MethodCall", 3, iv(500), iv(400), sv("find")),
		"CallArg":    makeRel("CallArg", 3, iv(500), iv(0), iv(800)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("expr"), v("kind")},
		Body:   []datalog.Literal{pos("TaintSink", v("expr"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(800), sv("nosql")) {
		t.Errorf("expected TaintSink(800, nosql) from .find() heuristic, got %v", rs.Rows)
	}
}

// ─── B7: Sanitizer tests ───────────────────────────────────────────────

func TestSanitizer_DomPurify(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"ImportBinding":  makeRel("ImportBinding", 3, iv(50), sv("dompurify"), sv("sanitize")),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(50), iv(7)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("kind")},
		Body:   []datalog.Literal{pos("Sanitizer", v("fn"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(7), sv("xss")) {
		t.Errorf("expected Sanitizer(7, xss) from dompurify, got %v", rs.Rows)
	}
}

func TestSanitizer_SqlString(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"ImportBinding":  makeRel("ImportBinding", 3, iv(50), sv("sqlstring"), sv("escape")),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(50), iv(7)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("kind")},
		Body:   []datalog.Literal{pos("Sanitizer", v("fn"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(7), sv("sql")) {
		t.Errorf("expected Sanitizer(7, sql) from sqlstring, got %v", rs.Rows)
	}
}

func TestSanitizer_ShellQuote(t *testing.T) {
	baseRels := frameworkBaseRels(map[string]*eval.Relation{
		"ImportBinding":  makeRel("ImportBinding", 3, iv(50), sv("shell-quote"), sv("quote")),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(50), iv(7)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("kind")},
		Body:   []datalog.Literal{pos("Sanitizer", v("fn"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(7), sv("command_injection")) {
		t.Errorf("expected Sanitizer(7, command_injection) from shell-quote, got %v", rs.Rows)
	}
}
