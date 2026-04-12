/**
 * Find function calls whose callee resolves to a known symbol.
 *
 * Clean-room query written from scratch against public CodeQL API
 * documentation (https://codeql.github.com/docs/). Not derived from
 * CodeQL source code.
 *
 * Simple AST-level query: locates CallExpr nodes whose callee is a
 * VarAccess that resolves to a declared symbol, and reports the
 * symbol name.
 */
import javascript

from CallExpr call, VarAccess callee, Symbol s
where
    call.getCallee() = callee and
    callee.getSym() = s
select call, s.getName()
