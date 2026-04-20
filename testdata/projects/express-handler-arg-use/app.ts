// Phase D PR2 — ExpressHandlerArgUse fixture.
//
// Exercises the additive `ExpressHandlerArgUse(useExpr, fn, paramIdx)`
// predicate in bridge/tsq_express.qll. The predicate asks: for a given
// use-site expression `useExpr`, does value-flow resolve it back to
// parameter `paramIdx` of Express route handler `fn`?
//
// Two shapes covered:
//
//   1. Named-variable handler (namedHandler / app.get('/named', …)).
//      The callback passed to `app.get` is the identifier `namedHandler`,
//      which `mayResolveTo` walks through the VarDecl init back to the
//      function expression. Inside the handler body, `req.query` and
//      `res.send` both sit on parameter uses that the predicate must
//      link back to (namedHandler fn, idx=0) and (namedHandler fn, idx=1)
//      respectively.
//
//   2. Inline arrow handler (app.get('/inline', (req, res) => …)).
//      Baseline shape — the callback argument is the function expression
//      itself (no var indirection). Parameter uses inside should still
//      surface via the predicate.
//
// Expected ExpressHandlerArgUse rows (by param use line):
//
//   line  paramIdx  handler
//   ----  --------  -------------
//   50    0         namedHandler        (req.query → req)
//   51    1         namedHandler        (res.send → res)
//   57    0         inlineHandler       (req.query → req)
//   58    1         inlineHandler       (res.send → res)
//
// Forbidden rows (must NOT appear — adversarial guards):
//
//   line  reason
//   ----  -------------------------------------------------------
//   69    `decoyHandler` fn never reaches app.<method> — CallArg filter
//   70    `decoyHandler` fn never reaches app.<method> — CallArg filter
//   81    `otherHandler` reached via app.on (method filter excludes "on")
//   82    `otherHandler` reached via app.on (method filter excludes "on")
//
// Pinned in express_handler_arg_use_test.go.

const express = require('express');
const app = express();

// Named-variable handler — handler is registered by identifier, so the
// `app.get` CallArg's argNode is the `namedHandler` Ident expression,
// not the function expression. `mayResolveTo` bridges the VarDecl init.
const namedHandler = function(req, res) {
    const id = req.query;
    res.send('ok ' + id);
};
app.get('/named', namedHandler);

// Inline arrow handler — baseline case, no var indirection.
app.get('/inline', (req, res) => {
    const id = req.query;
    res.send('inline ' + id);
});

// Decoy: same (req, res) signature, bound to a var (so the decoy fn
// literal IS an ExprValueSource and MayResolveTo-reachable from the
// `decoyHandler` identifier), but the `decoyHandler` identifier is
// never passed to any `app.<method>` call. If the predicate ever
// drops its `CallArg(call, _, handlerArgExpr)` leg, the MayResolveTo
// edge `decoyHandler → fn` would surface these param uses (lines
// 69-70) as rows; they must not appear.
const decoyHandler = function(req: any, res: any) {
    const x = req.query;
    res.send(x);
};
// Use the decoy in a non-handler context so the extractor keeps the
// ref live (prevents dead-code pruning of the var binding).
const _decoyRef: any = decoyHandler;

// Negative method-filter fixture: `app.on(...)` is NOT in the allow-list
// (get|post|put|delete|patch|use). A handler registered via `app.on` must
// not produce ExpressHandlerArgUse rows for its param uses (lines 81-82),
// proving the method filter actually bites.
const otherHandler = function(req: any, res: any) {
    const y = req.query;
    res.send(y);
};
app.on('evt', otherHandler);
