/**
 * Find all calls to eval().
 *
 * Clean-room query written from scratch against public CodeQL API
 * documentation (https://codeql.github.com/docs/). Not derived from
 * CodeQL source code.
 *
 * Simple AST-level query: locates CallExpr nodes whose callee is a
 * VarAccess referencing a symbol named "eval".
 */
import javascript

from CallExpr call, VarAccess callee
where
    call.getCallee() = callee and
    exists(Symbol s | callee.getSym() = s and s.getName() = "eval")
select call, "Call to eval()"
