# CodeQL-Compatibility Test Fixtures

These `.ql` query files are **written from scratch** based on public
CodeQL API documentation at <https://codeql.github.com/docs/>. They are
not derived from, copied from, or based on CodeQL's own query source
code.

The queries exercise tsq's CodeQL-compatible bridge layer (`import
javascript`, `DataFlow::Configuration`, etc.) and are designed to be run
against the small JS project in `projects/basic/`.

For the overall compatibility plan, see
[/docs/impl-plans/05-compat-query-fixtures.md](../../docs/impl-plans/05-compat-query-fixtures.md)
and the broader compat roadmap in the impl-plans directory.
