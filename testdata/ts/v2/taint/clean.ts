// Test fixture: clean code — no taint sources, no alerts expected.
import { query } from "./db";

function getUsers() {
    const sql = "SELECT * FROM users"; // hardcoded string, not tainted
    query(sql); // TaintSink: sql — but no source, so no alert
    return sql;
}
