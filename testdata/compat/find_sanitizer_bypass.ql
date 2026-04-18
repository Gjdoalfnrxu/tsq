/**
 * Find taint alerts from HTTP input, testing sanitizer bypass behavior.
 * Correct sanitizers should block taint; wrong-kind sanitizers should not.
 * Clean-room query against public CodeQL API docs.
 *
 * KNOWN FALSE NEGATIVES (golden has 2 rows; ideal would have 4):
 *   - Route 3 `xssWrongSanitizer` (uses sqlEscape on XSS sink) — should alert (xss), missing.
 *   - Route 6 `sqlWrongSanitizer` (uses escapeHtml on SQL sink) — should alert (sql), missing.
 * Root cause: FlowStar does not propagate across CallResult, so taint never
 * reaches the post-sanitizer symbol — see issues #128 (FlowStar/CallResult)
 * and #127 (sanitizer kind mismatch). Pre-PR #126 the cross-product caught
 * these by accident; post-PR they are genuine false negatives, surfaced and
 * tracked rather than fixed by issue #113.
 */
import javascript

from TaintAlert alert
where alert.getSrcKind() = "http_input"
select alert.getSinkExpr(), alert.getSinkKind()
