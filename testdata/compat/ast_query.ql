/**
 * Find named functions in the source code, covering multiple AST
 * shapes: regular function declarations, arrow functions, and
 * class methods.
 *
 * Clean-room query written from scratch against public CodeQL API
 * documentation (https://codeql.github.com/docs/). Not derived from
 * CodeQL source code.
 *
 * Queries the Function class from compat_javascript.qll to find all
 * functions with a non-empty name.
 */
import javascript

from Function f
where
    not f.getName() = ""
select f, f.getName()
