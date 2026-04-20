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
//   41    0         namedHandler        (req.query → req)
//   42    1         namedHandler        (res.send → res)
//   48    0         inlineHandler       (req.query → req)
//   49    1         inlineHandler       (res.send → res)
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
