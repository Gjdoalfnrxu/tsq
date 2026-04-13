package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// s returns a StringConst term.
func s(val string) datalog.StringConst { return datalog.StringConst{Value: val} }

// intc returns an IntConst term (shorthand for datalog.IntConst{Value: n}).
func intc(n int64) datalog.IntConst { return datalog.IntConst{Value: n} }

// FrameworkRules returns the system Datalog rules for framework-specific
// taint source/sink identification (Phase F). These are pattern-based
// heuristics that match on function names and method names to populate
// TaintSource, TaintSink, Sanitizer, and handler relations.
func FrameworkRules() []datalog.Rule {
	var rules []datalog.Rule

	rules = append(rules, expressRules()...)
	rules = append(rules, httpHandlerRules()...)
	rules = append(rules, koaRules()...)
	rules = append(rules, fastifyRules()...)
	rules = append(rules, lambdaRules()...)
	rules = append(rules, nextjsRules()...)
	rules = append(rules, databaseRules()...)
	rules = append(rules, sanitizerRules()...)

	// ─── Node.js sinks: child_process.exec ──────────────────────────
	rules = append(rules, rule("TaintSink",
		[]datalog.Term{v("argExpr"), s("command_injection")},
		pos("CallCalleeSym", v("call"), v("execSym")),
		pos("FunctionSymbol", v("execSym"), v("execFn")),
		pos("Function", v("execFn"), s("exec"), w(), w(), w(), w()),
		pos("CallArg", v("call"), intc(0), v("argExpr")),
	))

	// ─── React XSS: dangerouslySetInnerHTML ─────────────────────────
	rules = append(rules, rule("TaintSink",
		[]datalog.Term{v("valueExpr"), s("xss")},
		pos("JsxAttribute", w(), s("dangerouslySetInnerHTML"), v("valueExpr")),
	))

	// ─── SQL sinks: *.query() (heuristic fallback) ──────────────────
	rules = append(rules, rule("TaintSink",
		[]datalog.Term{v("argExpr"), s("sql")},
		pos("MethodCall", v("call"), w(), s("query")),
		pos("CallArg", v("call"), intc(0), v("argExpr")),
	))

	return rules
}

// ─── Express (existing) ─────────────────────────────────────────────────────

func expressRules() []datalog.Rule {
	var rules []datalog.Rule

	for _, method := range []string{"get", "post", "put", "delete", "use", "patch"} {
		rules = append(rules, expressHandlerRule(method))
	}

	for _, field := range []string{"query", "params", "body"} {
		rules = append(rules, rule("TaintSource",
			[]datalog.Term{v("expr"), s("http_input")},
			pos("FieldRead", v("expr"), v("reqSym"), s(field)),
			pos("Parameter", v("fn"), intc(0), w(), w(), v("reqSym"), w()),
			pos("ExpressHandler", v("fn")),
		))
	}

	rules = append(rules, rule("TaintSink",
		[]datalog.Term{v("argExpr"), s("xss")},
		pos("MethodCall", v("call"), v("recv"), s("send")),
		pos("ExprMayRef", v("recv"), v("resSym")),
		pos("Parameter", v("fn"), intc(1), w(), w(), v("resSym"), w()),
		pos("ExpressHandler", v("fn")),
		pos("CallArg", v("call"), intc(0), v("argExpr")),
	))

	return rules
}

func expressHandlerRule(methodName string) datalog.Rule {
	return rule("ExpressHandler",
		[]datalog.Term{v("fn")},
		pos("MethodCall", v("call"), w(), s(methodName)),
		pos("CallArg", v("call"), w(), v("cbExpr")),
		pos("ExprMayRef", v("cbExpr"), v("cbSym")),
		pos("FunctionSymbol", v("cbSym"), v("fn")),
	)
}

// ─── B1: Node.js HTTP module ────────────────────────────────────────────────

func httpHandlerRules() []datalog.Rule {
	var rules []datalog.Rule

	for _, mod := range []string{"http", "https"} {
		rules = append(rules, rule("HttpHandler",
			[]datalog.Term{v("fn")},
			pos("CallCalleeSym", v("call"), v("calleeSym")),
			pos("ImportBinding", v("calleeSym"), s(mod), s("createServer")),
			pos("CallArg", v("call"), w(), v("cbExpr")),
			pos("ExprMayRef", v("cbExpr"), v("cbSym")),
			pos("FunctionSymbol", v("cbSym"), v("fn")),
		))
	}

	for _, field := range []string{"url", "headers", "method"} {
		rules = append(rules, rule("TaintSource",
			[]datalog.Term{v("expr"), s("http_input")},
			pos("FieldRead", v("expr"), v("reqSym"), s(field)),
			pos("Parameter", v("fn"), intc(0), w(), w(), v("reqSym"), w()),
			pos("HttpHandler", v("fn")),
		))
	}

	for _, method := range []string{"write", "end"} {
		rules = append(rules, rule("TaintSink",
			[]datalog.Term{v("argExpr"), s("xss")},
			pos("MethodCall", v("call"), v("recv"), s(method)),
			pos("ExprMayRef", v("recv"), v("resSym")),
			pos("Parameter", v("fn"), intc(1), w(), w(), v("resSym"), w()),
			pos("HttpHandler", v("fn")),
			pos("CallArg", v("call"), intc(0), v("argExpr")),
		))
	}

	return rules
}

// ─── B2: Koa.js ─────────────────────────────────────────────────────────────

func koaRules() []datalog.Rule {
	var rules []datalog.Rule

	rules = append(rules, rule("KoaHandler",
		[]datalog.Term{v("fn")},
		pos("MethodCall", v("call"), w(), s("use")),
		pos("CallArg", v("call"), w(), v("cbExpr")),
		pos("ExprMayRef", v("cbExpr"), v("cbSym")),
		pos("FunctionSymbol", v("cbSym"), v("fn")),
	))

	rules = append(rules, rule("TaintSource",
		[]datalog.Term{v("expr"), s("http_input")},
		pos("FieldRead", v("expr"), v("ctxSym"), s("query")),
		pos("Parameter", v("fn"), intc(0), w(), w(), v("ctxSym"), w()),
		pos("KoaHandler", v("fn")),
	))

	// ctx.request.body / ctx.request.query — chain through ExprMayRef to
	// bridge from FieldRead's expr (AST node ID) to the next FieldRead's
	// baseSym (symbol ID).
	for _, field := range []string{"body", "query"} {
		rules = append(rules, rule("TaintSource",
			[]datalog.Term{v("expr"), s("http_input")},
			pos("FieldRead", v("reqExpr"), v("ctxSym"), s("request")),
			pos("ExprMayRef", v("reqExpr"), v("reqSym")),
			pos("FieldRead", v("expr"), v("reqSym"), s(field)),
			pos("Parameter", v("fn"), intc(0), w(), w(), v("ctxSym"), w()),
			pos("KoaHandler", v("fn")),
		))
	}

	rules = append(rules, rule("TaintSink",
		[]datalog.Term{v("rhsExpr"), s("xss")},
		pos("FieldWrite", w(), v("ctxSym"), s("body"), v("rhsExpr")),
		pos("Parameter", v("fn"), intc(0), w(), w(), v("ctxSym"), w()),
		pos("KoaHandler", v("fn")),
	))

	return rules
}

// ─── B3: Fastify ────────────────────────────────────────────────────────────

func fastifyRules() []datalog.Rule {
	var rules []datalog.Rule

	for _, method := range []string{"get", "post", "put", "delete", "patch", "head", "options"} {
		rules = append(rules, rule("FastifyHandler",
			[]datalog.Term{v("fn")},
			pos("MethodCall", v("call"), w(), s(method)),
			pos("CallArg", v("call"), w(), v("cbExpr")),
			pos("ExprMayRef", v("cbExpr"), v("cbSym")),
			pos("FunctionSymbol", v("cbSym"), v("fn")),
		))
	}

	for _, field := range []string{"body", "query", "params"} {
		rules = append(rules, rule("TaintSource",
			[]datalog.Term{v("expr"), s("http_input")},
			pos("FieldRead", v("expr"), v("reqSym"), s(field)),
			pos("Parameter", v("fn"), intc(0), w(), w(), v("reqSym"), w()),
			pos("FastifyHandler", v("fn")),
		))
	}

	rules = append(rules, rule("TaintSink",
		[]datalog.Term{v("argExpr"), s("xss")},
		pos("MethodCall", v("call"), v("recv"), s("send")),
		pos("ExprMayRef", v("recv"), v("replySym")),
		pos("Parameter", v("fn"), intc(1), w(), w(), v("replySym"), w()),
		pos("FastifyHandler", v("fn")),
		pos("CallArg", v("call"), intc(0), v("argExpr")),
	))

	return rules
}

// ─── B4: AWS Lambda ─────────────────────────────────────────────────────────

func lambdaRules() []datalog.Rule {
	var rules []datalog.Rule

	rules = append(rules, rule("LambdaHandler",
		[]datalog.Term{v("fn")},
		pos("ExportBinding", s("handler"), v("localSym"), w()),
		pos("FunctionSymbol", v("localSym"), v("fn")),
	))

	rules = append(rules, rule("TaintSource",
		[]datalog.Term{v("expr"), s("http_input")},
		pos("FieldRead", v("expr"), v("eventSym"), w()),
		pos("Parameter", v("fn"), intc(0), w(), w(), v("eventSym"), w()),
		pos("LambdaHandler", v("fn")),
	))

	return rules
}

// ─── B5: Next.js API routes ─────────────────────────────────────────────────

func nextjsRules() []datalog.Rule {
	var rules []datalog.Rule

	rules = append(rules, rule("NextjsHandler",
		[]datalog.Term{v("fn")},
		pos("ExportBinding", s("default"), v("localSym"), w()),
		pos("FunctionSymbol", v("localSym"), v("fn")),
	))

	for _, field := range []string{"query", "body"} {
		rules = append(rules, rule("TaintSource",
			[]datalog.Term{v("expr"), s("http_input")},
			pos("FieldRead", v("expr"), v("reqSym"), s(field)),
			pos("Parameter", v("fn"), intc(0), w(), w(), v("reqSym"), w()),
			pos("NextjsHandler", v("fn")),
		))
	}

	for _, method := range []string{"send", "json"} {
		rules = append(rules, rule("TaintSink",
			[]datalog.Term{v("argExpr"), s("xss")},
			pos("MethodCall", v("call"), v("recv"), s(method)),
			pos("ExprMayRef", v("recv"), v("resSym")),
			pos("Parameter", v("fn"), intc(1), w(), w(), v("resSym"), w()),
			pos("NextjsHandler", v("fn")),
			pos("CallArg", v("call"), intc(0), v("argExpr")),
		))
	}

	return rules
}

// ─── B6: Database drivers ───────────────────────────────────────────────────

func databaseRules() []datalog.Rule {
	var rules []datalog.Rule

	for _, mod := range []string{"pg", "mysql", "mysql2", "better-sqlite3"} {
		for _, method := range []string{"query", "execute"} {
			rules = append(rules, rule("TaintSink",
				[]datalog.Term{v("argExpr"), s("sql")},
				pos("ImportBinding", v("dbSym"), s(mod), w()),
				pos("MethodCall", v("call"), v("recv"), s(method)),
				pos("ExprMayRef", v("recv"), v("dbSym")),
				pos("CallArg", v("call"), intc(0), v("argExpr")),
			))
		}
	}

	// Mongoose: .find()/.findOne()/.aggregate() are NoSQL query methods.
	// We use a heuristic: any .find/.findOne/.aggregate call is a potential
	// NoSQL injection sink. This is similar to the .query() heuristic for SQL.
	// We can't easily constrain to mongoose models without type resolution.
	for _, method := range []string{"find", "findOne", "aggregate"} {
		rules = append(rules, rule("TaintSink",
			[]datalog.Term{v("argExpr"), s("nosql")},
			pos("MethodCall", v("call"), w(), s(method)),
			pos("CallArg", v("call"), intc(0), v("argExpr")),
		))
	}

	rules = append(rules, rule("TaintSink",
		[]datalog.Term{v("argExpr"), s("sql")},
		pos("ImportBinding", v("dbSym"), s("sequelize"), w()),
		pos("MethodCall", v("call"), v("recv"), s("query")),
		pos("ExprMayRef", v("recv"), v("dbSym")),
		pos("CallArg", v("call"), intc(0), v("argExpr")),
	))

	return rules
}

// ─── B7: Sanitizer libraries ────────────────────────────────────────────────

func sanitizerRules() []datalog.Rule {
	var rules []datalog.Rule

	xssSanitizers := []struct {
		module string
		name   string
	}{
		{"dompurify", "sanitize"},
		{"xss", "default"},
		{"escape-html", "default"},
		{"he", "encode"},
		{"he", "escape"},
		{"sanitize-html", "default"},
	}

	for _, san := range xssSanitizers {
		rules = append(rules, rule("Sanitizer",
			[]datalog.Term{v("fn"), s("xss")},
			pos("ImportBinding", v("localSym"), s(san.module), s(san.name)),
			pos("FunctionSymbol", v("localSym"), v("fn")),
		))
	}

	sqlSanitizers := []struct {
		module string
		name   string
	}{
		{"sqlstring", "escape"},
		{"sqlstring", "format"},
	}

	for _, san := range sqlSanitizers {
		rules = append(rules, rule("Sanitizer",
			[]datalog.Term{v("fn"), s("sql")},
			pos("ImportBinding", v("localSym"), s(san.module), s(san.name)),
			pos("FunctionSymbol", v("localSym"), v("fn")),
		))
	}

	cmdSanitizers := []struct {
		module string
		name   string
	}{
		{"shell-escape", "default"},
		{"shell-quote", "quote"},
	}

	for _, san := range cmdSanitizers {
		rules = append(rules, rule("Sanitizer",
			[]datalog.Term{v("fn"), s("command_injection")},
			pos("ImportBinding", v("localSym"), s(san.module), s(san.name)),
			pos("FunctionSymbol", v("localSym"), v("fn")),
		))
	}

	return rules
}
