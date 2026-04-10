// Test fixture: sanitized flow — tainted input passes through sanitizer before sink.
import { Request, Response } from "express";
import { query } from "./db";

function escapeSQL(input: string): string {
    return input.replace(/'/g, "''"); // Sanitizer for sql kind
}

function handleRequest(req: Request, res: Response) {
    const userInput = req.query.name; // TaintSource: http_input
    const safe = escapeSQL(userInput); // Sanitizer blocks taint propagation
    const sql = "SELECT * FROM users WHERE name = '" + safe + "'";
    query(sql); // TaintSink: sql — should NOT produce alert
    res.send("done");
}
